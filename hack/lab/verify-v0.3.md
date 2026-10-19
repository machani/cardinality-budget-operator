# Lab verification for v0.2 and v0.3

One session (about 2 hours) on the lab cluster that closes the features CI cannot check:

| Part | Checks |
| --- | --- |
| 1 | F-0307 upgrade from v0.2 with an active drop |
| 2 | F-0302 two replicas after the switch to short timings |
| 3 | F-0213 Enforce lifecycle, F-0302 failover, F-0211 drift and resume, F-0203 kill switch, F-0209 restore, F-0305 alerts `Paused` and `KillSwitchActive` |
| 4 | F-0305 alerts `MeasurementInvalid` and `OperatorDown`; ADR 0005 no writes on invalid measurement |
| 5 | F-0204 deletion cleanup |
| 6 | Phase 1 fact 3: namespace-label spoofing (ADR 0003) |

Rules: only `--context kind-cb-dev`; every `make` call with `KUBECONFIG=/root/.kube/kind-cb-dev.yaml` (exported
in Part 0). Stop any `go run` operator first, or two operators fight over the same monitor. If a step fails,
stop there and collect the operator logs (end of this file). To stop halfway, see "Stop and reset".

Expected noise, not a failure:

- `ERROR leaderelection Failed to update lease ... the object has been modified` right after a start: the two
  replicas race for the lease; one wins, the other retries.
- Both pods on `cb-dev-worker`: kind's control-plane node is tainted, and anti-affinity is only preferred.

Run state: Parts 1, 2, 3a-3d passed on 2026-10-15 (the resume found the bugs fixed in PR #19 and #20). The
remaining checks run as the "Final run" below, on a build that includes PR #20. Its steps repeat drift and
resume once to confirm the fix in the cluster.

## Final run (remaining checks, about 2 hours)

Every step is one command block; run them in order. "Pass" says what must hold before the next step.

F0. Start the lab and get the code. If `git status` shows `config/manager/kustomization.yaml` as modified,
that is the image tag `make deploy` writes; reset it:

```bash
cd /opt/cardinality-budget-operator && export KUBECONFIG=/root/.kube/kind-cb-dev.yaml && git checkout -- config/manager/kustomization.yaml && git checkout dev && git pull && git log --oneline -1 && df -h /
```

Pass: the top commit is the merge of PR #20 or later; at least 10 GB free.

```bash
docker start cb-dev-control-plane cb-dev-worker && kubectl --context kind-cb-dev wait node --all --for=condition=Ready --timeout=300s && kubectl --context kind-cb-dev label namespace monitoring metrics=enabled --overwrite
```

Second terminal (keep open):

```bash
kubectl --context kind-cb-dev -n monitoring port-forward svc/kps-prometheus 9090:9090
```

Prometheus restarts with kind and needs 15 minutes of warm-up before measurements are valid. Note its start
time; F1 and F2 fit into the wait:

```bash
curl -s localhost:9090/api/v1/status/runtimeinfo | jq -r .data.startTime; date -u
```

F1. Build and deploy the fixed operator, then switch to short timings (1m measure, 3m settle):

```bash
make docker-build IMG=cardinality-budget-operator:dev && kind load docker-image cardinality-budget-operator:dev --name cb-dev && make deploy IMG=cardinality-budget-operator:dev
```

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system patch deployment cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--measure-interval=1m"},{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enforcement-settle-period=3m"}]' && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager && kubectl --context kind-cb-dev -n cardinality-budget-operator-system get pods
```

Pass: two pods Running.

F2. Demo workload and budget (limit 900, Enforce, grace 1m):

```bash
kubectl --context kind-cb-dev apply -f hack/lab/demo/avalanche.yaml -f hack/lab/demo/budget.yaml && kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"mode":"Enforce","enforcement":{"gracePeriod":"1m"}}}'
```

Third terminal (keep open):

```bash
kubectl --context kind-cb-dev -n demo get events --field-selector involvedObject.name=default -w
```

Status at any time:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.spec.maxActiveSeries} {.status.usagePercent}% {.status.phase} last={.status.lastEnforcementTime}{"\n"}{range .status.conditions[*]}{.type}={.reason} {end}{"\n"}{range .status.enforcedDrops[*]}{.metric} {.since} {.state}{"\n"}{end}'
```

F3. Wait for drops. While Prometheus warms up the budget is `Unknown/BackendWarmingUp`. About 2 minutes
after the warm-up ends, the first `Dropped` Event arrives, then one every 3 minutes: 1006 → 956 → 906 → 856,
3 drops in total.

Pass: 3 drops 3 minutes apart, then the phase stays `Enforcing` under 900 (95%).

