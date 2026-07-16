# SESSION HANDOFF — Bob family naming and identity (canonical machine-identity slice)

**Repo:** `iam-bob-eino` · **Branch:** `feat/canonical-bob-eino-identity` · **Date:** 2026-07-16
**Companion docs:** 004 (decision) · 005 (contract) · 006 (collision matrix) · 007 (Intent OS proposal) · 008 (migration).

The 49 handoff items:

1. **Starting main SHA:** `7a54ff323b56879104729778b94bb3b21c3dc519`
2. **Ending branch SHA:** _updated at PR open — see PR head_
3. **Ending main SHA when merged:** _pending merge_
4. **PR number:** _pending — filled at PR open_
5. **CI result:** _pending — gofmt/vet/build/test -race/coverage/govulncheck must pass_
6. **Review findings:** _pending AI review lanes_
7. **Review-thread status:** _pending_
8. **Repositories inspected:** `iam-bob-eino` (edited); audit references (read-only, from the
   prior audit): `iam-bob-pydantic`, `iam-bob-adk-python`, `iam-bob-langgraph`,
   `iam-bob-intendant`, `bobs-big-brain-umbrella`. **No sibling repo modified.**
9. **Naming collisions found:** (a) binary `bob` — `iam-bob-eino` (`go build -o bob`) vs
   `iam-bob-intendant` (`"bin":{"bob":…}`); (b) env `BOB_` — `iam-bob-eino` vs
   `iam-bob-pydantic`, with `BOB_MODEL` meaning different things. See doc 006.
10. **Family ID:** `intent-agent-model`
11. **Persona ID:** `bob` (human-facing only)
12. **Stable agent ID:** `intent-agent-model/bob`
13. **Runtime ID:** `eino-go`
14. **Implementation ID:** `iam-bob-eino`
15. **Component ID:** `intent-bob-eino`
16. **Default role ID:** `coding`
17. **Instance-ID format:** `intent-bob-eino:<env>:<opaque-ulid>` (26-char lowercase, time +
    crypto/rand; opaque — never parsed for meaning)
18. **Run-ID format:** `run-<opaque-ulid>`; shape-distinct from instance ids; must differ from
    `instance_id` (validated)
19. **Canonical binary:** `bob-eino` (`cmd/bob-eino`, `make build`)
20. **Legacy binary behavior:** `bob` (`cmd/bob`, `make build-legacy`) — identical
    `internal/cli` implementation; exactly one stderr deprecation line per process; stdout
    byte-identical (test-asserted)
21. **Canonical environment prefix:** `INTENT_BOB_EINO_*`
22. **Legacy environment behavior:** `BOB_MODEL` still read when canonical unset; warned once
    per process; value never printed
23. **Configuration precedence:** CLI flag → `INTENT_BOB_EINO_MODEL` → `BOB_MODEL` (warn) →
    default `deepseek/deepseek-chat`
24. **Canonical config path:** none on main yet — `internal/config` (and the full 12-var
    namespace) lands in the MiniMax rebase; documented in docs 005/008
25. **Canonical state path:** `$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/`
26. **Legacy-state behavior:** `$XDG_STATE_HOME/iam-bob-eino/` discovered for reads;
    hash-verified (VerifyChain) before an optional byte-identical copy to canonical; broken
    chains NOT copied (kept for forensics); never moved/deleted; idempotent
27. **Identity schema:** `schemas/intent-agent-identity.v1.schema.json` (draft-07, closed,
    `schema_version: intent-agent-identity/v1`); Go-struct equivalence test-asserted
28. **Receipt-schema change:** none on main (no receipts yet) — receipt `agent_identity` +
    hash binding land in the MiniMax rebase
29. **Evidence-schema change:** `Record.AgentIdentity *identity.AgentIdentity`
    (`agent_identity`, omitempty) — evidence schema v2 (`version.EvidenceSchemaVersion`);
    inside `json.Marshal`, so `chain()`/`VerifyChain` bind it automatically
30. **Legacy receipt proof:** n/a on main (no receipts exist to prove)
31. **Legacy evidence proof:** `TestLegacyRecordStillParsesAndVerifies` — v1 records (no field)
    round-trip under the v2 struct, emit no `agent_identity` key, and the chain verifies
32. **Telemetry naming contract:** `ResourceAttributes()` — `service.namespace=intent-solutions`,
    `service.name=intent-bob-eino` (never `bob`, test-asserted), `service.version`,
    `service.instance.id`, `intent.agent.{family_id,persona_id,agent_id,runtime_id,impl_id,role_id,schema}`.
    Contract only; no telemetry backend wired
33. **Eino mapping:** runtime `eino-go`, component `intent-bob-eino`, role `coding` — active
    (this PR)
34. **Pydantic mapping:** runtime `pydantic-python`, component `intent-bob-pydantic` — scaffold
    (proposal only)
35. **ADK mapping:** runtime `adk-python`, component `intent-bob-adk-python` — historical V1
    (proposal only)
36. **LangGraph mapping:** runtime `langgraph-python`, component `intent-bob-langgraph` —
    reserved V3 (proposal only)
37. **Intendant classification:** agent **application/role** composed on AGP (role `intendant`,
    component `intent-bob-intendant`) — **NOT a runtime**; guard test asserts no `runtime_id`
38. **Big Brain classification:** support system (`component_type: knowledge-system`, component
    `intent-bob-big-brain`) — a consumer, not an agent; guard test asserts no `runtime_id`
39. **Cross-family proposal location:**
    `000-docs/007-RL-PROP-bob-family-naming-proposal-for-intent-os.md` (written HERE, not into
    Intent OS)
40. **Intent OS ratification still required:** YES — the proposal is explicitly marked
    "PROPOSED FOR INTENT OS RATIFICATION — NOT YET ESTATE-WIDE AUTHORITY"; §7 carries the exact
    HQ decision text
41. **Test count:** 108 passing test cases (incl. subtests) across 13 packages at handoff
    authoring; all pre-existing governed-tool + offline-model-fixture tests kept green
42. **Race-test result:** `go test -race ./...` — all packages ok
43. **Static-analysis result:** `gofmt -l` clean; `go vet ./...` clean
44. **Sensitive-data scan:** warnings print no env values (test-asserted:
    `TestResolveLegacyEnvFallbackWarnsOnceWithoutValue`); evidence redaction tests unchanged
    and green; no secrets in code/docs
45. **Breaking changes:** none — legacy binary, env var, state path, and v1 evidence records
    all still work
46. **Compatibility behavior:** legacy names retained + warned + tested (see items 20/22/26/31);
    removal is explicitly a separate future decision
47. **Remaining uncertainties:** intendant role classification confirmation; legacy-alias
    sunset policy; env prefixes for dormant repos; whether v2 of the identity schema relaxes
    `persona_id` for non-Bob personas (all listed in doc 007 §5)
48. **Explicit non-claims:** no estate-wide naming authority; no Intent OS ratification; no
    identity-management/authentication/authorization functionality; no AGP / Mission Control /
    Big Brain integration; no production deployment; no multi-agent operation; no
    repository-family migration completion; no sibling-repo compliance; no removal of legacy
    names; no external service adoption
49. **Exact next recommended implementation slice:** rebase `feat/minimax-task-lifecycle`
    (@ `ec686b1`) onto merged main; apply `internal/identity` to `internal/config` (full
    `INTENT_BOB_EINO_*` 12-var namespace + precedence + legacy warnings) and
    `internal/receipt` (`agent_identity` in the run receipt + receipt hash); then resume the
    MiniMax plan/run/verify build — no second MiniMax implementation from scratch

— Jeremy Longshore, intentsolutions.io
