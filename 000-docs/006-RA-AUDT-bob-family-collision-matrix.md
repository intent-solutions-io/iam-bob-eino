# 006-RA-AUDT — Bob family cross-repo machine-identity collision matrix

**Status:** Audit record (source of truth for the collisions this slice fixes).
**Date:** 2026-07-16
**Executable form:** the matrix below is encoded as fixtures in
`internal/identity/identity_test.go` (`preContractFamily` / `contractFamily`) and checked by
`DetectCollisions` — the pre-contract family must reproduce exactly the collisions found; the
target family must be collision-free.

## Confirmed collisions (pre-contract, in the wild)

| # | Kind | Value | Claimants | Evidence |
|---|---|---|---|---|
| 1 | binary on PATH | `bob` | `iam-bob-eino` (`go build -o bob`), `iam-bob-intendant` (`package.json "bin": {"bob": …}`) | both install a `bob` executable; last-write-wins on shared machines |
| 2 | env namespace | `BOB_*` | `iam-bob-eino` (`BOB_MODEL`), `iam-bob-pydantic` (`BOB_*`) | `BOB_MODEL` means a *different thing* in each runtime — silent cross-configuration |

Latent (not yet colliding, same root cause): service/container/systemd names, telemetry
`service.name`, state dirs (`~/.local/state/...`), and future Mission Control / AGP record keys
would all have received bare `bob`.

## Family matrix (audit + target state)

| Repo | Runtime (`runtime_id`) | Persona / `agent_id` | `component_id` (target) | Role | Classification | Status |
|---|---|---|---|---|---|---|
| `iam-bob-eino` | `eino-go` | `intent-agent-model/bob` | `intent-bob-eino` | `coding` | agent runtime | active (this PR implements the contract) |
| `iam-bob-pydantic` | `pydantic-python` | `intent-agent-model/bob` | `intent-bob-pydantic` | (runtime default) | agent runtime | scaffold |
| `iam-bob-adk-python` | `adk-python` | `intent-agent-model/bob` | `intent-bob-adk-python` | (runtime default) | agent runtime | historical V1 |
| `iam-bob-langgraph` | `langgraph-python` | `intent-agent-model/bob` | `intent-bob-langgraph` | — | agent runtime | reserved V3 (not built) |
| `iam-bob-intendant` | — (AGP-composed, bun/ts) | `intent-agent-model/bob` | `intent-bob-intendant` | `intendant` | **agent application (role on AGP) — NOT a runtime** | live |
| `bobs-big-brain-umbrella` | — (knowledge) | consumer, not an agent | `intent-bob-big-brain` | — | **support system (`component_type: knowledge-system`) — NOT a runtime** | active |

Classification guards are executable: `TestFamilyClassificationGuards` asserts Big Brain never
claims a `runtime_id` and Intendant is an application, not a runtime.

## What this repo changed (the only repo edited)

- binary `bob` → canonical `bob-eino` (legacy alias retained, deprecation-warned, tested)
- env `BOB_MODEL` → canonical `INTENT_BOB_EINO_MODEL` (legacy read + warned, value never printed)
- state `iam-bob-eino/` → canonical `intent-solutions/agents/bob/eino-go/` (legacy readable)
- Eino agent name `bob` → `intent-bob-eino`
- evidence records now carry the structured `agent_identity` (in the hash chain)

Sibling repos are **not** modified here — their target `component_id`s above are the
recommendation carried by doc 007 (proposal for Intent OS ratification).

— Jeremy Longshore, intentsolutions.io
