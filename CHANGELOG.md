# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Every pull request with a user-visible change
adds a line under `[Unreleased]`; a release renames that section to the version and date
([docs/releasing.md](docs/releasing.md)).

## [Unreleased]

## [1.0.1] - 2026-11-11

Fixes found in the first production install. No API or enforcement change.

### Security

- Helm chart: the NetworkPolicy let every namespace reach the metrics port (8443); only namespaces labelled
  `metrics=enabled` were meant to. The health-probe port now has its own policy, and CI checks the rendered
  chart. Kustomize and `install.yaml` were not affected. **Upgrading closes the port:** label the namespace of
  the Prometheus that scrapes the operator `metrics=enabled` first. Check a 1.0.0 install with
  `kubectl -n <namespace> get networkpolicy -o yaml`.
- Built with Go 1.27.4 for GO-2026-6859 (HTTP/1 server crash in `net/http`, reported by govulncheck).

### Fixed

- Helm chart: `prometheus.labels` now applies to the PrometheusRule as well as the ServiceMonitor, so a
  Prometheus's `ruleSelector` can load the alerts. The PrometheusRule follows `prometheus.enabled`.
- Alert `CardinalityBudgetOperatorDown` fired permanently for Helm release names that did not contain
  `cardinality-budget-operator`. It now checks for the operator's own metric instead of a fixed job name.
- Grafana dashboard: per-budget panels showed two lines after a leader change.
- Docs: the install guide explains which Prometheus is measured and which scrapes the operator, the selector
  labels and the `metrics=enabled` namespace; the kill-switch runbook named a Helm value that does not exist;
  the `RateLimited` alert assumes the default `--max-drops-per-hour`.

### Changed

- Grafana dashboard: an "Operator health" row (replicas scraped, leader, reconcile rate, duration and queue
  depth), colour thresholds, panel descriptions and a cleaner paused-budgets table.
- Dependencies: Kubernetes libraries 0.37.2, Prometheus Operator API 0.94.2, `prometheus/common` 0.73.1,
  `prometheus/prometheus` 0.315.1, Ginkgo 2.34.0, cosign-installer 4.1.3.

## [1.0.0] - 2026-11-06

The first published release. v0.1 to v0.3 were lab milestones and were never published.

### Added

- `CardinalityBudget` API (`cardinality.telemetry-compass.com/v1beta1`): one budget per namespace, named
  `default`, with CEL validation and defaults.
- Measurement of each namespace's active series and top 10 metrics through the Prometheus 3.x HTTP API, trusted
  only when Prometheus is ready, warmed up, fresh and returned no warnings.
- Modes `Observe`, `Warn` (with `plannedDrops`) and `Enforce`: drop rules on the namespace's ServiceMonitors
  and PodMonitors after a grace period, one step per settle period, within per-budget caps and a cluster-wide
  hourly limit.
- Safety: protected metrics, recording rules never dropped, reload and effectiveness checks, drift detection
  with pause and resume, restore newest first, cleanup on deletion, and a kill switch
  (`--enforcement-paused`).
- Status, conditions and Events on the budget; operator metrics, alerts with runbooks, and a Grafana dashboard.
- High availability (two replicas, leader election, PodDisruptionBudget), restricted pod security, minimal
  RBAC, NetworkPolicy and authenticated metrics.
- Kustomize and Helm installs for linux/amd64 and linux/arm64; signed images with an SBOM and build
  provenance.

### Changed

- The operator logs JSON in production mode (stack traces only on errors, no panic on `DPanic`). Pass
  `--zap-devel` for the human-readable development format.

### Deprecated

- API version `v1alpha1`. It is still served with the same schema and will be removed in v2.0. Run
  `hack/migrate-storage.sh` after upgrading (runbook "Upgrade to v1.0") and move manifests to `v1beta1`.
