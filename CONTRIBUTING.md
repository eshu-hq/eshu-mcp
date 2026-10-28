# Contributing

## Local gates

`make ci` runs the same lint, test, and race commands CI runs: `go vet`,
`golangci-lint run` plus the gofumpt format check, `go mod tidy -diff`,
the 500-line `.go` file cap, `go build`, the full test suite, and the
race suite. Run it before every push. It needs Go 1.26.10 or newer and
golangci-lint v2.12.2 (the version Eshu uses and CI installs).

Until the first `go.mod` lands, every Go target self-skips with a clear
message instead of failing; the skips disappear with the first module, no
Makefile change needed. When that module lands, switch every `setup-go`
step in `ci.yml` from the fixed `GO_VERSION` to `go-version-file: go.mod`
and measure the coverage baseline (issues #3, #11).

Shell scripts pass `shellcheck` (see the `shell` lane). The contract
surface keeps its own gate: `python3 scripts/check-parity.py` (see the
`contract-parity` workflow), which stays separate until the
reusable-workflow move (issue #10).

## CI lanes

Job names are frozen because branch protection requires `ci-complete`:

| Lane | Runs when |
| --- | --- |
| `lint` | Go sources, module or lint-config changes, workflow changes, plus unclassified files |
| `test` | Above, plus fixtures and config the tests read, plus unclassified files |
| `race` | Go sources, module, lint-config, or workflow changes, plus unclassified files, plus a coverage artifact and floor check |
| `shell` | Shell script changes |
| `image` | `Dockerfile` / `.dockerignore` changes (build only, no push); skipped until one lands |
| `govulncheck` | Dependency changes, pushes to `main`, weekly schedule, manual dispatch; skipped until the first `go.mod` lands |
| `gosec` | Pushes to `main`, weekly schedule, and manual dispatch; advisory, SARIF to the Security tab; skipped until the first `go.mod` lands |
| `ci-complete` | Always; fails on any needed lane failure, passes on legitimate skips |

Docs-only PRs (`README.md`, `LICENSE`, other `**/*.md`) run no lane and
stay mergeable through `ci-complete`. Anything unclassified (for example
`Makefile`, `CODEOWNERS`, `.gitignore`) runs `lint`, `test`, and `race`.
Fixture-JSON-only or shell-only PRs run `test` or `shell` only, with no
lint, format, or race. A manual `workflow_dispatch` run records every
lane; the weekly schedule runs `govulncheck` and `gosec` only.

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
