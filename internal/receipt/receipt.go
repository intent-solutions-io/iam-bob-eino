// Package receipt produces Bob's deterministic run receipt — a single
// content-safe JSON document that summarizes one agent run end-to-end
// (identity, capabilities, policy decisions, workspace SHAs, results) — and
// provides the evidence-JSONL loader used to re-verify the hash-chained
// evidence log a run left behind.
//
// A receipt is sealed with a canonical content hash so any post-hoc edit is
// detectable, and it always carries authority "local_untrusted": a receipt is
// the agent's own account of what happened, never a trusted attestation.
package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// SchemaVersion identifies the receipt schema emitted by this package.
const SchemaVersion = "bob.receipt.v1"

// AuthorityLocalUntrusted is the only authority value a receipt may carry:
// receipts are self-reported by the agent process and must never claim any
// higher trust level.
const AuthorityLocalUntrusted = "local_untrusted"

// MaxAgentClaimLen bounds the free-text agent_claim field so a runaway model
// summary cannot bloat or smuggle content into the audit record.
const MaxAgentClaimLen = 2000

// RunReceipt is the deterministic, content-safe summary of one agent run.
// All fields hold hashes, identifiers, short summaries, or workspace-relative
// paths — never raw file contents or secrets.
type RunReceipt struct {
	SchemaVersion          string         `json:"schema_version"`
	RunID                  string         `json:"run_id"`
	PlanID                 string         `json:"plan_id"`
	PlanHash               string         `json:"plan_hash"`
	Task                   string         `json:"task"`
	AgentName              string         `json:"agent_name"`
	AgentVersion           string         `json:"agent_version"`
	Engine                 string         `json:"engine"`
	EngineVersion          string         `json:"engine_version"`
	Provider               string         `json:"provider"`
	Model                  string         `json:"model"`
	WorkspaceIdentity      string         `json:"workspace_identity"`
	WorkspaceStartSHA      string         `json:"workspace_start_sha"`
	WorkspaceEndSHA        string         `json:"workspace_end_sha"`
	RequestedCapabilities  []string       `json:"requested_capabilities"`
	AuthorizedCapabilities []string       `json:"authorized_capabilities"`
	PolicyDecisions        []string       `json:"policy_decisions"`
	Approvals              []string       `json:"approvals"`
	ToolCalls              int            `json:"tool_calls"`
	FilesChanged           []string       `json:"files_changed"`
	PatchesApplied         int            `json:"patches_applied"`
	CommandsRun            []string       `json:"commands_run"`
	TestResults            []string       `json:"test_results"`
	AgentClaim             string         `json:"agent_claim"`
	ExecutionResult        string         `json:"execution_result"`
	VerifierResult         string         `json:"verifier_result"`
	FinalStatus            string         `json:"final_status"`
	StartedAt              string         `json:"started_at"`
	CompletedAt            string         `json:"completed_at"`
	Usage                  map[string]any `json:"usage,omitempty"`
	ContentHash            string         `json:"content_hash"`
	Authority              string         `json:"authority"`
}

// CanonicalHash computes the deterministic content hash of a receipt: the
// sha256 of its canonical JSON encoding (object keys sorted, content_hash
// zeroed). Two receipts with identical content always hash identically,
// byte-for-byte, regardless of when or where they are hashed.
func CanonicalHash(r RunReceipt) string {
	r.ContentHash = ""
	b, err := canonicalJSON(r)
	if err != nil {
		// A RunReceipt of plain strings/ints/slices/maps cannot fail to
		// marshal unless Usage carries an unmarshalable value; treat that as
		// an un-hashable receipt with a sentinel no real hash can equal.
		return "unhashable:" + err.Error()
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalJSON renders v as deterministic JSON: marshal, round-trip through
// map[string]any, and marshal again so encoding/json emits object keys in
// sorted order at every level.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical json: marshal: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("canonical json: round-trip: %w", err)
	}
	out, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("canonical json: re-marshal: %w", err)
	}
	return out, nil
}

// Seal redacts the receipt, then sets its content hash. It refuses to seal a
// receipt whose authority is anything other than "local_untrusted" — receipts
// must never launder themselves into a higher trust level.
func Seal(r RunReceipt) (RunReceipt, error) {
	if r.Authority != "" && r.Authority != AuthorityLocalUntrusted {
		return RunReceipt{}, fmt.Errorf("receipt: refusing to seal with authority %q; only %q is allowed", r.Authority, AuthorityLocalUntrusted)
	}
	r = Redact(r)
	if r.SchemaVersion == "" {
		r.SchemaVersion = SchemaVersion
	}
	r.ContentHash = CanonicalHash(r)
	return r, nil
}

// VerifyHash reports whether the receipt's content_hash matches its content.
func VerifyHash(r RunReceipt) bool {
	return r.ContentHash != "" && r.ContentHash == CanonicalHash(r)
}

