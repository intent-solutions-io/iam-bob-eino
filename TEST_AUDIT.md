# TEST_AUDIT.md — iam-bob-eino

> Diagnostic output of the Intent Solutions testing SOP (`/audit-tests`), **molded
> for Go**. The `@intentsolutions/audit-harness` is Node/Python/Rust-first; a Go
> repo maps the same 7-layer taxonomy onto Go-native gates (`go test`, `go vet`,
> `gofmt`, `govulncheck`, `gocyclo`). Read-only diagnostic; the fixes it triggered
> are in the accompanying `test(harness)` commit.

## Grade: B (83/100)

Strong unit layer and green CI; gaps were at the enforcement edges (git hook,
security scan, CLI smoke), addressed in the same change.

## Classification

- **Type:** Go CLI (`cmd/bob`) over a set of libraries (`internal/*`).
- **Applicable layers:** L1 (hooks+CI), L2 (static+security), L3 (unit), L4
  (integration — the agent↔Eino end-to-end test), L6 (CLI smoke).
- **Waived:** L5 a11y (no UI), L5 perf (small local tool), L7 UAT (pre-release
  library slice). Recorded in `tests/TESTING.md#Waived`.

## Per-layer map (after remediation)

| Layer | Before | After | Notes |
|---|---|---|---|
| L1 hooks + CI | CI only | CI + pre-commit hook | `scripts/hooks/pre-commit` (fmt+vet+build+test); `make hooks` installs it |
| L2 static + security | gofmt+vet in CI | + govulncheck in CI | golangci-lint optional (`make lint`); gosec advisory (future) |
| L3 unit | 8 files, 60.9% | 14 files, ~63% | added governor, verify, approval suites; core packages now covered |
| L4 integration | agent↔Eino e2e | same | `internal/agent` drives the real ADK with a fake model |
| L6 CLI smoke | none | `cmd/bob` smoke test | builds the binary, exercises `-version` + usage |
| L5 / L7 | waived | waived | see above |

## Deterministic gates

| Gate | Result |
|---|---|
| `gofmt -l .` | clean |
| `go vet ./...` | clean |
| `go build ./...` | clean |
| `go test -race ./...` | green |
| coverage (total) | ~63% after remediation (evidence 92 / verify 88 / agent 85 / governor 83 / policy 76) |

## Gaps remaining (P2 / follow-up)

- **P2** golangci-lint not pinned in CI (advisory `make lint` only); add
  `golangci/golangci-lint-action` when a config is agreed.
- **P2** `gosec` security linter not wired (govulncheck covers known CVEs; gosec
  covers code patterns).
- **P2** no RTM / personas / journeys (`tests/TESTING.md` carries the classification
  + thresholds; full traceability deferred with the beads spine).
- **P2** `@intentsolutions/audit-harness` not vendored (Go curl path) — deferred;
  the Go-native gates above provide equivalent enforcement for this slice.

No P0/P1 gaps remain after remediation.