F4. Drift and resume on the fixed build (F-0211, ADR 0016). Remove every rule by hand:

```bash
kubectl --context kind-cb-dev -n demo patch servicemonitor avalanche --type=json -p '[{"op":"remove","path":"/spec/endpoints/0/metricRelabelings"}]'
```

Pass: within a minute **one** `Reverted` Event naming all 3 metrics; all entries `Reverted`; nothing
re-applied; the series come back (usage 111%). Note `last=` from the status command, then resume:

```bash
kubectl --context kind-cb-dev -n demo annotate cb default --overwrite cardinality.telemetry-compass.com/resume-enforcement=final1
```

Pass:

- one `Resumed` and one `Restored` Event (naming all 3)
- the cleanup does not move `last=` (it changed only the annotation, so it is not a step). If the last drop
  has already settled, the next drop may follow in the same run; `last=` then moves to that drop's `since`
- the next `Dropped` arrives by itself, with no reload trick, then one every 3 minutes

F5. Kill switch (F-0203), alert `CardinalityBudgetKillSwitchActive`. Wait until F4 has dropped at least one
metric again, then:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system patch deployment cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enforcement-paused"}]'
```

After about 7 minutes:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.conditions[?(@.type=="Enforced")].reason}{"\n"}' && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings[*].regex}{"\n"}' && curl -s localhost:9090/api/v1/alerts | jq -r '.data.alerts[] | "\(.labels.alertname) \(.state)"'
```

Pass: `Paused`; the same rules as before the switch; `CardinalityBudgetKillSwitchActive firing`. Turn it
off (back to the F1 short timings) and wait for the rollout:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout undo deployment cardinality-budget-operator-controller-manager && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager
```

Pass: `Enforced` back to `Applied`; drops continue while over budget.

F6. Restore (F-0209). Raise the limit so usage is below the 90% warn threshold:

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"maxActiveSeries":1300}}'
```

Pass: one `Restored` Event about every 3 minutes, newest first, until no drops are left and the phase is
`Healthy`. Then only `check done` is printed:

```bash
kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o yaml | grep -E "cardinality|metricRelabelings"; echo "check done"
```

F7. Alert `CardinalityBudgetMeasurementInvalid` and no writes on an invalid measurement (ADR 0005). Lower
the limit and wait for one `Dropped` Event:

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"maxActiveSeries":900}}'
```

Point the operator at a Prometheus that does not exist (the last `--prometheus-url` wins):

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system patch deployment cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--prometheus-url=http://nowhere.monitoring.svc:9090"}]'
```

After about 17 minutes:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.phase} {.status.conditions[?(@.type=="Ready")].reason}{"\n"}' && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings[*].regex}{"\n"}' && curl -s localhost:9090/api/v1/alerts | jq -r '.data.alerts[] | "\(.labels.alertname) \(.state)"'
```

Pass: `Unknown BackendUnreachable`; the drop rule is still there; `CardinalityBudgetMeasurementInvalid
firing`. Undo:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout undo deployment cardinality-budget-operator-controller-manager && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager
```

F8. Alert `CardinalityBudgetOperatorDown`:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system scale deployment cardinality-budget-operator-controller-manager --replicas=0
```

After about 12 minutes, the alerts `curl` from F7 shows `CardinalityBudgetOperatorDown firing`. Start it again
and check all 7 rules are loaded:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system scale deployment cardinality-budget-operator-controller-manager --replicas=2 && curl -s localhost:9090/api/v1/rules | jq -r '.data.groups[] | select(.name=="cardinality-budget-operator") | .rules[].name'
```

F9. Deletion cleanup (F-0204). With at least one drop active:

```bash
kubectl --context kind-cb-dev -n demo delete cb default && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o yaml | grep -E "cardinality|metricRelabelings"; echo "check done"
```

Pass: the delete finishes within seconds; only `check done` is printed.

F10. Fact 3, namespace-label spoofing (ADR 0003):

```bash
kubectl --context kind-cb-dev apply -f hack/lab/fact3/spoof.yaml
```

After 2 minutes:

```bash
for q in spoof_metric 'namespace:spoof_metric:sum'; do curl -s localhost:9090/api/v1/query --data-urlencode "query=$q" | jq -c '.data.result[].metric'; done
```

Pass: every line shows `"namespace":"demo"` and none `"victim"`. Remove it:

```bash
kubectl --context kind-cb-dev delete -f hack/lab/fact3/spoof.yaml
```

F11. Reset: run "Stop and reset" at the end of this file.

## Prerequisites (once per box)

