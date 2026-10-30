# 0017. API v1beta1 without a conversion webhook

- Status: Accepted
- Date: 2026-10-29

## Context

The HLD's production readiness bar for v1.0 asks for API `v1beta1` "with a conversion webhook from `v1alpha1`".
A CRD can serve several versions, but etcd stores each object in one of them, the storage version. Reading an
object in another version needs a conversion: with identical schemas the API server only rewrites
`apiVersion` (strategy `None`); with different schemas it calls a conversion webhook.

The lab runs of v0.2 and v0.3 found no field that needs to change: names, ranges, defaults and status all
held up. A webhook would then convert nothing, yet it adds an HTTPS server, a certificate the API server must
trust, and a new failure mode. While the webhook is down, every request that needs conversion fails, including
the finalizer cleanup and namespace deletion. Enforce mode deletes data, so a component with no benefit and a
new outage path goes against "safety beats features".

## Options considered

1. **Same schema, no webhook** (strategy `None`).
2. **Same schema, webhook anyway**, ready for later changes.
3. **Change fields in `v1beta1`, plus a webhook.** No change was found that is worth breaking `v1alpha1` users
   for.

Certificates, should a webhook be needed: cert-manager (the Kubebuilder default, one more prerequisite) or
self-managed (the cert-controller library, which needs write access to Secrets and to the CRD, breaking the
"no Secrets" RBAC rule of ADR 0002).

Storage migration: a documented upgrade step, the operator migrating at startup (needs cluster-wide update on
CRDs), or the Kubernetes `StorageVersionMigration` API (not enabled on every supported version).

## Decision

- `v1beta1` has exactly the `v1alpha1` schema. Both versions are served; `v1beta1` is the storage version;
  conversion strategy `None`. The operator reads and writes only `v1beta1`.
- A test compares the two schemas in the generated CRD and fails if they differ. Changing a field later means
  a new ADR and a conversion webhook, with **cert-manager** issuing its certificate.
- `v1alpha1` is deprecated in v1.0 (the API server warns on every request) and removed in v2.0.
- Existing objects are migrated by a documented upgrade step (`hack/migrate-storage.sh`): re-save every budget
  so etcd holds `v1beta1`, then set the CRD's `status.storedVersions` to `["v1beta1"]`. Required before v2.0;
  the operator needs no extra RBAC.

## Consequences

- No new component, certificate or prerequisite in v1.0; RBAC is unchanged.
- The HLD rows "API `v1beta1`" and "API stability" change to match this ADR.
- Users move manifests to `apiVersion: cardinality.telemetry-compass.com/v1beta1` at their own pace during v1.x.
- An upgrade that skips the migration step works in v1.x but blocks removing `v1alpha1` in v2.0; the v2.0
  upgrade notes must check `storedVersions`.
- The validating webhook for cross-field rules listed in the HLD V2 table stays out: CEL covers every rule.
