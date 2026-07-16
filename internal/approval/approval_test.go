package approval

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
)

func req() Request {
	return Request{ActionID: "a1", Tool: "write_file", Risk: policy.R3, Summary: "write x (10 bytes)"}
}

func TestAutoApproveApproves(t *testing.T) {
	d := AutoApprove{}.Approve(context.Background(), req())
	if !d.Approved || d.ApprovalID == "" {
		t.Fatalf("AutoApprove = %+v, want approved with id", d)
	}
}

func TestDenyAllDenies(t *testing.T) {
	if (DenyAll{}).Approve(context.Background(), req()).Approved {
		t.Fatal("DenyAll approved an action")
	}
}

func TestPromptApprovesOnYes(t *testing.T) {
	var out bytes.Buffer
	p := Prompt{In: strings.NewReader("y\n"), Out: &out}
	d := p.Approve(context.Background(), req())
	if !d.Approved {
		t.Fatalf("Prompt('y') = %+v, want approved", d)
	}
	// The prompt must show the faithful summary, not just the tool name.
	if !strings.Contains(out.String(), "write x (10 bytes)") {
		t.Fatalf("prompt output missing the action summary: %q", out.String())
	}
}

func TestPromptDeniesOnNo(t *testing.T) {
	for _, ans := range []string{"n\n", "\n", "nope\n"} {
		p := Prompt{In: strings.NewReader(ans), Out: &bytes.Buffer{}}
		if p.Approve(context.Background(), req()).Approved {
			t.Fatalf("Prompt(%q) approved, want denied", ans)
		}
	}
}
