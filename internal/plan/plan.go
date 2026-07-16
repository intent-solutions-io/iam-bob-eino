// Package plan defines Bob's read-only plan artifact: a structured, hashed,
// locally-authored proposal of what the agent intends to do before any write
// or execute capability is granted. A plan is advisory input produced by an
// untrusted local model call — its authority is always "local_untrusted" and
// nothing downstream may treat it as an approval.
//
// The artifact is content-addressed: CanonicalHash covers the canonical JSON
// of the plan with content_hash zeroed, and the plan id is derived from the
// same content so identical plan content always yields the identical id
// (idempotent re-planning). Save/Load round-trips re-validate and re-verify
// the content hash so a tampered plan file on disk is rejected on load.
package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SchemaVersion is the only plan schema version this build understands.
const SchemaVersion = "1"

// AuthorityLocalUntrusted is the only permitted value for Plan.Authority: a
// plan is produced by an untrusted local model call and never carries any
// approval weight of its own.
const AuthorityLocalUntrusted = "local_untrusted"

// MaxProposedFiles caps proposed_files so a runaway model cannot propose an
// unbounded change surface.
const MaxProposedFiles = 200

// Typed sentinel errors returned (wrapped) by Validate, Save, and Load.
// Callers match them with errors.Is.
var (
	ErrSchemaVersion    = errors.New("plan: unsupported schema_version")
	ErrPlanID           = errors.New("plan: missing or malformed plan_id")
	ErrAuthority        = errors.New("plan: authority must be \"local_untrusted\"")
	ErrForbiddenFile    = errors.New("plan: forbidden proposed file")
	ErrForbiddenCommand = errors.New("plan: command not in allowed dev-command set")
	ErrNoAcceptance     = errors.New("plan: at least one acceptance check is required")
	ErrContentHash      = errors.New("plan: missing or malformed content_hash")
	ErrTooManyFiles     = errors.New("plan: too many proposed files")
	ErrHashMismatch     = errors.New("plan: content_hash does not match plan content")
)

// Plan is the read-only plan artifact. Every field is content-safe: paths are
// workspace-relative, commands are shell-free argv-style specs, and no secret
// material or raw model output belongs in any field.
type Plan struct {
	SchemaVersion        string   `json:"schema_version"`
	PlanID               string   `json:"plan_id"`
	Task                 string   `json:"task"`
	WorkspaceIdentity    string   `json:"workspace_identity"`
	WorkspaceStartSHA    string   `json:"workspace_start_sha"`
	Provider             string   `json:"provider"`
	Model                string   `json:"model"`
	CreatedAt            string   `json:"created_at"`
	ProposedActions      []string `json:"proposed_actions"`
	ProposedFiles        []string `json:"proposed_files"`
	ProposedCommands     []string `json:"proposed_commands"`
	RequiredCapabilities []string `json:"required_capabilities"`
	AcceptanceChecks     []string `json:"acceptance_checks"`
	Risks                []string `json:"risks"`
	Assumptions          []string `json:"assumptions"`
	Questions            []string `json:"questions"`
	Status               string   `json:"status"`
	ContentHash          string   `json:"content_hash"`
	Authority            string   `json:"authority"`
}

// allowedCommands is the closed set of first tokens permitted in
// proposed_commands and acceptance_checks. These are ordinary dev-loop tools;
// anything else (shells, network fetchers, package publishers) is rejected.
var allowedCommands = map[string]bool{
	"go":     true,
	"make":   true,
	"pytest": true,
	"npm":    true,
	"pnpm":   true,
	"cargo":  true,
	"git":    true,
}

// secretBaseNames are file base names that must never appear in a plan's
// proposed files — credential and key material is out of scope for any plan.
var secretBaseNames = map[string]bool{
	".env":         true,
	".envrc":       true,
	".netrc":       true,
	".npmrc":       true,
	".pypirc":      true,
	"credentials":  true, // e.g. .aws/credentials
	"id_rsa":       true,
	"id_dsa":       true,
	"id_ecdsa":     true,
	"id_ed25519":   true,
	"secrets.yaml": true,
	"secrets.yml":  true,
	"secrets.json": true,
}

