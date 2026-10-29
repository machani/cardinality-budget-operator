# Introduction to CardinalityBudget

## The problem

Prometheus memory and cost grow with the number of **active series**: every unique combination of a metric
name and its labels. One team adding a label like `user_id` or `request_id` can create hundreds of thousands
of series in minutes, slow down or crash a shared Prometheus, and hurt every other team on it. Usually nobody
notices until Prometheus is already in trouble.

## What it is

CardinalityBudget is a Kubernetes **policy operator**. It gives each namespace a budget of Prometheus series,
like a spending limit on a shared credit card:

- The platform team writes one `CardinalityBudget` per namespace (always named `default`).
- The operator measures the namespace's usage in Prometheus and shows it on the resource.
- Depending on the mode, it only reports (Observe), also says what it would cut (Warn), or carefully cuts the
  biggest metrics itself (Enforce).

```console
$ kubectl get cb -A
NAMESPACE   NAME      PHASE     SERIES   LIMIT   USAGE %   MODE   AGE
demo        default   Warning   1012     1100    92        Warn   3m
```

## The resource

```yaml
apiVersion: cardinality.telemetry-compass.com/v1beta1
kind: CardinalityBudget
metadata:
  name: default          # always "default": one budget per namespace
  namespace: team-a
spec:
  maxActiveSeries: 50000 # the budget
  warnThresholdPercent: 90
  mode: Warn             # Observe | Warn | Enforce
  enforcement:
    gracePeriod: 30m     # how long over budget before the first drop
    maxMetricsToDrop: 1  # per step
    maxTotalDrops: 5     # in total
    protectedMetrics:    # never dropped (regex, fully anchored)
      - "up"
      - "http_requests_total"
    monitorSelector: {}  # which ServiceMonitors/PodMonitors may get drop rules (empty = all)
```

The status shows the phase (`Healthy`, `Warning`, `Exceeded`, `Enforcing`, `Unknown`), the series count, the
top 10 metrics, `plannedDrops` (Warn), `enforcedDrops` (Enforce) and conditions. `kubectl describe cb default`
also shows an Event for every state change.

## How it enforces

The operator never sits in the data path and never deletes samples itself. In Enforce mode it adds a standard
Prometheus drop rule to the namespace's own ServiceMonitors and PodMonitors:

```yaml
metricRelabelings:
  - action: drop
    sourceLabels: [__name__]
    regex: avalanche_gauge_metric_mmmmm_0_0
```

Prometheus then stops storing that metric at scrape time. The rule is marked as owned by the operator (an
annotation and a label on the monitor), so it can be removed again later without touching the team's own
rules.

## Who does what

| Role | Can |
| --- | --- |
| Platform team | Create and edit budgets, choose the mode, resume a paused budget, use the kill switch |
| Application team | Read their budget (`kubectl get/describe cb`), fix their metrics |
| Operator | Measure, report, add and remove its own drop rules |

## Design principles

1. **Safety beats features.** Enforce deletes data, so every uncertain situation stops instead of acting:
   invalid measurement, failed Prometheus reload, a drop that did not help, someone removing a rule.
2. **Gradual.** One small step, then wait and check, then the next.
3. **Reversible.** Every drop is recorded and is undone newest first when usage goes down, when Enforce is
   switched off, or when the budget is deleted.
4. **Level-triggered.** Everything is derived from the cluster and Prometheus on each run, so restarts,
   failovers and upgrades are safe.

## Scope

In: Prometheus 3.x, ServiceMonitor and PodMonitor drop rules, one budget per namespace, Observe/Warn/Enforce.

Out (for now): other backends (VictoriaMetrics, Mimir), label-level enforcement, cluster-wide budgets,
churn budgets, forecasting, team-editable budgets.

## Status

Alpha (v0.3). v0.1 (Observe and Warn), v0.2 (Enforce) and v0.3 (hardening) are done and verified on a lab
cluster; scale tests are still open. v1.0 (API v1beta1, release pipeline, full docs) is next. Run v0.x on lab clusters only.

Next: [capabilities](capabilities.md), [how it works](how-it-works.md), [FAQ](faq.md). Design: [HLD](../design/hld.md); decisions: [ADRs](../adr/README.md).
