# Security policy

## Supported versions

Only the latest minor release gets security fixes, as a patch release. v0.x lab milestones were never
published and are not supported. See the [deprecation policy](docs/deprecation-policy.md).

## Reporting a vulnerability

Please do not open a public issue. Report it privately through GitHub:
**Security → Report a vulnerability** on this repository
([private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)).

Include the affected version, how to reproduce it, and the impact you expect. You get an answer within 7
days. Fixes are released as a patch version with a GitHub security advisory, and credit if you want it.

## Scope

In scope: the operator, its manifests and Helm chart, and the release artifacts (images, signatures, SBOMs).
Especially relevant, because Enforce mode deletes data: any way for a namespace's users to make the operator
drop another namespace's metrics, to bypass `protectedMetrics`, or to make the operator write to resources
outside ServiceMonitors and PodMonitors.

## Verifying releases

Images and charts are signed with cosign keyless signing and carry an SBOM and build provenance. See
[docs/releasing.md](docs/releasing.md#verify-a-release). Design and controls: [threat model](docs/security/threat-model.md).
