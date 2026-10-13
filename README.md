# CardinalityBudget operator

A Kubernetes operator that gives each namespace a Prometheus metric-series budget and reports usage on a
`CardinalityBudget` resource. It never sits in the data path and never drops samples itself.

> **Status: alpha (v0.3).** Observe, Warn and Enforce modes. Enforce adds drop rules to ServiceMonitors and
> PodMonitors and deletes data on purpose: run v0.x on lab clusters only. Production readiness is v1.0.

```console
$ kubectl get cb -A
NAMESPACE   NAME      PHASE     SERIES   LIMIT   USAGE %   MODE   AGE
demo        default   Warning   1012     1100    92        Warn   3m
```

## How it works

- One budget per namespace, always named `default`, written by the platform team (teams get read-only).
- Every `--measure-interval` (5 minutes), the operator counts the namespace's active series in Prometheus
  3.x, excluding cluster exporters and recording-rule outputs, and lists the top 10 metrics.
- A measurement is trusted only if Prometheus is ready, warmed up and returned no warnings. Otherwise the
  phase is `Unknown` and nothing is planned or changed.
- Phases: `Healthy`, `Warning` (at `warnThresholdPercent`, default 90%), `Exceeded` (100%), `Unknown`.
- In `Warn` mode, `status.plannedDrops` shows what Enforce mode would drop. Every mode records an Event
  on each state change (`kubectl describe cb default`).
- In `Enforce` mode, after `gracePeriod` over budget, the largest allowed metrics are dropped through
  `metricRelabelings`, one step per settle period, within `maxMetricsToDrop`, `maxTotalDrops` and the global
  `--max-drops-per-hour`. Protected metrics and recording rules are never dropped; team rules are never touched.
- Each drop is checked: Prometheus must reload, and at least 90% of the metric's series must disappear.
  Otherwise, or if someone removes a rule, the budget pauses until the platform team resumes it.
- Below the warn threshold, or when leaving Enforce, drops are restored newest first. Deleting a budget
  removes all its rules. `--enforcement-paused` stops every monitor write.

Operations: alerts and dashboard in `config/prometheus` and `config/grafana`, runbooks in
[docs/runbooks](docs/runbooks/README.md), threat model in [docs/security](docs/security/threat-model.md).

## Quick start on kind

Requirements: Go, Docker, kind, kubectl, Helm (versions in `hack/lab/`).

```bash
kind create cluster --config hack/lab/kind-cb-dev.yaml
```
```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts && helm repo update
```
```bash
helm --kube-context kind-cb-dev install kps prometheus-community/kube-prometheus-stack -n monitoring --create-namespace -f hack/lab/kube-prometheus-stack-values.yaml --version 92.3.0 --wait --timeout 10m
```
```bash
kind get kubeconfig --name cb-dev > /tmp/kind-cb-dev.yaml && KUBECONFIG=/tmp/kind-cb-dev.yaml make install
```
```bash
kubectl --context kind-cb-dev apply -f hack/lab/demo/avalanche.yaml -f hack/lab/demo/budget.yaml
```

Run the operator on your machine against the lab Prometheus (port-forward in another terminal):

```bash
kubectl --context kind-cb-dev -n monitoring port-forward svc/kps-prometheus 9090:9090
```
```bash
KUBECONFIG=/tmp/kind-cb-dev.yaml go run ./cmd/main.go --prometheus-url=http://localhost:9090 --backend-warmup=0s --measure-interval=1m --query-timeout=20s
```

Or deploy it in the cluster:

```bash
make docker-build IMG=cardinality-budget-operator:dev && kind load docker-image cardinality-budget-operator:dev --name cb-dev && KUBECONFIG=/tmp/kind-cb-dev.yaml make deploy IMG=cardinality-budget-operator:dev
```

## Operator flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `--prometheus-url` | (required) | Prometheus base URL, no credentials |
| `--prometheus-http-config` | none | Prometheus-format HTTP client config file; secrets only as `*_file` (see `config/manager/prometheus-http-config-patch.yaml`) |
| `--namespace-label` | `namespace` | Label that attributes series to a namespace |
| `--exclude-series` | kube-prometheus-stack exporters and recording rules | PromQL matchers removed from usage |
| `--measure-interval` | `5m` | Time between measurements |
| `--query-timeout` | `30s` | Timeout per backend request |
| `--backend-warmup` | `15m` | Minimum Prometheus uptime for a valid measurement |
| `--max-measurement-age` | 2 x interval | Age after which numbers are `Stale` |
| `--enforcement-settle-period` | `10m` | Wait between enforcement steps |
| `--max-drops-per-hour` | `10` | New drops per hour across all budgets |
| `--enforcement-paused` | `false` | Kill switch: no monitor writes at all |

Enforce mode requires Prometheus `enforcedNamespaceLabel`. The Prometheus namespace needs the label
`metrics=enabled` to scrape the operator (NetworkPolicy).

## Documentation

- Design: [docs/design/hld.md](docs/design/hld.md)
- Decisions: [docs/adr/](docs/adr/README.md)
- Feature tracker: [docs/features/](docs/features/README.md)

## License

Apache-2.0. See [LICENSE](LICENSE).
