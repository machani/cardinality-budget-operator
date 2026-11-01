# 0019. Monitors synced by GitOps tools: a documented limit in v1

- Status: Accepted
- Date: 2026-11-01
- Refines: [ADR 0008](0008-drift-pause-and-resume.md) (GitOps documentation)

## Context

ADR 0008 planned to document Argo CD `ignoreDifferences` and Flux options that keep the operator's drop rules
through a sync, "verified in the lab before documenting". The v1.0 lab ([verify-v1.0.md](../../hack/lab/verify-v1.0.md)
Part 3, Argo CD v3.5.6, Prometheus Operator CRDs v0.94) found that no such option exists for Argo CD:

- Argo CD's first sync of a ServiceMonitor that already had a drop rule removed the rule, with no change in Git.
- After that, a rule the operator added did not show as drift (`Synced`), so self-heal did not act, but the next
  sync of the monitor (a change in Git) removed it.
- With the guide's `ignoreDifferences` (single relabel items) and `RespectIgnoreDifferences=true`, with the
  whole `metricRelabelings` field ignored, and with `ServerSideApply=true`: the rule was removed every time.

The cause is the ServiceMonitor schema: `endpoints` (and PodMonitor `podMetricsEndpoints`) has no
`x-kubernetes-list-type`, so it is an atomic list. Client-side apply sends a JSON merge patch and server-side
apply owns the list as one value; both replace the whole list with the Git copy, which has no drop rule. The
same is expected for Flux (server-side apply), `kubectl apply` and Helm upgrades of the team's own manifests;
those were not tested.

In every case the operator behaved as designed: it saw its rule gone, marked the drop `Reverted`, paused the
budget and fired `CardinalityBudgetPaused`. Nothing was lost or fought over; enforcement just stops.

## Options considered

1. **Document the limit** for v1: the GitOps guide says that a sync removes the operator's drops and how to
   work with that.
2. **Hold v1.0 and research a fix**, for example Argo CD `managedFieldsManagers` or putting drop rules
   somewhere other than the monitor. Anything beyond `metricRelabelings` on monitors is outside the V1 scope.
3. **Skip monitors managed by Argo CD or Flux** (detected by their tracking labels and annotations) and report
   them in status. New behaviour that guesses which tools are in use.

## Decision

Option 1.

- The GitOps guide states the limit with the lab results and drops the `ignoreDifferences` recommendation.
- For namespaces whose monitors a GitOps tool syncs, it recommends Warn mode (teams add the drops from
  `plannedDrops` to Git), or Enforce with the "move the drop into Git" step when `CardinalityBudgetPaused`
  fires.
- The readiness review lists it as exception E6. The HLD risk row and Idea log record it; a fix is V2 work
  under "GitOps coexistence".

## Consequences

- No code change in v1.0. ADR 0008's pause stays the safety net and is what users see.
- Moving a budget from `v1alpha1` to `v1beta1` through Argo CD worked in the same lab, with a plain sync and
  with `ServerSideApply=true`; the runbook no longer sends Argo CD users to a workaround.
- Flux, `kubectl apply` and Helm are documented as expected to behave like Argo CD until tested.
