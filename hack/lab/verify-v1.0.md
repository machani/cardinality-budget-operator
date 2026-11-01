# Lab verification for v1.0

The lab items of the [readiness review](../../docs/readiness-review.md) checklist. Same kind lab as
[verify-v0.3.md](verify-v0.3.md); all commands run on the Ubuntu box from `/opt/cardinality-budget-operator`.

| Part | Checks | Time |
| --- | --- | --- |
| 1 | Upgrade a v0.3 install with an active drop to v1.0, then migrate storage (F-1001, ADR 0017) | about 50 min |
| 2 | Release dry run and verifying it like a user (F-1002, ADR 0018) | about 20 min |
| 3 | Argo CD `ignoreDifferences` from the GitOps guide | later, written after Part 1 passes |

Before you start: [prerequisites](verify-v0.3.md#prerequisites-once-per-box) and
[Part 0](verify-v0.3.md#part-0-prepare) of the v0.3 doc, on an up-to-date `dev` that contains v1.0. Part 0
ends with no operator, no budget, no CRD and a clean demo monitor.

## Part 1: upgrade and storage migration

Goal: a budget created through `v1alpha1` by v0.3, with a drop in place, keeps its spec, status and drop when
v1.0 is installed; the migration script moves it to `v1beta1` storage without changing anything else.

1a. Build both images first. The v0.3 build is `dev` before the v1.0 merge (`1e0a2ef`), in a worktree outside
`/tmp`. Two builds need more memory than the 3.8 GiB box has next to a running lab, and the kernel kills a kind
node (`docker ps -a` shows `Exited (137)`). So pause the lab, build, then start it and load the images:

```bash
docker stop cb-dev-worker cb-dev-control-plane
```

```bash
git worktree add /opt/cbo-v0.3 1e0a2ef && cd /opt/cbo-v0.3 && make docker-build IMG=cardinality-budget-operator:v0.3 && cd /opt/cardinality-budget-operator && make docker-build IMG=cardinality-budget-operator:v1.0
```

```bash
docker start cb-dev-control-plane cb-dev-worker && kubectl --context kind-cb-dev wait node --all --for=condition=Ready --timeout=300s && kind load docker-image cardinality-budget-operator:v0.3 cardinality-budget-operator:v1.0 --name cb-dev
```

Prometheus restarted with the nodes: wait out its 15-minute warm-up before 1d
([Part 0](verify-v0.3.md#part-0-prepare) shows how to check). 1b and 1c can run meanwhile.

1b. Deploy v0.3 and create the budget **from the worktree**: its CRD and `budget.yaml` know only `v1alpha1`,
like a real v0.3 user:

```bash
cd /opt/cbo-v0.3 && make deploy IMG=cardinality-budget-operator:v0.3 && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager && kubectl --context kind-cb-dev apply -f hack/lab/demo/budget.yaml && cd /opt/cardinality-budget-operator
```

1c. Check the starting point. Expected: one version `v1alpha1 served=true storage=true`, and
`storedVersions` `["v1alpha1"]`:

```bash
kubectl --context kind-cb-dev get crd cardinalitybudgets.cardinality.telemetry-compass.com -o jsonpath='{range .spec.versions[*]}{.name} served={.served} storage={.storage} deprecated={.deprecated}{"\n"}{end}storedVersions={.status.storedVersions}{"\n"}'
```

1d. Switch to Enforce and wait (up to about 12 min: v0.3 measures every 5m, plus the 1m grace period) until
`enforcedDrops` has one entry:

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"mode":"Enforce","enforcement":{"gracePeriod":"1m"}}}'
```

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.phase}{"\n"}{.status.enforcedDrops}{"\n"}'
```

1e. Save the state, only once 1d shows a drop:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o json | jq -S '{spec, drops: .status.enforcedDrops, resume: .status.lastResumeToken}' > /root/v03-budget.json && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings}{"\n"}{.metadata.annotations.cardinality\.telemetry-compass\.com/dropped-metrics}{"\n"}' > /root/v03-sm.txt && cat /root/v03-budget.json /root/v03-sm.txt
```

1f. Upgrade to v1.0. `make deploy` from the main checkout applies the new CRD (two versions) and the new
image:

```bash
make deploy IMG=cardinality-budget-operator:v1.0 && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager
```

1g. Run the command from 1c again. Expected:

```text
v1alpha1 served=true storage=false deprecated=true
v1beta1 served=true storage=true deprecated=
storedVersions=["v1alpha1"]
```

`storedVersions` still says only `v1alpha1`: the object in etcd has not been rewritten yet. (It may show
`["v1alpha1","v1beta1"]` if the operator already wrote the budget; both are fine here.)

1h. Read the budget through both versions. The `v1alpha1` read prints a deprecation warning, the `v1beta1` read
does not, and both show the same phase and drops:

```bash
kubectl --context kind-cb-dev -n demo get cardinalitybudgets.v1alpha1.cardinality.telemetry-compass.com default -o jsonpath='{.apiVersion} {.status.phase} {.status.enforcedDrops[*].metric}{"\n"}'; kubectl --context kind-cb-dev -n demo get cardinalitybudgets.v1beta1.cardinality.telemetry-compass.com default -o jsonpath='{.apiVersion} {.status.phase} {.status.enforcedDrops[*].metric}{"\n"}'
```

1i. Wait 10 minutes (one settle period of the new build), then compare with 1e:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o json | jq -S '{spec, drops: .status.enforcedDrops, resume: .status.lastResumeToken}' > /root/v10-budget.json; diff /root/v03-budget.json /root/v10-budget.json; kubectl --context kind-cb-dev -n demo describe cb default | tail -12
```

Pass:

- `spec` and `resume` unchanged; the v0.3 drop keeps its `metric`, `since` and `seriesAtDrop`; only its `state`
  may change (`Pending` → `Effective`)
- the new build may have added a second drop (usage is still over 900): only the first entry must be unchanged
- no `Reverted` Event and no `Ready=False` after the upgrade

1j. Migrate. Expected: `storedVersions before: ["v1alpha1"]` (or with `v1beta1`), `migrated demo/default`,
`storedVersions after: ["v1beta1"]`:

```bash
hack/migrate-storage.sh --context kind-cb-dev
```

1k. Run it a second time: same output, nothing fails (it is safe to repeat). Then confirm nothing changed:
the diff against 1i prints nothing except, at most, a drop state that moved on in the meantime, and the
operator logged no error since the upgrade:

```bash
hack/migrate-storage.sh --context kind-cb-dev && kubectl --context kind-cb-dev -n demo get cb default -o json | jq -S '{spec, drops: .status.enforcedDrops, resume: .status.lastResumeToken}' | diff /root/v10-budget.json - ; leader=$(kubectl --context kind-cb-dev -n cardinality-budget-operator-system get lease c4f5a295.telemetry-compass.com -o jsonpath='{.spec.holderIdentity}' | cut -d_ -f1); echo "leader: $leader"; kubectl --context kind-cb-dev -n cardinality-budget-operator-system logs "$leader" --since=40m | grep -cE '"level":"error"|[[:space:]]ERROR[[:space:]]'
```

The last number should be `0`. The count reads the **leader's** log: `kubectl logs deployment/...` picks one of the
two pods at random, and the standby logs nothing.

1l. New manifests use `v1beta1`. The repository's `budget.yaml` already does. A plain `kubectl apply` fails with
`metadata.resourceVersion: Invalid value: 0: must be specified for an update`, because the budget's
`last-applied-configuration` annotation (from 1b) still holds `v1alpha1` and the patch would change the
version. Expected: that error, then two `configured` lines after updating the annotation, and the budget still
`Enforcing` with its drops (runbook "Upgrade to v1.0", step 3):

```bash
kubectl --context kind-cb-dev apply -f hack/lab/demo/budget.yaml
```

```bash
kubectl --context kind-cb-dev apply set-last-applied -f hack/lab/demo/budget.yaml && kubectl --context kind-cb-dev apply -f hack/lab/demo/budget.yaml && kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.phase} {.status.enforcedDrops[*].metric}{"
"}'
```

Leave the lab as it is for Part 2 or go to "Stop and reset".

## Part 2: release dry run

Only after the v1.0 pull request is merged into `dev` (the Run button appears only for workflows on the
default branch).

2a. GitHub → Actions → Release → Run workflow → branch `dev`. Expected: every step green except the skipped
chart push and GitHub release. The "Verify the signature and the SBOM attestation" step prints
`Signature and SBOM attestation verified`. Note the version from the "Work out the version" step
(`0.0.0-dryrun.N`) and download the `dry-run-…` artifact (SBOM, `install.yaml`, chart).

2b. Make the package public (GitHub → your profile → Packages → `cardinality-budget-operator` → Package
settings → Change visibility → Public), so it can be pulled and verified without logging in, like a user.

2c. Verify as a user on the Ubuntu box. Needs cosign (`cosign version`; the install is a single binary from the
sigstore releases page). Replace `N`; the identity ends in `refs/heads/dev` for a dry run:

```bash
cosign verify ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N --certificate-identity https://github.com/machani/cardinality-budget-operator/.github/workflows/release.yml@refs/heads/dev --certificate-oidc-issuer https://token.actions.githubusercontent.com | jq -r '.[0].optional.Subject'
```

```bash
cosign verify-attestation ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N --type spdxjson --certificate-identity https://github.com/machani/cardinality-budget-operator/.github/workflows/release.yml@refs/heads/dev --certificate-oidc-issuer https://token.actions.githubusercontent.com | jq -r .payload | base64 -d | jq -r '.predicate.packages | length'
```

Pass: the first prints the workflow identity, the second a package count above 0. A wrong identity (for example
`refs/heads/main`) must fail with `no matching signatures`.

2d. Both platforms are in the image:

```bash
docker buildx imagetools inspect ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N | grep Platform
```

Expected: `linux/amd64` and `linux/arm64` (plus `unknown/unknown` entries, which are the attestations).

## Stop and reset

[Stop and reset](verify-v0.3.md#stop-and-reset) from the v0.3 doc (R1 to R4), but in R3 remove this worktree:

```bash
git worktree remove --force /opt/cbo-v0.3; git worktree prune
```

Delete old dry-run versions on the package page when you no longer need them.

## When a step fails

Stop, keep the lab as it is, and save what you see before changing anything:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o yaml > /root/fail-budget.yaml; kubectl --context kind-cb-dev get crd cardinalitybudgets.cardinality.telemetry-compass.com -o yaml > /root/fail-crd.yaml; for p in $(kubectl --context kind-cb-dev -n cardinality-budget-operator-system get pods -o name); do kubectl --context kind-cb-dev -n cardinality-budget-operator-system logs "$p" --since=1h > "/root/fail-$(basename "$p").log"; done
```
