# Contributing to iam-bob-eino

## Prerequisites

- Go 1.25+ (developed on 1.26).
- No network is required for the test suite; it uses a deterministic offline model stub.

## Build, test, run

```bash
make build        # compile the canonical bob-eino binary (./cmd/bob-eino)
make build-legacy # compile the deprecated `bob` alias (./cmd/bob, same internal/cli)
make test         # go test ./...
make test-race    # go test -race ./...
make vet          # go vet
make fmtcheck     # fail on unformatted files
make ci           # fmtcheck + vet + test  (the required gate)
```

Run Bob against this repo (BYOK, zero GCP):

```bash
export DEEPSEEK_API_KEY=...        # or OPENAI_API_KEY / GROQ_API_KEY / ZHIPU_API_KEY
make run-local TASK='describe the governance model'
```

## Conventions

- **gofmt + go vet clean**; exported symbols carry GoDoc comments (golangci-lint
  is the intended linter — run `make lint` if installed).
- **Tests are real**: exact assertions, asymmetric inputs, no tautologies. New
  behavior ships with a test. Security-relevant behavior ships with a regression
  test (see `workspace_test.go` symlink escape, `tools_test.go` shell injection).
- **The governor is the only control point.** Any new tool must route every side
  effect through `governor` (policy → approval → verify → evidence) and must not
  perform file I/O outside `workspace`'s symlink-safe methods.
- **Identity strings have one home.** Machine names come from `internal/identity`
  / `internal/version`; never bare `"bob"` as a machine key (binary, service,
  env prefix, telemetry, agent name). Contract:
  `000-docs/005-DR-STND-bob-eino-identity-contract.md`.
- **Commits**: `type(scope): imperative subject`, body explains what + why + how
  verified. Branch from `main`; open a PR; do not commit to `main`.

## Architecture at a glance

See [`README.md`](README.md) for the package map and
[`SECURITY.md`](SECURITY.md) for the threat model. Founding decisions and the
operating rules are in [`000-docs/`](000-docs/).

## Testing SOP

This repo follows the Intent Solutions testing standard (7-layer taxonomy). L3
unit tests are in place; `TEST_AUDIT.md` (when present) records the layer map and
gaps. Do not lower a coverage or quality threshold to make CI pass — add tests.
