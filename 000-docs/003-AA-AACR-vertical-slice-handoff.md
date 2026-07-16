# 003-AA-AACR — Vertical-slice build + review handoff

- **Kind:** AA-AACR (after-action / handoff) · **Scope:** first vertical slice of `iam-bob-eino`
- **Branch:** `feat/governed-coding-agent-vertical-slice` · **PR:** intent-solutions-io/iam-bob-eino#1

## What shipped

The Go / CloudWeGo-Eino runtime of the Intent Agent Model as a **governed local
coding agent**: Eino's ADK supplies the loop; Bob supplies persona, five governed
tools, an R0–R4 policy boundary, an approval boundary, independent outcome
verification, a Mission-Control-projectable **hash-chained** evidence record, a
provider-neutral BYOK model boundary (zero Google), and BigBrain/AGP/MC seams.

~2,750 LOC across `cmd/bob` + eleven `internal/*` packages, 12 test files.

## How it was built and verified

- API grounded against real `eino@v0.9.12` + `eino-ext` (via `go doc` + a research
  agent), not assumed from docs.
- `go build`/`go vet` clean; `go test -race ./...` green. Coverage: evidence 92%,
  verify 88%, agent 85%, governor 83%, policy 76%, tools 63%, workspace 57%.
- Two independent adversarial reviews (security + correctness) run as subagents.
  All criticals/highs fixed **before** the PR opened: shell-injection (shell-free
  exec + metachar/dangerous-flag rejection), symlink escape (`os.Root`), opaque
  approval (full command/content shown), tamperable evidence (moved out of the
  workspace + sha256 chain), redaction on every egress, secret-file refusal,
  bounded I/O, and the two previously-untested core packages (`governor`,
  `verify`) now covered.

## Decisions of record

- `000-docs/001-AT-DECR` — Go/Eino runtime; single-agent+governed-tools shape;
  interactive (not headless — headless is `iam-bob-intendant`'s role per intent-os
  D88); **kernel consumption deferred to HQ**.
- `000-docs/002-DR-STND` — operating rules P1–P8.
- `SECURITY.md` — threat model + accepted residual risks.

## Open decisions (need HQ / owner)

1. **IEP kernel consumption from Go** (`@intentsolutions/core` has no Go path):
   in-process JSON-Schema validation vs. Python sidecar vs. AGP — architectural,
   taken to HQ. Blocks the kernel/AGP/signing wiring behind `internal/seams`.
2. **Repo license posture** — matched the direct sibling `iam-bob-pydantic`
   (Intent Solutions Proprietary) on a public repo; confirm vs. Apache-2.0 (which
   `iam-bob-intendant` uses).
3. **Single-agent vs. Eino-ADK department** — built single-agent per the ratified
   pydantic decision; a department is a separate explicit call.

## Follow-up work (not done this slice)

- Beads spine (`.beads/` + `bd-sync` three-layer mirror) — repo has none yet.
- `@intentsolutions/audit-harness` 7-layer install (Go curl path) + `TEST_AUDIT.md`
  via `/audit-tests`.
- Live-LLM demo run (`make run-local` with a BYOK key).
- A real BigBrain MCP adapter behind the existing seam.
- **Upstream contribution** to `cloudwego/eino-examples`: a fake-`ChatModel`
  deterministic-agent-testing example, issue-first (drafted; route via
  `/contribute`). CLA status unconfirmed — needs owner.

## Residual security posture (accepted)

`run_command` runs project-defined code by design (control = approval with the
full command shown, not sandboxing); read content egresses to the BYOK provider;
env is not fully scrubbed. For untrusted workspaces, run Bob in a container/VM.
A real sandbox (bubblewrap/nsjail) via the AGP seam is the future hardening.
