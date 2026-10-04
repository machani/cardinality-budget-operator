# 0008. Drift, pause and resume

- Status: Accepted
- Date: 2026-10-04

## Context

Drop rules can disappear without the operator's involvement:

- Argo CD or Flux self-heal syncs the monitor from Git (rule, annotation and label gone).
- `helm upgrade` replaces the relabel list (rule gone, annotation may survive).
- A person removes the rule as a manual emergency undo, which the project promises must work.

In each case `status.enforcedDrops` still claims the drop is active. The HLD placed revert detection in V2,
but without it status is wrong, which is a V1 correctness problem.

Several situations need a person before enforcement continues: ineffective drops and failed reloads
([ADR 0006](0006-enforcement-safety.md)), and drift.

## Options considered

1. Re-apply every time (endless fight with GitOps; overrides deliberate human undo).
2. Re-apply up to N times (still fights; more status fields).
3. Detect, never re-apply, pause.

## Decision

**Detection (V1)**

- Every reconcile, for each `enforcedDrops` entry, check that every selected monitor has both the rule and the
  annotation entry, using the informer cache.
- A missing rule is drift, except for new or newly selected monitors
  ([ADR 0009](0009-spec-changes-and-restore-order.md)).

**Response**

- The entry's state becomes `Reverted`; `Enforced=False, reason=Reverted`; one Event naming the monitors;
  operator metric.
- The operator never writes to the monitor again for that drop, including the annotation.

**Pause**

- A budget is paused when `Enforced` has reason `Reverted`, `Ineffective` or `ConfigReloadFailed`.
- Paused means no new drops and no extension to new monitors. Restores still run when usage is below the warn
  threshold.

**Resume (one mechanism for all three reasons)**

- The platform sets an annotation on the budget:
  `cardinality.telemetry-compass.com/resume-enforcement: "<any new value>"`, for example a timestamp.
- If the value differs from `status.lastResumeToken`, the operator clears the pause, removes `Reverted` entries
  from `enforcedDrops` (they are not re-applied), records the token, and continues normally.
- An annotation is used instead of a spec field so the API meaning and `generation` do not change.

**Cleanup**

- Restores and deletion cleanup tolerate a missing rule, annotation or monitor (NotFound means done).

**GitOps documentation**

- Argo CD `ignoreDifferences` for `metricRelabelings`, the label and the annotation; Flux
  `kustomize.toolkit.fluxcd.io/ssa` annotation options. Both verified in the lab before documenting.

## Consequences

- New status field `lastResumeToken`; new state `Reverted`; new condition reasons.
- The HLD V2 row "GitOps coexistence" shrinks to smarter handling (for example ownership awareness).
- Tests: Argo-style patch revert, Helm-style partial revert, manual removal, resume.
