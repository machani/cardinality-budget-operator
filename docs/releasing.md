# Releasing

How a release is cut and how users verify it ([ADR 0018](adr/0018-release-pipeline.md)). The workflow is
[`.github/workflows/release.yml`](../.github/workflows/release.yml).

## What a release publishes

| Artifact | Where |
| --- | --- |
| Image, linux/amd64 and linux/arm64 | `ghcr.io/machani/cardinality-budget-operator:X.Y.Z` |
| Image signature, SBOM attestation (SPDX), build provenance | Attached to the image digest in ghcr.io |
| Helm chart, signed | `oci://ghcr.io/machani/charts/cardinality-budget-operator`, version `X.Y.Z` |
| `install.yaml` (image pinned by digest), `sbom.spdx.json`, chart `.tgz` | GitHub release `vX.Y.Z` |

## Cut a release

1. On `dev`, rename `## [Unreleased]` in [CHANGELOG.md](../CHANGELOG.md) to `## [X.Y.Z] - YYYY-MM-DD`, add a
   new empty `## [Unreleased]` above it, and merge through a pull request.
2. Merge `dev` into `main` through a pull request; wait for green CI on `main`.
3. Tag the merge commit on `main` and push the tag:

   ```bash
   git switch main && git pull && git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z
   ```

4. Watch the Release workflow. It stops before publishing if the tag is not on `main`, the CHANGELOG has no
   `[X.Y.Z]` section, or Trivy finds a fixable critical or high vulnerability.
5. First release only: on the repository's Packages page, set both packages (image and chart) to public.

A release candidate uses `vX.Y.Z-rc.N` and needs its own CHANGELOG section; it is published as a pre-release.

## Dry run

Actions → Release → Run workflow (on any branch). It builds, scans, pushes, signs and verifies
`ghcr.io/machani/cardinality-budget-operator:0.0.0-dryrun.N`, and keeps the SBOM, `install.yaml` and the chart
as workflow artifacts. It publishes no chart and no GitHub release. Delete old dry-run versions from the
package page.

## Verify a release

Needs [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) v2 or later. The identity is this
repository's release workflow, so no public key is needed.

```bash
cosign verify ghcr.io/machani/cardinality-budget-operator:X.Y.Z --certificate-identity-regexp '^https://github\.com/machani/cardinality-budget-operator/\.github/workflows/release\.yml@refs/tags/v' --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The SBOM attestation (prints the SPDX document):

```bash
cosign verify-attestation ghcr.io/machani/cardinality-budget-operator:X.Y.Z --type spdxjson --certificate-identity-regexp '^https://github\.com/machani/cardinality-budget-operator/\.github/workflows/release\.yml@refs/tags/v' --certificate-oidc-issuer https://token.actions.githubusercontent.com | jq -r .payload | base64 -d | jq .predicate.name
```

The build provenance, with the GitHub CLI:

```bash
gh attestation verify oci://ghcr.io/machani/cardinality-budget-operator:X.Y.Z --repo machani/cardinality-budget-operator
```

The chart:

```bash
cosign verify ghcr.io/machani/charts/cardinality-budget-operator:X.Y.Z --certificate-identity-regexp '^https://github\.com/machani/cardinality-budget-operator/\.github/workflows/release\.yml@refs/tags/v' --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
