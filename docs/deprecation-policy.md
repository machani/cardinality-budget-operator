# Deprecation policy

What users can rely on between releases. Versions follow [semantic versioning](https://semver.org/) from
v1.0 ([ADR 0018](adr/0018-release-pipeline.md)).

## API

| Change | Allowed in | Notice |
| --- | --- | --- |
| New optional field, new condition reason, new status field | Minor release | CHANGELOG `Added` |
| Removing or renaming a field, changing a default or a validation range in a served version | Not allowed | — |
| Such a change in a new API version (for example `v1`) | Minor release, with a conversion webhook | CHANGELOG `Added`, new ADR |
| Removing a deprecated API version | Major release only | Deprecated for at least one minor release before |

The API server warns on every request that uses a deprecated version. Current state
([ADR 0017](adr/0017-api-v1beta1.md)): `v1beta1` is stored; `v1alpha1` is deprecated and removed in v2.0. Run
the storage migration ([runbook](runbooks/README.md#upgrade-to-v10-move-budgets-to-v1beta1)) before upgrading
to v2.0.

## Behaviour, flags, metrics and alerts

| Item | Rule |
| --- | --- |
| Operator flags | Removed or renamed only in a major release; deprecated flags keep working and log a warning for at least one minor release |
| Defaults that make enforcement more aggressive | Only in a major release |
| Metric names and labels, alert names | Renamed or removed only in a major release; announced one minor release ahead |
| Annotation and label keys on monitors and budgets | Never changed: tools and people rely on them to find and remove drops by hand |

## Supported versions of other systems

From the [support policy](adr/0011-version-support-policy.md): dropping a Kubernetes, Prometheus Operator or
Prometheus version is announced in the CHANGELOG (`Deprecated`) one minor release ahead. The
[compatibility matrix](compatibility.md) lists the versions each release is tested with.

## Releases that get fixes

Only the latest minor release gets bug and security fixes, as patch releases. Upgrade within a major version
must always be possible without manual steps beyond those in the CHANGELOG.
