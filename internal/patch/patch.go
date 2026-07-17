// Package patch implements Bob's governed patch format,
// "intent-bob-eino-patch/v1": a JSON document of literal find/replace hunks
// with mandatory pre-image hashing and exact occurrence counts. It is
// deliberately NOT unified diff and NOT shell: no external tool runs, no
// line-number arithmetic drifts, and a patch built against stale content
// fails loudly (pre-hash mismatch) instead of applying somewhere unexpected.
//
// Apply is two-phase and atomic per patch: every pre-hash is verified and
// every post-image is computed in memory BEFORE the first byte is written
// (any failure = zero writes); a partial write failure rolls back from the
// held pre-images; every written file is re-read and hash-verified. The
// returned Result is evidence-safe — paths, hashes, sizes, and hunk counts,
// never content.
package patch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// SchemaVersion is the only patch schema this build understands.
const SchemaVersion = "intent-bob-eino-patch/v1"

// Bounds. A patch is a surgical instrument, not a bulk loader; new files are
// write_file's job and large rewrites should be plans with multiple actions.
const (
	MaxFiles        = 20
	MaxHunksPerFile = 64
	MaxPatchBytes   = 1 << 20 // 1 MiB of patch document
	MaxResultBytes  = 1 << 20 // 1 MiB per resulting file
)

// Typed sentinel errors; every failure wraps exactly one.
var (
	ErrMalformed          = errors.New("patch: malformed document")
	ErrTooManyFiles       = errors.New("patch: too many files")
	ErrTooManyHunks       = errors.New("patch: too many hunks in one file")
	ErrTooLarge           = errors.New("patch: size bound exceeded")
	ErrPreHashMismatch    = errors.New("patch: file pre-hash does not match on-disk content")
	ErrOccurrenceMismatch = errors.New("patch: find-text occurrence count does not match expectation")
	ErrBinary             = errors.New("patch: refusing to patch binary content")
	ErrForbiddenPath      = errors.New("patch: forbidden path")
	ErrDuplicatePath      = errors.New("patch: duplicate path in one patch")
	ErrRollbackFailed     = errors.New("patch: rollback after partial failure FAILED — workspace may be inconsistent")
)

// Patch is one atomic set of file changes.
type Patch struct {
	SchemaVersion string       `json:"schema_version"`
	Files         []FileChange `json:"files"`
}

// FileChange edits one existing file. PreSHA256 is required and verified —
// the hex sha256 of the file's full current content; a patch written against
// any other version of the file refuses to apply.
type FileChange struct {
	Path      string `json:"path"`
	PreSHA256 string `json:"pre_sha256"`
	Hunks     []Hunk `json:"hunks"`
}

// Hunk is one literal find/replace. ExpectCount is the exact number of times
// Find must occur in the file (>= 1); any other count is an error, so an
// ambiguous or drifted patch can never half-apply. Occurrence selects which
// match to replace: 0 = all of them, N = only the N-th (1-based).
type Hunk struct {
	Find        string `json:"find"`
	Replace     string `json:"replace"`
	ExpectCount int    `json:"expect_count"`
	Occurrence  int    `json:"occurrence"`
}

// Result is the evidence-safe outcome of a successful Apply.
type Result struct {
	Files []FileResult `json:"files"`
}

// FileResult reports one applied file change — hashes, sizes, counts only.
type FileResult struct {
	Path         string `json:"path"`
	PreSHA256    string `json:"pre_sha256"`
	PostSHA256   string `json:"post_sha256"`
	HunksApplied int    `json:"hunks_applied"`
	BytesBefore  int    `json:"bytes_before"`
	BytesAfter   int    `json:"bytes_after"`
}

