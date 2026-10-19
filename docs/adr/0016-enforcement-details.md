# 0016. Enforcement details: effectiveness baseline, restore step, ownership edge cases

- Status: Accepted
- Date: 2026-10-12
- Refines: [ADR 0006](0006-enforcement-safety.md), [ADR 0007](0007-drop-rule-ownership-and-write-strategy.md),
  [ADR 0008](0008-drift-pause-and-resume.md), [ADR 0009](0009-spec-changes-and-restore-order.md)

## Context

Implementing Enforce mode (v0.2) found gaps between the ADRs and what the code must decide:

1. ADR 0006: a drop is Ineffective if its metric's series "fell by less than 90%", but status kept no
   count from before the drop.
2. ADR 0009 / CLAUDE.md: restores happen "one step per settle period" without a step size.
3. ADR 0007 tracks ownership per monitor, but rules live per endpoint. If one endpoint already has an
   identical rule written by the team, adding ours to the other endpoints and removing "one occurrence per
   endpoint" on undo could remove the team's rule.
4. Monitors are written before status. If the status write fails, monitors carry owned rules that
   `enforcedDrops` does not list.
5. ADR 0008 removes Reverted entries on resume, but the drop's remaining rules on other monitors still
   carry the ownership annotation. Reading that annotation again would re-apply the reverted drop.
6. ADR 0006's reload check, read literally, would pause a budget whenever Prometheus fails to reload at
   any later time, even for changes the operator did not make.

## Decision

1. **Effectiveness baseline.** New field `enforcedDrops[].seriesAtDrop`: the metric's series in the
   namespace when dropped (from `topMetrics`). After the settle period, and only once Prometheus has loaded
   the change, the drop is `Effective` if at most 10% of `seriesAtDrop` remain, otherwise `Ineffective`.
2. **Restore step size** is `maxMetricsToDrop` metrics per settle period, newest first. Leaving Enforce
   uses the same step (ADR 0009); a lowered `maxTotalDrops` restores `min(maxMetricsToDrop, excess)`.
3. **Team rules.** A monitor where any endpoint already has the identical canonical rule (and the metric is
   not in our annotation) is left alone for that metric: no rule added, no annotation, never counted as drift.
   Known limit: if someone deletes our annotation but keeps our rules, those rules look like the team's and
   are no longer removed on restore; the label and Events still identify them.
4. **Adoption.** An owned metric found in a monitor annotation but missing from `enforcedDrops` is adopted:
   a `Pending` entry is added (baseline from a fresh series count) and the drop is extended to every
   selected monitor. A `Pending` drop younger than the settle period, adopted or not, blocks the next drop
   or restore step, so a failover between the monitor write and the status write cannot skip the settle
   period. Adoption does not move `lastEnforcementTime`: the rule was written earlier, and Prometheus may
   already have reloaded for it.
   A run whose writes change `metricRelabelings` (an immediate restore of a newly protected metric, rules
   left over after a resume, cleanup of a deselected monitor, extension to a new monitor) is a step: `lastEnforcementTime` moves and no
   drop or restore step follows in that run. A write that changes only the annotation or label (for example
   cleanup after someone removed every rule by hand) is not a step: Prometheus has nothing to reload, and
   waiting for a reload after it would block enforcement forever (found in the lab, 2026-10-15).
5. **Reverted cleanup.** Resume only lifts the pause. When a budget is not paused, Reverted drops are
   cleaned up: the rules the operator still owns for that metric are removed, then the entry is forgotten.
   The drop is never re-applied. While paused, nothing about a Reverted drop changes. After cleanup the
   metric is an ordinary candidate again: resume means "continue", so a budget still over its limit may
   drop the same metric one settle period later. To keep it, add it to `protectedMetrics` before resuming
   (decided 2026-10-15 after the lab run; remembering reverted metrics was rejected as extra state).
6. **Reload check window.** `ConfigReloadFailed` is raised only while the last change is unverified (inside
   the settle period, or while a drop is `Pending`) and more than 5 minutes have passed without a successful
   reload at or after the change (`lastConfigTime` compared in whole seconds). Outside that window a failed
   reload only stops the next step, because the settle period requires a successful reload.
7. **Phase `Enforcing`** whenever active (not Reverted) drops exist and the measurement is valid.
8. **Enforced condition precedence:** a pause reason (kept until resume, even while the kill switch is on),
   then `Paused` (kill switch), then `LimitReached`, then `Applied`. Without drops and pause the condition is
   removed.
9. **Cache freshness.** Inside the enforcement mutex the budget is re-read without the cache; if its
   resourceVersion differs from the cached copy, the step waits one second instead of acting on old status.
10. The global hourly limit (ADR 0010) stays in v0.3 (F-0301); the mutex and the uncached read are in place.

## Consequences

- Additive API change: `seriesAtDrop` (optional). HLD status table and CLAUDE.md updated.
- Tests (envtest): drop after grace on every endpoint with team rules untouched, settle period, Effective and
  Ineffective, reload failure, LimitReached, kill switch per write type, drift and resume cleanup, new monitor
  extension, deselected cleanup, newly protected restore, LIFO restore, leaving Enforce, deletion with and
  without the kill switch, no writes on an invalid measurement. Unit and fuzz tests for the rule functions.
