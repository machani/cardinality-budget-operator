#!/usr/bin/env bash
# Copyright 2026 Machani Vidyasagar.
# SPDX-License-Identifier: Apache-2.0
#
# Fails when a version tested in CI is missing from docs/compatibility.md (F-1003: the published matrix
# matches CI). Reads the Kubernetes and Prometheus Operator versions from the e2e matrix and the Prometheus
# versions from the compatibility workflow.
set -euo pipefail

doc=docs/compatibility.md
e2e=.github/workflows/test-e2e.yml
compat=.github/workflows/prometheus-compat.yml

versions=$(
  {
    sed -nE 's/^[[:space:]]*- kubernetes: "?([0-9.]+)"?.*/\1/p' "$e2e"
    sed -nE 's/^[[:space:]]*prometheus-operator: (v[0-9.]+).*/\1/p' "$e2e"
    sed -nE 's/^[[:space:]]*prometheus: \[([^]]*)\].*/\1/p' "$compat" | tr ',' '\n' | tr -d ' '
  } | sort -u
)

if [ -z "$versions" ]; then
  echo "check-compat: found no versions in $e2e or $compat; did the workflow format change?" >&2
  exit 1
fi

missing=0
for v in $versions; do
  if ! grep -qF "\`$v\`" "$doc"; then
    echo "check-compat: CI tests $v but $doc does not list it" >&2
    missing=1
  fi
done

if [ "$missing" -ne 0 ]; then
  exit 1
fi
echo "check-compat: $doc lists every CI version ($(echo $versions | tr '\n' ' '))"
