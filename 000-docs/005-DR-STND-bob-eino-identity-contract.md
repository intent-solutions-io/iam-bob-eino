# 005-DR-STND — Bob Eino identity contract (intent-agent-identity/v1)

**Status:** Active standard for `iam-bob-eino` (repository-local; the estate-wide version is a
proposal, see doc 007).
**Machine definition:** `schemas/intent-agent-identity.v1.schema.json` (JSON Schema draft-07,
closed — `additionalProperties: false`).
**Reference implementation:** `internal/identity` (the single creation path; equivalence is
test-asserted by `TestSchemaEquivalence`).

## 1. Field definitions

| Field | JSON name | Required | Pattern | Notes |
|---|---|---|---|---|
| Schema version | `schema_version` | yes | const `intent-agent-identity/v1` | unknown versions MUST be rejected |
| Family | `family_id` | yes | kebab | `intent-agent-model` — the project family. NOT an IAM/auth system |
| Persona | `persona_id` | yes | const `bob` | human-facing only; never a machine key |
| Agent | `agent_id` | yes | kebab(/kebab)+ | `family_id/persona_id`; stable across compatible runtimes |
| Runtime | `runtime_id` | yes | kebab | implementation technology (`eino-go`) |
| Implementation | `implementation_id` | yes | kebab | codebase (`iam-bob-eino`) |
| Component | `component_id` | yes | kebab, ≠ `bob` | canonical operational name (`intent-bob-eino`) for binary/service/telemetry/state |
| Role | `role_id` | yes | kebab | what the run does (`coding` default); orthogonal to persona |
| Instance | `instance_id` | yes | `component:env:opaque` | one running copy; prefix MUST equal `component_id` |
| Run | `run_id` | no | `run-<opaque>` | one operation; MUST differ from `instance_id` |
| Version | `version` | yes | non-empty | application semver of the implementation |

**kebab** = `^[a-z0-9]+(-[a-z0-9]+)*$`: lowercase ASCII letters/digits with single hyphens;
no uppercase, no spaces, no leading/trailing separators. `agent_id` additionally allows `/`
between kebab segments.

## 2. Construction and validation rules

1. `identity.New(role, env, version)` is the **only** constructor. No package builds identity
   strings by hand.
2. `Validate()` enforces (typed sentinels): supported schema version; kebab shape on all machine
   ids; `persona_id == "bob"`; `component_id != "bob"` (bare persona as machine key is an error);
   instance prefix = component; `run_id != instance_id`; non-empty version.
3. The opaque instance/run suffix is a ULID-style value (time + `crypto/rand`) and MUST NOT be
   parsed for meaning.
4. `Canonical()` is the deterministic JSON byte serialization (struct declaration order); it is
   what hash chains bind. `Equal` compares canonical bytes.

## 3. Versioning rules

- The identity contract version lives **in the payload** (`schema_version`). Additive-only
  evolution: new optional fields → still `v1`; any change to required fields, patterns, or
  semantics → `intent-agent-identity/v2` and a new schema file (the v1 file is immutable).
- `internal/version.IdentitySchemaVersion` mirrors the constant and is test-asserted equal.
- Evidence records embed the identity; the evidence contract has its own version
  (`version.EvidenceSchemaVersion`, `intent-bob-eino-evidence/v2`). Do not overload one version
  field for both contracts.

## 4. Surface bindings (this repo)

| Surface | Canonical | Legacy (retained + warned) |
|---|---|---|
| Binary | `bob-eino` | `bob` (one stderr deprecation line/process) |
| Env namespace | `INTENT_BOB_EINO_*` | `BOB_*` (read, warned once, values never printed) |
| Model selection precedence | CLI flag → `INTENT_BOB_EINO_MODEL` → `BOB_MODEL` (warn) → default | — |
| State dir | `$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/` | `$XDG_STATE_HOME/iam-bob-eino/` (read-only discovery; hash-verified optional copy; never moved/deleted) |
| Evidence | `agent_identity` object in every record (in-chain) | v1 records without the field parse + verify |
| Telemetry (contract only) | `service.name=intent-bob-eino`, `service.namespace=intent-solutions`, `intent.agent.*` | — (`service.name` must never be `bob`) |
| Eino agent name | `intent-bob-eino` | — |

Provider-native variables (`DEEPSEEK_API_KEY`, `MINIMAX_*`, …) are NOT part of this namespace
and are unchanged.

## 5. Prohibitions

- Never use bare `bob` as: binary name, service/container/systemd unit name, telemetry
  `service.name`, env prefix, state dir leaf, or agent machine name.
- Never mint identity strings outside `internal/identity`.
- Never write the phrase "fake model" — the offline test double is the "deterministic Eino
  model fixture" / "offline model stub".

— Jeremy Longshore, intentsolutions.io
