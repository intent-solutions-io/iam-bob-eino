package runverify

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// chainedEvidence builds a valid hash-chained evidence log of n records using
// the real evidence sink, so the chain algorithm is the production one.
func chainedEvidence(t *testing.T, n int) []evidence.Record {
	t.Helper()
	sink := &evidence.MemorySink{}
	for i := 0; i < n; i++ {
		rec := evidence.Record{
			ActionID:  fmt.Sprintf("act-%d", i+1),
			Timestamp: "2026-07-16T00:00:00Z",
			Tool:      evidence.ToolRef{Name: "write_file", Version: "1"},
			Asset:     fmt.Sprintf("file-%d.go", i+1),
			Execution: "ok",
		}
		if err := sink.Write(rec); err != nil {
			t.Fatalf("MemorySink.Write: %v", err)
		}
	}
	return sink.Records
}

// baseInput is a fully healthy run: workspace matches, git end SHA matches,
// the only changed file is the planned one, acceptance passed, no secrets.
func baseInput(t *testing.T) Input {
	t.Helper()
	return Input{
		WorkspaceRoot: "/work/repo",
		Plan: Plan{
			WorkspaceRoot: "/work/repo",
			ProposedFiles: []string{"pkg/a.go"},
			StartSHA:      "aaa111",
			EndSHA:        "bbb222",
		},
		Evidence:          chainedEvidence(t, 3),
		ExpectedActionIDs: []string{"act-1", "act-2", "act-3"},
		ChangedFiles:      []string{"pkg/a.go"},
		GitState:          func() (string, error) { return "bbb222", nil },
		Acceptance:        map[string]int{"go test ./...": 0},
		RequiredChecks:    []string{"go test ./..."},
		Diffs:             map[string]string{"pkg/a.go": "+func A() int { return 1 }"},
		AgentClaim:        "success",
	}
}

func TestVerifyHappyPath(t *testing.T) {
	v := Verify(baseInput(t))
	if v.Result != ResultVerified {
		t.Fatalf("Result = %q, want %q; failures: %v", v.Result, ResultVerified, v.Failures)
	}
	if len(v.Failures) != 0 || len(v.Warnings) != 0 {
		t.Fatalf("expected clean verdict, got warnings=%v failures=%v", v.Warnings, v.Failures)
	}
	for _, check := range []string{
		CheckEvidenceChain, CheckEvidenceCompleteness, CheckWorkspaceIdentity,
		CheckGitEndSHA, CheckChangedVsPlan, CheckForbiddenPaths, CheckAcceptance, CheckSecretScan,
	} {
		if got := v.Checks[check]; got != "pass" {
			t.Errorf("Checks[%q] = %q, want pass", check, got)
		}
	}
}

// Item 95: an agent claim of success carries zero weight — a recorded
// acceptance exit code of 1 fails the run.
func TestClaimedSuccessButAcceptanceFailsIsFailed(t *testing.T) {
	in := baseInput(t)
	in.AgentClaim = "all tests pass"
	in.Acceptance["go test ./..."] = 1
	v := Verify(in)
	if v.Result != ResultFailed {
		t.Fatalf("Result = %q, want %q", v.Result, ResultFailed)
	}
	if v.Checks[CheckAcceptance] != "fail" {
		t.Fatalf("Checks[acceptance] = %q, want fail", v.Checks[CheckAcceptance])
	}
	found := false
	for _, f := range v.Failures {
		if strings.Contains(f, "exited 1") && strings.Contains(f, "ignored") {
			found = true
		}
	}
	if !found {
		t.Fatalf("failures should record the exit code and note the claim is ignored: %v", v.Failures)
	}
}

// Item 96 + load-bearing receipt property: the plan says file A changed but the
// observed change set says file B → the run does not verify.
func TestUnplannedChangedFileIsFailed(t *testing.T) {
	tests := []struct {
		name    string
		planned []string
		changed []string
	}{
		{"receipt claims A, observed B", []string{"pkg/a.go"}, []string{"pkg/b.go"}},
		{"extra file beyond the plan", []string{"pkg/a.go"}, []string{"pkg/a.go", "internal/sneaky.go"}},
		{"empty plan but changes observed", nil, []string{"pkg/a.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput(t)
			in.Plan.ProposedFiles = tt.planned
			in.ChangedFiles = tt.changed
			v := Verify(in)
			if v.Result != ResultFailed {
				t.Fatalf("Result = %q, want %q; failures: %v", v.Result, ResultFailed, v.Failures)
			}
			if v.Checks[CheckChangedVsPlan] != "fail" {
				t.Fatalf("Checks[changed_vs_plan] = %q, want fail", v.Checks[CheckChangedVsPlan])
			}
		})
	}
}

