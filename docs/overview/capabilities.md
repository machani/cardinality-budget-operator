# CardinalityBudget capabilities

Status as of 2026-10-19. Build tracker with feature IDs: [docs/features](../features/README.md).

## Modes

| Mode | What happens |
| --- | --- |
| **Observe** (default) | Measure, show status, one Event per state change |
| **Warn** | Observe + `plannedDrops`: what Enforce would drop next |
| **Enforce** | Warn + add drop rules after the grace period, step by step; restore when usage goes down |

## Measurement (v0.1)

- Counts the namespace's active series in Prometheus 3.x:
  `count({namespace="<ns>", <exclude-series>})`, plus `topk(10, count by (__name__) (...))` for the top metrics.
- Cluster exporters (kubelet, kube-state-metrics, node-exporter, ...) and recording-rule outputs are excluded,
  so a namespace pays only for its own series.
- A measurement is trusted only if Prometheus is ready, has been up longer than `--backend-warmup`, returned
  no warnings (partial data), and the result is fresh. Otherwise the phase is `Unknown` and nothing changes.
- UTF-8 metric names (Prometheus 3) are handled.

## Status and reporting (v0.1)

- Phases: `Healthy`, `Warning` (at the warn threshold), `Exceeded` (100%), `Enforcing`, `Unknown`.
- Printer columns: Phase, Series, Limit, Usage %, Mode, Age.
- Conditions: `Ready`, `BackendReachable`, `BudgetExceeded`, `Enforced`, each with a clear reason.
- `topMetrics` (10), `plannedDrops`, `enforcedDrops`, `exceededSince`, `lastMeasured`.
- Events on every state change, in every mode.

## Validation (v0.1)

- CEL rules in the CRD, checked by the API server: name must be `default`, ranges for every number,
  `maxMetricsToDrop <= maxTotalDrops`, non-negative grace period, valid and limited `protectedMetrics` regexes.
- Defaults filled in by the API server (mode Observe, threshold 90%, grace 30m, 1 per step, 5 in total).
- Startup checks on the operator's flags (bad `--exclude-series` or HTTP config → clear error, exit).

## Enforcement (v0.2)

- Drop rule `{action: drop, sourceLabels: [__name__], regex: <escaped name>}` appended to every endpoint of
  every selected ServiceMonitor and PodMonitor in the namespace.
- Ownership: annotation `cardinality.telemetry-compass.com/dropped-metrics` (list) and label
  `cardinality.telemetry-compass.com/enforced: "true"` on the monitor. Team rules are never touched.
- Safe writes: read-modify-write with optimistic locking; on conflict, try again.

## Safety rules (v0.2)

| Rule | Effect |
| --- | --- |
| Grace period | Over budget for `gracePeriod` before the first drop |
| Settle period | `--enforcement-settle-period` (10m) between steps |
| Caps | `maxMetricsToDrop` per step, `maxTotalDrops` in total |
| Protected metrics | Never dropped; also recording rules (names with `:`) |
| Reload check | Prometheus must load the new config, else pause (`ConfigReloadFailed`) |
| Effectiveness check | ≥ 90% of the metric's series must disappear, else pause (`Ineffective`) |
| Drift detection | Someone removed or changed our rule → pause (`Reverted`), never re-apply |
| Resume | Platform team sets the resume annotation to a new value |
| Invalid measurement | No drop added **or removed** |
| Kill switch | `--enforcement-paused`: no monitor write at all |

## Restore (v0.2)

- Below the warn threshold, or when leaving Enforce: one step per settle period, newest drop first.
- Newly protected metrics are restored at once.
- Monitors that are no longer selected lose their rules; new monitors get the existing drops.
- Deleting the budget removes every rule (finalizer `cardinality.telemetry-compass.com/cleanup`).

## Hardening (v0.3)

- Global hourly limit `--max-drops-per-hour` (10) across all budgets.
- High availability: 2 replicas, leader election, PodDisruptionBudget, anti-affinity.
- Security: restricted security context, read-only root filesystem, NetworkPolicy, authenticated metrics,
  minimal RBAC (no Secrets, no Prometheus objects), threat model.
- Operator metrics: `cardinalitybudget_active_series`, `_limit`, `_usage_ratio`, `_drops_active`,
  `_measurement_valid`, `_paused`, `_enforcement_paused`, `_drops_last_hour`.
- 7 alerts with runbooks, a Grafana dashboard.
- Helm chart (CRDs kept on uninstall) and Kustomize install.
- CI: lint, unit and envtest, generated-files and license checks, govulncheck, e2e on Kubernetes 1.35–1.37 and
  Prometheus Operator v0.86.2–v0.94.1, real Prometheus 3.13 and 3.15, multi-arch image with Trivy scan,
  Helm install/upgrade/uninstall.

## Operator flags

| Flag | Default |
| --- | --- |
| `--prometheus-url` | required |
| `--prometheus-http-config` | none (credentials only as files) |
| `--namespace-label` | `namespace` |
| `--exclude-series` | cluster exporters and empty job |
| `--measure-interval` | 5m |
| `--query-timeout` | 30s |
| `--backend-warmup` | 15m |
| `--max-measurement-age` | 2 × interval |
| `--enforcement-settle-period` | 10m |
| `--max-drops-per-hour` | 10 |
| `--enforcement-paused` | false |

## Planned

- v0.3: scale tests on a larger cluster (blocked).
- v1.0: API `v1beta1` with conversion webhook, release pipeline (signed images, SBOM, CHANGELOG), published
  compatibility matrix, full documentation (install, user, admin, GitOps), production readiness review.
