#!/usr/bin/env bash
# install.sh — checksum-verified install / upgrade / rollback / uninstall for
# the intent-bob-eino release archives.
#
# Safety contract:
#   * NEVER curl|bash — you download the archive + checksums yourself, then
#     run this script against local files.
#   * The archive checksum is verified BEFORE anything is installed.
#   * Upgrades preserve the previous binary (bob-eino.prev) for rollback.
#   * Replacement is atomic (write to .new in the same directory, then mv).
#   * Uninstall removes BINARIES ONLY — configuration, plans, receipts, and
#     evidence under $XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/
#     are never touched.
#   * An existing `bob` executable that is NOT our compatibility alias is
#     never overwritten or removed (iam-bob-intendant also ships a `bob`).
#   * Shell configuration is never modified; if the bin dir is not on PATH
#     the script says so and stops there.
#
# Usage:
#   install.sh install  --archive <intent-bob-eino_*.tar.gz> --checksums <checksums.txt> [--bin-dir DIR]
#   install.sh upgrade  --archive <intent-bob-eino_*.tar.gz> --checksums <checksums.txt> [--bin-dir DIR]
#   install.sh rollback  [--bin-dir DIR]
#   install.sh uninstall [--bin-dir DIR]
#
# Default bin dir: $HOME/.local/bin

set -euo pipefail

BIN_DIR="${HOME}/.local/bin"
ARCHIVE=""
CHECKSUMS=""

die() { echo "install.sh: $*" >&2; exit 1; }
note() { echo "install.sh: $*"; }

CMD="${1:-}"
[ -n "$CMD" ] || die "usage: install.sh <install|upgrade|rollback|uninstall> [--archive A --checksums C] [--bin-dir D]"
shift

while [ $# -gt 0 ]; do
  case "$1" in
    --archive)   ARCHIVE="$2"; shift 2 ;;
    --checksums) CHECKSUMS="$2"; shift 2 ;;
    --bin-dir)   BIN_DIR="$2"; shift 2 ;;
    *) die "unknown option: $1" ;;
  esac
done

CANONICAL="$BIN_DIR/bob-eino"
PREV="$BIN_DIR/bob-eino.prev"
LEGACY="$BIN_DIR/bob"

# is_our_legacy_alias: true when the `bob` at $1 is OUR compatibility alias
# (its stderr deprecation line names bob-eino and its output carries the
# component id). Anything else — e.g. iam-bob-intendant's bob — is foreign.
is_our_legacy_alias() {
  local candidate="$1" out
  [ -x "$candidate" ] || return 1
  out="$("$candidate" version 2>&1 || true)"
  case "$out" in
    *intent-bob-eino*) return 0 ;;
    *) return 1 ;;
  esac
}

verify_archive() {
  [ -n "$ARCHIVE" ] || die "--archive is required for $CMD"
  [ -n "$CHECKSUMS" ] || die "--checksums is required for $CMD (checksum verification is mandatory)"
  [ -f "$ARCHIVE" ] || die "archive not found: $ARCHIVE"
  [ -f "$CHECKSUMS" ] || die "checksums file not found: $CHECKSUMS"
  local base want got
  base="$(basename "$ARCHIVE")"
  want="$(awk -v f="$base" '$2 == f || $2 == "*"f {print $1}' "$CHECKSUMS")"
  [ -n "$want" ] || die "no checksum entry for $base in $CHECKSUMS — refusing to install"
  got="$(sha256sum "$ARCHIVE" | awk '{print $1}')"
  [ "$got" = "$want" ] || die "CHECKSUM MISMATCH for $base (expected $want, got $got) — refusing to install"
  note "checksum verified: $base"
}

extract_and_stage() {
  STAGE="$(mktemp -d)"
  trap 'rm -rf "$STAGE"' EXIT
  tar -xzf "$ARCHIVE" -C "$STAGE"
  [ -f "$STAGE/bob-eino" ] || die "archive does not contain bob-eino"
  chmod +x "$STAGE/bob-eino"
  [ -f "$STAGE/bob" ] && chmod +x "$STAGE/bob"
  mkdir -p "$BIN_DIR"
}

atomic_place() { # atomic_place <src> <dst>
  local src="$1" dst="$2"
  cp "$src" "$dst.new"
  chmod +x "$dst.new"
  mv -f "$dst.new" "$dst"
}

install_legacy_alias() {
  [ -f "$STAGE/bob" ] || { note "archive carries no legacy bob alias; skipping"; return 0; }
  if [ -e "$LEGACY" ] && ! is_our_legacy_alias "$LEGACY"; then
    note "SKIPPING legacy alias: $LEGACY exists and is not intent-bob-eino's alias (left untouched)"
    return 0
  fi
  atomic_place "$STAGE/bob" "$LEGACY"
  note "installed legacy alias: $LEGACY (deprecated; prefer bob-eino)"
}

path_note() {
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) note "NOTE: $BIN_DIR is not on your PATH. Add it yourself — this script never edits shell configuration." ;;
  esac
}

case "$CMD" in
  install)
    verify_archive
    extract_and_stage
    [ -e "$CANONICAL" ] && die "bob-eino already installed at $CANONICAL — use 'upgrade'"
    atomic_place "$STAGE/bob-eino" "$CANONICAL"
    note "installed: $CANONICAL"
    install_legacy_alias
    "$CANONICAL" version >/dev/null || die "installed binary failed its version check"
    path_note
    ;;
  upgrade)
    verify_archive
    extract_and_stage
    [ -e "$CANONICAL" ] || die "nothing to upgrade: $CANONICAL not found — use 'install'"
    cp "$CANONICAL" "$PREV"
    note "previous binary preserved: $PREV"
    atomic_place "$STAGE/bob-eino" "$CANONICAL"
    note "upgraded: $CANONICAL"
    install_legacy_alias
    "$CANONICAL" version >/dev/null || die "upgraded binary failed its version check (rollback available: install.sh rollback)"
    path_note
    ;;
  rollback)
    [ -f "$PREV" ] || die "no previous binary at $PREV — nothing to roll back to"
    atomic_place "$PREV" "$CANONICAL"
    rm -f "$PREV"
    note "rolled back: $CANONICAL restored from $PREV"
    "$CANONICAL" version >/dev/null || die "rolled-back binary failed its version check"
    ;;
  uninstall)
    removed=0
    for f in "$CANONICAL" "$PREV"; do
      if [ -e "$f" ]; then rm -f "$f"; note "removed: $f"; removed=1; fi
    done
    if [ -e "$LEGACY" ]; then
      if is_our_legacy_alias "$LEGACY"; then
        rm -f "$LEGACY"; note "removed legacy alias: $LEGACY"; removed=1
      else
        note "kept: $LEGACY is not intent-bob-eino's alias"
      fi
    fi
    [ "$removed" = 1 ] || note "nothing to remove in $BIN_DIR"
    note "configuration, plans, receipts, and evidence were NOT touched (see 000-docs/008 for state cleanup)"
    ;;
  *)
    die "unknown command: $CMD (install|upgrade|rollback|uninstall)"
    ;;
esac