- Docker with BuildKit. The legacy builder ignores the `!**/*.go` re-includes in `.dockerignore` and fails
  with `package cmd/main.go is not in std`. Check with `docker buildx version`; install `docker-buildx`
  (Ubuntu `docker.io`) or `docker-buildx-plugin` (Docker CE) if missing.
- At least 10 GB free on `/`. Each image build adds 1-2 GB of build cache, and a full disk breaks the build
  and puts etcd and Prometheus in kind at risk.

```bash
docker buildx version && df -h / && docker system df
```

If space is short, remove caches that rebuild themselves (never `docker system prune` or `--volumes`, which can
delete the kind nodes' data):

```bash
docker builder prune -af && go clean -cache && journalctl --vacuum-size=200M
```

## Part 0: prepare

```bash
cd /opt/cardinality-budget-operator && git checkout dev && git pull && export KUBECONFIG=/root/.kube/kind-cb-dev.yaml
```

If the kind containers were stopped, start them and wait until both nodes are Ready:

```bash
docker start cb-dev-control-plane cb-dev-worker && kubectl --context kind-cb-dev wait node --all --for=condition=Ready --timeout=300s && kubectl --context kind-cb-dev get pods -A | grep -vE 'Running|Completed'
```

The NetworkPolicy only lets namespaces labelled `metrics=enabled` scrape the operator:

```bash
kubectl --context kind-cb-dev label namespace monitoring metrics=enabled --overwrite
```

Start clean: no operator deployed, no budget, no rules left on the demo monitor. Expected: `No resources
found`, then `the server doesn't have a resource type "cb"` (the CRD was removed by `make undeploy`), then at
most kubectl's own `last-applied-configuration` annotation, with no `metricRelabelings` and no
`cardinality.telemetry-compass.com/...` annotation:

```bash
kubectl --context kind-cb-dev get deployment -n cardinality-budget-operator-system; kubectl --context kind-cb-dev get cb -A; kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings}{.metadata.annotations}{"\n"}'
```

If not, run "Stop and reset" first. Then apply the demo (Warn mode, limit 900):

```bash
kubectl --context kind-cb-dev apply -f hack/lab/demo/avalanche.yaml
```

Second terminal, open for the whole session:

```bash
kubectl --context kind-cb-dev -n monitoring port-forward svc/kps-prometheus 9090:9090
```

Prometheus must have been up for 15 minutes (the operator's default `--backend-warmup`), or every
measurement is `Unknown/BackendWarmingUp`. Check its start time:

```bash
curl -s localhost:9090/api/v1/status/runtimeinfo | jq -r .data.startTime; date -u
```

Third terminal:

```bash
kubectl --context kind-cb-dev -n demo get cb -w
```

## Part 1: F-0307 upgrade test (about 40 min)

Goal: a drop **written by v0.2** survives the upgrade unchanged. The new version adopts it from status and
monitor annotations without adding, removing or re-applying anything, and finishes its effectiveness check.
A drop made by the new build proves nothing here, so check which image wrote it (1d).

1a. Build both images first, so no build runs while drops are timed. The v0.2 build uses a worktree outside
`/tmp` (which is cleared on reboot); the first build of each is slow:

```bash
git worktree add /opt/cbo-v0.2 819197e && cd /opt/cbo-v0.2 && make docker-build IMG=cardinality-budget-operator:v0.2 && cd /opt/cardinality-budget-operator && make docker-build IMG=cardinality-budget-operator:dev && kind load docker-image cardinality-budget-operator:v0.2 cardinality-budget-operator:dev --name cb-dev
```

If the worktree already exists, skip `git worktree add` (or remove it first with
`git worktree remove --force /opt/cbo-v0.2`).

1b. Deploy v0.2 and create the budget:

```bash
cd /opt/cbo-v0.2 && make deploy IMG=cardinality-budget-operator:v0.2 && cd /opt/cardinality-budget-operator && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager && kubectl --context kind-cb-dev apply -f hack/lab/demo/budget.yaml
```

1c. Switch the budget to Enforce:

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"mode":"Enforce","enforcement":{"gracePeriod":"1m"}}}'
```

1d. Wait (up to about 12 min: v0.2 measures every 5m, plus the 1m grace period) until a drop appears:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.phase}{"\n"}{.status.enforcedDrops}{"\n"}'
```

Then confirm v0.2 wrote it: the image must be `cardinality-budget-operator:v0.2`, with pods older than the
drop's `since` (v0.2 already records `seriesAtDrop`, so that field does not tell the versions apart):

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system get deployment cardinality-budget-operator-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}' && kubectl --context kind-cb-dev -n cardinality-budget-operator-system get pods
```

1e. Save the state, only once 1d shows a drop:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.enforcedDrops}{"\n"}' > /root/before-drops.json && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o yaml > /root/before-sm.yaml && cat /root/before-drops.json
```

