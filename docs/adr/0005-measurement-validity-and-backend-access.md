# 0005. Measurement validity and backend access

- Status: Accepted
- Date: 2026-10-04

## Context

The safety rule "backend unreachable or stale data: phase Unknown, keep existing drops, never add new ones"
does not define stale data, and it only protects one direction. A Prometheus that answers but is wrong is
more dangerous than one that does not answer:

- Just after a restart (WAL replay, first scrapes), counts are falsely low. Low usage triggers restore, and
  the spike returns to a Prometheus that is already struggling.
- `count()` over no series returns an empty result, not `0`.
- Thanos or HA proxies can return partial results with warnings.
- If reconciles keep failing, status shows old numbers that look current.

Separately, production Prometheus is usually protected (bearer token, mTLS, basic auth, private CA), and the
HLD only mentions a URL.

## Options considered

Validity:

1. Minimal: queries returned HTTP 200 in time.
2. Layered: option 1 plus readiness, warm-up, and no warnings.
3. Option 2 plus cross-checks against target counts (more complexity, little extra safety).

Backend access:

1. URL only.
2. A flag per auth method.
3. URL plus a Prometheus-format HTTP client config file (`prometheus/common/config`).
4. Read a Secret through the Kubernetes API (needs `get secrets`; overlaps the V2 `BackendConfig` kind).

## Decision

**Valid measurement**: all of the following hold.

- Both usage queries succeeded within `--query-timeout` (default `30s`).
- Prometheus `/-/ready` returns 200.
- Prometheus has been up longer than `--backend-warmup` (default `15m`), using `startTime` from
  `/api/v1/status/runtimeinfo`.
- The query responses contain no warnings.

An empty result counts as `0` only when the measurement is otherwise valid.

**Fresh**: the last valid measurement is newer than `--max-measurement-age` (default 2 x `--measure-interval`,
so `10m`).

**When not valid or not fresh**:

- Phase `Unknown`; `Ready=False` with reason `BackendUnreachable`, `BackendWarmingUp`, `PartialData` or
  `Stale`; `BackendReachable` set accordingly.
- **No drop rules are added or removed.**
- Status keeps the last valid numbers; `lastMeasured` shows their age.

**Backend access**:

- `--prometheus-url` plus optional `--prometheus-http-config`, a file in Prometheus's HTTP client config
  format, loaded with `prometheus/common/config` (bearer token file, TLS, basic auth, OAuth2, proxy).
- Credentials are files mounted from Secrets into the operator pod. Never in the CR, environment variables or
  flags, and never read through the Kubernetes API (no `get secrets` RBAC).
- Token files are re-read on each request, so rotation works without restarts.
- The config is validated at startup; the operator refuses to start if it is invalid.
- `insecure_skip_verify: true` logs a startup warning and sets an operator metric.
- Logs never contain headers or tokens; `user:pass@` is stripped from URLs before logging.
- The URL should point at a single Prometheus replica or a deduplicating layer. Thanos Query speaks the same
  API but is not in the V1 tested matrix.

## Consequences

- The reconcile rule changes from "never add new drops" to "never add **or remove** drops" on an invalid or
  stale measurement.
- Two extra HTTP calls per measurement (`/-/ready`, `runtimeinfo`); both are cheap.
- New flags: `--query-timeout`, `--backend-warmup`, `--max-measurement-age`, `--prometheus-url`,
  `--prometheus-http-config`.
- Tests: fake Prometheus returns warm-up start times, warnings, empty results and timeouts.

## Verify in Phase 1

- `runtimeinfo` fields in Prometheus 3.x.
