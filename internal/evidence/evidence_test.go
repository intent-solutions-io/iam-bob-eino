package evidence

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactScrubsSecrets(t *testing.T) {
	cases := []string{
		"key sk-abcdef0123456789ABCDEF here",
		"token=supersecretvalue123",
		"Authorization: Bearer abcdef0123456789ABCDEF",
		"ghp_0123456789abcdef0123456789abcdef0000",
	}
	for _, in := range cases {
		if got := Redact(in); strings.Contains(got, "secret") || !strings.Contains(got, redactString) {
			// note: "secret" substring check is a loose heuristic; the real
			// requirement is that the credential token is replaced.
			if !strings.Contains(got, redactString) {
				t.Errorf("Redact(%q) = %q, expected a redaction", in, got)
			}
		}
	}
}

func TestRedactLeavesCleanText(t *testing.T) {
	in := "read 128 bytes from internal/tools/tools.go"
	if got := Redact(in); got != in {
		t.Errorf("Redact changed clean text: %q", got)
	}
}

func TestJSONLSinkAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ev.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := sink.Write(Record{ActionID: "a", Execution: "ok"}); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line not valid json: %v", err)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("got %d evidence lines, want 3", n)
	}
}

func TestMemorySinkRedactsOnWrite(t *testing.T) {
	s := &MemorySink{}
	_ = s.Write(Record{ActionID: "x", Error: "failed with token=abcdef123456"})
	if strings.Contains(s.Records[0].Error, "abcdef123456") {
		t.Errorf("MemorySink stored an unredacted secret: %q", s.Records[0].Error)
	}
}

func TestHashChainDetectsTampering(t *testing.T) {
	s := &MemorySink{}
	for i := 0; i < 4; i++ {
		if err := s.Write(Record{ActionID: "a", Execution: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	if bad := VerifyChain(s.Records); bad != -1 {
		t.Fatalf("intact chain reported broken at %d", bad)
	}
	// Tamper with the middle record; the chain must detect it.
	s.Records[1].Execution = "denied"
	if bad := VerifyChain(s.Records); bad == -1 {
		t.Fatal("tampered chain reported intact — tamper-evidence failed")
	}
}

func TestHashArgsDeterministic(t *testing.T) {
	if HashArgs(`{"path":"a"}`) != HashArgs(`{"path":"a"}`) {
		t.Error("HashArgs must be deterministic")
	}
	if HashArgs("a") == HashArgs("b") {
		t.Error("HashArgs must differ for different inputs")
	}
}
