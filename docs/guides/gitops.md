# GitOps guide

How the operator works with Argo CD and Flux. The short version: budgets in Git work as usual, but **a sync of
a ServiceMonitor or PodMonitor removes the operator's drop rules**, and no Argo CD setting prevents it. The
operator then pauses the budget and alerts ([ADR 0008](../adr/0008-drift-pause-and-resume.md)), so nothing
breaks, but enforcement stops. In namespaces whose monitors a GitOps tool syncs, use Warn mode or move drops
into Git ([ADR 0019](../adr/0019-gitops-managed-monitors.md)).

## Budgets in Git

Budgets belong in the platform team's repository, like any other policy. Use `apiVersion:
cardinality.telemetry-compass.com/v1beta1`. The operator writes only the status subresource and its finalizer,
so budgets never show as out of sync. The resume annotation is the one field the operator reacts to that people
set by hand: set it with `kubectl annotate`, or keep it in Git and change the value to resume.

Moving existing budgets from `v1alpha1` to `v1beta1` in Git: Argo CD v3.5 syncs the change with a plain sync
and with `ServerSideApply=true` (tested in the lab). A client-side `kubectl apply` fails once with
`metadata.resourceVersion: Invalid value: 0`; the [runbook](../runbooks/README.md#upgrade-to-v10-move-budgets-to-v1beta1)
has the fix. Flux uses server-side apply and is expected to work (not tested).

## Monitors in Git: what a sync does

In Enforce mode the operator appends a drop rule to `metricRelabelings` and adds an annotation and a label to
the monitor. In the monitor CRDs, `endpoints` (`podMetricsEndpoints` on PodMonitors) is an atomic list: every
apply, client-side or server-side, replaces the whole list with the copy from Git, which has no drop rule.

Tested with Argo CD v3.5.6 and Prometheus Operator v0.94 ([lab](../../hack/lab/verify-v1.0.md#part-3-argo-cd)):

| What happened | Result |
| --- | --- |
| Argo CD takes over a monitor that already has a drop | The first sync removes the rule, with no change in Git |
| The operator adds a drop to a monitor Argo CD manages | The app stays `Synced`: Argo CD shows no drift, so self-heal does nothing |
| The next sync of that monitor (for example a change in Git) | The rule is removed |
| Same, with `ignoreDifferences` on the drop items, on the whole `metricRelabelings` field, or with `ServerSideApply=true` | The rule is removed every time |

Each removal makes the operator mark the drop `Reverted`, pause the budget and fire `CardinalityBudgetPaused`;
it never re-applies the rule. The annotation stays on the monitor, which is how the operator knows the rule was
its own.

Expected to behave the same, not tested: Flux (server-side apply), `kubectl apply` of the team's manifests,
and Helm upgrades of the team's chart.

## Recommended: Warn mode for GitOps-managed monitors

In a namespace whose monitors are synced from Git, use `mode: Warn`. The operator lists the drops it would make
in `status.plannedDrops`; the team adds them to its own monitors in Git. Nothing the operator writes can be
lost, and the budget never pauses.

## Enforce anyway: move the drop into Git when it pauses

Enforce mode still works until the next sync of the monitor, and a drop happens without waiting for a person.
Every drop is lost at the following sync, so expect to repeat this. When `CardinalityBudgetPaused` fires with
reason `Reverted`:

1. Add a drop rule for each `Reverted` metric in `status.enforcedDrops` to the team's monitor in Git, and sync.
2. Resume the budget (`cardinality.telemetry-compass.com/resume-enforcement`, any new value). The metric is
   now dropped by the team's own rule, so the operator does not pick it again.

To move a drop into Git **before** a sync has removed it, first add the metric to `protectedMetrics`: the
operator then removes its own rule right away and never drops that metric again, so the sync finds nothing of
the operator's to remove and the budget does not pause.

## Leaving Enforce mode or deleting a budget

The operator removes only the rules listed in its annotation, newest first. A rule a sync already removed
counts as removed; nothing needs to happen in Git.
