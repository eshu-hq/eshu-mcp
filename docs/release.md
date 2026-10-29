# Release (Refs `eshu-hq/eshu#9521`, part of `eshu-hq/eshu#6707`)

Tags `v*` are the only publishing event. A tag push builds the
`Dockerfile` for `linux/amd64,linux/arm64`, pushes
`ghcr.io/eshu-hq/eshu-mcp`, and attaches SLSA provenance plus an SPDX
SBOM to the image. Trivy blocks the release on any `HIGH` or `CRITICAL`
CVE in the pushed digest. The workflow then cuts a GitHub Release
carrying the SBOM. Nothing here deploys: QA wiring lives in
`iac-eks-eshu` (the `mcpServer.image` knob) and stays non-authoritative
until the router-surface/ranker delta (blocker 3 on #9521) lands.

## Cut a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Watch the `release` run on the tag: `build-and-push`, then `trivy`,
then `github-release`. The release body records the pushed digest;
verify it before deploying anywhere:

```bash
gh release view v0.1.0 --repo eshu-hq/eshu-mcp
gh attestation verify \
  oci://ghcr.io/eshu-hq/eshu-mcp:<tag> --repo eshu-hq/eshu-mcp
```

The tag also stamps the binary: the Dockerfile `VERSION` arg reaches
`-ldflags "-X main.version=..."`, so the `initialize` handshake reports
the release instead of `dev` (pinned by
`TestBuildServerReportsBuildVersion`).

## What the workflow proves

- `build-and-push`: reproducible metadata (pinned labels, source epoch),
  push on tag events only, SLSA provenance and SBOM attestations pushed
  to GHCR and verified back with `gh attestation verify`.
- `trivy`: image scan fails on `HIGH`/`CRITICAL`; the SARIF uploads as
  an artifact for triage when it fails.
- `github-release`: the tag carries a Release with the SBOM and the
  digest in the notes, so a deploy pin never comes from a hand-typed tag.

## Prove the workflow without pushing

Run it by hand from the branch: Actions, `release`, Run workflow. The
dispatch builds `linux/amd64` only and skips login, push, attestations,
SBOM, scan, and the Release, so it proves the Dockerfile and workflow
syntax without publishing anything.

## First-cut status

The workflow is wired and `actionlint` plus the `image` lane prove it
builds, but no tag has been cut from it yet. The first `v*` push, its
provenance verification, and its Trivy gate stay `NOT_CHECKED` until an
owner cuts that tag. The router-surface/ranker delta (blocker 3 on
`eshu-hq/eshu#9521`) still gates any QA deploy and any cutover.
