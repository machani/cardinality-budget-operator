# 0015. Events in every mode, condition reasons, and Stale

- Status: Accepted
- Date: 2026-10-12

## Context

Implementing the reconcile loop (F-0109 to F-0111) needed three answers the HLD did not give:

1. The HLD reconcile loop says "Observe: status only; Warn: also Events and plannedDrops". Observe is the
   default mode (ADR 0002), so most budgets would show no Events in `kubectl describe`, not even when the
   backend is unreachable.
2. `metav1.Condition` requires a reason. The HLD lists reasons for `Ready` and `Enforced` only.
3. ADR 0005 defines "stale" (no valid measurement newer than `--max-measurement-age`) but not how it shows
   next to the other `Ready` reasons.

## Options considered

Events: (a) keep Observe silent; (b) every mode records state-change Events, Warn adds `plannedDrops`.

## Decision

**Events (option b).** Every mode records one Event on the budget per state change. The state is the
phase plus the `Ready` reason, so going Healthy → Warning gives one Event, and the backend going from
warming up to unreachable gives one Event, but an unchanged measurement gives none. Event reasons:
`BudgetHealthy`, `BudgetWarning`, `BudgetExceeded`, or the `Ready` reason when the measurement or spec is
not usable. Type `Normal` for Healthy, `Warning` otherwise. Events use the `events.k8s.io` API.

Mode differences are now: Observe = status + Events; Warn = + `plannedDrops`; Enforce = + drop rules.

**Condition reasons.**

| Condition | True | False |
| --- | --- | --- |
| `Ready` | `MeasurementValid` | `InvalidSpec`, `BackendUnreachable`, `BackendWarmingUp`, `PartialData`, `Stale` |
| `BackendReachable` | `BackendReady` (also when a query returned warnings: the backend answered) | `BackendUnreachable`, `BackendWarmingUp` |
| `BudgetExceeded` | `OverBudget` | `WithinBudget` |

While the measurement is invalid, `BudgetExceeded` and `exceededSince` keep their last values.

**Stale.** When the current measurement is invalid and the last valid one (`lastMeasured`) is older than
`--max-measurement-age`, `Ready` uses reason `Stale` (the numbers in status are too old to read); the
underlying cause stays on `BackendReachable` and in the message. A budget never measured shows the
underlying reason.

**plannedDrops** is filled in Warn and Enforce mode from the warn threshold on (Warning or Exceeded),
so teams see what would be dropped before reaching 100%. This is marked `TODO(machani)` in the code for
review.

## Consequences

- HLD reconcile step 6, the conditions table and CLAUDE.md updated.
- Tests: an Event per state change and none for unchanged measurements; every reason above; Stale after
  the maximum age; never measured is not Stale.
