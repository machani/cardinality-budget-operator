# Threat model

Scope: the CardinalityBudget operator at v0.3 (F-0303). The operator deletes data on purpose (Enforce mode),
so the main question is: who can make it drop the wrong metrics, or stop it from protecting Prometheus?

## Assets

| Asset | Why it matters |
| --- | --- |
| Teams' metrics | A wrong drop removes dashboards and alerts data |
| The shared Prometheus | One namespace's explosion degrades monitoring for everyone |
| ServiceMonitors / PodMonitors | The operator writes to them; a bad write breaks scraping or Prometheus config |
| Prometheus credentials | Read access to every namespace's metrics |

## Trust boundaries and controls

| Threat | Control | Where |
| --- | --- | --- |
| A team raises its own limit or turns Enforce off | Only the platform team writes budgets; teams get the viewer role | ADR 0002, `config/rbac` |
| A team labels series with another namespace to get its metrics dropped | Enforce requires Prometheus `enforcedNamespaceLabel`; fact 3 verifies it on the lab | ADR 0003, `hack/lab/fact3` |
| Cluster exporters counted against a namespace | Default `--exclude-series`; documented rule for `excludedFromEnforcement` | ADR 0013 |
| Wrong data leads to drops or restores | Validity checks; Unknown blocks all writes | ADR 0005 |
| An invalid spec fails open | CEL plus runtime check; InvalidSpec freezes | ADR 0012 |
| The operator removes or reorders team rules | Canonical rule only, owned per annotation, fuzz-tested | ADR 0007, ADR 0016 |
| Runaway enforcement | Per-step and total caps, settle period, effectiveness and reload checks, hourly limit, kill switch | ADR 0006, ADR 0010 |
| GitOps fights the operator | Drift detection pauses instead of re-applying | ADR 0008 |
| Leaked Prometheus credentials | Credentials only from mounted files; inline secrets rejected; URL userinfo rejected and never logged | ADR 0005, `internal/config` |
| Compromised operator pod changes more than monitors | Minimal RBAC: budgets, budget status/finalizers, monitors (patch only), events; no Secrets, no Prometheus objects | `config/rbac/role.yaml` |
| Unauthenticated metrics scraping | Metrics served over HTTPS with Kubernetes authn/authz; NetworkPolicy allows only labelled namespaces | `config/default`, `config/network-policy` |
| Container escape or tampering | Non-root, read-only root filesystem, no privilege escalation, all capabilities dropped, seccomp RuntimeDefault, distroless image | `config/manager/manager.yaml`, `Dockerfile` |
| Vulnerable dependencies or base image | govulncheck and Trivy in CI, Dependabot weekly | `.github/` |
| Two replicas both enforcing | Leader election; only the leader reconciles; status-derived counts | F-0302, ADR 0010 |

## RBAC review (2026-10-13)

| Resource | Verbs | Needed for |
| --- | --- | --- |
| cardinalitybudgets | get, list, watch, patch, update | Reading budgets; adding and removing the finalizer |
| cardinalitybudgets/status | get, update, patch | Writing status |
| cardinalitybudgets/finalizers | update | Owner finalizer semantics |
| servicemonitors, podmonitors | get, list, watch, patch | Drop rules (no create or delete) |
| events.k8s.io events | create, patch | Events on budgets |
| leases (leader election role) | as scaffolded | Leader election |

Not granted: Secrets, ConfigMaps, Prometheus objects, any cluster-scoped write.

## Known gaps

- The operator's own ServiceMonitor uses `insecureSkipVerify` for the metrics certificate (scaffold default).
  Production should use cert-manager certificates (Helm `certManager.enabled`, Kustomize
  `config/prometheus/monitor_tls_patch.yaml`). Exception E3 in the [readiness review](../readiness-review.md);
  v1.0 has no webhook, so no other certificates exist ([ADR 0017](../adr/0017-api-v1beta1.md)).
- Images and charts are signed (cosign keyless) with an SBOM and provenance from v1.0
  ([ADR 0018](../adr/0018-release-pipeline.md)).
- NetworkPolicy covers ingress only; egress to Prometheus and the API server is not restricted, because their
  addresses differ per cluster.
