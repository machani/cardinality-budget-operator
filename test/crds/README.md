# Prometheus Operator CRDs for envtest

envtest starts a bare API server, so the ServiceMonitor and PodMonitor CRDs the operator works with are kept
here and loaded by `internal/controller/suite_test.go`.

- Source: the Prometheus Operator release that matches the Go module in `go.mod`
  (`github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring`).
- Current version: **v0.94.1** (same as the lab, kube-prometheus-stack 92.3.0).
- Do not edit these files. To update, bump the Go module and download the same version:

```bash
V=v0.94.1; for r in servicemonitors podmonitors; do curl -fsSLo test/crds/monitoring.coreos.com_$r.yaml "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/$V/example/prometheus-operator-crd/monitoring.coreos.com_$r.yaml"; done
```
