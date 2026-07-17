package receipt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

// sampleReceipt returns a fully populated receipt for tests. Values are
// deterministic so hash tests are byte-stable.
func sampleReceipt() RunReceipt {
	return RunReceipt{
		SchemaVersion:          SchemaVersion,
		RunID:                  "run-0001",
		PlanID:                 "plan-0001",
		PlanHash:               "sha256:abc123",
		Task:                   "add unit tests for the parser",
		AgentName:              version.Agent,
		AgentVersion:           version.AgentVersion,
		Engine:                 version.Engine,
		EngineVersion:          version.EngineVersion,
		Provider:               "anthropic",
		Model:                  "deterministic-model-stub",
		WorkspaceIdentity:      "workspace-a",
		WorkspaceStartSHA:      "1111111111111111111111111111111111111111",
		WorkspaceEndSHA:        "2222222222222222222222222222222222222222",
		RequestedCapabilities:  []string{"read", "write", "exec"},
		AuthorizedCapabilities: []string{"read", "write"},
		PolicyDecisions:        []string{"write allowed by policy v1", "exec denied: not in AllowedCommands"},
		Approvals:              []string{"approval-7: write internal/parser"},
		ToolCalls:              5,
		FilesChanged:           []string{"internal/parser/parser.go", "internal/parser/parser_test.go"},
		PatchesApplied:         2,
		CommandsRun:            []string{"go test ./internal/parser/..."},
		TestResults:            []string{"ok  	internal/parser	0.01s"},
		AgentClaim:             "Added table-driven tests covering all parser branches.",
		ExecutionResult:        "ok",
		VerifierResult:         "verified",
		FinalStatus:            "success",
		StartedAt:              "2026-07-16T00:00:00Z",
		CompletedAt:            "2026-07-16T00:05:00Z",
		Usage:                  map[string]any{"input_tokens": 100, "output_tokens": 50},
		Authority:              AuthorityLocalUntrusted,
	}
}

// TestSchemaRoundTrip checks the exact JSON tag set and lossless round-trip
// (item 89).
func TestSchemaRoundTrip(t *testing.T) {
	r := sampleReceipt()
	sealed, err := Seal(r)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	b, err := json.Marshal(sealed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatalf("unmarshal to key map: %v", err)
	}
	wantKeys := []string{
		"schema_version", "run_id", "plan_id", "plan_hash", "task",
		"agent_name", "agent_version", "engine", "engine_version",
		"provider", "model", "workspace_identity", "workspace_start_sha",
		"workspace_end_sha", "requested_capabilities", "authorized_capabilities",
		"policy_decisions", "approvals", "tool_calls", "files_changed",
		"patches_applied", "commands_run", "test_results", "agent_claim",
		"execution_result", "verifier_result", "final_status",
		"started_at", "completed_at", "usage", "content_hash", "authority",
	}
	for _, k := range wantKeys {
		if _, ok := keys[k]; !ok {
			t.Errorf("marshaled receipt missing JSON key %q", k)
		}
	}
	if len(keys) != len(wantKeys) {
		t.Errorf("marshaled receipt has %d keys, want %d", len(keys), len(wantKeys))
	}

	var back RunReceipt
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal back: %v", err)
	}
	// Usage round-trips through JSON with numbers as float64; normalize both
	// sides through canonical JSON for comparison.
	gotJSON, _ := canonicalJSON(back)
	wantJSON, _ := canonicalJSON(sealed)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("round-trip mismatch:\n got  %s\n want %s", gotJSON, wantJSON)
	}
	if !VerifyHash(back) {
		t.Error("round-tripped receipt fails VerifyHash")
	}
}