// Parse decodes a patch document with strict field checking and the document
// size bound.
func Parse(raw []byte) (Patch, error) {
	if len(raw) > MaxPatchBytes {
		return Patch{}, fmt.Errorf("%w: patch document %d bytes exceeds %d", ErrTooLarge, len(raw), MaxPatchBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p Patch
	if err := dec.Decode(&p); err != nil {
		return Patch{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if dec.More() {
		return Patch{}, fmt.Errorf("%w: trailing data after the patch object", ErrMalformed)
	}
	return p, nil
}

// sha256Hex is the canonical content-hash form used throughout the package.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var hexHashLen = len(sha256Hex(nil))

// Validate checks a parsed patch against the schema, bounds, and path rules.
// It is purely structural — no filesystem access.
func Validate(p Patch) error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema_version %q, want %q", ErrMalformed, p.SchemaVersion, SchemaVersion)
	}
	if len(p.Files) == 0 {
		return fmt.Errorf("%w: no files", ErrMalformed)
	}
	if len(p.Files) > MaxFiles {
		return fmt.Errorf("%w: %d files exceeds %d", ErrTooManyFiles, len(p.Files), MaxFiles)
	}
	seen := map[string]bool{}
	for _, fc := range p.Files {
		if err := checkPath(fc.Path); err != nil {
			return err
		}
		if seen[fc.Path] {
			return fmt.Errorf("%w: %q", ErrDuplicatePath, fc.Path)
		}
		seen[fc.Path] = true
		if len(fc.PreSHA256) != hexHashLen || !isHex(fc.PreSHA256) {
			return fmt.Errorf("%w: file %q pre_sha256 must be a hex sha256", ErrMalformed, fc.Path)
		}
		if len(fc.Hunks) == 0 {
			return fmt.Errorf("%w: file %q has no hunks", ErrMalformed, fc.Path)
		}
		if len(fc.Hunks) > MaxHunksPerFile {
			return fmt.Errorf("%w: file %q has %d hunks, max %d", ErrTooManyHunks, fc.Path, len(fc.Hunks), MaxHunksPerFile)
		}
		for i, h := range fc.Hunks {
			if h.Find == "" {
				return fmt.Errorf("%w: file %q hunk %d has empty find text", ErrMalformed, fc.Path, i)
			}
			if h.ExpectCount < 1 {
				return fmt.Errorf("%w: file %q hunk %d expect_count must be >= 1", ErrMalformed, fc.Path, i)
			}
			if h.Occurrence < 0 || h.Occurrence > h.ExpectCount {
				return fmt.Errorf("%w: file %q hunk %d occurrence %d outside [0, expect_count]", ErrMalformed, fc.Path, i, h.Occurrence)
			}
		}
	}
	return nil
}

// secretBase matches base filenames that must never be patch targets.
var secretBase = map[string]bool{
	".env": true, ".envrc": true, ".netrc": true, ".npmrc": true,
	"credentials": true, "id_rsa": true, "id_ed25519": true, "id_ecdsa": true,
}

// checkPath rejects absolute paths, traversal, git internals, and secret
// material — the same ladder the write tools enforce, kept here too so the
// package is safe even if reached through a future non-tool caller.
func checkPath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrForbiddenPath)
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "~") {
		return fmt.Errorf("%w: absolute or home-relative path %q", ErrForbiddenPath, p)
	}
	norm := strings.ReplaceAll(p, "\\", "/")
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: path traversal in %q", ErrForbiddenPath, p)
		}
		if seg == ".git" {
			return fmt.Errorf("%w: git internals in %q", ErrForbiddenPath, p)
		}
	}
	base := strings.ToLower(norm[strings.LastIndex(norm, "/")+1:])
	if secretBase[base] || strings.HasPrefix(base, ".env.") || secretExtension(base) {
		return fmt.Errorf("%w: secret-material file %q", ErrForbiddenPath, p)
	}
	return nil
}