// Item 97: forbidden paths — git internals and secret files — fail the run.
func TestForbiddenPathTouchedIsFailed(t *testing.T) {
	tests := []string{
		".git/config",
		".git/hooks/pre-commit",
		"sub/.git/HEAD",
		".env",
		"config/.env.production",
		"deploy/server.pem",
		".ssh/id_rsa",
		"creds/credentials.json",
	}
	for _, forbidden := range tests {
		t.Run(forbidden, func(t *testing.T) {
			in := baseInput(t)
			in.Plan.ProposedFiles = append(in.Plan.ProposedFiles, forbidden) // even a PLANNED forbidden path fails
			in.ChangedFiles = append(in.ChangedFiles, forbidden)
			v := Verify(in)
			if v.Result != ResultFailed {
				t.Fatalf("Result = %q, want %q for %q; failures: %v", v.Result, ResultFailed, forbidden, v.Failures)
			}
			if v.Checks[CheckForbiddenPaths] != "fail" {
				t.Fatalf("Checks[forbidden_paths] = %q, want fail", v.Checks[CheckForbiddenPaths])
			}
		})
	}
}

// Item 98: secret material in a diff fails the run, and the verdict never
// echoes the secret itself.
func TestSecretInDiffIsFailedAndRedacted(t *testing.T) {
	secrets := map[string]string{
		"aws key":       "+aws_key = AKIAIOSFODNN7EXAMPLE",
		"private key":   "+-----BEGIN RSA PRIVATE KEY-----",
		"openai style":  `+key := "sk-abcdefghijklmnopqrstuvwxyz123456"`,
		"github token":  "+token = ghp_" + strings.Repeat("a", 36),
		"assignment":    `+api_key = "supersecretvalue123"`,
		"canary marker": "+// SECRET_CANARY planted by the test harness",
	}
	for name, diff := range secrets {
		t.Run(name, func(t *testing.T) {
			in := baseInput(t)
			in.Diffs["pkg/a.go"] = diff
			v := Verify(in)
			if v.Result != ResultFailed {
				t.Fatalf("Result = %q, want %q; failures: %v", v.Result, ResultFailed, v.Failures)
			}
			if v.Checks[CheckSecretScan] != "fail" {
				t.Fatalf("Checks[secret_scan] = %q, want fail", v.Checks[CheckSecretScan])
			}
			// The secret text must never appear in the verdict.
			payload := strings.TrimLeft(diff, "+/ ")
			for _, f := range v.Failures {
				if strings.Contains(f, payload) {
					t.Fatalf("verdict leaked secret text: %q", f)
				}
			}
		})
	}
}

// Item 99: a tampered evidence chain yields the tampered verdict, and it takes
// precedence over every other finding.
func TestTamperedEvidenceChain(t *testing.T) {
	t.Run("edited record breaks the chain", func(t *testing.T) {
		in := baseInput(t)
		in.Evidence[1].Execution = "error" // rewrite history
		v := Verify(in)
		if v.Result != ResultTampered {
			t.Fatalf("Result = %q, want %q", v.Result, ResultTampered)
		}
		if v.Checks[CheckEvidenceChain] != "fail" {
			t.Fatalf("Checks[evidence_chain] = %q, want fail", v.Checks[CheckEvidenceChain])
		}
	})
	t.Run("deleted record breaks the chain", func(t *testing.T) {
		in := baseInput(t)
		in.Evidence = append(in.Evidence[:1], in.Evidence[2:]...) // drop record 2
		in.ExpectedActionIDs = nil
		v := Verify(in)
		if v.Result != ResultTampered {
			t.Fatalf("Result = %q, want %q", v.Result, ResultTampered)
		}
	})
	t.Run("tampered wins over workspace mismatch and failures", func(t *testing.T) {
		in := baseInput(t)
		in.Evidence[0].Asset = "rewritten.go"
		in.Plan.WorkspaceRoot = "/somewhere/else"
		in.Acceptance["go test ./..."] = 2
		v := Verify(in)
		if v.Result != ResultTampered {
			t.Fatalf("Result = %q, want %q (precedence)", v.Result, ResultTampered)
		}
	})
}

// Item 100: model-free by construction. The Input type only carries
// fs/git/evidence data — this test constructs one from nothing but those and
// runs Verify — and the package's imports contain no model/provider/agent
// package, asserted structurally against the source.
func TestModelFreeByConstruction(t *testing.T) {
	// Behavioral half: Verify runs to a verdict with zero model anywhere.
	in := Input{
		WorkspaceRoot: "/work/repo",
		Plan:          Plan{WorkspaceRoot: "/work/repo"},
		GitState:      func() (string, error) { return "", errors.New("not a git repo") },
	}
	v := Verify(in)
	if v.Result == "" {
		t.Fatal("Verify returned an empty result")
	}

	// Structural half: parse this package's non-test source and assert no
	// model/provider/agent import can ever be reached from here.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "runverify.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse runverify.go: %v", err)
	}
	banned := []string{"/internal/provider", "/internal/agent", "/model", "cloudwego/eino"}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		for _, b := range banned {
			if strings.Contains(p, b) {
				t.Errorf("runverify imports %q — the verifier must stay model-free", p)
			}
		}
	}
}

