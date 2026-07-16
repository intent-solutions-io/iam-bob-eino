# 008-DR-GUID — bob-eino naming migration guide (old → new)

**Audience:** anyone with an existing `bob` install or scripts against this repo.
**Guarantee:** nothing legacy breaks in this release — every old name still works, warned.

## Commands

| Old | New | Behavior of old |
|---|---|---|
| `go build -o bob ./cmd/bob` / `make build` (→ `bob`) | `make build` (→ `bob-eino`, from `./cmd/bob-eino`) | `make build-legacy` still builds `bob` |
| `./bob [flags] <task>` | `./bob-eino [flags] <task>` | identical behavior + **one** stderr line: ``warning: the `bob` command is deprecated; use `bob-eino` `` — never on stdout |

Both binaries are wrappers over `internal/cli`; there is no behavioral difference besides the
warning (test-asserted: `cmd/bob/main_test.go`).

## Environment variables

| Old | New | Behavior of old |
|---|---|---|
| `BOB_MODEL=provider/model` | `INTENT_BOB_EINO_MODEL=provider/model` | still read when the new var and `-model` are unset; warns once per process; the value is never printed |

Precedence: `-model` flag → `INTENT_BOB_EINO_MODEL` → `BOB_MODEL` (warn) → default
(`deepseek/deepseek-chat`). Provider-native keys (`DEEPSEEK_API_KEY`, `OPENAI_API_KEY`, …)
are unchanged. The full `INTENT_BOB_EINO_*` namespace (12 vars) arrives with `internal/config`
in the MiniMax lifecycle rebase; this slice migrates the only variable that exists on `main`.

## State / evidence log

| Old | New |
|---|---|
| `$XDG_STATE_HOME/iam-bob-eino/evidence.jsonl` (or `~/.local/state/iam-bob-eino/…`) | `$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/evidence.jsonl` |

Migration behavior (automatic, idempotent, non-destructive):

1. Canonical log exists → used, nothing else happens.
2. No canonical, legacy exists → the legacy chain is **hash-verified** (`VerifyChain`); if
   intact it is **copied** byte-identically to the canonical path (legacy file left in place);
   if broken it is **not copied** (kept for forensics) and a fresh canonical log starts.
3. Neither exists → fresh canonical log.

The legacy file is never moved, edited, or deleted.

## Receipt / evidence compatibility

- Evidence records now include `agent_identity` (structured identity, evidence schema v2,
  in the hash chain). **Legacy records without the field parse and self-verify unchanged** —
  the nil field is omitted from JSON, reproducing the v1 byte shape the original chain hashed.
- Run receipts do not exist on `main` yet; the receipt `agent_identity` lands in the MiniMax
  lifecycle rebase.

## Rollback

Check out the previous tag/commit and run `make build` there — legacy evidence was never
rewritten, and a canonical log created in the meantime is simply ignored by old binaries
(they read the legacy path). No data migration to undo.

## Troubleshooting

- **"warning: the `bob` command is deprecated"** — expected; switch scripts to `bob-eino`.
  The warning is stderr-only, so piped stdout is unaffected.
- **"warning: BOB_MODEL is deprecated"** — set `INTENT_BOB_EINO_MODEL` instead (same value).
- **"legacy evidence chain broken at record N; NOT copying"** — the old log fails hash
  verification; it is preserved at the legacy path for inspection and a fresh canonical log
  starts. Nothing was deleted.
- **Two `bob` binaries on PATH** (this repo's alias + iam-bob-intendant) — this is collision #1
  from doc 006; use `bob-eino` and let PATH keep `bob` for the intendant (or vice versa).

— Jeremy Longshore, intentsolutions.io
