package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJSONLSinkResumesChainAcrossReopens pins the multi-session contract:
// one file is ONE continuous chain even when different processes (plan, then
// run) append to it, so whole-file verification stays intact.
func TestJSONLSinkResumesChainAcrossReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")

	first, err := NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := first.Write(Record{ActionID: "a", CorrelationID: "session-1"}); err != nil {
			t.Fatal(err)
		}
	}
	first.Close()

	second, err := NewJSONLSink(path) // a later process appends
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := second.Write(Record{ActionID: "b", CorrelationID: "session-2"}); err != nil {
			t.Fatal(err)
		}
	}
	second.Close()

	sink3, err := NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	sink3.Close()

	records := readAll(t, path)
	if len(records) != 5 {
		t.Fatalf("records = %d, want 5", len(records))
	}
	if i := VerifyChain(records); i != -1 {
		t.Fatalf("cross-session chain broke at %d", i)
	}
	// The second session's first record must bind to the first session's
	// last hash — never restart from "".
	if records[3].PrevHash != records[2].RecordHash {
		t.Errorf("reopened sink forked the chain: prev=%q want %q", records[3].PrevHash, records[2].RecordHash)
	}
}

// TestVerifyChainAcceptsContiguousSegment: one run's records filtered out of
// a multi-session log verify as a segment; a tampered segment still fails.
func TestVerifyChainAcceptsContiguousSegment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	s1, _ := NewJSONLSink(path)
	s1.Write(Record{ActionID: "plan-1", CorrelationID: "plan"})
	s1.Close()
	s2, _ := NewJSONLSink(path)
	s2.Write(Record{ActionID: "run-1", CorrelationID: "run-x"})
	s2.Write(Record{ActionID: "run-2", CorrelationID: "run-x"})
	s2.Close()

	all := readAll(t, path)
	var segment []Record
	for _, r := range all {
		if r.CorrelationID == "run-x" {
			segment = append(segment, r)
		}
	}
	if i := VerifyChain(segment); i != -1 {
		t.Fatalf("contiguous segment broke at %d", i)
	}
	segment[1].ExecutionInfo = "forged"
	if i := VerifyChain(segment); i != 1 {
		t.Fatalf("tampered segment must break at 1, got %d", i)
	}
}

func readAll(t *testing.T, path string) []Record {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("parse line: %v", err)
		}
		out = append(out, rec)
	}
	return out
}
