# Runbooks

One section per alert in `config/prometheus/alerts.yaml`, plus procedures the operator's design depends on.
Commands assume the operator runs in `cardinality-budget-operator-system`; replace `<ns>` with the budget's
namespace. Read the budget first in every case:

```bash
kubectl -n <ns> describe cardinalitybudget default
```

Useful anywhere:

```bash
# Every monitor the operator has changed, cluster-wide (ADR 0007)
kubectl get servicemonitors,podmonitors -A -l cardinality.telemetry-compass.com/enforced=true
# What it dropped on one monitor
kubectl -n <ns> get servicemonitor <name> -o jsonpath='{.metadata.annotations.cardinality\.telemetry-compass\.com/dropped-metrics}'
```

## CardinalityBudgetOperatorDown

No replica is scraped. Budgets keep their last status; nothing is dropped or restored, which is safe.

1. `kubectl -n cardinality-budget-operator-system get pods,events`
2. Crash at start: `kubectl -n cardinality-budget-operator-system logs deploy/cardinality-budget-operator-controller-manager`.
   "invalid operator configuration" lists every bad flag; fix the Deployment args.
3. Running but not scraped: the Prometheus namespace needs the label `metrics=enabled` (NetworkPolicy), and
   Prometheus's service account needs the `cardinality-budget-operator-metrics-reader` ClusterRole.

## CardinalityBudgetReconcileErrors

Unexpected API errors (backend outages are not errors; they show as `Unknown`).

1. Logs: `kubectl -n cardinality-budget-operator-system logs deploy/cardinality-budget-operator-controller-manager | grep -i error`
2. `forbidden`: RBAC drifted from the release; reinstall the chart or `config/rbac`.
3. Webhook or API server problems: check `kubectl get --raw /readyz?verbose`.

## CardinalityBudgetMeasurementInvalid

The budget is `Unknown`. `Ready` says why: `BackendUnreachable`, `BackendWarmingUp`, `PartialData`, `Stale` or
`InvalidSpec`. Nothing is added or removed until it clears (ADR 0005).

- `BackendWarmingUp` right after a Prometheus restart: wait `--backend-warmup` (15m).
- `BackendUnreachable`: check Prometheus pods, the `--prometheus-url`, the HTTP config Secret, and NetworkPolicies.
- `PartialData`: the query returned warnings (Thanos, HA proxies). Point `--prometheus-url` at one replica.
- `InvalidSpec`: fix `protectedMetrics` or `monitorSelector`; the message names the bad value.

## CardinalityBudgetPaused

The `Enforced` reason tells why. A paused budget adds no drops; restores still run (ADR 0008).

- `Reverted`: someone removed an owned drop rule, often a GitOps tool. Fix the source (ignore the
  `metricRelabelings` drift in Argo CD / Flux, or add the drop to Git), then resume.
- `Ineffective`: the drop left more than 10% of the metric's series; they come from outside the selected
  monitors (Pushgateway, ScrapeConfig, other monitors). Narrow `monitorSelector` or protect the metric, then resume.
- `ConfigReloadFailed`: see below.

Resume means "continue": Reverted entries are forgotten, and a budget still over its limit may drop the same
metric again one settle period later. If the rule was removed on purpose (the team needs that metric), add it
to `protectedMetrics` first, then resume (ADR 0016).

Resume (any new value works once):

```bash
kubectl -n <ns> annotate cardinalitybudget default --overwrite cardinality.telemetry-compass.com/resume-enforcement="$(date -u +%FT%TZ)"
```

## CardinalityBudgetConfigReloadFailed

Prometheus did not load its configuration within 5 minutes of a change. It may be running an old
configuration for every team.

1. `kubectl -n <prometheus-ns> logs <prometheus-pod> -c config-reloader` and the `prometheus` container.
2. `curl <prometheus>/api/v1/status/runtimeinfo`: `reloadConfigSuccess` and `lastConfigTime`.
3. If a drop rule caused it, turn the kill switch on, remove the rule by hand (below), fix, then resume.

## CardinalityBudgetKillSwitchActive

`--enforcement-paused=true` blocks every monitor write, including restores and deletion cleanup.

- Turn it off: set `--enforcement-paused=false` on the Deployment (Helm: `enforcementPaused: false`) and roll out.
- While it is on, budgets being deleted wait. To delete one anyway, remove its rules by hand (below), then
  remove the finalizer.

## CardinalityBudgetRateLimited

`--max-drops-per-hour` (default 10) is reached across all budgets: new drops wait until the hour moves on.
Several namespaces exploding at once usually means a shared library or exporter change. Find it with the
dashboard's usage panel before raising the limit.

## Procedures

### Remove the operator's rules by hand

For each monitor listed by the `enforced=true` label: remove the `metricRelabelings` entries
`{action: drop, sourceLabels: [__name__], regex: <escaped name>}` for the metrics in the
`dropped-metrics` annotation (one occurrence per endpoint, the last one), then delete the annotation and label.
Never remove other rules.

### Budget stuck deleting

A budget being deleted keeps the finalizer `cardinality.telemetry-compass.com/cleanup` until its rules are
removed. If the operator is gone or the kill switch is on, remove the rules by hand, then:

```bash
kubectl -n <ns> patch cardinalitybudget default --type=json -p '[{"op":"remove","path":"/metadata/finalizers"}]'
```

### Uninstall order

1. Delete every budget and wait until they are gone (the operator removes its rules).
2. Then uninstall the operator (chart or `make undeploy`).
3. Only then delete the CRD. Removing the operator first leaves budgets stuck deleting (see above).

### Upgrade to v1.0: move budgets to v1beta1

v1.0 serves `v1beta1` (stored) and the deprecated `v1alpha1` with the same schema, so nothing breaks on upgrade
([ADR 0017](../adr/0017-api-v1beta1.md)). Requests that use `v1alpha1` get a deprecation warning.

1. Upgrade the CRD and the operator (Helm upgrades the CRD from `templates/crd`; Kustomize with `make deploy`).
2. Re-save the budgets in the new storage version and record it on the CRD:

   ```bash
   hack/migrate-storage.sh --context <your-context>
   ```

   It ends with `storedVersions after: ["v1beta1"]`. Safe to run again; required before v2.0, which removes
   `v1alpha1`.
3. Change `apiVersion` in your manifests (GitOps repositories) to `cardinality.telemetry-compass.com/v1beta1`.
