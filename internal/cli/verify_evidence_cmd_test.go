package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// evidenceSinkAt writes two chained records with a recognizable correlation
// id to path and returns the sink for closing.
func evidenceSinkAt(path string) (*evidence.JSONLSink, error) {
	sink, err := evidence.NewJSONLSink(path)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 2; i++ {
		if err := sink.Write(evidence.Record{ActionID: "a", CorrelationID: "legacy-only-run"}); err != nil {
			return nil, err
		}
	}
	return sink, nil
}

// runVerifiedLifecycle executes a happy-path run (as in run_cmd_test) and
// returns the run id.
func runVerifiedLifecycle(t *testing.T) (runID, ws string) {
	t.Helper()
	ws = lifecycleEnv(t)
	fixedClock(t)
	planID := savePlan(t, minimalPlan(ws))
	scriptModel(t,
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_1", Type: "function",
			Function: schema.FunctionCall{Name: "run_command", Arguments: `{"command":"go version"}`},
		}}),
		schema.AssistantMessage("acceptance ran clean", nil),
	)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("lifecycle run failed: %d\n%s", code, stderr.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "run_id: ") {
			return strings.TrimPrefix(line, "run_id: "), ws
		}
	}
	t.Fatal("no run_id in run output")
	return "", ""
}

func TestVerifyPassesOnSealedReceiptByRunID(t *testing.T) {
	runID, _ := runVerifiedLifecycle(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify", "-receipt", runID}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("verify exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"evidence_chain", "acceptance", "result: verified"} {
		if !strings.Contains(out, want) {
			t.Errorf("verify output missing %q:\n%s", want, out)
		}
	}
}

func TestVerifyJSONShape(t *testing.T) {
	runID, _ := runVerifiedLifecycle(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"verify", "-receipt", runID, "-json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("verify -json exit: %s", stderr.String())
	}
	var payload struct {
		RunID    string            `json:"run_id"`
		Result   string            `json:"result"`
		Checks   map[string]string `json:"checks"`
		Verified bool              `json:"verified"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("verify -json unparseable: %v\n%s", err, stdout.String())
	}
	if payload.RunID != runID || !payload.Verified || payload.Result != "verified" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestVerifyRejectsTamperedReceipt(t *testing.T) {
	runID, _ := runVerifiedLifecycle(t)
	path := filepath.Join(ReceiptsDir(), runID+".receipt.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(raw, []byte(`"final_status": "verified"`), []byte(`"final_status": "FORGED"`), 1)
	if bytes.Equal(raw, tampered) {
		t.Fatal("fixture: nothing replaced")
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify", "-receipt", runID}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("a tampered receipt must never verify")
	}
	if !strings.Contains(stderr.String(), "tampered") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
}

func TestVerifyFailsOnTamperedEvidenceChain(t *testing.T) {
	runID, _ := runVerifiedLifecycle(t)
	evPath := filepath.Join(StateDir(), "evidence.jsonl")
	raw, err := os.ReadFile(evPath)
	if err != nil {
		t.Fatal(err)
	}
	// Flip the recorded execution of the first record: the hash chain breaks.
	tampered := bytes.Replace(raw, []byte(`"execution":"ok"`), []byte(`"execution":"no"`), 1)
	if bytes.Equal(raw, tampered) {
		t.Fatal("fixture: nothing replaced")
	}
	if err := os.WriteFile(evPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify", "-receipt", runID}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("verify must fail on a broken evidence chain")
	}
	if !strings.Contains(stdout.String(), "result: tampered") {
		t.Errorf("stdout:\n%s", stdout.String())
	}
}

func TestEvidenceListGroupsByCorrelation(t *testing.T) {
	runID, _ := runVerifiedLifecycle(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"evidence", "list"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("evidence list exit: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), runID) {
		t.Errorf("list missing run correlation %s:\n%s", runID, stdout.String())
	}
}

func TestEvidenceShowFiltersByRunID(t *testing.T) {
	runID, _ := runVerifiedLifecycle(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"evidence", "show", runID}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("evidence show exit: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "run_command") {
		t.Errorf("show output:\n%s", stdout.String())
	}
	// Unknown run id: honest non-zero.
	var so2, se2 bytes.Buffer
	if code := Run([]string{"evidence", "show", "run-nonexistent"}, strings.NewReader(""), &so2, &se2); code == 0 {
		t.Error("show with an unknown id must exit non-zero")
	}
}

func TestEvidenceVerifyChainIntactAndBroken(t *testing.T) {
	runVerifiedLifecycle(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("intact chain exit: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "chain intact") {
		t.Errorf("stdout:\n%s", stdout.String())
	}

	evPath := filepath.Join(StateDir(), "evidence.jsonl")
	raw, _ := os.ReadFile(evPath)
	tampered := bytes.Replace(raw, []byte(`"execution":"ok"`), []byte(`"execution":"no"`), 1)
	os.WriteFile(evPath, tampered, 0o644)
	var so2, se2 bytes.Buffer
	if code := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &so2, &se2); code == 0 {
		t.Fatal("broken chain must exit non-zero")
	}
	if !strings.Contains(so2.String(), "BROKEN") {
		t.Errorf("stdout:\n%s", so2.String())
	}
}

func TestEvidenceVerifyChainReportsMalformedLineNumber(t *testing.T) {
	runVerifiedLifecycle(t)
	evPath := filepath.Join(StateDir(), "evidence.jsonl")
	f, err := os.OpenFile(evPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("this is not json\n")
	f.Close()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("malformed log must exit non-zero")
	}
	if !strings.Contains(stderr.String(), "line") {
		t.Errorf("stderr must carry the line number:\n%s", stderr.String())
	}
}

// TestEvidenceCommandsDiscoverLegacyOnlyLog: a user whose only evidence
// lives at the legacy state path must be able to run the read-only commands
// BEFORE any plan/run — legacy discovery (hash-verified, non-destructive
// copy) must fire on the read path too.
func TestEvidenceCommandsDiscoverLegacyOnlyLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	legacyDir := LegacyStateDir()
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(legacyDir, "evidence.jsonl")
	sink, err := evidenceSinkAt(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	sink.Close()

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"evidence", "list"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("evidence list on a legacy-only log exit = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "legacy-only-run") {
		t.Fatalf("legacy-only records not discovered:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	// Non-destructive: the legacy file must still exist untouched.
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy log was moved or deleted: %v", err)
	}
	// And the chain must verify through the same discovery path.
	var so2, se2 bytes.Buffer
	if code := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &so2, &se2); code != 0 {
		t.Fatalf("verify-chain on discovered legacy log exit = %d\n%s", code, se2.String())
	}
}

func TestEvidenceUnknownSchemaVersionWarns(t *testing.T) {
	runVerifiedLifecycle(t)
	evPath := filepath.Join(StateDir(), "evidence.jsonl")
	f, err := os.OpenFile(evPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// A structurally valid record from a future schema; the chain will be
	// broken (verify-chain's business), but list/show must WARN, not crash.
	f.WriteString(`{"schema_version":"intent-bob-eino-evidence/v99","action_id":"zz","correlation_id":"future-run","timestamp":"2027-01-01T00:00:00Z","agent":{"name":"x","version":"1"},"engine":"e","engine_version":"1","tool":{"name":"t","version":"1"},"asset":"a","environment":"local","risk_class":"R0","policy_version":"1","policy_hash":"h","authorization":"allowed","args_hash":"h","execution":"ok","verified":"n/a"}` + "\n")
	f.Close()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"evidence", "list"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("list exit: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported schema_version") {
		t.Errorf("expected a schema WARN on stderr:\n%s", stderr.String())
	}
}
