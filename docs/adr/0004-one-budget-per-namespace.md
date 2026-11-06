# 0004. One budget per namespace and the Ready condition

- Status: Accepted
- Date: 2026-10-04

## Context

Every budget in a namespace measures the same series ([ADR 0003](0003-series-attribution.md)), so two budgets
can only conflict: different limits, different modes, and two controllers adding drop rules to the same
monitors. The earlier working assumption ("oldest wins, others get `Ready=False, reason=Conflict`") used a
`Ready` condition that was not in the conditions list.

CEL rules cannot look at other objects, so "reject if another budget exists" is impossible without a webhook.
CRD CEL rules at the root can read `self.metadata.name`, and Kubernetes already guarantees unique names per
namespace.

## Options considered

1. Allow many; the oldest wins; others report a conflict (extra code and edge cases in every reconcile).
2. Exactly one, enforced by a fixed name (no code, no races).
3. Many, each covering a subset of monitors (cannot be measured with a per-namespace query; V2 at earliest).

## Decision

- A CRD validation rule requires the name `default`:
  `x-kubernetes-validations: [{rule: "self.metadata.name == 'default'", message: "a CardinalityBudget must be named 'default'"}]`.
- Add a summary condition **`Ready`**: `True` when the last reconcile succeeded and the measurement is valid
  and fresh ([ADR 0005](0005-measurement-validity-and-backend-access.md)); otherwise `False` with a reason.
- Conditions in V1: `Ready`, `BackendReachable`, `BudgetExceeded`, `Enforced`.

## Consequences

- No conflict handling in the reconciler.
- `kubectl get cb default -n <ns>` always works; `kubectl wait --for=condition=Ready` and kstatus-based tools
  work.
- Relaxing the name rule later is backward compatible; tightening it after release would not be, so starting
  strict is the safe direction.
- The HLD example changes from `name: payments` to `name: default`.

## Verify in Phase 1

- Root-level `self.metadata.name` access in CRD CEL on the supported Kubernetes versions.
