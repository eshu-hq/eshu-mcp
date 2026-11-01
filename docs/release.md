# Release (Refs `eshu-hq/eshu#9521`, part of `eshu-hq/eshu#6707`)

Pushes to `main` and `v[0-9]*` tag runs (a tag push, or a
`workflow_dispatch` on a tag ref) are the publishing events.
Either event builds a local-only `Dockerfile` image and Trivy blocks the
release on any `HIGH` or `CRITICAL` CVE before anything is pushed, so a
failed scan leaves nothing pullable in GHCR. Only after the scan passes
does the workflow push `ghcr.io/eshu-hq/eshu-mcp` for
`linux/amd64,linux/arm64` and attach SLSA provenance plus an SPDX SBOM
to the image. Main pushes publish the moving `main`, `latest`, and
`sha-<full-sha>` tags; tag runs publish the semver tags and then cut a
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
bumps and merges without the label stay manual. A tag pushed with the
default `GITHUB_TOKEN` starts no workflow run (GitHub does not trigger
runs from events that token causes, except `workflow_dispatch` and
`repository_dispatch`), so `auto-tag` follows the push with
`gh workflow run release.yml --ref vX.Y.Z`, and `release` runs the same
publish path for a dispatch on a tag ref as for a tag push. The dispatch
is skipped when a release run for that tag already exists, so a tag
cannot be published twice. To dry-run the bump computation without
pushing, run Actions, `auto-tag`, Run workflow.

Recovery: if a tag exists but has no `release` run or Release (the
dispatch failed, or an `auto-tag` re-run hit the "already tagged"
no-op), start the release by hand:

```bash
gh workflow run release.yml --repo eshu-hq/eshu-mcp --ref vX.Y.Z
```

A dispatch runs the workflow file as it exists at that ref, so this
works only for tags cut after the dispatch handling landed; an older tag
needs a new patch tag instead. The dispatch is refused (fails closed,
before the scan and push) when the tag already has a Release or its
commit is not on `main`, or it is not the newest `vX.Y.Z` tag (strictly
matched; pre-release tags are ignored). Recovery can therefore complete a
missing release for the newest tag only: it never replays a published
tag or moves the floating `X.Y` and `X` tags backward. `v0.2.6` is
intentionally left unreleased and is superseded by the next patch tag;
a superseded tag whose release failed is not recovered, the next patch
carries the fix forward.

Every tag run (push or dispatch) ends with `verify-published`, which
fails the run unless `:X.Y.Z` (the `v` is dropped from image tags) resolves to the pushed digest
and the Release records it. `auto-tag` fails if the dispatch errors or
no release run appears within about a minute, so a bare tag is never a
silent outcome.

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
- `push`: runs only after the scan passes; pushes on tag runs and `main`
  pushes with reproducible metadata (pinned labels, source epoch), then
  SLSA provenance and SBOM attestations pushed to GHCR and verified back
  with `gh attestation verify`.
- `github-release`: the tag carries a Release with the SBOM and the
  digest in the notes, so a deploy pin never comes from a hand-typed tag.

## Prove the workflow without pushing

Run it by hand from a branch: Actions, `release`, Run workflow. A
dispatch on a branch (never a tag) builds the local `linux/amd64` image only and skips the scan,
login, push, attestations, SBOM, and the Release, so it proves the
Dockerfile and workflow syntax without publishing anything.

## First-cut status

The workflow is wired and `actionlint` plus the `image` lane prove it
builds, but no tag has been cut from it yet. The first `v[0-9]*` push,
its provenance verification, and its Trivy gate stay `NOT_CHECKED` until
an owner cuts that tag. The router-surface/ranker delta (blocker 3 on
`eshu-hq/eshu#9521`) still gates any QA deploy and any cutover.
