// Package verify performs independent outcome verification: after a governed
// tool reports success, Bob re-checks reality rather than trusting the tool's
// self-report. This is the "require-verdict" discipline — a write is only
// "verified" when the bytes on disk hash to what was intended, and a command
// outcome is only "verified" when its captured exit status is inspected
// independently of the tool's own claim.
package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// Verdict is the result of an independent verification check.
type Verdict struct {
	// Verified is true only when the observed outcome matches the intended one.
	Verified bool
	// Info is a short, content-safe explanation of the check.
	Info string
}

// The canonical verdict labels used in evidence records.
const (
	StatusVerified   = "verified"
	StatusMismatch   = "mismatch"
	StatusUnverified = "unverified"
	StatusNA         = "n/a"
)

// Label maps a Verdict to its evidence label.
func (v Verdict) Label() string {
	if v.Verified {
		return StatusVerified
	}
	return StatusMismatch
}

// NA returns a verdict for actions with no independent check (pure reads whose
// result is itself the observation).
func NA(info string) Verdict { return Verdict{Verified: true, Info: info} }

// FileContent re-reads a file and confirms its contents hash to wantHash,
// proving a write landed exactly as intended. wantHash is the sha256 (hex) of
// the intended bytes.
func FileContent(path, wantHash string) Verdict {
	got, err := os.ReadFile(path)
	if err != nil {
		return Verdict{Verified: false, Info: fmt.Sprintf("re-read failed: %v", err)}
	}
	sum := sha256.Sum256(got)
	gotHash := hex.EncodeToString(sum[:])
	if gotHash != wantHash {
		return Verdict{Verified: false, Info: "on-disk content hash does not match intended write"}
	}
	return Verdict{Verified: true, Info: fmt.Sprintf("on-disk sha256 matches (%d bytes)", len(got))}
}

// CommandExit confirms a command's captured exit code equals the expected value.
// The exit code is ground truth from os/exec, inspected here independently of any
// textual success claim the command may have printed.
func CommandExit(gotExit, wantExit int) Verdict {
	if gotExit != wantExit {
		return Verdict{Verified: false, Info: fmt.Sprintf("exit code %d != expected %d", gotExit, wantExit)}
	}
	return Verdict{Verified: true, Info: fmt.Sprintf("exit code %d as expected", gotExit)}
}

// HashBytes returns the sha256 (hex) of b, for computing an intended-write hash.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
