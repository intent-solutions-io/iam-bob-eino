# CLAUDE.md — iam-bob-eino

Guidance for Claude Code working in this repository.

## What this is

`iam-bob-eino` is the **Go / CloudWeGo Eino** runtime of the **Intent Agent
Model (IAM)** — "Bob" (IAM = Intent Agent Model, *not* Identity and Access
Management). This slice is a **governed local coding agent**: Eino supplies the
agent loop; Bob supplies persona, governed tools, and the policy / approval /
verification / evidence boundaries. See `README.md`, `SECURITY.md`, and
`000-docs/001-AT-DECR-eino-runtime-decision.md`. (`AGENTS.md` is the tool-neutral
sibling of this file.)

**Naming:** Bob = persona (human-facing only) · `intent-agent-model/bob` = agent ·
`eino-go` = runtime · `iam-bob-eino` = implementation · `intent-bob-eino` = component.
Canonical binary `bob-eino` (`bob` = deprecated tested alias); canonical env
`INTENT_BOB_EINO_*` (`BOB_*` legacy, warned). Contract:
`000-docs/005-DR-STND-bob-eino-identity-contract.md`.

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
6. **Identity has one creation path.** All identity strings come from
   `internal/identity` (`identity.New`) and `internal/version` constants — never
   bare `"bob"` as a machine key (binary/service/env/telemetry/agent name), and
   never restate the id values as literals. Never write the phrase "fake model"
   (say "deterministic Eino model fixture" / "offline model stub").

## Build & test

```bash
make ci            # fmtcheck + vet + test  (the gate)
make build         # canonical bob-eino binary
make build-legacy  # legacy `bob` alias (same internal/cli)
make run-local TASK='...'   # BYOK: set DEEPSEEK_API_KEY / OPENAI_API_KEY / ...
```

The test suite needs no network (offline model stub in `internal/provider/fake.go`).

## Package map

`cmd/bob-eino` canonical CLI + `cmd/bob` legacy alias (both thin wrappers) ·
`internal/cli` (subcommand dispatch version/doctor/plan/run/verify/evidence +
deprecated flat fallback + state paths; `plans/` and `receipts/` live under the
state dir) · `internal/config` (typed config, `INTENT_BOB_EINO_*`, precedence
merge) · `internal/identity` (structured machine identity — single creation
path) · `internal/version` (build identity + ldflags commit/date) ·
`internal/agent` (Eino wiring + persona) · `internal/provider` (BYOK model,
MiniMax-first; `FromConfig` for merged config, `Resolve` for the flat selector;
offline stub `fake.go`) · `internal/governor` (control point: limits → policy →
plan guard → approval → seam → evidence) · `internal/policy` (R0–R4,
AllowWrites/AllowExec) · `internal/approval` (variance-aware; AutoApprove
refuses out-of-plan actions) · `internal/plan` (hashed plan artifacts) ·
`internal/planguard` (plan-variance guard + HEAD invalidation) ·
`internal/limits` (per-run usage bounds, typed cancel causes) ·
`internal/patch` (intent-bob-eino-patch/v1, two-phase atomic) ·
`internal/gitstate` (read-only git state, clean degradation) · `internal/tools`
(governed tools incl. `apply_patch`; `ReadOnly` planning set never constructs
mutation tools) · `internal/verify` (per-action verification) ·
`internal/runverify` (model-free run verifier) · `internal/receipt` (sealed run
receipts + evidence-log loader) · `internal/doctor` (preflight checks) ·
`internal/evidence` (MC-projectable, session-spanning hash chain, carries
`agent_identity` + `schema_version`) · `internal/workspace` (os.Root
confinement) · `internal/seams` (BigBrain / AGP / Mission Control interfaces).

CLI surface + migration guidance: `000-docs/009-DR-GUID-cli-subcommands-and-migration.md`.
The live MiniMax smoke is double-gated (`INTENT_BOB_EINO_LIVE_SMOKE=1` +
`MINIMAX_API_KEY`; `scripts/live-smoke.sh`) and NEVER runs in CI — do not arm it
from any workflow.

## Deferred / do not guess

How Bob consumes the IEP `@intentsolutions/core` governance kernel from Go (no Go
path exists) is an **open HQ decision** — see `001-AT-DECR` D7. Do not wire the
kernel, AGP, or signing until that lands; keep them behind `internal/seams`.

## Operating rules

`000-docs/002-DR-STND-operating-rules.md` (P1–P8). Enforcement travels with the
code (CI: fmt, vet, test/race).
