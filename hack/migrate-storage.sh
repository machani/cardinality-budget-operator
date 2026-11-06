#!/usr/bin/env bash
# Copyright 2026 Machani Vidyasagar.
# SPDX-License-Identifier: Apache-2.0
#
# Moves every CardinalityBudget to the v1beta1 storage version (ADR 0017). Run it once after upgrading to
# v1.0; it must have run before upgrading to v2.0, which removes v1alpha1.
#
#   hack/migrate-storage.sh [kubectl flags]     for example: hack/migrate-storage.sh --context kind-cb-dev
#
# 1. Re-saves every budget unchanged. The API server writes it again in the storage version (v1beta1),
#    because the stored bytes differ from the new encoding. Spec and status stay as they are.
# 2. Sets the CRD's status.storedVersions to ["v1beta1"], so Kubernetes knows no v1alpha1 objects are left.
#
# Safe to run again. Needs permission to list and update budgets in all namespaces and to patch the CRD
# status (cluster admin); the operator itself needs neither.
set -euo pipefail

crd=cardinalitybudgets.cardinality.telemetry-compass.com
resource=cardinalitybudgets.v1beta1.cardinality.telemetry-compass.com

kubectl_flags=("$@")
kc() { kubectl "${kubectl_flags[@]}" "$@"; }

storage=$(kc get crd "$crd" -o jsonpath='{.spec.versions[?(@.storage==true)].name}')
if [ "$storage" != "v1beta1" ]; then
  echo "The CRD stores $storage, not v1beta1: install the v1.0 CRD first." >&2
  exit 1
fi

echo "storedVersions before: $(kc get crd "$crd" -o jsonpath='{.status.storedVersions}')"

failed=0
for ns in $(kc get "$resource" -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\n"}{end}'); do
  ok=false
  # The operator writes status every measurement, so a replace can hit a conflict: read again and retry.
  for attempt in 1 2 3 4 5; do
    if kc get "$resource" default -n "$ns" -o json | kc replace -f - >/dev/null; then
      ok=true
      break
    fi
    echo "  $ns: attempt $attempt failed, retrying" >&2
    sleep 2
  done
  if $ok; then
    echo "migrated $ns/default"
  else
    echo "FAILED $ns/default" >&2
    failed=1
  fi
done

if [ "$failed" -ne 0 ]; then
  echo "Some budgets were not migrated; storedVersions left unchanged. Fix the errors and run again." >&2
  exit 1
fi

kc patch crd "$crd" --subresource=status --type=merge -p '{"status":{"storedVersions":["v1beta1"]}}' >/dev/null
echo "storedVersions after: $(kc get crd "$crd" -o jsonpath='{.status.storedVersions}')"
