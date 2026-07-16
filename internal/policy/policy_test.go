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

func TestEvaluateExecRequiresApproval(t *testing.T) {
	d := Default().Evaluate(R2)
	if !d.Allowed || !d.RequiresApproval {
		t.Errorf("R2: got %+v, want allowed with approval", d)
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
