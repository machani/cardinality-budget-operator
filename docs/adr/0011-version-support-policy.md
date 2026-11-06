# 0011. Version support policy

- Status: Accepted
- Date: 2026-10-04

## Context

The operator depends on specific features in three systems:

- **Kubernetes**: CRD CEL validation (GA in 1.29), root-level `self.metadata.name` in CEL
  ([ADR 0004](0004-one-budget-per-namespace.md)), and CRD validation ratcheting for safe upgrades.
- **Prometheus Operator**: `metricRelabelings` on ServiceMonitor and PodMonitor endpoints,
  `enforcedNamespaceLabel` and `excludedFromEnforcement` ([ADR 0003](0003-series-attribution.md)), and the list
  type of `metricRelabelings` ([ADR 0007](0007-drop-rule-ownership-and-write-strategy.md)).
- **Prometheus**: `/-/ready`, `/api/v1/status/runtimeinfo` (`startTime`, `reloadConfigSuccess`,
  `lastConfigTime`) and query warnings ([ADR 0005](0005-measurement-validity-and-backend-access.md),
  [ADR 0006](0006-enforcement-safety.md)).

Prometheus 3.x allows UTF-8 metric names (for example with dots). That affects regex escaping in drop rules and
name quoting in PromQL (`{"my.metric", namespace="x"}`).

## Decision

| System | Policy | CI |
| --- | --- | --- |
| Kubernetes | The three minors under upstream support when each release ships | kind e2e on all three |
| Prometheus Operator | Releases from about the last 12 months; the floor is the oldest with every feature above | Oldest and newest supported |
| Prometheus | 3.x only, including the current LTS | LTS and latest |
| Go, Kubebuilder, controller-runtime | Latest stable at the start of Phase 1, pinned, updated by Renovate or Dependabot | Build |
| Images | `linux/amd64`, `linux/arm64` | Build |

- Exact minimum versions are set in Phase 1 by checking real releases and CRD schemas, then recorded in the
  compatibility matrix.
- CI is the source of truth: the published matrix lists only versions CI tests.
- During v0.x the lab needs only the latest versions; the full matrix is required from v0.3.
- Dropping a version is announced in the CHANGELOG one minor release ahead.

## Consequences

- Dedicated tests for UTF-8 metric names in rule regexes and PromQL.
- Prometheus 2.x users are not supported.
- The compatibility matrix is part of the v1.0 documentation.
