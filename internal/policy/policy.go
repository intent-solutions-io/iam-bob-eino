// Package policy defines Bob's risk model and the deterministic policy boundary
// that every governed tool call passes through before it runs. Risk classes and
// the policy decision are content-free and cheap to evaluate, so the boundary is
// safe to place on the hot path of each tool.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// RiskClass ranks a tool action by potential blast radius. It mirrors the R0–R4
// risk tiers used across the Intent Agent Model family so evidence projects
// cleanly into Mission Control's risk-class field.
type RiskClass int

const (
	// R0 read-only: inspect state, no mutation (read_file, list_dir).
	R0 RiskClass = iota
	// R1 low-mutation/search: derived reads over many files (search_code).
	R1
	// R2 execution: run an allowlisted command such as the test suite.
	R2
	// R3 write: mutate a file inside the workspace.
	R3
	// R4 destructive: delete/overwrite broadly or act outside the workspace.
	R4
)

// String renders the canonical R0–R4 label used in evidence records.
func (r RiskClass) String() string {
	switch r {
	case R0:
		return "R0"
	case R1:
		return "R1"
	case R2:
		return "R2"
	case R3:
		return "R3"
	case R4:
		return "R4"
	default:
		return "R?"
	}
}

// Decision is the outcome of evaluating a tool action against the policy.
type Decision struct {
	// Allowed reports whether the action may proceed at all.
	Allowed bool
	// RequiresApproval reports whether an approver must also authorize it.
	RequiresApproval bool
	// Reason is a content-safe explanation, surfaced to the model on denial.
	Reason string
}

// Policy is the deterministic rule set governing tool actions. It is immutable
// after construction; Hash provides a stable fingerprint for evidence.
type Policy struct {
	// Version identifies the policy revision in evidence records.
	Version string `json:"version"`
	// AllowWrites gates whether R3 (write) actions may proceed. Default false.
	AllowWrites bool `json:"allow_writes"`
	// AllowedCommands is the exact allowlist of command names R2 tools may run.
	AllowedCommands []string `json:"allowed_commands"`
}

// Default returns the safe baseline policy: reads and search are allowed, the
// test-runner commands are allowlisted but require approval, writes are denied
// until explicitly enabled, and anything R4 is refused.
func Default() Policy {
	return Policy{
		Version:         "1",
		AllowWrites:     false,
		AllowedCommands: []string{"go", "make", "pytest", "npm", "pnpm", "cargo", "git"},
	}
}

// Hash returns a sha256 fingerprint of the canonical policy, so evidence can
// bind each action to the exact policy revision that authorized it.
func (p Policy) Hash() string {
	cp := p
	cp.AllowedCommands = append([]string(nil), p.AllowedCommands...)
	sort.Strings(cp.AllowedCommands)
	b, _ := json.Marshal(cp)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CommandAllowed reports whether the first token of a command line is on the
// allowlist. Callers pass the raw command; only the program name is matched.
func (p Policy) CommandAllowed(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	program := fields[0]
	for _, allowed := range p.AllowedCommands {
		if program == allowed {
			return true
		}
	}
	return false
}

// Evaluate applies the policy to a tool action of the given risk class.
func (p Policy) Evaluate(risk RiskClass) Decision {
	switch risk {
	case R0, R1:
		return Decision{Allowed: true, RequiresApproval: false, Reason: "read-only action permitted"}
	case R2:
		return Decision{Allowed: true, RequiresApproval: true, Reason: "execution requires approval"}
	case R3:
		if !p.AllowWrites {
			return Decision{Allowed: false, RequiresApproval: false, Reason: "writes are disabled by policy (enable with --allow-writes)"}
		}
		return Decision{Allowed: true, RequiresApproval: true, Reason: "write requires approval"}
	default: // R4 and unknown
		return Decision{Allowed: false, RequiresApproval: false, Reason: "destructive action refused by policy"}
	}
}