// secretExtension matches key-material file extensions — kept aligned with
// internal/plan's secretExtensions set so a patch can never target what a
// plan may never propose.
func secretExtension(base string) bool {
	for _, ext := range []string{".pem", ".key", ".pfx", ".p12", ".jks"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// pendingWrite is one fully-computed post-image awaiting phase 2.
type pendingWrite struct {
	change   FileChange
	preImage []byte
	post     []byte
}

// Apply performs the two-phase atomic application of p inside ws.
//
// Phase 1 (in memory, zero writes on any failure): every file is read through
// the symlink-safe workspace root, NUL-scanned, pre-hash-verified, and every
// hunk applied against the in-memory image with exact occurrence checking.
//
// Phase 2: post-images are written in order; a write failure triggers a
// rollback of every previously-written file from its held pre-image
// (ErrRollbackFailed if that itself fails). Every written file is then
// re-read and its hash verified against the intended post-image.
func Apply(ws *workspace.Workspace, p Patch) (Result, error) {
	if err := Validate(p); err != nil {
		return Result{}, err
	}

	// Phase 1: verify and compute everything before touching the disk.
	pending := make([]pendingWrite, 0, len(p.Files))
	for _, fc := range p.Files {
		data, truncated, err := ws.ReadFileLimited(fc.Path, MaxResultBytes)
		if err != nil {
			return Result{}, fmt.Errorf("patch: read %q: %w", fc.Path, err)
		}
		if truncated {
			return Result{}, fmt.Errorf("%w: %q exceeds %d bytes before patching", ErrTooLarge, fc.Path, MaxResultBytes)
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return Result{}, fmt.Errorf("%w: %q contains NUL bytes", ErrBinary, fc.Path)
		}
		if got := sha256Hex(data); got != strings.ToLower(fc.PreSHA256) {
			return Result{}, fmt.Errorf("%w: %q (the file changed since the patch was built)", ErrPreHashMismatch, fc.Path)
		}
		post := string(data)
		for i, h := range fc.Hunks {
			count := strings.Count(post, h.Find)
			if count != h.ExpectCount {
				return Result{}, fmt.Errorf("%w: %q hunk %d: found %d occurrences, expected %d", ErrOccurrenceMismatch, fc.Path, i, count, h.ExpectCount)
			}
			if h.Occurrence == 0 {
				post = strings.ReplaceAll(post, h.Find, h.Replace)
			} else {
				post = replaceNth(post, h.Find, h.Replace, h.Occurrence)
			}
		}
		if len(post) > MaxResultBytes {
			return Result{}, fmt.Errorf("%w: %q would be %d bytes after patching, max %d", ErrTooLarge, fc.Path, len(post), MaxResultBytes)
		}
		pending = append(pending, pendingWrite{change: fc, preImage: data, post: []byte(post)})
	}

	// Phase 2: write, with rollback from held pre-images on partial failure.
	written := make([]pendingWrite, 0, len(pending))
	rollback := func(cause error) error {
		var failures []string
		for _, w := range written {
			if rerr := ws.WriteFile(w.change.Path, w.preImage, 0o644); rerr != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", w.change.Path, rerr))
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("%w: after %v; unrestored: %s", ErrRollbackFailed, cause, strings.Join(failures, "; "))
		}
		return fmt.Errorf("patch: rolled back cleanly after: %w", cause)
	}
	for _, w := range pending {
		if err := ws.WriteFile(w.change.Path, w.post, 0o644); err != nil {
			return Result{}, rollback(fmt.Errorf("write %q: %w", w.change.Path, err))
		}
		written = append(written, w)
	}

	// Post-write verification: re-read every file and compare hashes.
	res := Result{Files: make([]FileResult, 0, len(pending))}
	for _, w := range pending {
		got, _, err := ws.ReadFileLimited(w.change.Path, MaxResultBytes)
		if err != nil {
			return Result{}, rollback(fmt.Errorf("post-write re-read %q: %w", w.change.Path, err))
		}
		wantHash := sha256Hex(w.post)
		if sha256Hex(got) != wantHash {
			return Result{}, rollback(fmt.Errorf("post-write hash mismatch on %q", w.change.Path))
		}
		res.Files = append(res.Files, FileResult{
			Path:         w.change.Path,
			PreSHA256:    strings.ToLower(w.change.PreSHA256),
			PostSHA256:   wantHash,
			HunksApplied: len(w.change.Hunks),
			BytesBefore:  len(w.preImage),
			BytesAfter:   len(w.post),
		})
	}
	return res, nil
}

// replaceNth replaces only the n-th (1-based) occurrence of find in s. The
// caller has already verified n <= occurrence count.
func replaceNth(s, find, replace string, n int) string {
	idx := 0
	for i := 0; i < n; i++ {
		next := strings.Index(s[idx:], find)
		if next < 0 {
			return s // unreachable after occurrence verification
		}
		idx += next
		if i < n-1 {
			idx += len(find)
		}
	}
	return s[:idx] + replace + s[idx+len(find):]
}
