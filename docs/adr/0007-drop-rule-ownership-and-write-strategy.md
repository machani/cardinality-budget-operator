# 0007. Drop rule ownership and write strategy

- Status: Accepted
- Date: 2026-10-04

## Context

A relabel rule on a ServiceMonitor or PodMonitor has only `sourceLabels`, `separator`, `regex`, `targetLabel`,
`replacement`, `modulus` and `action`. There is no name, label or comment field. To restore, clean up and
detect drift, the operator must know which rules it added, without ever removing a rule the team wrote.

The HLD said the enforcer uses server-side apply. `metricRelabelings` is a list inside a list
(`spec.endpoints[]` / `spec.podMetricsEndpoints[]`). If the CRD schema marks it as an atomic list, server-side
apply would take ownership of the whole list and conflict with the team's own rules.

`enforcedDrops` in the HLD had one entry per metric and monitor pair. With 20 monitors and 5 drops that is 100
entries, far over the cap of 10.

## Options considered

Ownership:

1. Match rules by content (ambiguous with identical team rules).
2. Hide a marker in a field Prometheus ignores for drop rules, such as `replacement` (relies on undocumented
   behaviour).
3. Record ownership in an annotation on the monitor.

Write strategy:

1. Server-side apply.
2. Read-modify-write with optimistic locking (`resourceVersion`).

## Decision

**Ownership**

- Annotation on each modified monitor, the source of truth:
  `cardinality.telemetry-compass.com/dropped-metrics: '["metric_a","metric_b"]'` (JSON list).
- Label on each modified monitor for discovery:
  `cardinality.telemetry-compass.com/enforced: "true"`, so
  `kubectl get servicemonitors,podmonitors -A -l cardinality.telemetry-compass.com/enforced=true` lists them.
- Both are removed when the last owned rule on the monitor is removed.

**Rule form and placement**

- Canonical rule: `{action: drop, sourceLabels: [__name__], regex: "<escaped metric name>"}`. Prometheus
  anchors the regex; the name is regex-escaped so special characters cannot widen the match.
- Appended at the **end** of `metricRelabelings`, so it matches the final metric name after the team's own
  rules.
- Added to **every endpoint** of each selected monitor.
- If an identical rule already exists, it is not added and not claimed. On undo, one matching occurrence is
  removed, and only if the annotation lists the metric.

**Write strategy**

- Read-modify-write with `resourceVersion`. The rule and the annotation change in the same update. On a
  conflict, requeue.
- No server-side apply on monitors.

**Status**

- `status.enforcedDrops`: one entry per metric, max 10 items:
  `{metric, monitorCount, since, state}` with `state` one of `Pending`, `Effective`, `Ineffective`, `Reverted`.
- Per-monitor detail lives in the monitor annotations and in Events, which name every monitor changed in a
  step.

## Consequences

- The HLD Enforcer row and API example change.
- The team's own relabel rules are never removed or reordered.
- Fuzz and unit tests cover list merging, duplicates and escaping (including Prometheus 3 UTF-8 names,
  [ADR 0011](0011-version-support-policy.md)).

## Verify in Phase 1

- The list type of `metricRelabelings` in the Prometheus Operator CRD schema.
