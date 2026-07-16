# 004-AT-DECR — Bob Eino machine identity: persona ≠ machine key

**Status:** ACCEPTED (repository-local decision — this repo only)
**Date:** 2026-07-16
**Scope:** `iam-bob-eino`. Estate-wide adoption is a separate Intent OS decision
(see `007-RL-PROP-bob-family-naming-proposal-for-intent-os.md` — proposal only, not ratified).

## Problem

"Bob" is the shared human persona of the Intent Agent Model family, and it leaked into machine
surfaces. The audit confirmed two hard collisions in the wild:

1. **Binary `bob` collides.** This repo built `go build -o bob`; `iam-bob-intendant` ships
   `package.json "bin": {"bob": …}`. Both put `bob` on the same PATH.
2. **Env `BOB_` collides.** This repo read `BOB_MODEL`; `iam-bob-pydantic` also reads `BOB_*`,
   and `BOB_MODEL` means different things in each.

Left alone, the same collision reappears in services/containers/systemd units, telemetry
`service.name`, state dirs, and future Mission Control / AGP records.

## Decision

Separate the persona from a **structured, typed, deterministic machine identity**, owned by one
package (`internal/identity`) and one JSON Schema (`schemas/intent-agent-identity.v1.schema.json`,
`schema_version: intent-agent-identity/v1`):

| Field | Value (this repo) | Meaning |
|---|---|---|
| `family_id` | `intent-agent-model` | model/design family (**not** an IAM/auth system) |
| `persona_id` | `bob` | shared persona — human-facing only |
| `agent_id` | `intent-agent-model/bob` | stable across compatible Bob runtimes |
| `runtime_id` | `eino-go` | implementation technology |
| `implementation_id` | `iam-bob-eino` | codebase |
| `component_id` | `intent-bob-eino` | canonical operational component (binary/service/telemetry/state) |
| `role_id` | `coding` (default) | what this run does — separate from persona |
| `instance_id` | `intent-bob-eino:<env>:<opaque>` | one running copy |
| `run_id` | `run-<opaque>` | one operation |

Concrete canonical surfaces in this repo:

- **Binary:** `bob-eino` (`cmd/bob-eino`). Legacy `bob` kept as a tested one-warning compat
  alias (`cmd/bob`); both are thin wrappers over `internal/cli` and cannot drift.
- **Env:** `INTENT_BOB_EINO_*` canonical; legacy `BOB_*` still read with a once-per-process
  deprecation warning that never prints values. On this branch only `INTENT_BOB_EINO_MODEL` /
  `BOB_MODEL` exist; the full 12-var namespace lands with `internal/config` in the MiniMax
  lifecycle rebase.
- **State:** `$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/` canonical; legacy
  `$XDG_STATE_HOME/iam-bob-eino/` still readable, hash-verified before any optional copy,
  never destructively migrated.
- **Evidence:** the `Record` carries a structured `agent_identity` object (evidence schema v2)
  inside the hash chain; legacy v1 records still parse and self-verify.
- **Telemetry contract (naming only, no backend):** `service.namespace=intent-solutions`,
  `service.name=intent-bob-eino`, `intent.agent.*` attributes. `service.name` is never `bob`.
- **Eino agent name:** `intent-bob-eino` (was `bob`). Persona prose ("You are Bob") unchanged.

## Alternatives considered

- **Keep `bob` and disambiguate by install location** — rejected: PATH ordering is not identity;
  collisions recur on every shared machine and in telemetry.
- **Rename the repos** — rejected for this slice: repo renames are estate-wide actions requiring
  HQ sign-off; the implementation id `iam-bob-eino` is not itself colliding.
- **One flat name (`intent-bob-eino`) with no structure** — rejected: the family needs typed
  fields (runtime vs role vs instance) so Mission Control / AGP records can join on the right key.

## Compatibility policy

Legacy names are **retained, warned, and tested** — never removed in this slice. Removal (if
ever) is a separate decision after estate ratification.

## Consequences

- No other package may build identity strings by hand; `identity.New` is the single creation path
  (validated: lowercase machine ids, instance prefix, persona guard, bare-persona-as-component
  rejected).
- `internal/version` renamed the misleading `Bob` version const to `AgentVersion`, and gained
  `Component`, `Runtime`, `IdentitySchemaVersion`, `EvidenceSchemaVersion`.
- The MiniMax task-lifecycle branch rebases onto this and applies the identity to
  `internal/config` and `internal/receipt` (receipt identity + hash) — no second implementation.

## Non-claims

No estate-wide authority. No Intent OS ratification. No identity-management/auth/cloud-permission
features. No AGP/MC/Big-Brain integration. No deploy/systemd/container/telemetry backend.

— Jeremy Longshore, intentsolutions.io
