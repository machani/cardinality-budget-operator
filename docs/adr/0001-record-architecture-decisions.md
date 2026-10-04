# 0001. Record architecture decisions

- Status: Accepted
- Date: 2026-10-04

## Context

The operator enforces budgets by deleting data (drop rules on scrape configs). Contributors, reviewers and
operators need to know not only what the design is, but why it was chosen and which alternatives were rejected.
The HLD holds the current design; it does not hold the reasoning or the history.

## Decision

Keep Architecture Decision Records in `docs/adr/`, committed to git, using this template:

- **Status** and **Date**
- **Context**: the problem and the forces involved
- **Options considered**: each option with its trade-offs
- **Decision**: what was chosen, with concrete values (fields, flags, defaults)
- **Consequences**: what changes, what gets harder, what must be verified

Records are immutable once accepted, except for the status line. A changed decision gets a new ADR that
supersedes the old one. The HLD is updated in the same change and links to the ADR.

Personal learning material (plan, notes) lives in `doc/`, which is not committed.

## Consequences

- Every API or behaviour change needs an ADR before code, matching the project rule "if code and HLD disagree,
  propose an HLD change first".
- Reviews can point at a specific ADR instead of re-arguing a decision.