1f. Upgrade to the current build (image already loaded in 1a):

```bash
make deploy IMG=cardinality-budget-operator:dev && kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager
```

1g. Wait 10 minutes, then compare:

```bash
kubectl --context kind-cb-dev -n demo get cb default -o jsonpath='{.status.enforcedDrops}{"\n"}'; cat /root/before-drops.json; kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings[*].regex}{"\n"}' && kubectl --context kind-cb-dev -n demo describe cb default | tail -15
```

Pass:

- the v0.2 drop is still there with the same `metric`, `since` and `seriesAtDrop`; its `state` is now
  `Effective`
- exactly one rule per dropped metric
- no `Reverted` Event after the upgrade

The new build may add a second drop in these 10 minutes (usage is still over 900 and its settle period is
10m). That is correct; only the first entry must be unchanged.

## Part 2: short timings

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system patch deployment cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--measure-interval=1m"},{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enforcement-settle-period=3m"}]'
```

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout status deployment cardinality-budget-operator-controller-manager && kubectl --context kind-cb-dev -n cardinality-budget-operator-system get pods -o wide
```

Pass: two pods Running, preferably on different nodes.

## Part 3: Enforce lifecycle, failover, drift, kill switch, restore

3a. More drops. After Part 1 usage is usually just under the limit (about 856 of 900 with 3 drops), so nothing
new is dropped. Lower the limit so it is over again; after the 1m grace period the operator drops one more
metric (50 series each) every 3 minutes: 856 → 806 → 756, reaching `maxTotalDrops` (5).

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"maxActiveSeries":800}}'
```

3b. Failover (F-0302). When a new `Dropped` Event appears, kill the leader at once:

```bash
H=$(kubectl --context kind-cb-dev -n cardinality-budget-operator-system get lease c4f5a295.telemetry-compass.com -o jsonpath='{.spec.holderIdentity}'); echo "leader: $H"; kubectl --context kind-cb-dev -n cardinality-budget-operator-system delete pod "${H%%_*}" --grace-period=0 --force
```

After about 30 seconds:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system get lease c4f5a295.telemetry-compass.com -o jsonpath='{.spec.holderIdentity}{"\n"}' && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o jsonpath='{.spec.endpoints[0].metricRelabelings[*].regex}{"\n"}' && kubectl --context kind-cb-dev -n demo describe cb default | tail -25
```

Pass:

- the other pod is now the leader
- no regex appears twice; `enforcedDrops` matches the rules
- the next drop waits 3 minutes from `lastEnforcementTime`, not immediately

3c. Drift and pause (F-0211), alert `CardinalityBudgetPaused` (F-0305). Remove our rules by hand:

```bash
kubectl --context kind-cb-dev -n demo patch servicemonitor avalanche --type=json -p '[{"op":"remove","path":"/spec/endpoints/0/metricRelabelings"}]'
```

Pass: within a minute `Enforced=False/Reverted`, the entries show `Reverted`, nothing is re-applied. After
about 6 minutes:

```bash
curl -s localhost:9090/api/v1/alerts | jq -r '.data.alerts[] | "\(.labels.alertname) \(.state) \(.labels.budget_namespace // "") \(.labels.reason // "")"'
```

shows `CardinalityBudgetPaused firing demo Reverted`.

3d. Resume:

```bash
kubectl --context kind-cb-dev -n demo annotate cb default --overwrite cardinality.telemetry-compass.com/resume-enforcement=r1
```

Pass: the pause is cleared, the `Reverted` entries are gone, `lastResumeToken` is `r1`, drops start again
gradually.

3e. Kill switch (F-0203), alert `CardinalityBudgetKillSwitchActive`:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system patch deployment cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enforcement-paused"}]'
```

Pass: `Enforced` reason `Paused`, no monitor changes; after about 6 minutes the alerts `curl` shows
`CardinalityBudgetKillSwitchActive firing`. Turn it off again (back to the Part 2 arguments):

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout undo deployment cardinality-budget-operator-controller-manager
```

3f. Restore (F-0209). Raise the limit so usage is below the 90% warn threshold:

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"maxActiveSeries":1300}}'
```

Pass: one metric restored about every 3 minutes, newest first, until no rules are left and the phase is
`Healthy`.

## Part 4: remaining alerts (about 30 min, mostly waiting)

4a. `CardinalityBudgetMeasurementInvalid`, and no writes on an invalid measurement (ADR 0005). Lower the limit
again and wait for one drop:

```bash
kubectl --context kind-cb-dev -n demo patch cb default --type=merge -p '{"spec":{"maxActiveSeries":900}}'
```

Then point the operator at a Prometheus that does not exist (the last `--prometheus-url` wins):

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system patch deployment cardinality-budget-operator-controller-manager --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--prometheus-url=http://nowhere.monitoring.svc:9090"}]'
```

