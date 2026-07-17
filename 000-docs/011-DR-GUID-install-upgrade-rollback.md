# 011-DR-GUID — Install, upgrade, rollback, uninstall

**Status:** Active guidance · **Date:** 2026-07-17
**Applies to:** `intent-bob-eino` release archives (`intent-bob-eino_<version>_<os>_<arch>.tar.gz`)

Supported platforms: **linux/amd64, linux/arm64, darwin/amd64, darwin/arm64**. No Windows
claim — untested. Default install location: `$HOME/.local/bin`.

The installer never uses `curl | bash`, never edits shell configuration, verifies the archive
checksum before touching anything, and never removes configuration, plans, receipts, or
evidence. An existing `bob` executable that is not this runtime's compatibility alias (e.g.
iam-bob-intendant's) is never overwritten or removed.

## Install

1. Download from the GitHub release page: the archive for your platform AND
   `intent-bob-eino_<version>_checksums.txt`.
2. Verify + install (the script re-verifies the checksum itself and refuses on mismatch):

```bash
scripts/install.sh install \
  --archive intent-bob-eino_<version>_<os>_<arch>.tar.gz \
  --checksums intent-bob-eino_<version>_checksums.txt
```

3. If `~/.local/bin` is not on your `PATH`, the script tells you and stops there — add it
   yourself; nothing edits your shell config.

Each archive also contains `LICENSE`, `NOTICE`, `README.md`, this guide as `INSTALL.md`, and
`schemas/intent-agent-identity.v1.schema.json` (to inspect evidence/receipt identity offline).

## Upgrade

```bash
scripts/install.sh upgrade --archive <new-archive> --checksums <new-checksums>
```

The previous binary is preserved as `~/.local/bin/bob-eino.prev`; replacement is atomic
(write-then-rename in the same directory).

## Rollback

```bash
scripts/install.sh rollback
```

Restores `bob-eino.prev`. Receipts/evidence written meanwhile are unaffected (legacy records
always stay readable; see `000-docs/008`).

## Uninstall

```bash
scripts/install.sh uninstall
```

Removes `bob-eino`, `bob-eino.prev`, and the `bob` alias **only when it is ours**. State
(`$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/` — config, plans, receipts, evidence)
is never touched; manual state cleanup is a separate validated procedure in `000-docs/008`.

## Verification of this whole lifecycle

The lifecycle is covered end-to-end by `scripts/install_lifecycle_test.go` (disposable `$HOME`,
checksum-mismatch refusal, missing-entry refusal, foreign-`bob` preservation, state
preservation across uninstall, release version injection).
