# Feature tracker

This folder tracks every feature planned for v1.0, one file per milestone. The [HLD](../design/hld.md) says
**what** each feature is, the [ADRs](../adr/README.md) say **why**, and this folder says **how far along** it is.

| Milestone | File | Scope | Runs on |
| --- | --- | --- | --- |
| v0.1 | [v0.1.md](v0.1.md) | CRD, usage provider, Observe and Warn modes | Lab cluster |
| v0.2 | [v0.2.md](v0.2.md) | Enforce mode, undo, finalizer, drift, pause and resume, kill switch | Lab cluster |
| v0.3 | [v0.3.md](v0.3.md) | Hardening: HA, security, rate limit, Helm chart, CI matrix, scale tests | Hardware cluster |
| v1.0 | [v1.0.md](v1.0.md) | Production readiness bar, `v1beta1` API | Production |

V2 features and ideas are not tracked here. They live in the HLD sections "V2 scope" and "Idea log".

## Columns

| Column | Meaning |
| --- | --- |
| ID | `F-<milestone><number>`, for example `F-0105` is v0.1 feature 5. IDs are never reused or renumbered. |
| Feature | One feature that fits in one small commit (or a few) |
| Ref | The HLD section or ADR that defines it |
| Status | See below |
| Done when | A check that anyone can run to prove the feature works |
| Commit | Short hash of the commit that finished it |

## Status values

| Status | Meaning |
| --- | --- |
| Todo | Not started |
| In progress | Being built now |
| Done | "Done when" check passed and the commit is merged |
| Blocked | Waiting on something; say what in the Notes section of the file |
| Dropped | No longer planned; link the ADR or HLD change that removed it |

## Rules

Rows in each milestone file are listed in build order: work top to bottom unless a row is `Blocked`.

1. Update the status in the same commit as the code (`In progress` → `Done`, with the commit hash added afterwards).
2. A new feature must already be in the HLD. If it isn't, change the HLD first (with an ADR if behaviour or the API
   changes), then add a row here.
3. Out-of-scope items from CLAUDE.md never get a row, even as `Todo`.
4. A milestone is done when every row is `Done` or `Dropped`. Then update the "Current milestone" line in CLAUDE.md.
