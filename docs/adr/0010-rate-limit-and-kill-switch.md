# 0010. Global rate limit and kill switch

- Status: Accepted
- Date: 2026-10-04

## Context

`--max-drops-per-hour` applies across all budgets, but reconcile must not rely on in-memory state. With
parallel reconciles, two budgets can both see "9 of 10 used" and both drop. The informer cache also lags
slightly behind the operator's own status writes.

`--enforcement-paused` (the global kill switch) had no defined scope.

## Options considered

Count source:

1. Derive from `enforcedDrops[].since` in every budget.
2. Option 1 plus a per-budget history list in status.
3. A shared Lease or ConfigMap (extra RBAC, write bottleneck, invisible to teams).

## Decision

**Count**

- Drops in the last hour = `enforcedDrops` entries in all budgets whose `since` is within the last hour.
- Known undercount: a metric dropped, restored and dropped again within one hour is counted once. Restores
  need usage below the warn threshold plus a settle period, so this is rare and bounded; it is documented.
- Extending a drop to a new monitor does not count.
- Default `--max-drops-per-hour=10`.

**Concurrency**

- Measurement reconciles run in parallel (`MaxConcurrentReconciles` > 1).
- The enforcement step (check limit, apply drops, write status) runs under an in-process mutex. The mutex
  coordinates work; it is not stored state.
- Inside the mutex, budgets are read with the uncached `APIReader`.
- Leader election guarantees one active operator pod.

**Limit reached**

- `Enforced=False, reason=RateLimited`, one Event, operator metric.
- Not a pause: it clears automatically as the hour window moves. No priority between budgets in V1.

**Kill switch `--enforcement-paused`**

| Activity | When on |
| --- | --- |
| Measuring, status, Events, metrics, Warn-mode `plannedDrops` | Continue |
| New drops | Blocked |
| Restores and extension to new monitors | Blocked |
| Finalizer cleanup on budget deletion | Blocked; deletion waits; docs describe manual removal using the label from [ADR 0007](0007-drop-rule-ownership-and-write-strategy.md) |

- Every Enforce-mode budget gets `Enforced=False, reason=Paused`.
- Operator metric `cardinalitybudget_enforcement_paused` (0 or 1) feeds the "kill switch active" alert.
- It stays a flag (changing it needs a Deployment rollout); a watched ConfigMap is not used in V1.

## Consequences

- No new status fields or objects.
- Runbooks: "kill switch on", "budget stuck deleting while paused", "rate limit reached".
- Tests: two concurrent budgets at the limit, limit window rollover, kill switch blocks each write type.
