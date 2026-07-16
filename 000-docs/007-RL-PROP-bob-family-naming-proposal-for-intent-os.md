# 007-RL-PROP — Bob family naming proposal for Intent OS

> **PROPOSED FOR INTENT OS RATIFICATION — NOT YET ESTATE-WIDE AUTHORITY.**
> This document lives in `iam-bob-eino` and binds nothing outside this repo. It is an audit +
> recommendation packet for HQ. Until Intent OS records the decision, the only implemented scope
> is `iam-bob-eino` (docs 004/005/006).

**Date:** 2026-07-16
**Author:** Jeremy Longshore, intentsolutions.io

## 1. What HQ is being asked to decide

Adopt `intent-agent-identity/v1` (doc 005 + `schemas/intent-agent-identity.v1.schema.json`) as
the estate-wide machine-identity contract for the Bob family: persona (`bob`) is human-facing
only; every machine surface (binary, env prefix, service/unit name, telemetry `service.name`,
state dir, MC/AGP record key) uses the structured hierarchy
family → agent → runtime → implementation → **component** → role → instance → run.

## 2. Why (the two confirmed collisions)

See doc 006 for the full matrix. In short: binary `bob` is claimed by two repos
(`iam-bob-eino`, `iam-bob-intendant`) and env `BOB_*` by two repos (`iam-bob-eino`,
`iam-bob-pydantic`) with **divergent meanings** for `BOB_MODEL`. Every future surface inherits
the same collision until the persona/machine split is ratified.

## 3. Recommended sibling mappings

| Repo | `component_id` | Env prefix | Binary | Classification |
|---|---|---|---|---|
| `iam-bob-eino` | `intent-bob-eino` | `INTENT_BOB_EINO_` | `bob-eino` | agent runtime (done — this PR) |
| `iam-bob-pydantic` | `intent-bob-pydantic` | `INTENT_BOB_PYDANTIC_` | `bob-pydantic` | agent runtime (scaffold) |
| `iam-bob-adk-python` | `intent-bob-adk-python` | `INTENT_BOB_ADK_` | — | historical V1 (docs-only change) |
| `iam-bob-langgraph` | `intent-bob-langgraph` | `INTENT_BOB_LANGGRAPH_` | — | reserved V3 (name reserved now, built later) |
| `iam-bob-intendant` | `intent-bob-intendant` | `INTENT_BOB_INTENDANT_` | `bob-intendant` | **role/application on AGP — NOT a runtime** |
| `bobs-big-brain-umbrella` | `intent-bob-big-brain` | — | — | **support system, `component_type: knowledge-system` — not an agent, no runtime_id** |

## 4. Recommended migration order

1. **Ratify the contract** (this packet) — record the decision in `intent-os/decision-log/`.
2. **`iam-bob-intendant`** first (it co-claims the colliding `bob` bin): add `bob-intendant`
   bin, keep `bob` as a warned alias one release, then drop the alias only by separate decision.
3. **`iam-bob-pydantic`**: adopt `INTENT_BOB_PYDANTIC_*`, keep `BOB_*` read+warn.
4. **Historical/reserved repos** (`adk-python`, `langgraph`): README/docs mapping only.
5. **Big Brain**: classification note only (consumer, not agent) — no code change.
6. **Estate registries**: see §6.

Rollback at each step: legacy names are never removed during migration, so rollback = stop
advertising the canonical name.

## 5. Unresolved cases (HQ input needed)

- **Intendant's role id**: `intendant` is proposed as a *role* on AGP, not a runtime — confirm
  the classification and whether its records join MC via `component_id` or `agent_id`.
- **Legacy-alias sunset**: this packet proposes *retaining* all legacy names indefinitely;
  any removal date is a separate HQ decision.
- **Non-Bob agents**: whether `intent-agent-identity/v1` becomes the contract for future
  non-Bob personas (schema is persona-const today; v2 would relax `persona_id`).
- **Env prefix for adk/langgraph**: proposed above but those repos are dormant; confirm or defer.

## 6. Recommended Intent OS registry/schema changes (when ratified)

- Add the identity schema to the estate schema registry (`intent-os/schemas/` or the C8-governed
  location HQ prefers) as the canonical copy; this repo's copy then becomes a mirror.
- Mission Control projected-record contract: adopt `agent_identity` as an optional object on
  governance records (this repo's evidence already emits it), keyed for joins on
  `component_id` + `instance_id`.
- Team-atlas dossiers for the six repos: record the component ids + classifications from §3.
- Telemetry convention page: `service.namespace=intent-solutions`,
  `service.name=<component_id>`, `intent.agent.*` attributes.

## 7. Exact HQ decision required

> "Intent OS adopts `intent-agent-identity/v1` as the estate-wide Bob-family machine-identity
> contract; sibling repos migrate per §4; legacy names are retained until a separate sunset
> decision; the schema's canonical home moves to the estate registry."

Accepted / amended / rejected — record in `intent-os/decision-log/` with a D-number.

## 8. Non-claims

This packet is audit + recommendation only. It claims no estate-wide authority, performs no
sibling edits, renames no repos, and implements no identity-management/auth system.
