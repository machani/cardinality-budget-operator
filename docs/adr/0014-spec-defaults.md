# 0014. Spec defaults

- Status: Accepted
- Date: 2026-10-12

## Context

The HLD fixes the ranges of the spec fields and two defaults (`mode: Observe`, ADR 0002; `maxTotalDrops: 5`,
ADR 0006). It shows `warnThresholdPercent: 80`, `gracePeriod: 30m` and `maxMetricsToDrop: 3` only as example
values. Writing the Go types (F-0102) needs a decision for each optional field: a default, or required.

## Options considered

For each field: required, or a default that is safe for a platform team that leaves it out.

## Decision

| Field | Default | Why |
| --- | --- | --- |
| `warnThresholdPercent` | `90` | Fewer warnings while teams learn the budgets; still lead time before 100% |
| `mode` | `Observe` | ADR 0002 (unchanged) |
| `enforcement` | `{}` | So the defaults below apply when the block is left out |
| `enforcement.gracePeriod` | `30m` | Covers a rollout or a short spike before anything is dropped |
| `enforcement.maxMetricsToDrop` | `1` | One metric per step, then a settle period to see its effect (ADR 0006) |
| `enforcement.maxTotalDrops` | `5` | ADR 0006 (unchanged) |
| `enforcement.protectedMetrics` | empty | Nothing protected beyond the `:` filter |
| `enforcement.monitorSelector` | unset | All monitors in the namespace |

`maxActiveSeries` stays required: a budget without a limit has no meaning.

## Consequences

- Defaults are applied by the API server from `+kubebuilder:default` markers; an envtest test checks them.
- `gracePeriod` uses `format: duration`. A non-negative check is added with the CEL rules (F-0105).
- The HLD example keeps illustrative values that differ from the defaults; the HLD validation line lists the
  defaults.
