# 001-AT-DECR — Bob on Go / Eino: a governed local coding agent

- **Status:** ACCEPTED (vertical-slice scope)
- **Type:** Architecture Decision Record (AT-DECR)
- **Family:** Intent Agent Model (IAM) — *not* Identity and Access Management
- **Sibling reference:** `iam-bob-pydantic` (V2, Pydantic AI); `iam-bob-adk-python` (V1, frozen)

## Context

The Intent Agent Model (IAM) — "Bob" — is one agent identity implemented as
different **runtimes** of the same model. `iam-bob-eino` is the **Go / CloudWeGo
Eino** runtime. The owner's directive for this slice: build a **useful local
coding agent** like the original ADK Bob, on the **Eino ADK** framework, and get
the repository dressing (governance, contribution, CI) right.

Model portability is a commodity; the moat is governance / identity / provenance.
So this slice invests in the *governance boundaries around* the agent, not in the
agent loop itself — Eino supplies the loop.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Runtime = Go + CloudWeGo Eino** (`eino` v0.9.12 + `eino-ext`). | Native Go, single static binary, no Python/Vertex runtime. Eino's ADK (`ChatModelAgent`, `Runner`, tools, callbacks) is the agent machinery. |
| D2 | **Bob specializes Eino; it is not a new framework.** | Bob supplies persona, governed tools, policy/approval/verification/evidence, and integration seams. Eino owns the ReAct loop, tool dispatch, and streaming. |
| D3 | **This slice is a local coding agent** (read/list/search/run/write) with one governed tool set, not a multi-agent swarm, DeepAgent, or workflow engine. | "First useful vertical slice." Keep scope honest. |
| D4 | **Interactive runtime, not headless.** | Per intent-os D88 (`intent-os/000-docs/086` §4.5): the headless watcher `iam-bob-intendant` "may participate as an application, but is not the default harness." The default IAM Bob is the interactive reference runtime. |
| D5 | **Provider-neutral, BYOK, zero Google by default.** | One OpenAI-compatible gateway (`internal/provider`); Google is not in the registry and selecting it is an explicit error. Inherits the V2 thesis; no GCP anywhere. |
| D6 | **Governance flows through one boundary.** | `internal/governor` is the single control point; every side effect is policy-checked, approval-gated, verified, and recorded. No tool bypasses it. |
| D7 | **Kernel consumption is DEFERRED.** | How a Go runtime consumes the IEP `@intentsolutions/core` governance kernel (no Go path; JSON Schema vs. sidecar vs. AGP) is a serious architectural decision the owner is taking to HQ. This slice builds the evidence record shape to be MC-projectable and leaves the kernel/signing wiring behind the seams. |

## Preserved tension (recorded, not silently resolved)

The ratified `iam-bob-pydantic` decision (with Karpathy's dissent) abandoned the
ADK "8-specialist department" as a dated pattern, collapsing to "one agent +
tools." This slice deliberately builds the **single-agent + governed-tools**
shape (D3), consistent with that ratified direction, even though the owner framed
it as "like the original ADK Bob." A future multi-agent department on Eino ADK
(Supervisor / SetSubAgents) remains possible but should be a separate, explicit
decision — flagged here for HQ/council confirmation.

## Consequences

- A public `iam-bob-eino` repo with governed coding tools, tested end-to-end with
  a fake model (no network) and hardened against the security review (shell-free
  exec, symlink-safe I/O via `os.Root`, out-of-workspace hash-chained evidence,
  redaction, secret-file refusal).
- Estate integrations (Big Brain, AGP, Mission Control) are **seams** with local
  no-op defaults; real adapters land after the kernel-consumption decision (D7).
- Operating rules: `002-DR-STND-operating-rules.md`.
