# AGENTS.md — iam-bob-eino

Instructions for AI coding agents working in this repository. `CLAUDE.md` is the fuller
Claude-specific guide; this file is the tool-neutral core. Where they overlap, they agree.

## Orientation

Go / CloudWeGo Eino runtime of the **Intent Agent Model (IAM)** — "Bob". IAM = a project
family, **not** Identity and Access Management. Bob is the persona; the machine identity is
structured (see the table in `README.md` § Naming):

- persona `bob` (human-facing only) · agent `intent-agent-model/bob` · runtime `eino-go` ·
  implementation `iam-bob-eino` · component `intent-bob-eino`.
- Canonical binary **`bob-eino`**; `bob` is a deprecated tested alias. Canonical env
  **`INTENT_BOB_EINO_*`**; `BOB_*` legacy-read + warned.

## Hard rules

1. **Never use bare `bob` as a machine key** — not as a binary/service name, env prefix,
   telemetry `service.name`, state-dir leaf, or agent name. Persona prose ("You are Bob") in
   `internal/agent/persona.go` is the one place "Bob" belongs.
2. **All identity strings come from `internal/identity`** (`identity.New` is the single
   constructor) and `internal/version` constants. Never restate them as literals.
3. **The governor is the only control point** — every tool side effect routes through
   `internal/governor`; all file I/O through `internal/workspace`'s symlink-safe methods.
4. **Evidence is content-safe and hash-chained** — no secrets/file contents in evidence, traces,
   or warnings (env-var warnings never print values). Legacy (v1) records must keep parsing and
   verifying; never change the serialized byte shape of existing fields.
5. **Provider-neutral, zero Google** — models only through `internal/provider`.
6. **Never write the phrase "fake model"** — say "deterministic Eino model fixture" or
   "offline model stub".
7. **Both CLI entry points stay thin** — behavior lives in `internal/cli`; `cmd/bob` adds only
   the one-line deprecation warning.

## Build & test

```bash
make ci            # fmtcheck + vet + test — the required gate
make build         # canonical bob-eino
make build-legacy  # legacy alias bob
go test -race ./...
```

The suite needs no network (offline model stub in `internal/provider/fake.go`).

## Task tracking

Follow the repo's PR conventions in `CONTRIBUTING.md`: branch from `main`, never commit to
`main`, tests ship with the change, security behavior ships with a regression test.