// secretExtensions are file extensions that indicate key material.
var secretExtensions = map[string]bool{
	".pem": true,
	".key": true,
	".pfx": true,
	".p12": true,
	".jks": true,
}

// secretPathSegments are directory segments that indicate credential stores.
var secretPathSegments = map[string]bool{
	".aws":     true,
	".ssh":     true,
	".gnupg":   true,
	".kube":    true,
	".docker":  true,
	".config":  true,
	".gcloud":  true,
	".azure":   true,
	".netlify": true,
	".git":     true, // repository internals are never a proposed edit target
}

// shellMeta lists characters that would turn a "shell-free command spec" into
// a shell expression; commands containing any of them are rejected.
const shellMeta = "|&;<>$`\\\"'*?~#(){}[]\n"

var (
	contentHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	planIDRe      = regexp.MustCompile(`^plan-[0-9a-f]{24}$`)
)

// CanonicalHash returns the content address of a plan: the sha256 of the
// plan's canonical JSON with content_hash zeroed, formatted as "sha256:"+hex.
// encoding/json emits struct fields in declaration order and sorts map keys,
// so the encoding — and therefore the hash — is deterministic.
func CanonicalHash(p Plan) string {
	p.ContentHash = ""
	b, err := canonicalJSON(p)
	if err != nil {
		// A Plan of plain strings and string slices cannot fail to marshal;
		// guard anyway so a future field change cannot silently produce a
		// colliding empty hash.
		panic(fmt.Sprintf("plan: canonical marshal failed: %v", err))
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalJSON marshals the plan compactly with HTML escaping disabled so
// the byte stream is stable regardless of the caller's encoder settings.
func canonicalJSON(p Plan) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// NewPlanID derives the plan id from the plan's content: the canonical hash
// with plan_id and content_hash zeroed, prefixed "plan-". Identical plan
// content therefore always yields the identical id — no clock, no randomness.
func NewPlanID(p Plan) string {
	p.PlanID = ""
	p.ContentHash = ""
	h := CanonicalHash(p)
	return "plan-" + h[7:31]
}

// Finalize seals the plan: it derives the content-addressed plan id, then
// computes and sets the content hash over the sealed content (id included).
// Call it exactly once, after every other field is populated.
func (p *Plan) Finalize() {
	p.PlanID = NewPlanID(*p)
	p.ContentHash = CanonicalHash(*p)
}

// Validate checks a sealed plan against the schema and safety rules. It does
// NOT verify the content hash against the content (Load does that); it only
// checks the hash is well-formed.
func Validate(p Plan) error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %q, want %q", ErrSchemaVersion, p.SchemaVersion, SchemaVersion)
	}
	if p.PlanID == "" {
		return fmt.Errorf("%w: empty", ErrPlanID)
	}
	if !planIDRe.MatchString(p.PlanID) {
		return fmt.Errorf("%w: %q does not match %s", ErrPlanID, p.PlanID, planIDRe)
	}
	if p.Authority != AuthorityLocalUntrusted {
		return fmt.Errorf("%w: got %q", ErrAuthority, p.Authority)
	}
	if len(p.ProposedFiles) > MaxProposedFiles {
		return fmt.Errorf("%w: %d proposed files exceeds cap of %d", ErrTooManyFiles, len(p.ProposedFiles), MaxProposedFiles)
	}
	for _, f := range p.ProposedFiles {
		if err := checkProposedFile(f); err != nil {
			return err
		}
	}
	for _, c := range p.ProposedCommands {
		if err := checkCommand("proposed command", c); err != nil {
			return err
		}
	}
	if len(p.AcceptanceChecks) == 0 {
		return ErrNoAcceptance
	}
	for _, c := range p.AcceptanceChecks {
		if err := checkCommand("acceptance check", c); err != nil {
			return err
		}
	}
	if !contentHashRe.MatchString(p.ContentHash) {
		return fmt.Errorf("%w: %q", ErrContentHash, p.ContentHash)
	}
	return nil
}

