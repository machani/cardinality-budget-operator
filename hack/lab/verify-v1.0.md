# Lab verification for v1.0

The lab items of the [readiness review](../../docs/readiness-review.md) checklist. Same kind lab as
[verify-v0.3.md](verify-v0.3.md); all commands run on the Ubuntu box from `/opt/cardinality-budget-operator`.

| Part | Checks | Time |
| --- | --- | --- |
| 1 | Upgrade a v0.3 install with an active drop to v1.0, then migrate storage (F-1001, ADR 0017) | about 50 min |
| 2 | Release dry run and verifying it like a user (F-1002, ADR 0018) | about 20 min |
| 3 | Argo CD and the operator's drop rules, with and without the GitOps guide's `ignoreDifferences`; moving a budget to `v1beta1` through Argo CD | about 45 min |

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
cosign verify ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N --certificate-identity https://github.com/machani/cardinality-budget-operator/.github/workflows/release.yml@refs/heads/dev --certificate-oidc-issuer https://token.actions.githubusercontent.com | jq -r '.[0].critical.image."docker-manifest-digest"'
```

```bash
cosign verify-attestation ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N --type spdxjson --certificate-identity https://github.com/machani/cardinality-budget-operator/.github/workflows/release.yml@refs/heads/dev --certificate-oidc-issuer https://token.actions.githubusercontent.com | jq -r .payload | base64 -d | jq -r '.predicate.packages | length'
```

Pass: both list the three checks and exit 0 (cosign checks the identity itself); the first prints the signed
digest (`sha256:…`), the second a package count above 0. (cosign v3 leaves `.optional.Subject` empty, so don't
read the identity from there.) A wrong identity (for example `refs/heads/main`) must exit 1 with
`no matching CertificateIdentity found` and the expected and actual SAN (cosign v2 says `no matching signatures`).

2d. Both platforms are in the image:

```bash
docker buildx imagetools inspect ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N | grep Platform
```

Expected: `linux/amd64` and `linux/arm64` (plus `unknown/unknown` entries, which are the attestations).

## Part 3: Argo CD

Goal: find out what an Argo CD sync really does to the operator's drop rules, and check the
[GitOps guide](../../docs/guides/gitops.md). Argo CD's default sync is a client-side apply, which compares with
the `last-applied-configuration` annotation. Our rule is not in it, so a sync probably leaves the rule alone
until the team changes something under `endpoints` in Git: then the whole list is replaced. 3b and 3c find out;
3d checks the guide's fix; 3e checks the guide's advice for moving budgets to `v1beta1`.

Argo CD reads [hack/lab/demo](demo/) and [hack/lab/argocd](argocd/) from GitHub, branch
`fix/production-logging`, so push those files before you start. A team's change in Git is simulated with an
inline `kustomize.patches` on the Application, so no extra commits are needed.

Start: the end state of Part 1 (v1.0 deployed, demo budget in Enforce, drops `Effective`, not paused). If the
lab was reset, run Part 1 up to 1h first.

3a. Install Argo CD **core** (no UI, no Dex). Check memory before and after; stop if `available` falls under
about 400 MiB. Note the version for the guide:

```bash
free -m && VER=$(curl -fsSL https://api.github.com/repos/argoproj/argo-cd/releases/latest | jq -r .tag_name) && echo "Argo CD $VER" && kubectl --context kind-cb-dev create namespace argocd && kubectl --context kind-cb-dev apply -n argocd --server-side --force-conflicts -f "https://raw.githubusercontent.com/argoproj/argo-cd/$VER/manifests/core-install.yaml"
```

The ApplicationSet controller is not needed; scale it down, then wait for the rest:

```bash
kubectl --context kind-cb-dev -n argocd scale deployment argocd-applicationset-controller --replicas=0 && kubectl --context kind-cb-dev -n argocd rollout status statefulset argocd-application-controller --timeout=300s && kubectl --context kind-cb-dev -n argocd rollout status deployment argocd-repo-server --timeout=300s && free -m
```

A helper for the checks below (Application state, the monitor's interval and rules, the budget):

```bash
chk() { kubectl --context kind-cb-dev -n argocd get application demo -o jsonpath='app: {.status.sync.status} {.status.health.status} last-sync={.status.operationState.phase}{"\n"}'; kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='monitor: interval={.spec.endpoints[0].interval} rules={.spec.endpoints[0].metricRelabelings[*].regex}{"\n"}'; kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='budget: {.status.phase} {.status.usagePercent}% Enforced={.status.conditions[?(@.type=="Enforced")].reason}{"\n"}{range .status.enforcedDrops[*]}  {.metric} {.state}{"\n"}{end}'; }; chk
```

3b. Argo CD takes over the demo workload, **without** `ignoreDifferences`, self-heal on
([app-demo.yaml](argocd/app-demo.yaml)). Wait 2 minutes, then check:

```bash
kubectl --context kind-cb-dev apply -f hack/lab/argocd/app-demo.yaml
```

```bash
chk
```

Record: is the app `Synced` or `OutOfSync`, and are all drop rules still there with the budget not paused?
Expected (to be confirmed): `Synced Healthy`, rules unchanged, `Enforced=Applied`.

3c. The team changes the scrape interval in Git (simulated: `30s` to `20s`). Wait 2 minutes, then check:

```bash
kubectl --context kind-cb-dev -n argocd patch application demo --type=merge -p '{"spec":{"source":{"kustomize":{"patches":[{"target":{"kind":"ServiceMonitor","name":"avalanche"},"patch":"[{\"op\":\"replace\",\"path\":\"/spec/endpoints/0/interval\",\"value\":\"20s\"}]"}]}}}}'
```

```bash
chk
```

Record what happened. Expected (to be confirmed): interval `20s`, no rules left, every drop `Reverted`,
`Enforced=Paused`; the annotation is still on the monitor (it is not in `last-applied-configuration` either).
This is the case the guide's ignore is for.

3d. Switch to the guide's `ignoreDifferences` ([app-demo-ignore.yaml](argocd/app-demo-ignore.yaml)), then
resume the budget:

```bash
kubectl --context kind-cb-dev apply -f hack/lab/argocd/app-demo-ignore.yaml && kubectl --context kind-cb-dev -n demo annotate cb default --overwrite cardinality.telemetry-compass.com/resume-enforcement=argo1
```

Usage is over 100% again, so the operator adds a drop once the settle period since the last step has passed (up
to 10 minutes). Repeat until `rules=` shows at least one rule:

```bash
chk
```

Then the team changes the interval again (`20s` to `15s`). Wait 2 minutes and check:

```bash
kubectl --context kind-cb-dev -n argocd patch application demo --type=merge -p '{"spec":{"source":{"kustomize":{"patches":[{"target":{"kind":"ServiceMonitor","name":"avalanche"},"patch":"[{\"op\":\"replace\",\"path\":\"/spec/endpoints/0/interval\",\"value\":\"15s\"}]"}]}}}}'
```

```bash
chk
```

Pass: `Synced`, interval `15s`, the rules from before still there, no drop `Reverted`, `Enforced` not
`Paused`. If the rules are gone, the guide's snippet does not protect against this case: keep the lab as it is
and send me the output of `kubectl --context kind-cb-dev -n argocd get application demo -o yaml`.

3e. A budget applied through `v1alpha1` with client-side apply, then moved to `v1beta1` in Git and synced by
Argo CD ([app-budget.yaml](argocd/app-budget.yaml), no automated sync). The deprecation warning on the first
apply is expected:

```bash
kubectl --context kind-cb-dev apply -f hack/lab/argocd/budget-v1alpha1.yaml && kubectl --context kind-cb-dev apply -f hack/lab/argocd/app-budget.yaml
```

A helper for the sync result and the budget's limit (1000 from `v1alpha1`, 2000 once the `v1beta1` sync worked):

```bash
bchk() { kubectl --context kind-cb-dev -n argocd get application budget -o jsonpath='sync: {.status.operationState.phase} {.status.operationState.message}{"\n"}{range .status.operationState.syncResult.resources[*]}  {.kind} {.status} {.message}{"\n"}{end}'; kubectl --context kind-cb-dev -n argo-budget get cb default -o jsonpath='budget: limit={.spec.maxActiveSeries} {.status.phase}{"\n"}'; }
```

Plain sync (client-side apply), wait 30 seconds, check:

```bash
kubectl --context kind-cb-dev -n argocd patch application budget --type=merge -p '{"operation":{"initiatedBy":{"username":"lab"},"sync":{"revision":"fix/production-logging"}}}'
```

```bash
bchk
```

Expected (to be confirmed): the sync fails with `metadata.resourceVersion: Invalid value: 0`, limit still 1000.
Then the guide's fix, a sync with `ServerSideApply=true`, wait 30 seconds, check:

```bash
kubectl --context kind-cb-dev -n argocd patch application budget --type=merge -p '{"spec":{"syncPolicy":{"syncOptions":["ServerSideApply=true"]}},"operation":{"initiatedBy":{"username":"lab"},"sync":{"revision":"fix/production-logging","syncOptions":["ServerSideApply=true"]}}}'
```

```bash
bchk
```

Pass: `Succeeded`, limit 2000, phase `Healthy`.

3f. Remove Argo CD and the test budget. The Applications have no resources finalizer, so deleting them leaves
the demo objects in place; the operator removes the test budget's finalizer when the namespace goes:

```bash
kubectl --context kind-cb-dev -n argocd delete application demo budget && kubectl --context kind-cb-dev delete namespace argo-budget --timeout=120s && kubectl --context kind-cb-dev delete -n argocd -f "https://raw.githubusercontent.com/argoproj/argo-cd/$VER/manifests/core-install.yaml" && kubectl --context kind-cb-dev delete namespace argocd && free -m
```

(`$VER` is from 3a; set it again if the shell was closed.) The demo monitor keeps Argo CD's tracking annotation
and the `15s` interval until R4 of "Stop and reset" replaces it from the manifest.

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
