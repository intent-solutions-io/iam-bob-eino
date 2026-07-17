# 013-AA-AACR — Operational soak of v0.1.0-rc.1 (live MiniMax-M3, real repositories)

**Date:** 2026-07-17 · **Binary under test:** the *published* `v0.1.0-rc.1` linux/amd64 release
artifact (checksum-verified install), then a `main` build after the defect fix. **Model:** live
`minimax/MiniMax-M3`. **Workspaces:** disposable local clones of real repositories. **State:**
one shared soak state dir so the evidence log spans multiple sessions.

## Scenarios and results

| # | Workspace (real repo clone) | Task | Result |
|---|---|---|---|
| A | `iam-bob-eino` | Write a new unit test for `verify.CommandExit`'s failure path; acceptance `go test ./internal/verify/` | **verified** — plan `plan-f7451308…`, run `run-06fpw3e4…`, 5 tool calls, 2 approvals, new 13-line test file, acceptance exit 0; change confirmed out-of-band |
| B | `gastown-viewer-intent` | Add missing godoc comments in `internal/model/event.go`; acceptance `go vet` + `go build` | **PLAN FAILED on rc.1** — typed `ErrPlanDraft`, fail-closed, nothing written (defect §Findings 1) |
| B2 | same, re-run once on fixed `main` | same task | **verified** — plan `plan-b08189a5…`, run `run-06fpw707…`, 4 tool calls; the agent audited the file, found every exported identifier already documented, and honestly proposed/changed **nothing** (files_changed `[]`, acceptance both exit 0) |
| C | `iam-bob-eino` | Rename unexported `ensureRepo`→`requireRepo` across `internal/gitstate`; acceptance `go build` + package tests | **verified** — plan `plan-1f76a382…`, run `run-06fpw74t…`, 8 tool calls, 3 approvals, exactly one file changed (4+/4−), tests exit 0; rename confirmed out-of-band |

Post-soak: `evidence verify-chain` **intact** across all seven correlation groups (three plans,
three runs, one failed planning session) — the multi-session chain resume held under real use.

## Findings

1. **Defect (fixed): MiniMax-M3 `<think>` blocks broke plan-draft extraction.** On a real
   codebase the model interleaved a reasoning block containing braces; rc.1's
   first-to-last-brace extractor produced a typed `ErrPlanDraft`. Fail-closed behavior was
   correct (nothing written), but planning was unusable on that answer shape. Fixed on `main`
   by decode-driven, raw-first extraction (PR #7, hardened per review in PR #8, with the exact
   failure shape as a regression test). B2 re-ran once per the 012 failure policy and verified.
   **rc.1 remains affected — this alone warrants rc.2.**
2. **Honest no-op behavior confirmed.** Given a task whose precondition was already satisfied
   (B2), the agent verified rather than fabricating edits — plan proposed zero files, receipt
   recorded zero changes, verifier passed on acceptance evidence. Exactly the wanted behavior.
3. **`apply_patch` is under-used by the model.** All mutations in A and C went through
   `write_file` full rewrites (patches_applied = 0 everywhere), even for C's surgical 4-line
   rename. Not a safety issue (verification and variance guarding hold either way), but patch
   adoption likely needs explicit steering in the run prompt. Deferred as tuning, not blocking.
4. **Usage bounds never approached.** Peak 8 tool calls against the 64-call budget; no repeated-
   identical or consecutive-failure trips; zero variance denials (models stayed in-plan across
   all runs). Defaults look right; no tuning warranted from this sample.
5. **Usage metadata still absent** from MiniMax responses via the OpenAI-compatible client
   (receipts carry `usage: {}`), unchanged from the release proof.

## Recommendation

**Cut `v0.1.0-rc.2`** carrying the extraction fix (PRs #7/#8), after the formal fixture live
proof re-runs green on `main` per `000-docs/010` gates. Hold GA until an rc.2 soak pass adds at
least one non-Go workspace and one deliberate variance scenario against a live model.
