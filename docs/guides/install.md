# Install guide

For platform teams installing the operator. Versions: [compatibility matrix](../compatibility.md).

## Before you start

| Check | Why |
| --- | --- |
| Kubernetes, Prometheus Operator and Prometheus versions are in the matrix | Only tested versions are supported |
| Prometheus Operator CRDs (ServiceMonitor, PodMonitor) are installed | The operator watches and patches them |
| Prometheus has `enforcedNamespaceLabel: namespace` (or your `--namespace-label`) | Required for Enforce mode: without it a team can attribute series to another namespace ([ADR 0003](../adr/0003-series-attribution.md)). The operator does not check it |
| The Prometheus namespace is labelled `metrics=enabled` | The NetworkPolicy lets only such namespaces scrape the operator |
| You know the in-cluster Prometheus URL, for example `http://prometheus-operated.monitoring.svc:9090` | `--prometheus-url` (no credentials in the URL) |

Verify the image before installing it ([releasing](../releasing.md#verify-a-release)).

## Helm (recommended)

Write a values file. The chart defaults to one replica; production runs two, with leader election.

```yaml
# values-prod.yaml
manager:
  replicas: 2
  args:
    - --leader-elect
    - --prometheus-url=http://prometheus-operated.monitoring.svc:9090
```

```bash
helm install cardinality-budget-operator oci://ghcr.io/machani/charts/cardinality-budget-operator --version X.Y.Z -n cardinality-budget-operator-system --create-namespace -f values-prod.yaml
```

The chart contains the CRD (kept on uninstall), RBAC, the Deployment, a PodDisruptionBudget, the NetworkPolicy,
the operator's ServiceMonitor, alert rules and the Grafana dashboard (a ConfigMap for the Grafana sidecar).
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

Two pods are Running and one holds the leader lease. Then create a first budget in Observe mode
([user guide](user.md)) and wait one `--measure-interval` (5m): its phase becomes `Healthy`, `Warning` or
`Exceeded`, not `Unknown`. `Unknown` means the measurement is not trusted yet; the `Ready` condition says why
([runbooks](../runbooks/README.md#cardinalitybudgetmeasurementinvalid)).

## Upgrade

Read the [CHANGELOG](../../CHANGELOG.md) first. Helm upgrades the CRD too, because it is a chart template, not
in `crds/`:

```bash
helm upgrade cardinality-budget-operator oci://ghcr.io/machani/charts/cardinality-budget-operator --version X.Y.Z -n cardinality-budget-operator-system -f values-prod.yaml
```

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
