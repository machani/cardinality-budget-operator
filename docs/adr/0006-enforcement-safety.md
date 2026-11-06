# 0006. Enforcement safety: caps, effectiveness, settle period, reload check

- Status: Accepted
- Date: 2026-10-04

## Context

A drop rule only removes series scraped through the monitor it is added to. Some series in a namespace never
pass through a monitor: recording-rule outputs, ScrapeConfig and Probe targets, Pushgateway, remote write, and
monitors outside `monitorSelector`. Dropping such a metric changes nothing, and the operator would try again.

`maxMetricsToDrop` caps each step, but steps repeat. The only total limit was the schema cap of 10 on
`enforcedDrops`, with undefined behaviour when the list is full.

After a drop rule is added, its effect is delayed: the Prometheus Operator rewrites the config Secret
(seconds), the config-reloader triggers a reload (about 1-2 min), the next scrape applies the rule (one scrape
interval), and instant queries stop returning the series (up to the 5m lookback). About 10 minutes in total.
With a 5-minute measure interval, the next reconcile would see "still over budget" and drop more. Restores have
the same delay.

If the operator's change breaks the Prometheus config, every later reload fails while Prometheus keeps running
the old config. That affects the whole cluster.

TSDB head statistics keep counting removed series for up to 2-3 hours, until head compaction, so they cannot
show whether a drop worked.

## Options considered

1. Status quo: per-step cap only.
2. Explicit total cap.
3. Total cap plus effectiveness checks, a settle period tracked in status, and a reload check.

## Decision

**Total cap**

- New spec field `enforcement.maxTotalDrops`: integer 1-10, default `5`.
- CEL rule: `maxMetricsToDrop <= maxTotalDrops`.
- One drop is one metric name, however many monitors carry the rule.
- When the cap is reached and usage is still over budget: no more drops; `Enforced=False, reason=LimitReached`.

**Candidate filter**

- Skip candidate metrics whose name contains `:` (the recording-rule naming convention `level:metric:operation`).
- Skip metrics matching `protectedMetrics` (unchanged).

**Settle period**

- Flag `--enforcement-settle-period`, cluster-wide, default `10m`.
- New status field `lastEnforcementTime`, set on every apply and every restore.
- The next step (apply or restore) requires both `now > lastEnforcementTime + settle period` and a valid
  measurement taken after that point.
- `RequeueAfter` = the smaller of `--measure-interval` and the time left in the settle period.
- `gracePeriod` is unchanged: it decides when the first drop may happen (from `exceededSince`); the settle
  period decides the gap between steps.

**Reload check**

- After a change, before the settle period counts, `/api/v1/status/runtimeinfo` must show
  `reloadConfigSuccess: true` and `lastConfigTime` newer than the change, within 5 minutes.
- Otherwise: stop enforcement for the budget; `Enforced=False, reason=ConfigReloadFailed`; Event; operator
  metric. This pauses the budget ([ADR 0008](0008-drift-pause-and-resume.md)).

**Effectiveness check**

- After the settle period, a drop whose metric's series count in the namespace fell by less than 90% is marked
  `Ineffective`.
- The rule stays in place. No further drops happen for the budget (`Enforced=False, reason=Ineffective`), which
  pauses it ([ADR 0008](0008-drift-pause-and-resume.md)).
- 90% is a constant in V1.

**Reporting**

Reasons on the existing `Enforced` condition, not new condition types: `Applied` (True), and `Ineffective`,
`LimitReached`, `ConfigReloadFailed`, `Paused`, `Reverted`, `RateLimited` (False). Each comes with one Event and
an operator metric.

## Consequences

- New spec field, new status field, new flag, new condition reasons; HLD and CLAUDE.md updated.
- V1 uses the per-namespace `count()` instant query for measurement and verification. The scale tests decide
  whether its cost is acceptable.
- Tests: recording-rule name skipped, cap reached, ineffective drop, settle period blocks second step, reload
  failure pauses the budget.

## Verify in Phase 1

- Real time from a monitor change to the series disappearing from instant queries (checks the 10m default).
