# 012-DR-GUID — Live MiniMax proof (operator-controlled release gate)

**Status:** Active guidance · **Date:** 2026-07-17

The live proof runs the REAL plan → run → verify → evidence lifecycle against the real
MiniMax endpoint (`minimax/MiniMax-M3`) on a **disposable fixture**: a temporary git
repository containing a minimal Go module with one intentionally failing test. The
MiniMax-backed agent must inspect the fixture, produce a hashed read-only plan requesting
only the needed capabilities, apply one minimal approved correction, run the allowlisted
`go test`, pass it, and leave a sealed receipt that independent verification and
chain-verification both accept — with only the expected fixture files modified and the
fixture cleaned up afterwards.

## Gates (both required — CI never satisfies them)

```
INTENT_BOB_EINO_LIVE_SMOKE=1
MINIMAX_API_KEY=<credential>
```

Without both, the test reports **skipped** honestly (`SKIPPED_NO_CREDENTIAL` when the flag is
armed but no credential exists). No paid call ever runs from normal tests, CI, or pull
requests; no provider credential exists in GitHub Actions.

## Operator procedure

```bash
MINIMAX_API_KEY=... ./scripts/live-smoke.sh
```

(The script routes through the gated Go test `TestLiveMiniMaxSmoke` so operators and any
future authorized runner share one code path. Expect a handful of MiniMax-M3 calls.)

## What gets recorded

The test logs, and the session handoff must carry: provider, exact model, plan id, run id,
tool-call count, changed files, acceptance test result, verifier result, receipt hash,
evidence-chain result, and usage metadata when the provider returns it. The credential and
raw provider request/response bodies are never printed, logged, or committed.

## Failure policy

- A **real defect** exposed by the live run is fixed at the source with regression coverage,
  all tests re-run, and the live proof re-run **once**. Provider incompatibilities are never
  hidden behind test-only behavior.
- A MiniMax failure never triggers a provider fallback (no-fallback is contract, test-pinned
  in `internal/provider`).
- When the proof was skipped or failed, release-hardening work may merge but **no release
  tag is created** and no live success is claimed.
