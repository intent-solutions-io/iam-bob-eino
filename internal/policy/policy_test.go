package policy

import "testing"

func TestEvaluateReadOnlyAllowed(t *testing.T) {
	p := Default()
	for _, r := range []RiskClass{R0, R1} {
		d := p.Evaluate(r)
		if !d.Allowed || d.RequiresApproval {
			t.Errorf("risk %s: got %+v, want allowed without approval", r, d)
		}
	}
}

func TestEvaluateExecGatedByAllowExec(t *testing.T) {
	// R2 (command execution) is denied by default — it is a separate capability
	// from writes and from approval. This is the capability split that makes
	// --yes alone incapable of executing.
	denied := Default().Evaluate(R2)
	if denied.Allowed {
		t.Errorf("R2 with AllowExec=false: got %+v, want denied", denied)
	}
	p := Default()
	p.AllowExec = true
	allowed := p.Evaluate(R2)
	if !allowed.Allowed || !allowed.RequiresApproval {
		t.Errorf("R2 with AllowExec=true: got %+v, want allowed with approval", allowed)
	}
}

// TestCapabilityIndependence proves writes and exec are independent: enabling
// one never enables the other.
func TestCapabilityIndependence(t *testing.T) {
	writesOnly := Default()
	writesOnly.AllowWrites = true
	if writesOnly.Evaluate(R2).Allowed {
		t.Error("AllowWrites must not enable R2 execution")
	}
	execOnly := Default()
	execOnly.AllowExec = true
	if execOnly.Evaluate(R3).Allowed {
		t.Error("AllowExec must not enable R3 writes")
	}
}

// TestPolicyHashDistinguishesCapabilityCombos proves evidence can tell the four
// capability combinations apart via the policy hash.
func TestPolicyHashDistinguishesCapabilityCombos(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range []bool{false, true} {
		for _, e := range []bool{false, true} {
			p := Default()
			p.AllowWrites, p.AllowExec = w, e
			h := p.Hash()
			if seen[h] {
				t.Errorf("policy hash collision for writes=%v exec=%v", w, e)
			}
			seen[h] = true
		}
	}
	if len(seen) != 4 {
		t.Errorf("got %d distinct policy hashes, want 4", len(seen))
	}
}

func TestEvaluateWriteGatedByPolicy(t *testing.T) {
	denied := Default().Evaluate(R3)
	if denied.Allowed {
		t.Error("R3 with AllowWrites=false: got allowed, want denied")
	}
	p := Default()
	p.AllowWrites = true
	allowed := p.Evaluate(R3)
	if !allowed.Allowed || !allowed.RequiresApproval {
		t.Errorf("R3 with AllowWrites=true: got %+v, want allowed with approval", allowed)
	}
}

func TestEvaluateDestructiveRefused(t *testing.T) {
	if Default().Evaluate(R4).Allowed {
		t.Error("R4: got allowed, want refused")
	}
}

func TestCommandAllowed(t *testing.T) {
	p := Default()
	if !p.CommandAllowed("go test ./...") {
		t.Error("go test should be allowlisted")
	}
	if p.CommandAllowed("rm -rf /") {
		t.Error("rm must not be allowlisted")
	}
	if p.CommandAllowed("") {
		t.Error("empty command must not be allowed")
	}
}

func TestHashStableAndOrderInsensitive(t *testing.T) {
	a := Policy{Version: "1", AllowedCommands: []string{"go", "git"}}
	b := Policy{Version: "1", AllowedCommands: []string{"git", "go"}}
	if a.Hash() != b.Hash() {
		t.Error("Hash must be independent of allowlist order")
	}
	c := Policy{Version: "2", AllowedCommands: []string{"go", "git"}}
	if a.Hash() == c.Hash() {
		t.Error("Hash must change when the policy changes")
	}
}
