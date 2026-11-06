## What and why

<!-- One or two sentences. Link the feature row, e.g. F-0105. -->

## Checklist

- [ ] One feature per PR; the row in `docs/features/` is updated (status, and the commit after merge)
- [ ] Behaviour or API change: the HLD is updated and an ADR is linked
- [ ] `make manifests generate` run after changes in `api/` or RBAC markers
- [ ] `make test` and `make lint` pass
- [ ] New Go files carry the Apache-2.0 header
- [ ] Safety rules in the HLD still hold (no new path that adds or removes drop rules without the checks)
