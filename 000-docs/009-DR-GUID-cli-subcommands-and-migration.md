# 009-DR-GUID — bob-eino CLI subcommands and flat-form migration

**Status:** Active guidance
**Date:** 2026-07-16
**Applies to:** `iam-bob-eino` ≥ the MiniMax task-lifecycle build (branch `feat/minimax-task-lifecycle`)
**Related:** `005-DR-STND-bob-eino-identity-contract.md` (identity), `002-DR-STND-operating-rules.md` (operating rules)

---

## 1. The subcommand surface

`bob-eino` is subcommand-shaped. Every command exits non-zero on failure and never claims a
success it did not verify.

| Command | What it does | Exit 0 means |
|---|---|---|
| `bob-eino version [-json]` | Identity, build provenance (ldflags commit/date), Go/Eino versions, schema versions. | printed |
| `bob-eino doctor [-net] [-json]` | 14 stable-named preflight checks (`workspace.path`, `credential.presence`, …). Credential checks are boolean-only — key material is never printed. `-net` enables the endpoint reachability probe. | no **required** check failed |
| `bob-eino plan [flags] <task>` | Runs the model with **read-only tools only** (the mutation tool builders are never constructed) and saves a hashed, validated plan artifact outside the workspace (`$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/plans/`). | plan sealed + saved |
| `bob-eino run -plan <id\|path> [flags]` | Pre-flight gates (workspace identity, HEAD == plan start SHA, required capabilities granted, provider/model match), then executes under the plan-variance guard + usage limits, seals a run receipt, and verifies it model-free. | run completed **and** verdict `verified`/`verified_with_warnings` |
| `bob-eino verify -receipt <path\|run-id> [-plan <id\|path>] [-json]` | Re-verifies a sealed receipt post-hoc: tamper-rejecting load, evidence re-filtered by run id, live git end-SHA re-check, optional plan cross-check (PlanID + PlanHash must match). | re-derived verdict `verified*` |
| `bob-eino evidence list\|show <run-id>\|verify-chain [-json]` | Inspects the append-only evidence log: correlation groups, per-record content-safe metadata, hash-chain verification with malformed-line numbers. | listing ok / chain intact |

Shared flags on the lifecycle commands: `-config`, `-workspace`, `-model provider/model`,
`-max-steps`, `-timeout`, `-allow-writes`, `-allow-exec`, `-yes`, `-evidence-dir`, `-json`.
Only flags you explicitly set override config/env (`fs.Visit` semantics).

**Capability truth table:** `-yes` selects auto-approval for **in-plan** actions only — it grants
no capability and **structurally refuses plan variance**. `-allow-exec` without `-allow-writes`
is rejected (`config.ErrContradictoryPermissions`): a shell can always write, so the write denial
would be fiction.

## 2. The lifecycle in one session

```bash
make build
export MINIMAX_API_KEY=...        # BYOK; MiniMax-M3 is the documented default

./bob-eino doctor -net                       # preflight (exit != 0 → fix first)
./bob-eino plan -workspace ~/src/repo "add input validation to the /orders handler"
#   → plan_id: plan-…  (hashed artifact outside the workspace)

./bob-eino run -plan plan-… -workspace ~/src/repo -allow-writes -allow-exec -yes
#   → run_id: run-…, sealed receipt, model-free verdict; exit 0 only on verified*

./bob-eino verify -receipt run-…             # re-verify any time later
./bob-eino evidence verify-chain             # audit-trail integrity
```

## 3. Flat one-shot form — deprecated, kept working

The original `bob-eino [flags] <task>` form still works and now prints one stderr note pointing
at plan/run. Machine-read stdout is unchanged. Two rules:

- A task that **starts with a command word** (`run the tests`, `plan the sprint`, …) would
  dispatch as a subcommand — force the flat form with **`bob-eino -- <task>`**.
- New automation should target `plan` + `run`: the flat form has no plan artifact, no variance
  guard, no receipt, and no typed statuses.

The legacy `bob` binary alias remains a thin wrapper over the same implementation and adds its
own single deprecation line.

## 4. Typed run statuses

`run` never retries and never auto-continues. Abnormal ends seal a receipt with a typed
`final_status` and exit non-zero:

| Status | Trigger |
|---|---|
| `limit_exhausted:max_tool_calls` \| `…:max_repeated_identical` \| `…:max_consecutive_failures` | usage bounds (64 calls / 3 identical / 5 consecutive failures) |
| `plan_invalidated` | workspace HEAD moved off the plan's start SHA (pre-flight or mid-run) |
| `timeout` | `-timeout` elapsed |
| `max_steps_exhausted` | the agent loop hit `-max-steps` (`adk.ErrExceedMaxIterations`) |
| `provider_error` | the model call failed (rate limit, auth, network) |

## 5. Live smoke (operator-only, never CI)

`scripts/live-smoke.sh` runs the whole lifecycle against the real MiniMax endpoint on a scratch
repository. It is double-gated (`INTENT_BOB_EINO_LIVE_SMOKE=1` **and** `MINIMAX_API_KEY`); CI
never sets the gate, so CI honestly reports the test as skipped. There is no path to a claimed
live success without a real run.

```bash
MINIMAX_API_KEY=... ./scripts/live-smoke.sh
```

## 6. Where artifacts live

Everything sits outside every workspace, under
`$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/`:

| Artifact | Path | Integrity |
|---|---|---|
| Plans | `plans/<plan-id>.json` | content-addressed id + `content_hash`; tampering fails `plan.Load` |
| Receipts | `receipts/<run-id>.receipt.json` | canonical `content_hash`; tampering fails `receipt.Load` |
| Evidence | `evidence.jsonl` | per-record hash chain, resumed across sessions; `evidence verify-chain` |