// checkProposedFile rejects paths that escape the workspace, touch repository
// internals, or name secret material.
func checkProposedFile(f string) error {
	if f == "" {
		return fmt.Errorf("%w: empty path", ErrForbiddenFile)
	}
	if strings.HasPrefix(f, "/") || strings.HasPrefix(f, "\\") || filepath.IsAbs(f) || strings.HasPrefix(f, "~") {
		return fmt.Errorf("%w: absolute or home-relative path %q", ErrForbiddenFile, f)
	}
	// Evaluate every slash-separated segment; both separators are checked so
	// a Windows-style path cannot smuggle a forbidden segment past the gate.
	norm := strings.ReplaceAll(f, "\\", "/")
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: path traversal in %q", ErrForbiddenFile, f)
		}
		if secretPathSegments[seg] {
			return fmt.Errorf("%w: protected directory segment %q in %q", ErrForbiddenFile, seg, f)
		}
	}
	base := norm[strings.LastIndex(norm, "/")+1:]
	lower := strings.ToLower(base)
	if secretBaseNames[lower] || strings.HasPrefix(lower, ".env.") || strings.HasPrefix(lower, "id_rsa") || strings.HasPrefix(lower, "id_ed25519") {
		return fmt.Errorf("%w: secret-material file name %q", ErrForbiddenFile, f)
	}
	if secretExtensions[strings.ToLower(filepath.Ext(lower))] {
		return fmt.Errorf("%w: secret-material file extension in %q", ErrForbiddenFile, f)
	}
	return nil
}

// checkCommand enforces the shell-free command-spec contract: a non-empty
// argv-style string whose first token is in the allowed dev-command set and
// which contains no shell metacharacters.
func checkCommand(kind, c string) error {
	fields := strings.Fields(c)
	if len(fields) == 0 {
		return fmt.Errorf("%w: empty %s", ErrForbiddenCommand, kind)
	}
	if !allowedCommands[fields[0]] {
		return fmt.Errorf("%w: %s %q starts with %q", ErrForbiddenCommand, kind, c, fields[0])
	}
	if strings.ContainsAny(c, shellMeta) {
		return fmt.Errorf("%w: %s %q contains shell metacharacters", ErrForbiddenCommand, kind, c)
	}
	return nil
}

// Save validates the sealed plan, verifies its content hash, and writes it as
// pretty-printed JSON to dir/<plan_id>.json with mode 0600. dir must live
// OUTSIDE any workspace (e.g. ~/.local/state/iam-bob-eino/plans/) so a plan
// can never be edited by the very workspace it governs. It returns the path
// written.
func Save(p Plan, dir string) (string, error) {
	if err := Validate(p); err != nil {
		return "", err
	}
	if got := CanonicalHash(p); got != p.ContentHash {
		return "", fmt.Errorf("%w: stored %q, computed %q", ErrHashMismatch, p.ContentHash, got)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("plan: create plan dir %q: %w", dir, err)
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("plan: marshal plan: %w", err)
	}
	// Validate guarantees the plan id matches ^plan-[0-9a-f]{24}$, so the
	// file name cannot traverse out of dir.
	path := filepath.Join(dir, p.PlanID+".json")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("plan: write %q: %w", path, err)
	}
	return path, nil
}

// Load reads a plan file, re-validates it, and re-verifies that the stored
// content_hash matches the loaded content. Any tampering — an edited field or
// an edited hash — fails with ErrHashMismatch (or a Validate error).
func Load(path string) (Plan, error) {
	var p Plan
	b, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("plan: read %q: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("plan: decode %q: %w", path, err)
	}
	if err := Validate(p); err != nil {
		return Plan{}, err
	}
	if got := CanonicalHash(p); got != p.ContentHash {
		return Plan{}, fmt.Errorf("%w: stored %q, computed %q (plan file tampered?)", ErrHashMismatch, p.ContentHash, got)
	}
	return p, nil
}
