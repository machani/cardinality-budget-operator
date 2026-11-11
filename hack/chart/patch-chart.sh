#!/usr/bin/env bash
# Copyright 2026 Machani Vidyasagar.
# SPDX-License-Identifier: Apache-2.0
#
# Post-processes dist/chart after `kubebuilder edit --plugins=helm/v2-alpha` (run it as `make chart-patch`).
# The helm plugin copies the PrometheusRule into templates/extras/ with neither a labels value nor an `if`, so:
#   - the rule gets the ServiceMonitor's .Values.prometheus.labels, because a Prometheus's ruleSelector
#     usually needs the same label as its serviceMonitorSelector (for example release=<name>);
#   - the rule is rendered only when .Values.prometheus.enabled, like the ServiceMonitor.
# Safe to run more than once. hack/ci/check-chart.sh (make verify) fails when the patch is missing.
set -euo pipefail

f=${1:-dist/chart/templates/extras/alerts.yaml}
[ -f "$f" ] || { echo "patch-chart: $f not found" >&2; exit 1; }

if grep -q '\.Values\.prometheus\.labels' "$f"; then
  echo "patch-chart: $f already patched"
  exit 0
fi

tmp=$(mktemp)
awk '
  { line = $0; sub(/\r$/, "", line) }
  NR == 1 { print "{{- if .Values.prometheus.enabled }}" }
  { print }
  !done && line == "    app.kubernetes.io/instance: {{ .Release.Name }}" {
    print "    {{- with .Values.prometheus.labels }}"
    print "    {{- with omit . \"app.kubernetes.io/managed-by\" \"app.kubernetes.io/name\" \"helm.sh/chart\" \"app.kubernetes.io/instance\" }}"
    print "    {{- toYaml . | nindent 4 }}"
    print "    {{- end }}"
    print "    {{- end }}"
    done = 1
  }
  END {
    print "{{- end }}"
    if (!done) exit 2
  }
' "$f" >"$tmp" || { rc=$?; rm -f "$tmp"; echo "patch-chart: no 'app.kubernetes.io/instance' label in $f; did the plugin output change?" >&2; exit "$rc"; }

mv "$tmp" "$f"
echo "patch-chart: $f now uses .Values.prometheus.labels and .Values.prometheus.enabled"
