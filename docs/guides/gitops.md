# GitOps guide

How the operator works with Argo CD and Flux. The short version: the operator writes into monitors that Git
also owns, so tell your GitOps tool to leave the operator's drop rules alone, or accept that a sync undoes them
and pauses the budget ([ADR 0008](../adr/0008-drift-pause-and-resume.md)).

## Budgets in Git

Budgets belong in the platform team's repository, like any other policy. Use `apiVersion:
cardinality.telemetry-compass.com/v1beta1`. The operator writes only the status subresource and its finalizer,
so budgets never show as out of sync. The resume annotation is the one field the operator reacts to that people
set by hand: set it with `kubectl annotate`, or keep it in Git and change the value to resume.

## Monitors in Git: what happens on sync

In Enforce mode the operator appends a drop rule to `metricRelabelings` and adds an annotation and a label to
the monitor. Git does not have them, so:

| Tool behaviour | Result |
| --- | --- |
| Shows the monitor as OutOfSync, no self-heal | Visible drift; nothing breaks |
| Self-heal or a manual sync removes the rule | The operator sees the drop as `Reverted`, pauses the budget, never re-applies it, and fires `CardinalityBudgetPaused` |
| Helm upgrade of the team's chart replaces `metricRelabelings` | Same as self-heal |

A paused budget enforces nothing, so a self-healing sync quietly turns enforcement off for that namespace.

## Recommended: ignore the operator's changes

Let the GitOps tool ignore the fields the operator owns on monitors. The operator only appends at the end of
`metricRelabelings` and never touches the team's own rules, so ignoring is safe.

Argo CD (`Application` or `argocd-cm` resource customization):

```yaml
spec:
  ignoreDifferences:
    - group: monitoring.coreos.com
      kind: ServiceMonitor
      jqPathExpressions:
        - .spec.endpoints[].metricRelabelings[]? | select(.action == "drop" and .sourceLabels == ["__name__"])
        - .metadata.annotations["cardinality.telemetry-compass.com/dropped-metrics"]
        - .metadata.labels["cardinality.telemetry-compass.com/enforced"]
    - group: monitoring.coreos.com
      kind: PodMonitor
      jqPathExpressions:
        - .spec.podMetricsEndpoints[].metricRelabelings[]? | select(.action == "drop" and .sourceLabels == ["__name__"])
        - .metadata.annotations["cardinality.telemetry-compass.com/dropped-metrics"]
        - .metadata.labels["cardinality.telemetry-compass.com/enforced"]
  syncPolicy:
    syncOptions:
      - RespectIgnoreDifferences=true
```

`RespectIgnoreDifferences=true` matters: without it a sync still overwrites the ignored fields. Trade-off: the
expression matches every `__name__` drop rule, so Argo CD also stops syncing drop rules of that form that a
team keeps in Git. Teams that need their own drops can write them with a different form (for example
`action: keep` on the metrics they want), or the ignore can be limited to the affected monitors with `name`.

Flux has no field-level ignore. Either exclude the monitors from self-heal for namespaces in Enforce mode, or
move the drop into Git (below).

## Alternative: move the drop into Git

When a drop should stay for good, make it the team's own rule, in this order:

1. Add the metric to `protectedMetrics`. The operator removes its own drop rule right away (a protected metric
   is restored immediately) and never drops that metric again.
2. Add the team's drop rule to the monitor in Git and sync.

The other order (Git first) makes the operator see its rule removed by the sync, mark the drop `Reverted` and
pause the budget.

## Leaving Enforce mode or deleting a budget

The operator removes only the rules listed in its annotation, newest first. With the ignore in place, nothing
needs to happen in Git.
