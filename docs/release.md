# Release (Refs `eshu-hq/eshu#9521`, part of `eshu-hq/eshu#6707`)

Pushes to `main` and `v[0-9]*` tag pushes are the publishing events.
Either event builds a local-only `Dockerfile` image and Trivy blocks the
release on any `HIGH` or `CRITICAL` CVE before anything is pushed, so a
failed scan leaves nothing pullable in GHCR. Only after the scan passes
does the workflow push `ghcr.io/eshu-hq/eshu-mcp` for
`linux/amd64,linux/arm64` and attach SLSA provenance plus an SPDX SBOM
to the image. Main pushes publish the moving `main`, `latest`, and
`sha-<full-sha>` tags; tag pushes publish the semver tags and then cut a
GitHub Release carrying the SBOM (the Release cut stays tag-only).
Nothing here deploys: QA wiring lives in `iac-eks-eshu` (the
`mcpServer.image` knob) and stays non-authoritative until the
router-surface/ranker delta (blocker 3 on #9521) lands.

## Cut a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Automatic patch tags (Refs #40): a PR merged to `main` with the
`release:patch` label is tagged with the next patch version by
`.github/workflows/auto-tag.yml`, with no manual push. Minor/major
bumps and merges without the label stay manual. The tag push fires
`release` exactly like a hand-pushed tag. To dry-run the bump
computation without pushing, run Actions, `auto-tag`, Run workflow.

Watch the `release` run on the tag: `build-and-scan`, then `push`,
then `github-release`. The release body records the pushed digest;
verify that digest before deploying anywhere (never the mutable tag: the
tag can move between verify and deploy):

```bash
gh release view v0.1.0 --repo eshu-hq/eshu-mcp
DIGEST="$(gh release view v0.1.0 --repo eshu-hq/eshu-mcp --json body -q .body | grep -o 'sha256:[0-9a-f]*' | head -1)"
gh attestation verify \
  "oci://ghcr.io/eshu-hq/eshu-mcp@${DIGEST}" --repo eshu-hq/eshu-mcp
```

The tag also stamps the binary: the Dockerfile `VERSION` arg reaches
`-ldflags "-X main.version=..."`, so the `initialize` handshake reports
the release instead of `dev` (pinned by
`TestBuildServerReportsBuildVersion` for the handshake hop and
`TestReleaseStampPlumbingGuardsVersionArg` for the Dockerfile-to-workflow
link).

## What the workflow proves

- `build-and-scan`: local-only build plus the Trivy `HIGH`/`CRITICAL`
  gate; nothing is pushed, so a failed scan leaves nothing pullable. The
  SARIF uploads as an artifact for triage when the scan fails.
- `push`: runs only after the scan passes; pushes on tag and `main`
  events with reproducible metadata (pinned labels, source epoch), then
  SLSA provenance and SBOM attestations pushed to GHCR and verified back
  with `gh attestation verify`.
- `github-release`: the tag carries a Release with the SBOM and the
  digest in the notes, so a deploy pin never comes from a hand-typed tag.

## Prove the workflow without pushing

Run it by hand from the branch: Actions, `release`, Run workflow. The
dispatch builds the local `linux/amd64` image only and skips the scan,
login, push, attestations, SBOM, and the Release, so it proves the
Dockerfile and workflow syntax without publishing anything.

## First-cut status

The workflow is wired and `actionlint` plus the `image` lane prove it
builds, but no tag has been cut from it yet. The first `v[0-9]*` push,
its provenance verification, and its Trivy gate stay `NOT_CHECKED` until
an owner cuts that tag. The router-surface/ranker delta (blocker 3 on
`eshu-hq/eshu#9521`) still gates any QA deploy and any cutover.
