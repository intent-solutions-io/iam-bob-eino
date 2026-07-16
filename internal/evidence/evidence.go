// Package evidence defines Bob's structured execution-evidence record and an
// append-only sink for it. The record schema is deliberately shaped to be
// projectable into Mission Control's governance record (see intent-os D88): each
// field maps to an MC projected-record field, and every value is content-safe —
// no tool arguments, file contents, or secrets are ever stored, only hashes,
// workspace-relative paths, and short redacted summaries.
//
// This package is the Mission-Control-compatible evidence *boundary*, not a
// Mission Control implementation. Projection into MC happens through the
// seams package.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Identity names the acting agent for the evidence record.
type Identity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolRef names the tool and its version for the evidence record.
type ToolRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Record is a single content-safe execution-evidence entry. Field names mirror
// the MC projected-record contract so records project without transformation.
type Record struct {
	ActionID      string   `json:"action_id"`
	CorrelationID string   `json:"correlation_id"`
	Timestamp     string   `json:"timestamp"`
	Agent         Identity `json:"agent"`
	Engine        string   `json:"engine"`
	EngineVersion string   `json:"engine_version"`
	Tool          ToolRef  `json:"tool"`
	Asset         string   `json:"asset"` // workspace-relative path or command name
	Environment   string   `json:"environment"`
	RiskClass     string   `json:"risk_class"`
	PolicyVersion string   `json:"policy_version"`
	PolicyHash    string   `json:"policy_hash"`
	Authorization string   `json:"authorization"` // allowed | denied
	ApprovalID    string   `json:"approval_id,omitempty"`
	ArgsHash      string   `json:"args_hash"` // sha256 of raw args, content-safe
	Execution     string   `json:"execution"` // ok | error | denied | skipped
	ExecutionInfo string   `json:"execution_info,omitempty"`
	Verified      string   `json:"verified"` // verified | mismatch | unverified | n/a
	VerifyInfo    string   `json:"verify_info,omitempty"`
	Error         string   `json:"error,omitempty"`

	// PrevHash and RecordHash form a tamper-evident chain: RecordHash is the
	// sha256 of this record's content plus PrevHash. Deleting or editing a
	// record breaks the chain for every record after it.
	PrevHash   string `json:"prev_hash,omitempty"`
	RecordHash string `json:"record_hash,omitempty"`
}

// Sink accepts completed evidence records. Implementations must be safe for
// concurrent use by multiple governed tools.
type Sink interface {
	Write(rec Record) error
}

// JSONLSink appends evidence records as one JSON object per line to a file. The
// append-only shape gives a simple tamper-evident-friendly log that a downstream
// projector can stream into Mission Control.
type JSONLSink struct {
	mu   sync.Mutex
	file *os.File
	last string // hash of the previous record, for chaining
}

// NewJSONLSink opens (creating if needed) an append-only evidence log at path.
func NewJSONLSink(path string) (*JSONLSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open evidence log %q: %w", path, err)
	}
	return &JSONLSink{file: f}, nil
}

// Write appends one redacted, hash-chained record as a JSON line and fsyncs so
// the audit trail survives a crash.
func (s *JSONLSink) Write(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec = chain(redactRecord(rec), s.last)
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	if _, err := s.file.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write evidence: %w", err)
	}
	s.last = rec.RecordHash
	_ = s.file.Sync()
	return nil
}

// Close releases the underlying file.
func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

// MemorySink collects records in memory. Intended for tests and for callers that
// project evidence elsewhere.
type MemorySink struct {
	mu      sync.Mutex
	last    string
	Records []Record
}

// Write appends a redacted, hash-chained record to the in-memory slice.
func (s *MemorySink) Write(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec = chain(redactRecord(rec), s.last)
	s.Records = append(s.Records, rec)
	s.last = rec.RecordHash
	return nil
}

// HashArgs returns a content-safe sha256 fingerprint of raw tool arguments so
// evidence can bind an action to its inputs without ever storing them.
func HashArgs(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Now returns an RFC3339 timestamp for a record.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// redactRecord scrubs the free-text fields of a record as a defense-in-depth
// backstop, even though callers are expected to pass only content-safe values.
func redactRecord(rec Record) Record {
	rec.Asset = Redact(rec.Asset)
	rec.ExecutionInfo = Redact(rec.ExecutionInfo)
	rec.VerifyInfo = Redact(rec.VerifyInfo)
	rec.Error = Redact(rec.Error)
	return rec
}

// chain sets PrevHash and computes RecordHash over the record content plus the
// previous hash, forming a tamper-evident chain.
func chain(rec Record, prev string) Record {
	rec.PrevHash = prev
	rec.RecordHash = ""
	b, _ := json.Marshal(rec)
	sum := sha256.Sum256(append([]byte(prev), b...))
	rec.RecordHash = "sha256:" + hex.EncodeToString(sum[:])
	return rec
}

// VerifyChain checks that a slice of records forms an intact hash chain.
// It returns the index of the first broken record, or -1 if the chain is valid.
func VerifyChain(records []Record) int {
	prev := ""
	for i, rec := range records {
		want := rec.RecordHash
		if chain(rec, prev).RecordHash != want || rec.PrevHash != prev {
			return i
		}
		prev = want
	}
	return -1
}
