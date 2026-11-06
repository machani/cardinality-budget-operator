# Prometheus Operator CRDs for envtest and e2e

envtest starts a bare API server, and the e2e cluster has no Prometheus Operator, so the CRDs the operator
uses are kept here: ServiceMonitor and PodMonitor (watched and patched) and PrometheusRule (the operator's
own alerts, installed with it). envtest loads them in `internal/controller/suite_test.go`; e2e applies them.

- Source: the Prometheus Operator release that matches the Go module in `go.mod`
  (`github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring`).
- Current version: **v0.94.1** (same as the lab, kube-prometheus-stack 92.3.0). CI also tests the oldest
  supported release by replacing these files (see `.github/workflows/test-e2e.yml`).
- Do not edit these files. To update, bump the Go module and download the same version:

```bash
V=v0.94.1; for r in servicemonitors podmonitors prometheusrules; do curl -fsSLo test/crds/monitoring.coreos.com_$r.yaml "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/$V/example/prometheus-operator-crd/monitoring.coreos.com_$r.yaml"; done
```
