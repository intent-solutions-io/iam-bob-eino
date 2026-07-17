# 010-DR-GUID — Release process

**Status:** Active guidance · **Date:** 2026-07-17
**Applies to:** `iam-bob-eino` (component `intent-bob-eino`)

## Version policy

- `internal/version.AgentVersion` is Bob's OWN semantic version (base value in source;
  release builds inject the tag version via GoReleaser ldflags). It is never the Eino engine
  version and never a git SHA — `BuildCommit`/`BuildDate` carry provenance separately, and the
  engine/evidence/receipt/identity/plan schema versions are all distinct fields
  (`bob-eino version -json` reports each one).
- Tags are `v<semver>`; pre-releases use `-rc.N` (published as GitHub prereleases
  automatically via `prerelease: auto`).

## Release gates, in order

1. **PR merged to `main`** through the normal required checks (build·vet·test, govulncheck,
   gitleaks) + AI review.
2. **Local snapshot validation:** `goreleaser release --snapshot --clean --skip=publish`
   builds all 8 artifacts (2 binaries × 4 platforms) plus checksums; archive contents spot-
   checked.
3. **Install lifecycle green:** `go test ./scripts/` (install/upgrade/rollback/uninstall in a
   disposable HOME).
4. **Live MiniMax proof (operator-controlled):** the disposable-fixture lifecycle per
   `000-docs/012-DR-GUID-live-minimax-proof.md`. CI never runs it and holds no provider
   credential. **No tag is created when the proof was skipped or failed** — release-hardening
   work still merges, the release simply waits.
5. **Tag push** (`git tag v<version> && git push origin v<version>`) triggers
   `.github/workflows/release.yml`: full gates re-run (fmt, vet, build, test, race,
   govulncheck, gitleaks, `goreleaser check`), then `goreleaser release --clean` produces:
   - archives `intent-bob-eino_<version>_<os>_<arch>.tar.gz` containing `bob-eino`, the
     legacy `bob` alias, `LICENSE`, `NOTICE`, `README.md`, `INSTALL.md`, and the identity
     schema;
   - `intent-bob-eino_<version>_checksums.txt` (SHA-256);
   - one SPDX SBOM per archive (Syft);
   - build metadata baked into the binaries (tag version, short commit, commit date — commit
     date, not wall clock, for reproducibility).
   The workflow then re-verifies every checksum and archive's contents before the release is
   considered good.

## Explicit non-claims

- No signed provenance / attestation exists yet — checksums + SBOM only; do not describe
  releases as "signed".
- No Windows support claim.
- A GitHub prerelease is NOT a production-readiness claim; `local_untrusted` remains the only
  evidence authority.
