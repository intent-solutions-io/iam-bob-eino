# tests/TESTING.md — iam-bob-eino

The testing policy for this repo, per the Intent Solutions testing SOP (molded
for Go). Engineer owns the policy sections; audit-tests updates the observational
sections only.

## Classification

- **Repo type:** Go CLI + libraries (`cmd/bob` + `internal/*`).

## Thresholds (policy)

- `coverage.line`: 60 (project total; core governance packages target ≥ 80 —
  evidence 92, verify 88, agent 85, governor 83, policy 76). CLI wiring and the
  no-op seams pull the project total down; the governance core is what matters.
- Architecture: no package outside `internal/tools` may perform file I/O except
  through `internal/workspace`; no package but `internal/governor` mediates
  policy/approval/evidence. (Enforced by review + tests, not yet a linter rule.)

## Waived layers (policy)

- **L5 a11y** — no UI surface.
- **L5 perf** — small local tool; no perf budget.
- **L7 UAT** — pre-release library slice; no business-acceptance suite yet.

## Installed gates (observational)

- L1: `.github/workflows/ci.yml` (fmt check · vet · build · `test -race` +
  coverage) and `scripts/hooks/pre-commit` (install with `make hooks`).
- L2: `gofmt`, `go vet` (CI, blocking); `govulncheck` (CI); `golangci-lint`
  (advisory via `make lint`).
- L3: `go test -race ./...` across 12 test files.
- L4: `internal/agent` end-to-end through Eino's ADK with the deterministic Eino model fixture.
- L6: `cmd/bob` CLI smoke test.

## Frameworks (observational)

- Test framework: Go stdlib `testing` (no external assert lib).
- Agent framework under test: CloudWeGo Eino v0.9.12 + eino-ext.

## Last audit (observational)

- 2026-07-16 — molded Go audit; grade B (83/100); L1/L2/L6 gaps remediated in the
  same change. See `TEST_AUDIT.md`.

## Traceability (observational)

- RTM / personas / journeys: deferred with the beads spine (P2). Security
  behavior is regression-tested directly (`workspace` symlink escape, `tools`
  shell injection, `evidence` tamper chain).
