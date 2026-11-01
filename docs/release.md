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
`gh workflow run release.yml --ref refs/tags/vX.Y.Z`, and `release`
runs the same publish path for a dispatch on a tag ref as for a tag
push. The dispatch is skipped when a release run for that tag already
exists, so a tag is not published twice. To dry-run the bump
computation without pushing, run Actions, `auto-tag`, Run workflow.

Both routes publish the same image: a human `v[0-9]*` tag push (the
original trigger, unchanged) and the `auto-tag` dispatch. A human push
keeps the permissive tag check, so a pre-release tag such as
`v0.3.0-rc.1` still publishes (`verify-published` skips the moving
`X.Y` and `X` checks for it). A tag that matches `v[0-9]*` but is not
valid semver, such as `v1.0`, publishes only `sha-<full>` (the semver
tag patterns emit nothing for it), so `verify-published` checks that
tag against the pushed digest instead of `:X.Y.Z`. A dispatch must name
a strict `vX.Y.Z` tag.

## Recovery

If a tag exists but has no `release` run or Release (the dispatch
failed, or an `auto-tag` re-run hit the "already tagged" no-op), start
the release by hand:

```bash
gh workflow run release.yml --repo eshu-hq/eshu-mcp \
  --ref refs/tags/vX.Y.Z
```

A dispatch runs the workflow file as it exists at that ref, so this
works only for tags cut after the dispatch handling landed; an older
tag needs a new patch tag instead.

A tag dispatch is refused (before the scan and push, failing closed)
when any of these holds:

- the tag already has a Release, or the Release lookup fails with
  anything other than an explicit HTTP 404;
- the tag commit is not an ancestor of `main`;
- the tag is not the newest strict `vX.Y.Z` tag (pre-release tags are
  ignored), or no such tag can be listed.

So recovery can complete a missing release for the newest tag only. In
the normal case it does not replay a published tag or move the floating
`X.Y` and `X` tags backward. The newest-tag check runs twice, in
`build-and-scan` and again in `push` right before the image is pushed,
because a newer tag can be released while the scan runs. A window of a
few seconds between that last check and the push remains: GHCR offers
no compare-and-set.

`v0.2.6` is intentionally left unreleased and is superseded by the next
patch tag. A superseded tag whose release failed is not recovered; the
next patch carries the fix forward.

If a tag run fails after its Release exists (for example in
`verify-published`), use "Re-run failed jobs" on that run. Do not
dispatch again: the replay guard refuses a tag that already has a
Release.

Every tag run (push or dispatch) ends with `verify-published`, which
fails the run unless `:X.Y.Z` (the `v` is dropped from image tags)
resolves to the pushed digest, the moving `X.Y` and `X` tags resolve
(strict tags only), and the Release records the digest and carries the
SBOM. `auto-tag` fails if the dispatch errors or no release run appears
within about a minute, so a bare tag is never a silent outcome.

The local tests stub the GitHub and registry behavior, so the points
below were also checked against live runs (Refs `eshu-hq/eshu-mcp#53`).

Auto path, `v0.2.7`: merging #52 (label `release:patch`, merge commit
`290246a0`) ran `auto-tag` (run `41272337425`), which logged `tagged
290246a0... as v0.2.7` and `dispatched release.yml on v0.2.7`. The
dispatched `release` run `41272348688` (event `workflow_dispatch`,
`headBranch` `v0.2.7`, `headSha` `290246a0`) passed `build-and-scan`,
`push`, `github-release`, and `verify-published`. `:0.2.7` and the
Release body both record `sha256:5ae4cc13bc8ec5f4a50e4d35b56fc7b327001d1ee02b2c7ea3b24dbf7800d578`.

Human path, `v0.2.8`: a manual tag push on `8d32de38` ran `release`
`41273208189` (event `push`, same four jobs green). `:0.2.8`, `:0.2`,
`:0`, and the Release body all record
`sha256:fb370fd3ff8ef8cf19804bf2d1535cfd06d4ef275a165766611b3e658b9cc489`.

Confirmed by those runs:

- the dispatch API accepts `--ref refs/tags/vX.Y.Z`, and the run's
  `head_branch` is the tag name;
- a dispatched run on an annotated tag (both tags are annotated)
  reports the tag's commit, not the tag object, as `headSha`, which is
  what the `auto-tag` poll matches on;
- the full-history checkout (`fetch-depth: 0`) is enough for the
  ancestor check against `origin/main` and for the tag list;
- the dispatched run pushes with the workflow token
  (`packages: write`) the same way a tag-push run does;
- `docker buildx imagetools inspect --raw` hashes to the `Digest:` line
  against GHCR, so the `verify-published` digest comparison works.

Repeat run, `v0.2.9`: merging #55 (label `release:patch`, merge commit
`4c47d698`) ran auto-tag run 41288733120, which pushed the annotated tag
and dispatched release run 41288741739 (`headBranch` `v0.2.9`, `headSha`
`4c47d698`). `build-and-scan`, `push`, `github-release` and
`verify-published` all passed, and `:0.2.9`, `:0.2` and `:0` resolve to
`sha256:a3fbe4ee0cbd0bbab7b78aa1f3335c314283937e73d01361d5744ba8bde11bcf`,
the digest in the Release body. The auto path has two live runs, so
`v0.2.7` was not a one-off.

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
`TestReleaseStampPlumbingGuardsVersionArg` for the
Dockerfile-to-workflow link).

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
dispatch on a branch (never a tag) builds the local `linux/amd64`
image only and skips the scan, login, push, attestations, SBOM, and
the Release, so it proves the Dockerfile and workflow syntax without
publishing anything.

## Status

The workflow has cut real releases: `v0.2.7` (auto), `v0.2.8`
(manual) and `v0.2.9` (auto), each with Trivy, provenance, SBOM, and
`verify-published` green (see above). The router-surface/ranker delta (blocker 3 on
`eshu-hq/eshu#9521`) still gates any QA deploy and any cutover.
