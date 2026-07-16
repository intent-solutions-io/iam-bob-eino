# CLAUDE.md — iam-bob-eino

Guidance for Claude Code working in this repository.

## What this is

`iam-bob-eino` is the **Go / CloudWeGo Eino** runtime of the **Intent Agent
Model (IAM)** — "Bob" (IAM = Intent Agent Model, *not* Identity and Access
Management). This slice is a **governed local coding agent**: Eino supplies the
agent loop; Bob supplies persona, governed tools, and the policy / approval /
verification / evidence boundaries. See `README.md`, `SECURITY.md`, and
`000-docs/001-AT-DECR-eino-runtime-decision.md`.

## Golden rules

1. **The governor is the only control point.** Every tool side effect must route
   through `internal/governor` (policy → approval → execution seam → evidence),
   and all file I/O must use `internal/workspace`'s symlink-safe methods
   (`ReadFileLimited`, `WriteFile`, `ReadDir`, `FS`) — never `os.ReadFile` /
   `os.WriteFile` directly. This is what keeps Bob inside the workspace.
2. **Eino owns the loop; Bob does not reimplement a framework.** Add capability
   as governed tools, not new orchestration machinery.
3. **Provider-neutral, zero Google.** Models come only through
   `internal/provider`; never import a provider SDK elsewhere; never add Google.
4. **Evidence is content-safe.** Never put file contents or secrets in evidence,
   tool results without redaction, or the trace. The evidence log lives outside
   the workspace and is hash-chained.
5. **Tests are real and ship with the change**; security behavior ships with a
   regression test. Don't lower a threshold to go green.

## Build & test

```bash
make ci            # fmtcheck + vet + test  (the gate)
make run-local TASK='...'   # BYOK: set DEEPSEEK_API_KEY / OPENAI_API_KEY / ...
```

The test suite needs no network (fake model in `internal/provider/fake.go`).

## Package map

`cmd/bob` CLI · `internal/agent` (Eino wiring + persona) · `internal/provider`
(BYOK model + fake) · `internal/governor` (control point) · `internal/policy`
(R0–R4) · `internal/approval` · `internal/tools` (governed tools) ·
`internal/verify` · `internal/evidence` (MC-projectable, hash-chained) ·
`internal/workspace` (os.Root confinement) · `internal/seams` (BigBrain / AGP /
Mission Control interfaces).

## Deferred / do not guess

How Bob consumes the IEP `@intentsolutions/core` governance kernel from Go (no Go
path exists) is an **open HQ decision** — see `001-AT-DECR` D7. Do not wire the
kernel, AGP, or signing until that lands; keep them behind `internal/seams`.

## Operating rules

`000-docs/002-DR-STND-operating-rules.md` (P1–P8). Enforcement travels with the
code (CI: fmt, vet, test/race).