Pass: phase `Unknown`, `Ready=False/BackendUnreachable`, the drop rule stays (nothing restored); after about
16 minutes `CardinalityBudgetMeasurementInvalid firing`. Undo:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system rollout undo deployment cardinality-budget-operator-controller-manager
```

4b. `CardinalityBudgetOperatorDown`:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system scale deployment cardinality-budget-operator-controller-manager --replicas=0
```

Pass: after about 11 minutes `CardinalityBudgetOperatorDown firing`. Scale back:

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system scale deployment cardinality-budget-operator-controller-manager --replicas=2
```

4c. `ReconcileErrors`, `ConfigReloadFailed` and `RateLimited` are hard to trigger in the lab; their logic is
covered by tests. Check that all 7 rules are loaded:

```bash
curl -s localhost:9090/api/v1/rules | jq -r '.data.groups[] | select(.name=="cardinality-budget-operator") | .rules[].name'
```

## Part 5: F-0204 deletion cleanup

With one drop active:

```bash
kubectl --context kind-cb-dev -n demo delete cb default && kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o yaml | grep -E "cardinality|metricRelabelings"; echo "check done"
```

Pass: the delete finishes within seconds and `grep` prints nothing (no rule, annotation or label left).

## Part 6: fact 3, namespace-label spoofing

```bash
kubectl --context kind-cb-dev apply -f hack/lab/fact3/spoof.yaml
```

Wait 2 minutes:

```bash
for q in spoof_metric 'namespace:spoof_metric:sum'; do curl -s localhost:9090/api/v1/query --data-urlencode "query=$q" | jq -c '.data.result[].metric'; done
```

Pass: every line shows `"namespace":"demo"` and none `"victim"`, even with `honorLabels: true`.

Clean up:

```bash
kubectl --context kind-cb-dev delete -f hack/lab/fact3/spoof.yaml
```

Then "Stop and reset" to leave the lab clean.

## Stop and reset

Use this to stop halfway or after Part 6. It leaves the lab as Part 0 expects: no operator, no budget, no rules,
images kept. Order matters: delete the budget **while the operator still runs**, so its finalizer removes the
drop rules. If the kill switch is on (Part 3e), undo it first; deletion cleanup waits while it is on.

R1. Delete **every** budget (not only `demo`; an old sample in `default` blocks the CRD deletion in R2), then
check none is left and the demo monitor is clean (only `check done` is printed):

```bash
kubectl --context kind-cb-dev delete cb --all -A --timeout=90s; kubectl --context kind-cb-dev get cb -A --no-headers; kubectl --context kind-cb-dev -n demo get servicemonitor avalanche -o yaml | grep -E "cardinality|metricRelabelings"; echo "check done"
```

Do not run R2 while any budget is listed: without the operator nobody removes its finalizer, the CRD deletion
waits forever and `make undeploy` hangs.

If a delete times out because no operator is running, first list the monitors in that namespace that still
carry our rules (replace `<ns>`):

```bash
kubectl --context kind-cb-dev -n <ns> get servicemonitors,podmonitors -l cardinality.telemetry-compass.com/enforced=true -o name
```

If that prints nothing, remove the finalizer by hand. For `demo`, also reset the monitor from its manifest
(lab only; `replace` drops our annotation, label and rules):

```bash
kubectl --context kind-cb-dev -n <ns> patch cb default --type=merge -p '{"metadata":{"finalizers":null}}'
```

```bash
kubectl --context kind-cb-dev replace -f hack/lab/demo/avalanche.yaml
```

R2. Remove the operator (Deployment, RBAC, NetworkPolicy, alerts, CRD):

```bash
make undeploy ignore-not-found=true
```

R3. Remove the v0.2 worktree (the images stay, so the next build is quick):

```bash
git worktree remove --force /opt/cbo-v0.2; git worktree prune
```

R4. Optional, to free CPU and memory overnight: stop the port-forward (Ctrl+C) and pause the cluster. After
`docker start`, Prometheus needs its 15-minute warm-up before measurements are valid (Part 0).

```bash
docker stop cb-dev-worker cb-dev-control-plane
```

## When a step fails

```bash
kubectl --context kind-cb-dev -n cardinality-budget-operator-system logs deployment/cardinality-budget-operator-controller-manager --tail=50
```
