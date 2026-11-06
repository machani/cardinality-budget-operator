# User guide

For the platform team, which writes budgets, and application teams, which read them
([ADR 0002](../adr/0002-budget-ownership-and-rbac.md)). Background: [overview](../overview/README.md).

## Create a budget

One budget per namespace, always named `default`. Start in Observe mode and look at the numbers for a few days
before choosing a limit.

```yaml
apiVersion: cardinality.telemetry-compass.com/v1beta1
kind: CardinalityBudget
metadata:
  name: default
  namespace: team-a
spec:
  maxActiveSeries: 50000
```

| Field | Default | Meaning |
| --- | --- | --- |
| `maxActiveSeries` | required, > 0 | The limit on the namespace's active series |
| `warnThresholdPercent` | 90 (1 to 99) | Usage at which the phase becomes `Warning` |
| `mode` | `Observe` | `Observe`, `Warn` or `Enforce` |
| `enforcement.gracePeriod` | 30m | Time at or above 100% before the first drop |
| `enforcement.maxMetricsToDrop` | 1 (1 to 10) | Metrics dropped per step |
| `enforcement.maxTotalDrops` | 5 (1 to 10) | Metrics dropped at the same time; at least `maxMetricsToDrop` |
| `enforcement.protectedMetrics` | none | Regular expressions (fully anchored) for metrics never dropped |
| `enforcement.monitorSelector` | all monitors | Which ServiceMonitors and PodMonitors may receive drop rules |

Invalid values are rejected when you apply the budget. `v1alpha1` manifests still work but print a deprecation
warning; change them to `v1beta1`.

## Read a budget

```bash
kubectl get cb -A
```

```bash
kubectl -n team-a describe cb default
```

| Status field | Meaning |
| --- | --- |
| `phase` | `Healthy`, `Warning` (at or above the warn threshold), `Exceeded` (at or above 100%), `Enforcing` (drop rules in place), `Unknown` (no trusted measurement) |
| `activeSeries`, `usagePercent`, `lastMeasured` | The last trusted measurement |
| `topMetrics` | The 10 metrics with the most series: where to look first |
| `plannedDrops` | Warn and Enforce: what the next enforcement step would drop |
| `enforcedDrops` | What is dropped now, since when, on how many monitors, and whether it worked (`Pending`, `Effective`, `Ineffective`, `Reverted`) |
| Conditions | `Ready`, `BackendReachable`, `BudgetExceeded`, `Enforced`, each with a reason |

Events (`kubectl -n team-a get events --field-selector involvedObject.name=default`) record each phase change,
drop, restore and pause.

## Choose a mode

| Mode | Use it when |
| --- | --- |
| `Observe` | Starting out; the team only needs visibility |
| `Warn` | The limit is agreed; the team sees `plannedDrops` before anything happens |
| `Enforce` | The team agreed that the listed metrics may be dropped to protect the shared Prometheus |

Before Enforce, add every metric the team's alerts and SLOs depend on to `protectedMetrics`. Recording rules
(names with `:`) are never dropped.

## What Enforce does

When usage stays at or above 100% for `gracePeriod`, the operator appends a drop rule for the biggest
unprotected metric to every selected monitor in the namespace, then waits one settle period (10m by default)
and checks that Prometheus reloaded and the metric's series fell by at least 90%. It repeats until usage is
under 100% or a limit is reached. When usage falls below the warn threshold, it restores the drops one per
settle period, newest first. Leaving Enforce mode or deleting the budget restores all of them.

Drop rules are marked on the monitor (annotation `cardinality.telemetry-compass.com/dropped-metrics`, label
`cardinality.telemetry-compass.com/enforced: "true"`). The operator never changes the team's own relabel rules.

## When the budget is paused

A budget pauses (condition `Enforced=False`) when a drop was ineffective, Prometheus did not reload, or someone
removed a drop rule. Nothing more is dropped or re-applied until the platform team resumes it:

```bash
kubectl -n team-a annotate cb default cardinality.telemetry-compass.com/resume-enforcement="$(date +%s)" --overwrite
```

If the team removed a drop on purpose, protect that metric first, or it may be dropped again after the resume
([runbooks](../runbooks/README.md#cardinalitybudgetpaused)).

## For application teams

- You can read your budget but not change it; talk to the platform team.
- To bring usage down, look at `topMetrics` and remove high-cardinality labels (user IDs, request IDs, URLs)
  from your instrumentation.
- If a metric you need was dropped, ask the platform team to add it to `protectedMetrics` or raise the limit.
  Removing the drop rule yourself pauses the budget and the drop stays visible as `Reverted`.
