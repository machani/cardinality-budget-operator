# 0003. Series attribution: namespace label and exporter exclusion

- Status: Accepted
- Date: 2026-10-04

## Context

The budget counts "series that belong to a namespace". Two questions decide what that means:

1. **Which label identifies the namespace?** The Prometheus Operator sets `namespace` to the scrape target's
   namespace. With `honorLabels: true` on a monitor, the scraped app's own `namespace` label wins instead. Any
   team that can create such a monitor can emit series labelled with another team's namespace. That consumes
   the other team's budget and, in Enforce mode, causes drop rules on the other team's monitors.
2. **What about cluster-wide exporters?** kube-state-metrics and kubelet/cAdvisor produce series labelled with
   a team's namespace, but are scraped by platform monitors. Drop rules on the team's monitors cannot remove
   them, so in Enforce mode the operator would drop the team's application metrics to compensate for series
   the team does not control.

The Prometheus `spec.enforcedNamespaceLabel` setting forces the namespace label to the namespace of the
monitor that scraped the series, overriding `honorLabels`. With it, "series counted for namespace X" equals
"series scraped by monitors in namespace X", which is exactly the set the operator can enforce on.

## Options considered

Label:

1. Trust `namespace` as-is (spoofable; counts unenforceable series).
2. Same query, with `enforcedNamespaceLabel` documented as a prerequisite.
3. Option 2 plus the operator reading the `Prometheus` CR to check the setting (needs RBAC on
   `prometheuses`; close to the non-goal of managing Prometheus).

Exporters:

1. Charge them to the namespace in the label (unenforceable).
2. Exclude them with an operator-level selector.
3. Exclude them and report them separately in status.

## Decision

- Usage query: `count({<label>="<ns>", <exclusion>})`, where `<label>` is the flag `--namespace-label`
  (default `namespace`).
- `enforcedNamespaceLabel` on the Prometheus resource is a **hard requirement for Enforce mode** and a
  **recommendation for Observe and Warn**. The operator does not inspect the Prometheus CR in V1.
- Exporter exclusion is **one cluster-wide flag**, `--exclude-series`, holding PromQL label matchers. The
  default matches kube-prometheus-stack job names, for example
  `job!~"kube-state-metrics|kubelet|node-exporter"` (exact names verified in Phase 1).
- The operator parses `--exclude-series` at startup with the PromQL parser and refuses to start if it is
  invalid.
- The lab kind cluster enables `enforcedNamespaceLabel`.
- Recording-rule outputs are handled in [ADR 0006](0006-enforcement-safety.md).

## Consequences

- The budget means "series this namespace's monitors can control", the only definition Enforce mode can act
  on safely.
- Install docs and the compatibility matrix list the `enforcedNamespaceLabel` requirement.
- If the platform excludes exporters from namespace enforcement (`excludedFromEnforcement`), the exclusion flag
  must match them.
- Reporting excluded series in status (`status.infrastructureSeries`) can be added later as an optional field.

## Verify in Phase 1

- Behaviour of `enforcedNamespaceLabel` with `honorLabels: true` and with PrometheusRule outputs.
- kube-prometheus-stack job names for the default exclusion.
