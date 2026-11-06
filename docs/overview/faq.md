# CardinalityBudget FAQ

## General

**What is a "series"?**
One unique combination of a metric name and its labels. `http_requests_total{path="/a"}` and
`http_requests_total{path="/b"}` are two series. Prometheus memory grows with the number of active series.

**Why a budget per namespace?**
A namespace usually maps to a team or an app. A budget makes each team's share of a shared Prometheus
visible and limits how much one team can hurt the others.

**Why must the budget be called `default`?**
So there is exactly one budget per namespace and no question which one applies. The API server rejects other
names.

**Can teams edit their own budget?**
No. The platform team owns budgets; teams get read access. Otherwise a team could simply raise its own limit.

**Does the operator sit between my app and Prometheus?**
No. It only reads from Prometheus and edits ServiceMonitors/PodMonitors. If the operator is down, scraping
continues normally; only measuring and enforcing stop.

## Measuring

**Which series count against my namespace?**
Every series with `namespace="<your namespace>"`, except cluster exporters (kubelet, kube-state-metrics,
node-exporter, ...) and recording-rule outputs (names with `:`).

**Can a team put its series on another namespace's bill?**
Not if Prometheus runs with `enforcedNamespaceLabel: namespace`. Prometheus then overwrites the `namespace`
label with the monitor's own namespace, even with `honorLabels: true`. Enforce mode requires this setting.

**Why is my phase `Unknown`?**
The measurement could not be trusted. Check the `Ready` condition's reason: `BackendUnreachable`,
`BackendWarmingUp` (Prometheus restarted less than 15 minutes ago), `PartialData` (Prometheus returned
warnings), `Stale` or `InvalidSpec`. Nothing is dropped or restored while `Unknown`.

**How often is usage measured?**
Every `--measure-interval` (5 minutes by default), with a little jitter.

## Enforcing

**What exactly happens when I go over budget in Enforce mode?**
After `gracePeriod` (30m) over budget, the operator picks the largest metric that is not protected and adds a
drop rule for it to your monitors. It waits the settle period (10m), checks the drop worked, and drops the
next one if you are still over budget. At most `maxMetricsToDrop` per step and `maxTotalDrops` in total.

**Is my data deleted?**
New samples of the dropped metric are no longer stored. Existing data stays until normal retention removes
it. The metric disappears from queries after about 2 minutes.

**How do I protect important metrics?**
List them in `enforcement.protectedMetrics` (regexes, matched against the full name). Recording rules
(names with `:`) are always protected.

**Will the operator break my own relabel rules?**
No. It appends its rule at the end and only ever removes rules listed in its own annotation on the monitor.

**When do dropped metrics come back?**
When usage falls below the warn threshold (default 90%), one step per settle period, newest first. Also when
the budget leaves Enforce mode, when a metric becomes protected (immediately), or when the budget is deleted.

**What does "paused" mean?**
Something unexpected happened, so the budget stops adding drops until a human looks:
`Ineffective` (the drop removed less than 90% of the series), `ConfigReloadFailed` (Prometheus did not load
its new config) or `Reverted` (someone removed or changed a drop rule).

**How do I resume a paused budget?**
Fix the cause, then set the resume annotation to a new value:
`kubectl -n <ns> annotate cb default --overwrite cardinality.telemetry-compass.com/resume-enforcement=<token>`.

**I removed a drop rule by hand. Will the operator put it back?**
No. It treats that as a deliberate decision, marks the drop `Reverted` and pauses. It never re-applies a
reverted drop.

**After a resume, can the same metric be dropped again?**
Yes. Resume means "continue": if the budget is still over its limit, the operator may pick the same metric
again one settle period later. If the team needs that metric, add it to `protectedMetrics` before resuming.

**Can enforcement run away across many namespaces?**
`--max-drops-per-hour` (10) limits new drops across all budgets together. Beyond that, drops wait
(`RateLimited`).

**How do I stop all enforcement immediately?**
Start the operator with `--enforcement-paused`. No monitor is changed at all, including restores and
deletion cleanup.

## Operations

**What happens if I delete a budget?**
The operator removes all its drop rules from the namespace's monitors, then lets the budget go. While the kill
switch is on, deletion waits.

**A budget is stuck deleting. Why?**
The operator is not running (or the kill switch is on), so nobody removes the finalizer. Start the operator;
only as a last resort remove the finalizer by hand after checking no monitor still carries its rules. See the
[runbooks](../runbooks/README.md).

**Why does `make undeploy` hang?**
A budget still exists in some namespace when the operator is removed. Delete all budgets first
(`kubectl delete cb --all -A`), then undeploy.

**Is it safe to restart or upgrade the operator mid-enforcement?**
Yes. All state lives in the budget's status and the monitors' annotations, so a new leader or a new version
continues where the old one stopped. Two replicas with leader election take over within seconds.

**How do I know the operator itself is healthy?**
Alerts: `CardinalityBudgetOperatorDown`, `ReconcileErrors`, `MeasurementInvalid`, `Paused`,
`ConfigReloadFailed`, `KillSwitchActive`, `RateLimited`. Each links its runbook.

**Why do I see `Failed to update lease ... object has been modified` at startup?**
Both replicas race for the leader lease; one wins, the other retries. Harmless.

## Limits

**Does it support VictoriaMetrics, Mimir or Thanos?**
Not in V1. Only the Prometheus 3.x HTTP API.

**Can it drop single labels instead of whole metrics?**
Not in V1. Whole metrics only; label-level enforcement is an idea for later.

**Which versions are supported?**
Kubernetes 1.35-1.37, Prometheus Operator v0.86.2-v0.94.1, Prometheus 3.13 (LTS) and 3.15, tested in CI.
