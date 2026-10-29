# Local parity for the CI lanes: `make ci` runs the lint, test, and race
# commands the workflow runs. Coverage artifacts and SARIF upload stay in
# CI; use `go tool cover` locally for the same numbers.
#
# The first go.mod has landed, so every Go target below runs for real.
# golangci-lint must be v2.12.2, the version Eshu uses and CI installs.
GOLANGCI_LINT := golangci-lint

.PHONY: ci vet lint fmt tidy cap build test race

ci: vet lint fmt tidy cap build test race

vet:
	@if [ -f go.mod ]; then go vet ./...; else echo "skip vet: no go.mod yet"; fi

lint:
	@if [ -f go.mod ]; then $(GOLANGCI_LINT) run ./...; else echo "skip lint: no go.mod yet"; fi

fmt:
	@if [ -f go.mod ]; then test -z "$$($(GOLANGCI_LINT) fmt --diff ./... 2>/dev/null)"; else echo "skip fmt: no go.mod yet"; fi

tidy:
	@if [ -f go.mod ]; then go mod tidy -diff; else echo "skip tidy: no go.mod yet"; fi

cap:
	@if git ls-files '*.go' | grep -q .; then git ls-files '*.go' | xargs wc -l | grep -v ' total$$' | awk '$$1 > 500 {bad=1; print} END {exit bad}'; else echo "skip cap: no .go files yet"; fi

build:
	@if [ -f go.mod ]; then go build ./...; else echo "skip build: no go.mod yet"; fi

test:
	@if [ -f go.mod ]; then go test ./... -count=1; else echo "skip test: no go.mod yet"; fi

race:
	@if [ -f go.mod ]; then go test ./... -race -count=1; else echo "skip race: no go.mod yet"; fi
