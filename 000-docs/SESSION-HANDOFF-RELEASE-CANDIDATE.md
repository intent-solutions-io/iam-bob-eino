# SESSION HANDOFF — Live MiniMax Proof and First Release Candidate

**Date:** 2026-07-17 · **Author:** the release-candidate build session

## Git / PR / CI

1. **Starting main SHA:** `a9d4f38` (after audit-remediation PR #4).
2. **Ending branch SHA:** `52479e3` (`feat/live-minimax-release-candidate`).
3. **Ending main SHA (merged):** `ee64b82`.
4. **PR:** #5 — merged.
5. **CI:** all required checks green on every push (build·vet·test, govulncheck, gitleaks); release workflow run `29550977301` fully green (jobs: release gates; build·checksum·sbom·publish).
6. **Review findings:** 3 (gemini-code-assist: macOS `sha256sum` portability [high], silently-voided leak check on read error [medium], external checksum binary in tests [medium]).
7. **Review threads:** all 3 fixed in `52479e3` with the lifecycle suite re-run; none remain open.

## Release

8. **Version:** `v0.1.0-rc.1` (tag on `ee64b82`) — published as a **GitHub prerelease**: <https://github.com/intent-solutions-io/iam-bob-eino/releases/tag/v0.1.0-rc.1>
9. **Supported platforms:** linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. No Windows claim (untested).
10. **Artifacts:** `intent-bob-eino_0.1.0-rc.1_<os>_<arch>.tar.gz` ×4 (each: `bob-eino`, legacy `bob`, LICENSE, NOTICE, README.md, INSTALL.md, `schemas/intent-agent-identity.v1.schema.json`).
11. **Checksums:** `intent-bob-eino_0.1.0-rc.1_checksums.txt` (SHA-256); re-verified inside the release workflow and again on a real download.
12. **SBOM:** one SPDX JSON per archive (Syft), published as release assets.
13. **Install / upgrade / rollback / uninstall:** all pass — `scripts/install_lifecycle_test.go` (disposable HOME: version injection, upgrade preserves `bob-eino.prev`, rollback restores, uninstall leaves state + foreign `bob` untouched, checksum mismatch/missing entry refuse) **plus** a manual end-to-end install of the **published** linux/amd64 asset (checksum verified → `bob-eino version` reports `0.1.0-rc.1`, build `ee64b82` → uninstall clean).

## Live MiniMax proof — PASSED (real run, 2026-07-17)

14. **Result:** PASS in 38.8s, one attempt, no retry, no provider fallback.
15. **Provider / model:** `minimax` / `MiniMax-M3` (real endpoint; `doctor -net` green first).
16. **Plan ID:** `plan-628a8c7620e42a572ecfe0d9` (read-only planning; requested capabilities exactly `[exec writes]`).
17. **Run ID:** `run-06fpvx41mry5tbwv2d6f4bphs0`.
18. **Fixture:** disposable git repo, minimal Go module `fixture.invalid/mathx` with an intentionally failing test (`Add` subtracted). **Changed files: `mathx.go` only.**
19. **Tool calls:** 9. **Acceptance test:** `go test ./...` exit **0** (fixture re-tested green out-of-band too).
20. **Verifier:** `verified`; **final_status:** `verified`. Post-hoc `verify -receipt -plan` and `evidence verify-chain` both green.
21. **Receipt hash:** `sha256:ff9d090bfc7c5858713a0f7316afae9a1af207b34a67b3020a9945bf980bc63d`.
22. **Usage metadata:** none returned by the provider through this path (`usage={}`).
23. **Credential handling:** sourced from the SOPS-encrypted estate env in-process; per-command and durable-artifact leak checks passed; never printed, logged, or committed.
24. **Cleanup:** fixture and state via `t.TempDir()` — nothing persists.

## Gates & quality

25. Local: gofmt clean, `go vet` clean, `go build` clean, `go test ./...` + `go test -race ./...` fully green, `go mod tidy` no-op, govulncheck **0 vulnerabilities affecting code**, gitleaks **no leaks** (with the exact-value canary allowlist from PR #4).
26. Release workflow re-runs all of the above plus `goreleaser check` and post-publish archive/checksum validation.

## Remaining risks / uncertainties

27. MiniMax returned no usage metadata via the OpenAI-compatible client — usage reporting stays empty until the client surfaces it.
28. darwin archives are cross-compiled and checksum/content-validated but not executed on real macOS hardware.
29. No signed provenance/attestation (checksums + SBOM only) — deliberately not claimed.
30. golangci-lint / markdownlint / license CI gates remain deferred (recorded in ci.yml future-gates).

## Non-claims

No production readiness or deployment; no AGP / Mission Control / Big Brain integration; no multi-agent behavior; no provider failover; no Windows support; no signed provenance; evidence authority remains `local_untrusted`. The Go governance-kernel decision (D7) remains deferred.

## Next recommended slice

Operational soak: run the lifecycle against 2–3 real (non-fixture) repos with MiniMax-M3, collect receipts, and tune the planning prompt + usage bounds from observed behavior; then decide `v0.1.0` GA vs another rc. Estate-side: take the cross-family naming proposal (000-docs/007) to Intent OS for ratification.
