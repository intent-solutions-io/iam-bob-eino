# iam-bob-eino — Go / Eino runtime of the Intent Agent Model

> **Intent Agent Model (IAM)** — *not* Identity and Access Management.
> Bob is the **reference implementation family** for IAM. These repos are different **runtimes** of the same model, not separate products.
>
> | Repo | Runtime | Status |
> |------|---------|--------|
> | [`iam-bob-adk-python`](https://github.com/jeremylongshore/iam-bob-adk-python) | Google ADK | Historical V1 |
> | [`iam-bob-pydantic`](https://github.com/jeremylongshore/iam-bob-pydantic) | Pydantic AI + LiteLLM (BYOK, MCP) | Scaffold V2 |
> | [`iam-bob-langgraph`](https://github.com/jeremylongshore/iam-bob-langgraph) | LangGraph | Reserved (not built) |
> | [`iam-bob-intendant`](https://github.com/jeremylongshore/iam-bob-intendant) | Operational worker (AGP-composed) | Live automation |
> | **`iam-bob-eino`** | **Go / [CloudWeGo Eino](https://github.com/cloudwego/eino)** | **Vertical slice — governed local coding agent** |

`iam-bob-eino` is a **governed local coding agent** written in Go on the CloudWeGo **Eino**
agent framework. Eino supplies the agent machinery (the ReAct loop, tool dispatch, streaming);
Bob supplies the specialization: a coding persona, a policy + approval boundary over every tool
call, independent outcome verification, a Mission-Control-compatible evidence record, a
provider-neutral BYOK model boundary (zero Google by default), and narrow integration seams to
the rest of the Intent Solutions estate.

It is **not** a new agent framework, a multi-agent swarm, an identity system, or a
reimplementation of Eino / AGP / Mission Control / Big Brain. Those are consumed through seams.

## Naming — persona vs machine identity

**"Bob" is the human-facing persona, never a machine key.** Machine surfaces use the structured
identity contract (`internal/identity`, schema `schemas/intent-agent-identity.v1.schema.json`;
decision: [`000-docs/004-AT-DECR-bob-eino-machine-identity.md`](000-docs/004-AT-DECR-bob-eino-machine-identity.md)):

| Level | Value | Meaning |
|---|---|---|
| family | `intent-agent-model` | the IAM project family (**not** Identity & Access Management) |
| persona | `bob` | shared human-facing persona across runtimes |
| agent | `intent-agent-model/bob` | stable across compatible Bob runtimes |
| runtime | `eino-go` | this implementation technology |
| implementation | `iam-bob-eino` | this codebase |
| component | `intent-bob-eino` | canonical operational name (binary/service/telemetry/state) |

- **Canonical binary: `bob-eino`** (`cmd/bob-eino`). **`bob`** remains a tested compatibility
  alias that prints one deprecation line to stderr (`cmd/bob`); both share `internal/cli`.
- **Canonical env namespace: `INTENT_BOB_EINO_*`** (legacy `BOB_*` still read, warned once).
- **Canonical state: `$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/`** (legacy
  `iam-bob-eino/` still readable; never destructively migrated).

## What Bob does (this slice)

Bob runs a governed **plan → run → verify** task lifecycle over a workspace. Planning is
read-only by construction; execution is bound to the sealed plan by a variance guard; the outcome
is judged by a model-free verifier and sealed into a tamper-evident receipt. All model access is
MiniMax-first BYOK (`minimax/MiniMax-M3` is the documented default; any OpenAI-compatible
provider in the registry works; zero Google).

| Command | What it does |
|---|---|
| `bob-eino version` | identity, ldflags build provenance, Go/Eino/schema versions (`-json`) |
| `bob-eino doctor [-net]` | 14 stable-named preflight checks; boolean-only credential checks; non-zero exit on required failures |
| `bob-eino plan <task>` | read-only-tooled model run → hashed, validated plan artifact saved outside the workspace |
| `bob-eino run -plan <id>` | pre-flight gates → governed execution under the plan-variance guard + usage limits → sealed, independently verified receipt |
| `bob-eino verify -receipt <run-id>` | post-hoc re-verification: tamper-rejecting receipt load, evidence re-check, live git end-SHA |
| `bob-eino evidence list\|show\|verify-chain` | inspect and chain-verify the append-only evidence log |

The agent acts **only through governed tools**:

| Tool | Risk | Governance |
|---|---|---|
| `read_file`, `list_dir` | R0 | always allowed, read-only |
| `search_code` | R1 | always allowed, read-only |
| `run_command` | R2 | allowlisted programs only, **shell-free**, requires exec enabled + approval |
| `write_file` | R3 | requires writes enabled **and** approval; independently re-read and hash-verified |
| `apply_patch` | R3 | `intent-bob-eino-patch/v1`: literal find/replace hunks with verified pre-hashes and exact occurrence counts; two-phase atomic with rollback |

Every call — allowed, denied, executed, or failed — emits one content-safe evidence record
**carrying the full structured agent identity** (`agent_identity`, schema
`intent-agent-identity/v1`, bound inside the hash chain — tampering the identity breaks the
chain) to an append-only log, shaped to project into Mission Control. Run receipts carry the
same structured identity inside their sealed content hash. During a `run`, actions
outside the plan escalate to a **PLAN VARIANCE** approval that `-yes` structurally refuses.

## Quick start — the lifecycle (BYOK, zero GCP)

```bash
make build                        # canonical bob-eino binary, ldflags build metadata
export MINIMAX_API_KEY=...        # BYOK (or any registry provider's key)

./bob-eino doctor -net            # preflight the environment
./bob-eino plan -workspace ~/src/repo "add input validation to the /orders handler"
#   → plan_id: plan-…            (hashed artifact, saved outside the workspace)
./bob-eino run -plan plan-… -workspace ~/src/repo -allow-writes -allow-exec -yes
#   → run_id: run-…, sealed receipt; exit 0 only when the model-free verifier says verified
./bob-eino verify -receipt run-…  # re-verify any time later
./bob-eino evidence verify-chain  # audit-trail integrity
```

The original flat one-shot form (`./bob-eino -workspace . "task"`) still works with a stderr
deprecation note; `bob-eino -- <task>` forces it when a task starts with a command word. Details
and migration guidance:
[`000-docs/009-DR-GUID-cli-subcommands-and-migration.md`](000-docs/009-DR-GUID-cli-subcommands-and-migration.md).
`make build-legacy` builds the deprecated `bob` alias (same implementation, one stderr warning).

Evidence is written OUTSIDE the workspace to
`$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/evidence.jsonl` (default
`~/.local/state/...`), so the audited agent cannot reach its own audit trail;
override with `-evidence <path>`.

## Architecture

```
cmd/bob-eino       canonical CLI entry point (thin wrapper over internal/cli)
cmd/bob            legacy compatibility alias (one deprecation line, same internal/cli)
internal/
  cli              the single CLI implementation: subcommand dispatch (version/doctor/plan/run/verify/evidence), shared flags, deprecated flat fallback, state paths
  config           typed configuration (INTENT_BOB_EINO_* namespace, JSON file, precedence merge, typed validation errors)
  identity         structured machine identity (family/persona/agent/runtime/component/role/instance/run) — the single creation path
  version          build identity + ldflags-injected commit/date + engine introspection
  agent            persona + wires Bob onto Eino's adk.ChatModelAgent + Runner
  provider         provider-neutral BYOK model boundary (OpenAI-compatible; MiniMax-first; zero Google) + offline model stub for tests
  governor         the single control point: limits → policy → plan guard → approval → execution seam → evidence (one record per action)
  policy           R0–R4 risk model + deterministic policy decision + command allowlist
  approval         human-in-the-loop authorization (auto / deny-by-default / prompt; variance-aware — auto refuses out-of-plan actions)
  plan             hashed, validated read-only plan artifacts (content-addressed ids, tamper-rejecting Load)
  planguard        the plan-variance guard (listed → allow; unlisted → variance approval; HEAD drift → plan invalidated)
  limits           per-run usage bounds (tool-call budget, repeated-identical, consecutive-failure) with typed cancel causes
  patch            intent-bob-eino-patch/v1 (literal hunks, verified pre-hashes, exact occurrence counts, two-phase atomic apply)
  gitstate         read-only git state (HEAD, branch, dirty, changed files) with clean degradation
  tools            governed typed tools (read_file, list_dir, search_code, run_command, write_file, apply_patch) + the read-only planning set
  verify           independent outcome verification (re-read + hash writes; inspect command exit)
  runverify        model-free run-level verifier (evidence chain, workspace/git identity, plan conformance, acceptance exit codes, secret scan)
  receipt          sealed run receipts (canonical content hash, tamper-rejecting Load) + evidence-log loader
  doctor           preflight checks with stable machine names and injectable dependencies
  evidence         Mission-Control-compatible evidence record + append-only JSONL sink (session-spanning hash chain) + redaction
  workspace        root confinement (no path escapes)
  seams            BigBrain / AGP / Mission Control integration boundaries (interfaces + safe local no-ops)
```

**Governance flows through one boundary.** Every tool routes through `internal/governor`; no tool
performs a side effect without a passing `Authorize()`, and evidence is emitted on every path.

## Status

This is the **first useful vertical slice**, not a finished product. The governed coding-agent
loop, the policy/approval/evidence boundaries, and the provider-neutral model boundary are real
and tested. The estate integrations (Big Brain knowledge, AGP execution, Mission Control
projection) are **seams** with local no-op defaults; how Bob consumes the Intent Eval Platform
governance kernel from Go is an open architectural decision recorded in
[`000-docs/001-AT-DECR-eino-runtime-decision.md`](000-docs/001-AT-DECR-eino-runtime-decision.md).

See [`SECURITY.md`](SECURITY.md) for the threat model and residual risks, and
[`CONTRIBUTING.md`](CONTRIBUTING.md) to build and test.

## License

Apache-2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE). Bob builds on
CloudWeGo Eino and eino-ext (both Apache-2.0); matching that license keeps the
runtime consistent with the SDK/ADK it depends on.
