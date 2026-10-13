# Upgrade test: v0.2 to the current build (F-0307)

Goal: active drops survive an operator upgrade unchanged. The new version must adopt the existing state
from status and monitor annotations without adding, removing or re-applying anything.

Run on the lab only (`kind-cb-dev`), with the demo from `hack/lab/demo/` applied and the port-forward to
Prometheus running. `KUBECONFIG=/root/.kube/kind-cb-dev.yaml` is set for every `make` call.

## 1. Deploy v0.2

```bash
git worktree add /tmp/v0.2 819197e && cd /tmp/v0.2 && make docker-build IMG=cardinality-budget-operator:v0.2 && kind load docker-image cardinality-budget-operator:v0.2 --name cb-dev && KUBECONFIG=/root/.kube/kind-cb-dev.yaml make deploy IMG=cardinality-budget-operator:v0.2 && cd -
```

## 2. Create an active drop

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"mode":"Enforce","maxActiveSeries":900,"enforcement":{"gracePeriod":"1m"}}}'
```

Wait until `kubectl --context kind-cb-dev -n demo get cb` shows `Enforcing`, then record the state:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.enforcedDrops}{"\n"}' > /tmp/before-drops.json && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o yaml > /tmp/before-sm.yaml
```

## 3. Upgrade to the current build

```bash
make docker-build IMG=cardinality-budget-operator:dev && kind load docker-image cardinality-budget-operator:dev --name cb-dev && KUBECONFIG=/root/.kube/kind-cb-dev.yaml make deploy IMG=cardinality-budget-operator:dev
```

## 4. Check nothing changed

Wait two measure intervals, then:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.enforcedDrops}{"\n"}' | diff - /tmp/before-drops.json && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings}{"\n"}{.metadata.annotations}{"\n"}'
```

Done when: `enforcedDrops` is identical except for `state` moving from `Pending` to `Effective`, the monitor
still has exactly one drop rule per dropped metric, and `kubectl -n demo describe cb default` shows no
`Dropped`, `Restored` or `Reverted` Event after the upgrade.

## 5. Clean up

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"mode":"Warn"}}' && git worktree remove /tmp/v0.2
```
