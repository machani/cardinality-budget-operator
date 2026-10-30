# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Every pull request with a user-visible change
adds a line under `[Unreleased]`; a release renames that section to the version and date
([docs/releasing.md](docs/releasing.md)).

## [Unreleased]

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

### Deprecated

- API version `v1alpha1`. It is still served with the same schema and will be removed in v2.0. Run
  `hack/migrate-storage.sh` after upgrading (runbook "Upgrade to v1.0") and move manifests to `v1beta1`.
