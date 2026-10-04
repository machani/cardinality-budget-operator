# Architecture Decision Records

Each file records one design decision: the context, the options considered, what was chosen and why.
The HLD (`docs/design/hld.md`) describes the design as it is now; ADRs explain how it got there.

Rules:

- Files are numbered `NNNN-short-title.md` and never deleted or renumbered.
- To change a decision, add a new ADR and set the old one's status to `Superseded by NNNN`.
- Every HLD change that alters behaviour or the API links to an ADR.
- Statuses: `Proposed`, `Accepted`, `Superseded by NNNN`, `Deprecated`.

| ADR | Title | Status |
| --- | --- | --- |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-budget-ownership-and-rbac.md) | Budget ownership and RBAC | Accepted |
| [0003](0003-series-attribution.md) | Series attribution: namespace label and exporter exclusion | Accepted |
| [0004](0004-one-budget-per-namespace.md) | One budget per namespace and the Ready condition | Accepted |
| [0005](0005-measurement-validity-and-backend-access.md) | Measurement validity and backend access | Accepted |
| [0006](0006-enforcement-safety.md) | Enforcement safety: caps, effectiveness, settle period, reload check | Accepted |
| [0007](0007-drop-rule-ownership-and-write-strategy.md) | Drop rule ownership and write strategy | Accepted |
| [0008](0008-drift-pause-and-resume.md) | Drift, pause and resume | Accepted |
| [0009](0009-spec-changes-and-restore-order.md) | Spec changes and restore order | Accepted |
| [0010](0010-rate-limit-and-kill-switch.md) | Global rate limit and kill switch | Accepted |
| [0011](0011-version-support-policy.md) | Version support policy | Accepted |
