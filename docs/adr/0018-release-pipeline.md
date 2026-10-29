# 0018. Release pipeline

- Status: Accepted
- Date: 2026-10-29

## Context

v1.0 needs semantic versioning, signed images, an SBOM and a CHANGELOG (HLD "Production readiness"). Until
now CI builds and scans images but publishes nothing. Enforce mode deletes data, so users must be able to
check that the image they run is the one this repository built, and read what changed before upgrading.

## Options considered

- Registry: GitHub Container Registry, Docker Hub, Quay. The last two need a long-lived token as a secret.
- Signing: cosign keyless (Sigstore, GitHub OIDC), cosign with a stored key pair, or GoReleaser around the same
  tools. A stored key can leak and must be rotated; GoReleaser adds a tool for a project with no binaries.
- CHANGELOG: hand-written (Keep a Changelog), generated from commits (git-cliff), or release-please.
- Trigger: pushing a tag, or a manual workflow run that creates the tag.

## Decision

- **Versions**: semantic versioning. Tag `vX.Y.Z` (or `vX.Y.Z-rc.N`, published as a pre-release); images and
  charts use `X.Y.Z`. A tag must point at a commit on `main` and have a `## [X.Y.Z]` section in CHANGELOG.md.
- **Registry**: `ghcr.io/machani/cardinality-budget-operator` for the image and
  `oci://ghcr.io/machani/charts/cardinality-budget-operator` for the chart, pushed with the workflow's
  `GITHUB_TOKEN`. No stored secrets.
- **Images**: `linux/amd64` and `linux/arm64` (ADR 0011), scanned with Trivy before pushing (critical or high
  fixable findings stop the release).
- **Signing and SBOM**: cosign keyless signs the image and the chart by digest. syft makes an SPDX SBOM,
  attached to the image as a signed attestation and to the GitHub release. GitHub build provenance (SLSA) is
  attached too. The workflow verifies the signature before it creates the release.
- **CHANGELOG**: hand-written in Keep a Changelog format; every user-visible PR adds a line under
  `[Unreleased]`. Deprecations and removals are announced there one minor release ahead (ADR 0011, ADR 0017).
- **Trigger**: pushing a `v*` tag. A manual run is a dry run: it builds, pushes, signs and verifies a
  `0.0.0-dryrun.N` image, but publishes no chart and no GitHub release.

## Consequences

- `.github/workflows/release.yml` holds the pipeline; `docs/releasing.md` describes cutting a release and
  verifying one.
- Users verify images with `cosign verify` against this repository's workflow identity; no public key to
  distribute.
- Dry runs leave `0.0.0-dryrun.*` images in the registry; delete them from the package page when no longer
  needed.
- The "tested upgrade from the previous minor release" bar (HLD) can start only after the first release
  exists; until then the chart test upgrades the chart onto itself.