// Item 101: rerunnable — the same input yields an identical verdict.
func TestVerifyIsRerunnable(t *testing.T) {
	inputs := []func(t *testing.T) Input{
		baseInput,
		func(t *testing.T) Input { // a messy failing input
			in := baseInput(t)
			in.Acceptance = map[string]int{"lint": 1, "go test ./...": 1, "build": 0}
			in.ChangedFiles = []string{"z.go", "a.go", ".env"}
			in.Diffs = map[string]string{"z.go": "+AKIAIOSFODNN7EXAMPLE", "a.go": "+ok"}
			return in
		},
	}
	for i, mk := range inputs {
		in := mk(t)
		first := Verify(in)
		second := Verify(in)
		if !reflect.DeepEqual(first, second) {
			t.Errorf("input %d: Verify not deterministic:\nfirst:  %+v\nsecond: %+v", i, first, second)
		}
	}
}

func TestWorkspaceMismatch(t *testing.T) {
	in := baseInput(t)
	in.Plan.WorkspaceRoot = "/work/other-checkout"
	v := Verify(in)
	if v.Result != ResultWorkspaceMismatch {
		t.Fatalf("Result = %q, want %q", v.Result, ResultWorkspaceMismatch)
	}
	if v.Checks[CheckWorkspaceIdentity] != "fail" {
		t.Fatalf("Checks[workspace_identity] = %q, want fail", v.Checks[CheckWorkspaceIdentity])
	}
}

func TestGitEndSHAMismatchIsFailed(t *testing.T) {
	in := baseInput(t)
	in.GitState = func() (string, error) { return "ccc333-not-the-receipt-sha", nil }
	v := Verify(in)
	if v.Result != ResultFailed {
		t.Fatalf("Result = %q, want %q", v.Result, ResultFailed)
	}
	if v.Checks[CheckGitEndSHA] != "fail" {
		t.Fatalf("Checks[git_end_sha] = %q, want fail", v.Checks[CheckGitEndSHA])
	}
}

func TestInconclusiveOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(in *Input)
		check  string
	}{
		{
			"missing required acceptance result",
			func(in *Input) { in.Acceptance = map[string]int{} },
			CheckAcceptance,
		},
		{
			"missing expected evidence record",
			func(in *Input) { in.ExpectedActionIDs = append(in.ExpectedActionIDs, "act-99") },
			CheckEvidenceCompleteness,
		},
		{
			"git state unavailable",
			func(in *Input) { in.GitState = func() (string, error) { return "", errors.New("git: exec failed") } },
			CheckGitEndSHA,
		},
		{
			"changed-file state unavailable",
			func(in *Input) {
				in.ChangedFilesFn = func() ([]string, error) { return nil, errors.New("diff failed") }
			},
			CheckChangedVsPlan,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput(t)
			tt.mutate(&in)
			v := Verify(in)
			if v.Result != ResultInconclusive {
				t.Fatalf("Result = %q, want %q; failures: %v", v.Result, ResultInconclusive, v.Failures)
			}
			if v.Checks[tt.check] != "error" {
				t.Fatalf("Checks[%q] = %q, want error", tt.check, v.Checks[tt.check])
			}
		})
	}
}

func TestVerifiedWithWarnings(t *testing.T) {
	in := baseInput(t)
	// A planned file that shows no observed change is a warning, not a failure.
	in.Plan.ProposedFiles = []string{"pkg/a.go", "pkg/never-touched.go"}
	v := Verify(in)
	if v.Result != ResultVerifiedWarnings {
		t.Fatalf("Result = %q, want %q; failures: %v", v.Result, ResultVerifiedWarnings, v.Failures)
	}
	if len(v.Warnings) == 0 {
		t.Fatal("expected a warning about the unchanged planned file")
	}
	if len(v.Failures) != 0 {
		t.Fatalf("unexpected failures: %v", v.Failures)
	}
}

// ChangedFilesFn overrides the static ChangedFiles slice when provided.
func TestChangedFilesFnOverridesStaticList(t *testing.T) {
	in := baseInput(t)
	in.ChangedFiles = []string{"pkg/a.go"} // static list looks clean
	in.ChangedFilesFn = func() ([]string, error) { return []string{"pkg/a.go", "unexpected.go"}, nil }
	v := Verify(in)
	if v.Result != ResultFailed {
		t.Fatalf("Result = %q, want %q (fn-observed unplanned file)", v.Result, ResultFailed)
	}
}

// Precedence below tampered: workspace mismatch outranks plain failures.
func TestWorkspaceMismatchOutranksFailed(t *testing.T) {
	in := baseInput(t)
	in.Plan.WorkspaceRoot = "/work/other"
	in.Acceptance["go test ./..."] = 1
	v := Verify(in)
	if v.Result != ResultWorkspaceMismatch {
		t.Fatalf("Result = %q, want %q", v.Result, ResultWorkspaceMismatch)
	}
}

// Nil GitState skips the git check rather than failing the run.
func TestNilGitStateSkipsCheck(t *testing.T) {
	in := baseInput(t)
	in.GitState = nil
	v := Verify(in)
	if v.Result != ResultVerified {
		t.Fatalf("Result = %q, want %q; failures: %v", v.Result, ResultVerified, v.Failures)
	}
	if v.Checks[CheckGitEndSHA] != "skipped" {
		t.Fatalf("Checks[git_end_sha] = %q, want skipped", v.Checks[CheckGitEndSHA])
	}
}
