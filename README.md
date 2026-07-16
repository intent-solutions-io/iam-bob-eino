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

## What Bob does (this slice)

Point Bob at a workspace and give it a task. It inspects the repository, reasons about the work,
and acts **only through governed tools**:

| Tool | Risk | Governance |
|---|---|---|
| `read_file`, `list_dir` | R0 | always allowed, read-only |
| `search_code` | R1 | always allowed, read-only |
| `run_command` | R2 | allowlisted programs only, **shell-free**, requires approval |
| `write_file` | R3 | requires writes enabled **and** approval; the write is independently re-read and hash-verified |

Every call — allowed, denied, executed, or failed — emits one content-safe evidence record
(no secrets, no file contents; only hashes, workspace-relative paths, and short summaries) to an
append-only log, shaped to project into Mission Control.

## Quick start (BYOK, zero GCP)

```bash
go build -o bob ./cmd/bob

# Bring your own key for any non-Google provider:
export DEEPSEEK_API_KEY=...        # or OPENAI_API_KEY / GROQ_API_KEY / ZHIPU_API_KEY, or run Ollama locally

# Read-only by default:
./bob -workspace . "list the Go files and describe the governance model"

# Allow writes (still gated by an approval prompt):
./bob -workspace . -allow-writes "add a doc comment to internal/verify/verify.go"

# Non-interactive, pre-authorized:
./bob -workspace . -allow-writes -yes -model deepseek/deepseek-chat "run the tests"
```

Evidence is written to `<workspace>/.bob/evidence.jsonl`.

## Architecture

```
cmd/bob            CLI surface (flags → workspace, policy, approver, evidence, model → agent)
internal/
  agent            persona + wires Bob onto Eino's adk.ChatModelAgent + Runner
  provider         provider-neutral BYOK model boundary (OpenAI-compatible; zero Google) + fake model for tests
  governor         the single control point: policy → approval → execution seam → evidence (one record per action)
  policy           R0–R4 risk model + deterministic policy decision + command allowlist
  approval         human-in-the-loop authorization (auto / deny-by-default / interactive prompt)
  tools            governed typed tools (read_file, list_dir, search_code, run_command, write_file)
  verify           independent outcome verification (re-read + hash writes; inspect command exit)
  evidence         Mission-Control-compatible evidence record + append-only JSONL sink + redaction
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

Intent Solutions Proprietary. See [`LICENSE`](LICENSE).
