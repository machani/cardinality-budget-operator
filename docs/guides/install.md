# Install guide

For platform teams installing the operator. Versions: [compatibility matrix](../compatibility.md).

## Before you start

| Check | Why |
| --- | --- |
| Kubernetes, Prometheus Operator and Prometheus versions are in the matrix | Only tested versions are supported |
| Prometheus Operator CRDs (ServiceMonitor, PodMonitor) are installed | The operator watches and patches them |
| Prometheus has `enforcedNamespaceLabel: namespace` (or your `--namespace-label`) | Required for Enforce mode: without it a team can attribute series to another namespace ([ADR 0003](../adr/0003-series-attribution.md)). The operator does not check it |
| You know the in-cluster Prometheus URL, for example `http://prometheus-operated.monitoring.svc:9090` | `--prometheus-url` (no credentials in the URL) |
| You know which Prometheus will scrape the operator, and the labels its selectors need | For the operator's own metrics, alerts and dashboard ([below](#prometheus-roles)) |

Verify the image before installing it ([releasing](../releasing.md#verify-a-release)).

## Prometheus roles

The operator deals with Prometheus in two separate ways. In a cluster with one Prometheus, both are the same
instance; with several (one per tenant, plus a platform one), they are often different.

| Role | Which Prometheus | What it needs |
| --- | --- | --- |
| **Measured** | The one in `--prometheus-url`. The operator queries it for every namespace's series count | Prometheus 3.x; `enforcedNamespaceLabel` for Enforce. Nothing else: the operator calls it, so no label or NetworkPolicy change |
| **Scraping the operator** | The one that collects the operator's own metrics and evaluates its 7 alerts | Its `serviceMonitorSelector` and `ruleSelector` match the operator's ServiceMonitor and PrometheusRule; its namespace is labelled `metrics=enabled`; its service account may read `/metrics` |

One operator measures one Prometheus. A namespace that only another Prometheus scrapes shows close to 0
series, so give each budget's namespace to the measured Prometheus, or run one operator per Prometheus (not
supported in v1).

For the scraping Prometheus:

1. Selector labels. Find what it selects:

   ```bash
   kubectl get prometheus -A -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name,MONITORS:.spec.serviceMonitorSelector,RULES:.spec.ruleSelector
   ```

   Then set those labels in the chart's `prometheus.labels`; both the ServiceMonitor and the PrometheusRule get
   them (the PrometheusRule since 1.0.1). kube-prometheus-stack selects `release: <its Helm release name>`. With
   Kustomize, add the labels with a `labels` or patch entry in your overlay.
2. NetworkPolicy. Label **its** namespace (not the measured one), otherwise the metrics port refuses it:

   ```bash
   kubectl label namespace <scraping-prometheus-namespace> metrics=enabled
   ```

3. Access. Bind its service account to the `cardinality-budget-operator-metrics-reader` ClusterRole (the
   kube-prometheus-stack Prometheus can already read `/metrics` through its own ClusterRole).
4. Dashboard. The chart ships a ConfigMap labelled `grafana_dashboard: "1"`. The Grafana sidecar loads it only if
   it watches the operator's namespace (kube-prometheus-stack: `grafana.sidecar.dashboards.searchNamespace: ALL`).
   Otherwise import `config/grafana/dashboard.json` by hand and pick the scraping Prometheus as data source.

## Helm (recommended)

Write a values file. The chart defaults to one replica; production runs two, with leader election.

```yaml
# values-prod.yaml
manager:
  replicas: 2
  args:
    - --leader-elect
    - --prometheus-url=http://prometheus-operated.monitoring.svc:9090
# Labels the scraping Prometheus selects (ServiceMonitor and PrometheusRule).
prometheus:
  labels:
    release: kube-prometheus-stack
```

```bash
helm install cardinality-budget-operator oci://ghcr.io/machani/charts/cardinality-budget-operator --version X.Y.Z -n cardinality-budget-operator-system --create-namespace -f values-prod.yaml
```

The chart contains the CRD (kept on uninstall), RBAC, the Deployment, a PodDisruptionBudget, two
NetworkPolicies (metrics port from `metrics=enabled` namespaces, health probes from anywhere), the operator's
ServiceMonitor and alert rules (both only with `prometheus.enabled`, the default) and the Grafana dashboard (a
ConfigMap for the Grafana sidecar).
More flags go into `manager.args` ([admin guide](admin.md#operator-flags)).

Not available in the chart yet: mounting a Prometheus HTTP client config (auth, TLS). If your Prometheus needs
authentication, install with Kustomize.

## Kustomize

From a checkout of the release tag. `config/default` includes two replicas, the PodDisruptionBudget, the
NetworkPolicy, alerts and dashboard.

```bash
make deploy IMG=ghcr.io/machani/cardinality-budget-operator:X.Y.Z
```

Or apply the release's `install.yaml`, whose image is pinned by digest:

```bash
kubectl apply -f https://github.com/machani/cardinality-budget-operator/releases/download/vX.Y.Z/install.yaml
```

Set `--prometheus-url` in `config/manager/manager.yaml` (or a patch) before building. For a Prometheus that
needs authentication or TLS, enable `config/manager/prometheus-http-config-patch.yaml`: it mounts a Secret with
a Prometheus-format `http-config.yaml` that refers to credential files (`credentials_file`, `password_file`),
never inline secrets.

## Check the install

```bash
kubectl -n cardinality-budget-operator-system get deploy,pods,pdb
```

Two pods are Running and one holds the leader lease. In the scraping Prometheus, this returns 2 (one per
replica); empty means it does not scrape the operator yet ([Prometheus roles](#prometheus-roles)):

```promql
count(cardinalitybudget_enforcement_paused)
```

Its alert list (`/alerts`) shows the `cardinality-budget-operator` group with 7 rules. Then create a first budget in Observe mode
([user guide](user.md)) and wait one `--measure-interval` (5m): its phase becomes `Healthy`, `Warning` or
`Exceeded`, not `Unknown`. `Unknown` means the measurement is not trusted yet; the `Ready` condition says why
([runbooks](../runbooks/README.md#cardinalitybudgetmeasurementinvalid)).

## Upgrade

Read the [CHANGELOG](../../CHANGELOG.md) first. Helm upgrades the CRD too, because it is a chart template, not
in `crds/`:

```bash
helm upgrade cardinality-budget-operator oci://ghcr.io/machani/charts/cardinality-budget-operator --version X.Y.Z -n cardinality-budget-operator-system -f values-prod.yaml
```

From 1.0.0 to 1.0.1: the metrics port really is limited to `metrics=enabled` namespaces now (1.0.0's chart
left it open), so label the scraping Prometheus's namespace before upgrading. A PrometheusRule label you added
by hand can go into `prometheus.labels` instead.

State lives in budget status and monitor annotations, so an upgrade continues where it stopped. Coming from a
v0.x lab install: run the storage migration ([runbook](../runbooks/README.md#upgrade-to-v10-move-budgets-to-v1beta1)).

## Uninstall

Delete every budget first and wait until they are gone: the operator removes its drop rules while a budget is
deleted. Then remove the operator, and the CRD last.

```bash
kubectl delete cardinalitybudgets --all -A --wait
```

```bash
helm uninstall cardinality-budget-operator -n cardinality-budget-operator-system
```

Details and recovery: [runbooks, uninstall order](../runbooks/README.md#uninstall-order).
