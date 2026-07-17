package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// patchGov builds a governor over a fixture workspace with writes enabled and
// auto-approval, returning the apply_patch tool and the sink.
func patchGov(t *testing.T, files map[string]string, allowWrites bool) (tool.InvokableTool, *evidence.MemorySink, string) {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.Default()
	pol.AllowWrites = allowWrites
	sink := &evidence.MemorySink{}
	g := governor.New(ws, pol, approval.AutoApprove{}, sink)
	tl, err := newApplyPatch(g)
	if err != nil {
		t.Fatal(err)
	}
	return tl, sink, root
}

func patchDoc(path, content, find, replace string) string {
	doc := map[string]any{
		"schema_version": "intent-bob-eino-patch/v1",
		"files": []map[string]any{{
			"path": path, "pre_sha256": sha(content),
			"hunks": []map[string]any{{"find": find, "replace": replace, "expect_count": strings.Count(content, find), "occurrence": 0}},
		}},
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func callPatch(t *testing.T, tl tool.InvokableTool, patchJSON string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"patch_json": patchJSON})
	out, err := tl.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	return out
}

func TestApplyPatchToolAppliesAndEmitsEvidence(t *testing.T) {
	const content = "hello old world\n"
	tl, sink, root := patchGov(t, map[string]string{"a.txt": content}, true)
	out := callPatch(t, tl, patchDoc("a.txt", content, "old", "new"))
	if strings.HasPrefix(out, "DENIED") || strings.HasPrefix(out, "ERROR") {
		t.Fatalf("tool refused: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "hello new world\n" {
		t.Errorf("content = %q", got)
	}
	if len(sink.Records) != 1 {
		t.Fatalf("evidence records = %d, want 1", len(sink.Records))
	}
	rec := sink.Records[0]
	if rec.Tool.Name != "apply_patch" || rec.RiskClass != "R3" || rec.Execution != "ok" || rec.Verified != "verified" {
		t.Errorf("record = %+v", rec)
	}
	// Evidence-safe: the result carries hashes/counts, never content.
	if strings.Contains(out, "hello new world") {
		t.Error("tool result leaked file content")
	}
	var res struct {
		Files []struct {
			Path       string `json:"path"`
			PostSHA256 string `json:"post_sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Files) != 1 {
		t.Fatalf("result not parseable: %v / %s", err, out)
	}
	if res.Files[0].PostSHA256 != sha("hello new world\n") {
		t.Errorf("post hash = %s", res.Files[0].PostSHA256)
	}
}

func TestApplyPatchDeniedWithoutWrites(t *testing.T) {
	const content = "x\n"
	tl, sink, root := patchGov(t, map[string]string{"a.txt": content}, false)
	out := callPatch(t, tl, patchDoc("a.txt", content, "x", "y"))
	if !strings.HasPrefix(out, "DENIED") {
		t.Fatalf("want policy denial, got: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != content {
		t.Error("file mutated despite policy denial")
	}
	if sink.Records[0].Authorization != "denied" {
		t.Errorf("evidence authorization = %q", sink.Records[0].Authorization)
	}
}

func TestApplyPatchGuardLadder(t *testing.T) {
	cases := []struct{ name, path string }{
		{"traversal", "../escape.txt"},
		{"dotgit", ".git/config"},
		{"secret", ".env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tl, sink, _ := patchGov(t, map[string]string{"a.txt": "x"}, true)
			doc := fmt.Sprintf(`{"schema_version":"intent-bob-eino-patch/v1","files":[{"path":%q,"pre_sha256":%q,"hunks":[{"find":"x","replace":"y","expect_count":1,"occurrence":0}]}]}`, c.path, sha("x"))
			out := callPatch(t, tl, doc)
			// Either the tool guard (DENIED) or patch.Validate (ERROR:
			// forbidden path) may fire first; both must refuse with evidence.
			if !strings.HasPrefix(out, "DENIED") && !strings.Contains(out, "forbidden path") {
				t.Fatalf("path %q not refused: %s", c.path, out)
			}
			if len(sink.Records) != 1 {
				t.Errorf("evidence records = %d, want 1", len(sink.Records))
			}
		})
	}
}

func TestApplyPatchMalformedDocumentIsTypedError(t *testing.T) {
	tl, sink, _ := patchGov(t, map[string]string{"a.txt": "x"}, true)
	out := callPatch(t, tl, `{"nope": true}`)
	if !strings.HasPrefix(out, "ERROR") {
		t.Fatalf("malformed doc: %s", out)
	}
	if sink.Records[0].Execution != "error" {
		t.Errorf("execution = %q, want error", sink.Records[0].Execution)
	}
}

func TestApplyPatchStaleHashRefused(t *testing.T) {
	tl, _, root := patchGov(t, map[string]string{"a.txt": "current\n"}, true)
	out := callPatch(t, tl, patchDoc("a.txt", "stale\n", "stale", "x"))
	if !strings.Contains(out, "pre-hash") {
		t.Fatalf("want pre-hash refusal, got: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "current\n" {
		t.Error("file mutated despite stale pre-hash")
	}
}
