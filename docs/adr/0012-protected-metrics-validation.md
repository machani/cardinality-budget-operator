# 0012. Protected metrics validation

- Status: Accepted
- Date: 2026-10-10

## Context

`enforcement.protectedMetrics` is a list of regular expressions for metrics that must never be dropped. The CEL
rules in the HLD check ranges and the name, but not that each pattern is a valid regex. A pattern such as
`slo_(.*` is accepted by the API server. The operator then cannot tell which metrics are protected.

If the code skips the bad pattern, it fails open: the metric that pattern was meant to protect can appear in
`plannedDrops` (v0.1) and be dropped (v0.2). This breaks the safety rule "never drop a protected metric".

Two related gaps:

- Go's `regexp` matches substrings; Prometheus anchors relabel regexes to the whole value. The meaning of a
  pattern was not defined.
- CEL rules on lists and strings need `maxItems` and `maxLength`, or the API server cannot estimate their cost
  and rejects the CRD.

## Options considered

1. **Validate in CEL only.** Bad patterns are rejected at `kubectl apply`. Depends on whether CRD CEL on the
   minimum supported Kubernetes version can check that a regex compiles; objects stored before the rule existed
   are not re-checked.
2. **Validate at runtime only.** Any invalid pattern blocks drops for the budget and is reported in status. Always
   works, but the mistake is only visible after apply.
3. **Both.** CEL where available, plus the runtime check as the safety net.

## Decision

Option 3.

**Meaning of a pattern**

- Each pattern is fully anchored, like Prometheus: a metric is protected if it matches `^(?:pattern)$`.
- Patterns use Go RE2 syntax, the same syntax Prometheus uses.

**Schema limits**

- `protectedMetrics`: `maxItems: 64`, each item `maxLength: 256`, `minLength: 1`.

**CEL (apply time)**

- If Phase 1 confirms that CRD CEL on the minimum supported Kubernetes version can check that a string is a
  valid regex, add that rule to `protectedMetrics` items. If it cannot, skip this layer; the runtime check
  still applies.

**Runtime check (every reconcile)**

- The operator compiles every pattern, anchored as above. If any pattern fails to compile:
  - `Ready=False`, reason `InvalidSpec`; the message names the bad pattern. One Event on the budget.
  - Measurement, `activeSeries`, `usagePercent`, `topMetrics` and the phase still update.
  - `plannedDrops` is empty.
  - **Freeze:** no drop rules are added or removed for this budget, the same as an invalid measurement
    ([ADR 0005](0005-measurement-validity-and-backend-access.md)).
  - Deletion cleanup through the finalizer and the kill switch work as usual.
- `InvalidSpec` takes precedence over the measurement reasons in `Ready`, because only a person can fix it.
- The reason is general so that later spec checks (for example an invalid `monitorSelector`) can reuse it
  without another API change.

## Consequences

- New `Ready` reason `InvalidSpec`; HLD and CLAUDE.md updated.
- New schema limits on `protectedMetrics`.
- Tests: invalid pattern → `Ready=False/InvalidSpec`, empty `plannedDrops`, no monitor writes; status still
  updates; anchoring (`foo` protects `foo` but not `foo_total`); fixing the pattern clears the reason.

## Verify in Phase 1

- Whether CRD CEL on the minimum supported Kubernetes version can validate that a string is a regex.
