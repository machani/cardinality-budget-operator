# Admin guide

For the team that runs the operator. Install: [install guide](install.md). Alerts: [runbooks](../runbooks/README.md).

## Operator flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `--prometheus-url` | (required) | Prometheus base URL, no credentials |
| `--prometheus-http-config` | none | Prometheus-format HTTP client config file; secrets only as `*_file` |
| `--namespace-label` | `namespace` | Label that attributes series to a namespace (match `enforcedNamespaceLabel`) |
| `--exclude-series` | cluster exporters | PromQL matchers removed from usage ([ADR 0013](../adr/0013-default-series-exclusion.md)) |
| `--measure-interval` | `5m` | Time between measurements |
| `--query-timeout` | `30s` | Timeout per Prometheus request |
| `--backend-warmup` | `15m` | Minimum Prometheus uptime for a valid measurement |
| `--max-measurement-age` | 2 x interval | Age after which a measurement is `Stale` |
| `--enforcement-settle-period` | `10m` | Wait between enforcement steps |
| `--max-drops-per-hour` | `10` | New drops per hour across all budgets |
| `--enforcement-paused` | `false` | Kill switch: no monitor writes at all |

An invalid `--exclude-series` or `--prometheus-http-config` stops the operator at startup with a clear error.

## Kill switch

Stops every write to monitors (no drop, no restore, no cleanup), while measuring and status continue. Budgets
show `Enforced=False/Paused` and the alert `CardinalityBudgetKillSwitchActive` fires.

```bash
kubectl -n cardinality-budget-operator-system patch deploy cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enforcement-paused"}]'
```

With Helm, add `--enforcement-paused` to `manager.args` and upgrade. Remove the flag to resume. Budgets being
deleted wait while the switch is on.

## High availability

Two replicas with leader election and a PodDisruptionBudget (`minAvailable: 1`); only the leader reconciles.
Every decision is derived from the budget status and monitor annotations, so a failover or upgrade continues
where it stopped, also in the middle of an enforcement step. Check the leader:

```bash
kubectl -n cardinality-budget-operator-system get lease c4f5a295.telemetry-compass.com -o jsonpath='{.spec.holderIdentity}'
```

## Metrics, alerts and dashboard

Metrics are served over HTTPS on port 8443 and need a token allowed by the `metrics-reader` ClusterRole.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `cardinalitybudget_active_series`, `_limit`, `_usage_ratio` | `budget_namespace` | Usage per budget |
| `cardinalitybudget_drops_active` | `budget_namespace` | Metrics dropped now |
| `cardinalitybudget_measurement_valid` | `budget_namespace` | 1 when the last measurement is trusted |
| `cardinalitybudget_paused` | `budget_namespace`, `reason` | 1 when the budget is paused until resumed |
| `cardinalitybudget_enforcement_paused` | none | 1 while the kill switch is on |
| `cardinalitybudget_drops_last_hour` | none | Drops counted against `--max-drops-per-hour` |

Alerts (each links its runbook): `CardinalityBudgetOperatorDown`, `ReconcileErrors`, `MeasurementInvalid`,
`Paused`, `ConfigReloadFailed`, `KillSwitchActive`, `RateLimited`. The Grafana dashboard ships as a ConfigMap
labelled `grafana_dashboard: "1"`.

The operator's own ServiceMonitor uses `insecureSkipVerify` by default (scaffold). In production give the
metrics endpoint a cert-manager certificate: Helm `certManager.enabled=true`, Kustomize
`config/prometheus/monitor_tls_patch.yaml`.

## Security

- RBAC: budgets (and status, finalizers), ServiceMonitors and PodMonitors (get, list, watch, patch), Events.
  No Secrets, no Prometheus objects, no CRD writes.
- Pods run as non-root with a read-only root filesystem, no capabilities and seccomp `RuntimeDefault`.
- Credentials only from mounted files; headers, tokens and URL userinfo are never logged.
- Details: [threat model](../security/threat-model.md); reporting: [SECURITY.md](../../SECURITY.md).

## Sizing

Each budget costs two PromQL queries per `--measure-interval` plus `/-/ready` and runtimeinfo, all scoped to its
namespace. The defaults (`limits: 500m CPU, 128Mi`) were measured on the lab cluster only; scale tests on a
large cluster are still open (F-0308). Watch `controller_runtime_reconcile_time_seconds` and the operator's
memory when you add many budgets.
