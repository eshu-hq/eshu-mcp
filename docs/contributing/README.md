# Contributing to eshu-mcp

Thanks for helping. This page is the friendly front porch; the binding
rules live in the root [CONTRIBUTING.md](../../CONTRIBUTING.md). If the
two ever disagree, the root file wins.

## Setup

You'll need Go 1.26.10 or newer and `golangci-lint` v2.12.2. Then:

```bash
go build -o /tmp/eshu-mcp ./cmd/eshu-mcp
make ci
```

## Make targets

`make ci` runs the same checks as CI: `vet`, `lint`, `fmt`, `tidy`,
`cap` (the 500-line file cap), `build`, `test`, and `race`. Run it
before every push. Two extra gates to know:

- The contract surface has its own gate:
  `python3 scripts/check-parity.py`. It stays a separate workflow by
  design.
- Shell scripts pass `shellcheck` (the `shell` lane). If you change a
  shell script that a test executes, run `make ci` locally — the
  shell-only PR path can't know about that edge on its own.

The root [CONTRIBUTING.md](../../CONTRIBUTING.md) maps which paths run
which lanes.

## PR rules that don't bend

- Go files stay at or under 500 lines.
- The coverage floor is 90 and may only rise. Never lower it, exclude
  packages silently, or delete assertions to move the number.
- PR lanes never run live calls: tests run against inline fixtures,
  and fork PRs run with a read-only token and no secrets.
- Docs-only PRs run no lane and stay mergeable through `ci-complete`.

## Security and license

There's no `SECURITY.md` or `LICENSE` file here. If you find a
security issue, please tell the maintainers privately rather than
opening a public issue. And never commit credentials.
