# Contributing

## Local gates

`make ci` runs the same lint, test, and race commands CI runs: `go vet`,
`golangci-lint run` plus the gofumpt format check, `go mod tidy -diff`,
the 500-line `.go` file cap, `go build`, the full test suite, and the
race suite. Run it before every push. It needs Go 1.26.10 or newer and
golangci-lint v2.12.2 (the version Eshu uses and CI installs).

The first `go.mod` has landed, so every Go target runs for real and every
`setup-go` step in `ci.yml` reads `go-version-file: go.mod`. The coverage
baseline is measured at that floor (issues #3, #11).

Shell scripts pass `shellcheck` (see the `shell` lane). The contract
surface keeps its own gate: `python3 scripts/check-parity.py` (see the
`contract-parity` workflow), which stays a separate workflow by design:
the shared reusable has no parity lane.

## CI lanes

Lane bodies live in the shared `go-collector-ci` reusable workflow
(`eshu-hq/.github`, v2, pinned in `ci.yml`); check runs nest as
`go-ci / <lane>`, so branch protection requires `go-ci / ci-complete`:

| Lane | Runs when |
| --- | --- |
| `lint` | Go sources, module or lint-config changes, workflow changes, plus unclassified files |
| `test` | Above, plus fixtures and config the tests read, plus unclassified files |
| `race` | Go sources, module, lint-config, or workflow changes, plus unclassified files, plus a coverage artifact and floor check |
| `shell` | Shell script changes |
| `image` | `Dockerfile` / `.dockerignore` **or Go-source** changes (build only, no push), since the image go-builds the sources |
| Tag `v*` | `release`: multi-arch push to `ghcr.io/eshu-hq/eshu-mcp` with provenance, SBOM, Trivy gate, GitHub Release (see `docs/release.md`) |
| `govulncheck` | Dependency changes, pushes, weekly schedule, manual dispatch |
| `gosec` | Pushes, weekly schedule, and manual dispatch; advisory on PRs, SARIF to the Security tab via the caller-side job |
| `ci-complete` | Always; fails on any needed lane failure, passes on legitimate skips |

Docs-only PRs (`README.md`, `LICENSE`, other `**/*.md`) run no lane and
stay mergeable through `ci-complete`. Anything unclassified (for example
`Makefile`, `CODEOWNERS`, `.gitignore`) runs `lint`, `test`, and `race`.
Fixture-JSON-only or shell-only PRs run `test` or `shell` only, with no
lint, format, or race. A manual `workflow_dispatch` run records every
lane; scheduled and branch-push runs execute every lane.

If you change a shell script that a test executes, run `make ci` locally:
the `shell`-only PR path cannot know about that edge on its own.

## Coverage floor

The `race` job fails below `COVERAGE_FLOOR` in `ci.yml`. The floor starts
at the first measured baseline once Go code lands and may only rise toward
the 90% target as tests land (issue #11). Never lower it, exclude packages
silently, or delete assertions to move the number.

## Fixture-only CI

PR lanes never run live calls: tests run against inline fixtures, and fork
PRs run the required lanes with a read-only token and no secrets.