// TestUsageOmitEmpty checks that a nil usage map is omitted entirely.
func TestUsageOmitEmpty(t *testing.T) {
	r := sampleReceipt()
	r.Usage = nil
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"usage"`) {
		t.Error("nil usage map should be omitted from JSON")
	}
}

// TestCanonicalHashDeterministic checks the hash is stable and content-bound
// (item 90).
func TestCanonicalHashDeterministic(t *testing.T) {
	r := sampleReceipt()

	h1 := CanonicalHash(r)
	h2 := CanonicalHash(r)
	if h1 != h2 {
		t.Errorf("same receipt hashed differently: %s vs %s", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash %q lacks sha256: prefix", h1)
	}

	// content_hash is zeroed before hashing, so a stale/preset hash must not
	// change the result.
	withHash := r
	withHash.ContentHash = "sha256:stale"
	if got := CanonicalHash(withHash); got != h1 {
		t.Errorf("preset content_hash changed the canonical hash: %s vs %s", got, h1)
	}

	// The canonical byte encoding itself must be stable call-to-call.
	b1, err := canonicalJSON(r)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	b2, err := canonicalJSON(r)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	if string(b1) != string(b2) {
		t.Error("canonical JSON not byte-stable across calls")
	}

	// Any content change must change the hash.
	cases := []struct {
		name   string
		mutate func(*RunReceipt)
	}{
		{"task", func(r *RunReceipt) { r.Task = "different task" }},
		{"tool_calls", func(r *RunReceipt) { r.ToolCalls++ }},
		{"files_changed", func(r *RunReceipt) { r.FilesChanged = append(r.FilesChanged, "x.go") }},
		{"usage", func(r *RunReceipt) { r.Usage = map[string]any{"input_tokens": 999} }},
		{"final_status", func(r *RunReceipt) { r.FinalStatus = "failure" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sampleReceipt()
			tc.mutate(&m)
			if CanonicalHash(m) == h1 {
				t.Errorf("mutating %s did not change the canonical hash", tc.name)
			}
		})
	}
}

// TestSealAndVerifyHash checks the seal/verify pair.
func TestSealAndVerifyHash(t *testing.T) {
	sealed, err := Seal(sampleReceipt())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed.ContentHash == "" {
		t.Fatal("Seal left content_hash empty")
	}
	if !VerifyHash(sealed) {
		t.Error("sealed receipt fails VerifyHash")
	}

	tampered := sealed
	tampered.PatchesApplied++
	if VerifyHash(tampered) {
		t.Error("VerifyHash accepted a tampered receipt")
	}

	unsealed := sampleReceipt()
	if VerifyHash(unsealed) {
		t.Error("VerifyHash accepted a receipt with no content_hash")
	}
}

// TestSealRefusesForeignAuthority checks authority is pinned to
// local_untrusted (item 93).
func TestSealRefusesForeignAuthority(t *testing.T) {
	for _, authority := range []string{"trusted", "remote_attested", "LOCAL_UNTRUSTED", "verified"} {
		r := sampleReceipt()
		r.Authority = authority
		if _, err := Seal(r); err == nil {
			t.Errorf("Seal accepted authority %q, want refusal", authority)
		}
	}

	// Empty authority is fine: Seal normalizes it to local_untrusted.
	r := sampleReceipt()
	r.Authority = ""
	sealed, err := Seal(r)
	if err != nil {
		t.Fatalf("Seal with empty authority: %v", err)
	}
	if sealed.Authority != AuthorityLocalUntrusted {
		t.Errorf("sealed authority = %q, want %q", sealed.Authority, AuthorityLocalUntrusted)
	}
}

// TestRedact checks agent_claim bounding and credential scrubbing (item 91).
func TestRedact(t *testing.T) {
	const canaryKey = "sk-canary1234567890abcdef"
	r := sampleReceipt()
	r.AgentClaim = strings.Repeat("x", MaxAgentClaimLen+500)
	r.Task = "call the api with " + canaryKey
	r.ExecutionResult = "Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456"
	r.CommandsRun = []string{"curl -H 'api_key: " + canaryKey + "' https://example.invalid"}
	r.Usage = map[string]any{"note": "token=supersecretvalue123", "input_tokens": 7}
	r.Authority = "somewhere_else"

	got := Redact(r)

	if len(got.AgentClaim) != MaxAgentClaimLen {
		t.Errorf("agent_claim length = %d, want bounded to %d", len(got.AgentClaim), MaxAgentClaimLen)
	}
	if got.Authority != AuthorityLocalUntrusted {
		t.Errorf("authority = %q, want %q", got.Authority, AuthorityLocalUntrusted)
	}

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, secret := range []string{canaryKey, "abcdefghijklmnopqrstuvwxyz123456", "supersecretvalue123"} {
		if strings.Contains(s, secret) {
			t.Errorf("redacted receipt still contains secret %q", secret)
		}
	}
	if !strings.Contains(s, "[REDACTED]") {
		t.Error("redacted receipt carries no [REDACTED] placeholder — scrubbing did not run")
	}
	if n, ok := got.Usage["input_tokens"].(int); !ok || n != 7 {
		t.Errorf("non-string usage value altered: %v", got.Usage["input_tokens"])
	}
}

// TestSealRedacts checks Seal applies redaction before hashing.
func TestSealRedacts(t *testing.T) {
	r := sampleReceipt()
	r.AgentClaim = "leaked key sk-canary1234567890abcdef"
	sealed, err := Seal(r)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed.AgentClaim, "sk-canary") {
		t.Error("Seal did not scrub agent_claim")
	}
	if !VerifyHash(sealed) {
		t.Error("sealed+redacted receipt fails VerifyHash")
	}
}

// TestSaveLoadRoundTrip checks persistence with mode 0600 and hash re-check
// on load (item 92).
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sealed, err := Seal(sampleReceipt())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	path, err := Save(sealed, dir)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("Save wrote outside dir: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("receipt file mode = %o, want 0600", perm)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	gotJSON, _ := canonicalJSON(loaded)
	wantJSON, _ := canonicalJSON(sealed)
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Errorf("loaded receipt differs:\n got  %s\n want %s", gotJSON, wantJSON)
	}
}

// TestSaveRefusesUnsealed checks Save rejects a receipt without a valid hash.
func TestSaveRefusesUnsealed(t *testing.T) {
	if _, err := Save(sampleReceipt(), t.TempDir()); err == nil {
		t.Error("Save accepted an unsealed receipt")
	}
}

// TestLoadRejectsTampered checks tamper detection on load (item 92).
func TestLoadRejectsTampered(t *testing.T) {
	dir := t.TempDir()
	sealed, err := Seal(sampleReceipt())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	path, err := Save(sealed, dir)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Edit a field on disk after sealing.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tampered := strings.Replace(string(b), `"final_status": "success"`, `"final_status": "failure"`, 1)
	if tampered == string(b) {
		t.Fatal("test setup: tamper replacement did not apply")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	if _, err := Load(path); !errors.Is(err, ErrTampered) {
		t.Errorf("Load(tampered) err = %v, want ErrTampered", err)
	}
}

// evidenceRecord builds one deterministic evidence record for loader tests.
func evidenceRecord(i string) evidence.Record {
	return evidence.Record{
		ActionID:      "act-" + i,
		CorrelationID: "run-0001",
		Timestamp:     "2026-07-16T00:00:00Z",
		Agent:         evidence.Identity{Name: version.Agent, Version: version.AgentVersion},
		Engine:        version.Engine,
		EngineVersion: version.EngineVersion,
		Tool:          evidence.ToolRef{Name: "write_file", Version: "1"},
		Asset:         "internal/parser/parser.go",
		Environment:   "workspace",
		RiskClass:     "R2",
		PolicyVersion: "v1",
		PolicyHash:    "sha256:policy",
		Authorization: "allowed",
		ArgsHash:      evidence.HashArgs("args-" + i),
		Execution:     "ok",
		Verified:      "verified",
	}
}

// writeEvidenceLog writes n chained records through the real JSONL sink and
// returns the path.
func writeEvidenceLog(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	sink, err := evidence.NewJSONLSink(path)
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}
	defer sink.Close()
	for i := 0; i < n; i++ {
		if err := sink.Write(evidenceRecord(string(rune('a' + i)))); err != nil {
			t.Fatalf("sink.Write: %v", err)
		}
	}
	return path
}

// TestLoadEvidenceLog checks JSONL parsing of a real sink file (item 94).
func TestLoadEvidenceLog(t *testing.T) {
	path := writeEvidenceLog(t, 3)
	records, err := LoadEvidenceLog(path)
	if err != nil {
		t.Fatalf("LoadEvidenceLog: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("loaded %d records, want 3", len(records))
	}
	if records[0].ActionID != "act-a" || records[2].ActionID != "act-c" {
		t.Errorf("record order wrong: %s .. %s", records[0].ActionID, records[2].ActionID)
	}
	if records[0].PrevHash != "" {
		t.Errorf("first record prev_hash = %q, want empty", records[0].PrevHash)
	}
	if records[1].PrevHash != records[0].RecordHash {
		t.Error("chain linkage lost in load")
	}
}

// TestLoadEvidenceLogTolerantOfBlankLines checks blank/trailing lines are
// skipped.
func TestLoadEvidenceLogTolerantOfBlankLines(t *testing.T) {
	path := writeEvidenceLog(t, 2)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString("\n\n"); err != nil {
		t.Fatalf("append blanks: %v", err)
	}
	f.Close()

	records, err := LoadEvidenceLog(path)
	if err != nil {
		t.Fatalf("LoadEvidenceLog with blanks: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("loaded %d records, want 2", len(records))
	}
}

// TestLoadEvidenceLogMalformedLine checks the typed error carries the line
// number.
func TestLoadEvidenceLogMalformedLine(t *testing.T) {
	path := writeEvidenceLog(t, 2)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatalf("append garbage: %v", err)
	}
	f.Close()

	_, err = LoadEvidenceLog(path)
	var mle *MalformedLineError
	if !errors.As(err, &mle) {
		t.Fatalf("err = %v, want *MalformedLineError", err)
	}
	if mle.Line != 3 {
		t.Errorf("malformed line = %d, want 3", mle.Line)
	}
}

// TestLoadEvidenceLogMissingFile checks a missing file errors cleanly.
func TestLoadEvidenceLogMissingFile(t *testing.T) {
	if _, err := LoadEvidenceLog(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("LoadEvidenceLog on a missing file returned nil error")
	}
}

// TestVerifyChainFromFile checks intact and broken chains (item 94 / §23).
func TestVerifyChainFromFile(t *testing.T) {
	t.Run("intact", func(t *testing.T) {
		path := writeEvidenceLog(t, 3)
		idx, err := VerifyChainFromFile(path)
		if err != nil {
			t.Fatalf("VerifyChainFromFile: %v", err)
		}
		if idx != -1 {
			t.Errorf("intact chain reported broken at %d", idx)
		}
	})

	t.Run("tampered record", func(t *testing.T) {
		path := writeEvidenceLog(t, 3)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("test setup: %d lines, want 3", len(lines))
		}
		// Edit the middle record's content without recomputing its hash.
		var rec evidence.Record
		if err := json.Unmarshal([]byte(lines[1]), &rec); err != nil {
			t.Fatalf("unmarshal line 2: %v", err)
		}
		rec.Asset = "somewhere/else.go"
		edited, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal edited: %v", err)
		}
		lines[1] = string(edited)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("rewrite: %v", err)
		}

		idx, err := VerifyChainFromFile(path)
		if err != nil {
			t.Fatalf("VerifyChainFromFile: %v", err)
		}
		if idx != 1 {
			t.Errorf("broken chain index = %d, want 1", idx)
		}
	})

	t.Run("deleted record", func(t *testing.T) {
		path := writeEvidenceLog(t, 3)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		// Drop the middle record entirely; the chain must break at the
		// record that pointed at it.
		out := lines[0] + "\n" + lines[2] + "\n"
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatalf("rewrite: %v", err)
		}

		idx, err := VerifyChainFromFile(path)
		if err != nil {
			t.Fatalf("VerifyChainFromFile: %v", err)
		}
		if idx != 1 {
			t.Errorf("broken chain index = %d, want 1", idx)
		}
	})
}
