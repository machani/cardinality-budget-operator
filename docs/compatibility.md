# Compatibility matrix

Versions this release is tested with in CI ([ADR 0011](adr/0011-version-support-policy.md)). Only tested
versions are listed. `make verify` (`hack/ci/check-compat.sh`) fails when a version in the CI workflows is
missing here, so the table cannot drift from CI.

| Component | Supported | Tested in CI | Workflow |
| --- | --- | --- | --- |
| Kubernetes | 1.35 to 1.37 | `1.35`, `1.36`, `1.37` (kind) | `test-e2e.yml` |
| Prometheus Operator | v0.86.2 to v0.94.1 | `v0.86.2` (oldest, on Kubernetes 1.37), `v0.94.1` (newest, on every Kubernetes) | `test-e2e.yml` |
| Prometheus | 3.13 (LTS) to 3.15 | `v3.13.4`, `v3.15.0` | `prometheus-compat.yml` |
| Helm | 3.8 or later (OCI charts) | The GitHub runner's Helm (install, upgrade, uninstall) | `test-chart.yml` |
| Platforms | linux/amd64, linux/arm64 | Both built and scanned | `image.yml`, `release.yml` |
| API versions | `v1beta1` (stored), `v1alpha1` (deprecated, removed in v2.0) | Both, same schema | `test.yml` |

## What each component needs

| Component | Required feature | Why |
| --- | --- | --- |
| Kubernetes | CEL validation with `self.metadata.name`, CRD validation ratcheting | One budget per namespace named `default` ([ADR 0004](adr/0004-one-budget-per-namespace.md)) |
| Prometheus Operator | `metricRelabelings` on ServiceMonitor and PodMonitor endpoints | The only enforcement path ([ADR 0007](adr/0007-drop-rule-ownership-and-write-strategy.md)) |
| Prometheus Operator | `enforcedNamespaceLabel` on the Prometheus resource | Required for Enforce mode, not checked by the operator ([ADR 0003](adr/0003-series-attribution.md)) |
| Prometheus | `/-/ready`, `/api/v1/status/runtimeinfo`, query warnings, UTF-8 metric names | Measurement validity and reload checks ([ADR 0005](adr/0005-measurement-validity-and-backend-access.md)) |

Not supported: Prometheus 2.x, other backends (VictoriaMetrics, Mimir), and enforcement through anything other
than ServiceMonitor and PodMonitor drop rules.

## Changing the matrix

Change the CI workflow and this page in the same pull request. Dropping a version is announced in the
CHANGELOG one minor release ahead ([deprecation policy](deprecation-policy.md)).