// Redact returns a content-safe copy of the receipt: agent_claim is bounded
// to MaxAgentClaimLen, every free-text field is scrubbed of credential-shaped
// values (API keys, bearer tokens, key=value secrets), and authority is
// forced to "local_untrusted".
func Redact(r RunReceipt) RunReceipt {
	if len(r.AgentClaim) > MaxAgentClaimLen {
		r.AgentClaim = r.AgentClaim[:MaxAgentClaimLen]
	}
	r.Task = evidence.Redact(r.Task)
	r.AgentClaim = evidence.Redact(r.AgentClaim)
	r.ExecutionResult = evidence.Redact(r.ExecutionResult)
	r.VerifierResult = evidence.Redact(r.VerifierResult)
	r.FinalStatus = evidence.Redact(r.FinalStatus)
	r.PolicyDecisions = redactSlice(r.PolicyDecisions)
	r.Approvals = redactSlice(r.Approvals)
	r.FilesChanged = redactSlice(r.FilesChanged)
	r.CommandsRun = redactSlice(r.CommandsRun)
	r.TestResults = redactSlice(r.TestResults)
	r.RequestedCapabilities = redactSlice(r.RequestedCapabilities)
	r.AuthorizedCapabilities = redactSlice(r.AuthorizedCapabilities)
	r.Usage = redactUsage(r.Usage)
	r.Authority = AuthorityLocalUntrusted
	return r
}

// redactSlice scrubs each element of a string slice.
func redactSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = evidence.Redact(s)
	}
	return out
}

// redactUsage scrubs string values inside the free-form usage map; non-string
// values (token counts etc.) pass through unchanged.
func redactUsage(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = evidence.Redact(s)
		} else {
			out[k] = v
		}
	}
	return out
}

// Save seals-verified receipt r as pretty JSON into dir with mode 0600 and
// returns the written path. The receipt must already be sealed and intact;
// Save re-verifies rather than silently persisting a tampered or unsealed
// receipt.
func Save(r RunReceipt, dir string) (string, error) {
	if !VerifyHash(r) {
		return "", fmt.Errorf("receipt: refusing to save receipt with missing or mismatched content_hash (seal it first)")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("receipt: create dir %q: %w", dir, err)
	}
	name := sanitizeFilename(r.RunID)
	if name == "" {
		name = "receipt"
	}
	path := filepath.Join(dir, name+".receipt.json")
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("receipt: marshal: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("receipt: write %q: %w", path, err)
	}
	return path, nil
}

// sanitizeFilename keeps only filesystem-safe characters from a run id.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "._")
}

// Load reads a receipt from path and re-verifies its content hash, rejecting
// any receipt that was edited after sealing.
func Load(path string) (RunReceipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RunReceipt{}, fmt.Errorf("receipt: read %q: %w", path, err)
	}
	var r RunReceipt
	if err := json.Unmarshal(b, &r); err != nil {
		return RunReceipt{}, fmt.Errorf("receipt: parse %q: %w", path, err)
	}
	if !VerifyHash(r) {
		return RunReceipt{}, fmt.Errorf("receipt: %w: %q", ErrTampered, path)
	}
	return r, nil
}

// ErrTampered marks a receipt whose stored content_hash does not match its
// content — the file was edited (or corrupted) after sealing.
var ErrTampered = fmt.Errorf("content_hash mismatch, receipt tampered or unsealed")

// MalformedLineError reports an evidence-log line that failed to parse as an
// evidence.Record, identifying the 1-based line number.
type MalformedLineError struct {
	Path string
	Line int
	Err  error
}

// Error implements the error interface.
func (e *MalformedLineError) Error() string {
	return fmt.Sprintf("evidence log %q: malformed record on line %d: %v", e.Path, e.Line, e.Err)
}

// Unwrap exposes the underlying parse error.
func (e *MalformedLineError) Unwrap() error { return e.Err }

// LoadEvidenceLog reads a JSONL evidence sink (as written by
// evidence.JSONLSink) into typed records. Blank lines and a trailing newline
// are tolerated; any non-blank line that is not a valid JSON record returns a
// *MalformedLineError.
func LoadEvidenceLog(path string) ([]evidence.Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("evidence log: read %q: %w", path, err)
	}
	var records []evidence.Record
	for i, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec evidence.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, &MalformedLineError{Path: path, Line: i + 1, Err: err}
		}
		records = append(records, rec)
	}
	return records, nil
}

// VerifyChainFromFile loads the evidence log at path and verifies its hash
// chain via evidence.VerifyChain. It returns the index of the first broken
// record, or -1 if the chain is intact.
func VerifyChainFromFile(path string) (int, error) {
	records, err := LoadEvidenceLog(path)
	if err != nil {
		return 0, err
	}
	return evidence.VerifyChain(records), nil
}
