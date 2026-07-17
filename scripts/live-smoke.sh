#!/usr/bin/env bash
# live-smoke.sh — operator procedure for the LIVE MiniMax lifecycle smoke.
#
# What it does: doctor --net → plan → run → verify → evidence verify-chain
# against the real MiniMax endpoint on a throwaway scratch repository, via
# the gated Go test (TestLiveMiniMaxSmoke). Nothing in CI ever sets the gate;
# the only way this runs is an operator invoking this script with a real
# credential.
#
# Requirements:
#   - MINIMAX_API_KEY exported in the environment (BYOK; never stored).
#   - git and go on PATH.
#
# Usage:
#   MINIMAX_API_KEY=... ./scripts/live-smoke.sh
#
# Cost note: this makes real model calls (plan + run). Expect a handful of
# MiniMax-M3 requests.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -z "${MINIMAX_API_KEY:-}" ]]; then
  echo "live-smoke: MINIMAX_API_KEY is not set — refusing to pretend. Export it and re-run." >&2
  exit 2
fi

command -v git >/dev/null || { echo "live-smoke: git not found" >&2; exit 2; }
command -v go  >/dev/null || { echo "live-smoke: go not found"  >&2; exit 2; }

echo "live-smoke: building and preflighting..."
go build ./...

# The whole lifecycle (scratch repo, doctor --net, plan, run, verify,
# chain-verify, cleanup via the test framework's TempDir) lives in the gated
# test — one code path for operators and for any future authorized runner.
echo "live-smoke: running the gated lifecycle test against the live endpoint"
INTENT_BOB_EINO_LIVE_SMOKE=1 go test -count=1 -v -timeout 20m \
  -run '^TestLiveMiniMaxSmoke$' ./internal/cli/

echo "live-smoke: PASS — lifecycle held against the live MiniMax endpoint"
