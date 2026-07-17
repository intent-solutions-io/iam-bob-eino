package tools

import (
	"context"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// TestReadOnlyToolsetContainsNoMutationTools is the structural read-only
// proof for planning mode: the set is exactly the three read tools; the
// write/exec/patch builders were never even constructed.
func TestReadOnlyToolsetContainsNoMutationTools(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gov := governor.New(ws, policy.Default(), approval.DenyAll{}, &evidence.MemorySink{})
	ts, err := ReadOnly(gov)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"read_file": true, "list_dir": true, "search_code": true}
	if len(ts) != len(want) {
		t.Fatalf("ReadOnly returned %d tools, want %d", len(ts), len(want))
	}
	for _, tl := range ts {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !want[info.Name] {
			t.Errorf("unexpected tool %q in the read-only set", info.Name)
		}
		delete(want, info.Name)
	}
	for missing := range want {
		t.Errorf("read-only set missing %q", missing)
	}
}

// TestAllSupersetOfReadOnly keeps the two sets coherent: everything read-only
// is also in the full set.
func TestAllSupersetOfReadOnly(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gov := governor.New(ws, policy.Default(), approval.DenyAll{}, &evidence.MemorySink{})
	all, err := All(gov)
	if err != nil {
		t.Fatal(err)
	}
	allNames := map[string]bool{}
	for _, tl := range all {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		allNames[info.Name] = true
	}
	ro, err := ReadOnly(gov)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range ro {
		info, _ := tl.Info(context.Background())
		if !allNames[info.Name] {
			t.Errorf("read-only tool %q missing from All", info.Name)
		}
	}
}
