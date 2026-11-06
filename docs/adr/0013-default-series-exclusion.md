# 0013. Default series exclusion

- Status: Accepted
- Date: 2026-10-10
- Refines: [ADR 0003](0003-series-attribution.md) (exact default left to Phase 1)

## Context

[ADR 0003](0003-series-attribution.md) excludes cluster exporters from namespace usage with one flag,
`--exclude-series`, and gave `job!~"kube-state-metrics|kubelet|node-exporter"` as an example, with the exact
names to be verified in Phase 1.

Phase 1 fact 6 (lab: kube-prometheus-stack 92.3.0, Prometheus 3.15.0, `enforcedNamespaceLabel: namespace`, the
stack's own monitors in `excludedFromEnforcement`) ran `count by (job, namespace) ({namespace!=""})`:

| Source | `namespace` label values | Series |
| --- | --- | --- |
| `kubelet` (cAdvisor) | kube-system, local-path-storage, monitoring | 6,523 |
| `kube-state-metrics` | six namespaces incl. `default` | 1,176 |
| `apiserver` | `default` | 28,990 |
| `coredns` | `kube-system` | 397 |
| no `job` label (the stack's recording rules) | monitoring, kube-system, local-path-storage | 111 |
| `node-exporter`, `kps-*` | monitoring only | — |

Two sources were missing from the example:

- `apiserver`: the `kubernetes` Service lives in `default`, so every API server series gets
  `namespace="default"`. Its monitor is excluded from `enforcedNamespaceLabel`, so the label is never
  rewritten. A budget in `default` would count about 29,000 series the team cannot control and could try to
  drop them.
- `coredns`: the same effect for `kube-system`.

The general cause: a monitor in `excludedFromEnforcement` keeps the namespace of its **targets**, not of the
monitor. Every such monitor's job must therefore also be in `--exclude-series`.

Recording-rule outputs aggregate the `job` label away but keep `namespace`. The stack's rules add series to
every namespace that no team controls and no drop rule can remove.

## Options considered

1. Keep the ADR 0003 example; document the gaps.
2. Complete the job list.
3. Complete the job list and exclude series without a `job` label.

## Decision

Option 3.

- Default `--exclude-series`:
  `job!~"apiserver|coredns|kube-state-metrics|kubelet|node-exporter",job!=""`
  (verified against kube-prometheus-stack 92.3.0).
- `job!=""` removes recording-rule outputs, including a team's own rules. They cannot be dropped by a
  monitor drop rule ([ADR 0006](0006-enforcement-safety.md)), so leaving them out keeps the ADR 0003 meaning:
  the budget counts series this namespace's monitors can control.
- Documented rule for platform teams: every monitor listed in Prometheus `excludedFromEnforcement` must have
  its job matched by `--exclude-series`, or its targets' series count against the target's namespace.

## Consequences

- HLD flag default and risk table updated.
- Install docs show how to check a cluster:
  `count by (job, namespace) ({namespace!=""})` lists every job that reports other namespaces.
- Job names depend on how the stack is installed; clusters with other names set the flag themselves.
- Tests: the default flag parses at startup; the usage query contains both matchers; a fake Prometheus series
  without `job` is not counted.
- Recording-rule outputs of a team's own PrometheusRules are not counted. Fact 3 (Phase 1 batch C) checks how
  `enforcedNamespaceLabel` labels them; if that changes the picture, a new ADR follows.
