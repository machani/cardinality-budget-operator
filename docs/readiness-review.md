# Production readiness review (v1.0)

F-1005: every row of the [HLD production readiness bar](design/hld.md#production-readiness) is met or has an
accepted exception. Reviewed 2026-10-29 on branch `feat/v1.0`.

| Area | Bar | Status | Evidence |
| --- | --- | --- | --- |
| Availability | 2 replicas, leader election, PDB, anti-affinity; state in status and on monitors | Met (E5) | `config/manager`, chart `extras/`; failover mid-step verified in the lab (F-0302, [verify-v0.3](../hack/lab/verify-v0.3.md)) |
| Fail-safe enforcement | Every safety rule covered by tests | Met | envtest suites in `internal/controller` (caps, settle, reload, ineffective, drift, kill switch per write type, protected metrics, invalid or stale measurement) |
| Auditability | `enforcedDrops`, Events, annotation and label | Met | F-0203, F-0207; [runbook "remove rules by hand"](runbooks/README.md#remove-the-operators-rules-by-hand) |
| Security | Least privilege, read-only teams, mounted credentials, restricted pods, NetworkPolicy, authenticated metrics, signed images, SBOM, scan | Met (E3, E4) | [threat model](security/threat-model.md), F-0303, [ADR 0018](adr/0018-release-pipeline.md), Trivy in `image.yml` and `release.yml` |
| API stability | `v1beta1`, conversion, deprecation policy | Met | [ADR 0017](adr/0017-api-v1beta1.md), `api/versions_test.go`, round-trip envtest, [deprecation policy](deprecation-policy.md) |
| Upgrades | Helm upgrades CRDs; semver; tested upgrade from the previous minor | Met (E2) | CRD in chart templates with `keep`; `test-chart.yml`; v0.2 → v0.3 upgrade verified in the lab |
| Testing | Unit, envtest, e2e on kind, failure tests, scale tests | Met (E1) | `test.yml`, `test-e2e.yml` (4 combinations), `prometheus-compat.yml`; lab runs F0–F11 |
| Compatibility | Published matrix | Met | [compatibility matrix](compatibility.md), checked against CI by `make verify` |
| Operator observability | Metrics, dashboard, alerts | Met | `config/prometheus`, `config/grafana`, one runbook per alert |
| Documentation | Install, user, admin, GitOps, runbooks, uninstall, CHANGELOG, security policy | Met | [guides](guides/), [runbooks](runbooks/README.md), [CHANGELOG](../CHANGELOG.md), [SECURITY.md](../SECURITY.md) |

## Exceptions

Each needs explicit acceptance before v1.0.0 is tagged. Accepted exceptions are listed in the release notes.

| ID | Gap | Risk | Mitigation | Follow-up | Accepted |
| --- | --- | --- | --- | --- | --- |
| E1 | No scale tests on a large cluster (F-0308 blocked: no allowed cluster, no target numbers) | Unknown reconcile latency and query load with many budgets | Per-budget queries are scoped and rate is bounded by `--measure-interval`; sizing guidance in the [admin guide](guides/admin.md#sizing) | F-0308 in v1.1 | — |
| E2 | No upgrade test from a previous release: none has been published | An upgrade path breaks unnoticed | v0.2 → v0.3 upgrade tested in the lab; state is derived, not kept in memory | Upgrade-from-previous job once v1.0.0 exists | — |
| E3 | The operator's ServiceMonitor uses `insecureSkipVerify` unless cert-manager is enabled | A spoofed metrics endpoint could feed false operator metrics to Prometheus (no effect on enforcement) | Documented: Helm `certManager.enabled`, Kustomize `monitor_tls_patch.yaml` | Decide the default in v1.1 | — |
| E4 | NetworkPolicy restricts ingress only | A compromised operator pod can reach any address | Pod hardening; no Secrets access | Egress example in the docs | — |
| E5 | Helm chart: default `manager.replicas: 1` (generated) and no way to mount a Prometheus HTTP client config | Single replica if the install guide is not followed; Helm users cannot authenticate to Prometheus | Install guide sets 2 replicas; Kustomize supports the HTTP config | Chart values for both once the plugin allows it | — |

## Before tagging v1.0.0

- [ ] All CI checks green on the v1.0 pull request; merged into `dev`, then `main`.
- [ ] Lab: upgrade a v0.3 install to the v1.0 build, run `hack/migrate-storage.sh`, check
      `storedVersions` is `["v1beta1"]` and existing budgets keep their status and drops.
- [ ] Release dry run on `dev`: signed image, SBOM attestation and provenance verified with the commands in
      [releasing](releasing.md#verify-a-release).
- [ ] Exceptions E1 to E5 accepted (column above).
- [ ] CHANGELOG `[Unreleased]` renamed to `[1.0.0]`, README status changed from alpha.
