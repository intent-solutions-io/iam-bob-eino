// Package tools implements Bob's governed, typed coding tools. Each tool is a
// thin side-effect wrapped in the governor pipeline: every call is policy-
// checked, approval-gated when required, independently verified, and recorded as
// evidence. Eino supplies the tool machinery (utils.InferTool derives the JSON
// schema from the input struct tags); Bob supplies the specialization and
// governance. All file I/O goes through the workspace's symlink-safe root, tool
// results are redacted before returning, and known-secret files are refused.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
)

// Output and input caps keep tool results bounded so a large file, noisy
// command, or oversized write cannot exhaust memory or the model context.
const (
	maxReadBytes     = 64 * 1024
	maxCmdOutput     = 16 * 1024
	maxWriteBytes    = 1 * 1024 * 1024
	maxSearchResults = 100
	commandTimeout   = 60 * time.Second
)

// All builds the full set of governed tools bound to a governor. apply_patch
// lives ONLY here — planning's ReadOnly set never constructs it.
func All(g *governor.Governor) ([]tool.BaseTool, error) {
	return build(g, newReadFile, newListDir, newSearchCode, newRunCommand, newWriteFile, newApplyPatch)
}

// ReadOnly builds only the read-only tool set (read_file, list_dir,
// search_code) for planning mode. The write/exec/patch builders are never
// constructed here, so a planning agent is read-only by construction — there
// is no disabled-but-present mutation tool for a model to talk its way into.
func ReadOnly(g *governor.Governor) ([]tool.BaseTool, error) {
	return build(g, newReadFile, newListDir, newSearchCode)
}

func build(g *governor.Governor, builders ...func(*governor.Governor) (tool.InvokableTool, error)) ([]tool.BaseTool, error) {
	out := make([]tool.BaseTool, 0, len(builders))
	for _, b := range builders {
		t, err := b(g)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// secretName matches base filenames whose contents are almost always secrets.
// Reading them would ship the secret to the model provider, so they are refused.
var secretName = regexp.MustCompile(`(?i)(^\.env$|^\.env\..*|^secrets?\..*|^id_(rsa|ed25519|ecdsa|dsa)$|.*\.pem$|.*\.key$|^\.npmrc$|^\.netrc$|^credentials$|^keys?\.txt$|^age\.key$)`)

func isSecretPath(rel string) bool {
	return secretName.MatchString(filepath.Base(rel))
}

// --- read_file (R0) --------------------------------------------------------

type readFileInput struct {
	Path string `json:"path" jsonschema_description:"Workspace-relative path of the file to read."`
}

func newReadFile(g *governor.Governor) (tool.InvokableTool, error) {
	return utils.InferTool("read_file",
		"Read a UTF-8 text file inside the workspace and return its contents. Read-only. Known secret files are refused.",
		func(ctx context.Context, in *readFileInput) (string, error) {
			spec := governor.ActionSpec{Tool: "read_file", Risk: policy.R0, Asset: in.Path, Summary: "read " + in.Path, RawArgs: jsonOf(in)}
			t := g.Begin(spec)
			defer t.EnsureEmitted(ctx)
			if err := g.WS.Check(in.Path); err != nil {
				t.FinishDenied(ctx, err.Error())
				return "DENIED: " + err.Error(), nil
			}
			if isSecretPath(in.Path) {
				t.FinishDenied(ctx, "refused: looks like a secret file")
				return "DENIED: refusing to read a likely secret file", nil
			}
			if gate := t.Authorize(ctx, spec); !gate.Allowed {
				t.FinishDenied(ctx, gate.Reason)
				return "DENIED: " + gate.Reason, nil
			}
			data, truncated, rerr := g.WS.ReadFileLimited(in.Path, maxReadBytes)
			if rerr != nil {
				t.FinishError(ctx, rerr)
				return "ERROR: " + rerr.Error(), nil
			}
			info := fmt.Sprintf("read %d bytes", len(data))
			if truncated {
				info += " (truncated at cap)"
			}
			t.Finish(ctx, "ok", info, verify.NA("read observed directly"))
			out := evidence.Redact(string(data))
			if truncated {
				out += "\n...[truncated]"
			}
			return out, nil
		})
}

// --- list_dir (R0) ---------------------------------------------------------

type listDirInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Workspace-relative directory to list. Empty means the workspace root."`
}

func newListDir(g *governor.Governor) (tool.InvokableTool, error) {
	return utils.InferTool("list_dir",
		"List the entries of a directory inside the workspace. Read-only.",
		func(ctx context.Context, in *listDirInput) (string, error) {
			asset := in.Path
			if asset == "" {
				asset = "."
			}
			spec := governor.ActionSpec{Tool: "list_dir", Risk: policy.R0, Asset: asset, Summary: "list " + asset, RawArgs: jsonOf(in)}
			t := g.Begin(spec)
			defer t.EnsureEmitted(ctx)
			if err := g.WS.Check(in.Path); err != nil {
				t.FinishDenied(ctx, err.Error())
				return "DENIED: " + err.Error(), nil
			}
			if gate := t.Authorize(ctx, spec); !gate.Allowed {
				t.FinishDenied(ctx, gate.Reason)
				return "DENIED: " + gate.Reason, nil
			}
			entries, rerr := g.WS.ReadDir(in.Path)
			if rerr != nil {
				t.FinishError(ctx, rerr)
				return "ERROR: " + rerr.Error(), nil
			}
			var b strings.Builder
			for _, e := range entries {
				kind := "file"
				if e.IsDir() {
					kind = "dir"
				}
				fmt.Fprintf(&b, "%s\t%s\n", kind, e.Name())
			}
			t.Finish(ctx, "ok", fmt.Sprintf("%d entries", len(entries)), verify.NA("listing observed directly"))
			return b.String(), nil
		})
}

// --- search_code (R1) ------------------------------------------------------

type searchCodeInput struct {
	Pattern string `json:"pattern" jsonschema_description:"Regular expression to search for across files."`
	Path    string `json:"path,omitempty" jsonschema_description:"Workspace-relative subdirectory to scope the search. Empty means the whole workspace."`
}

func newSearchCode(g *governor.Governor) (tool.InvokableTool, error) {
	return utils.InferTool("search_code",
		"Search workspace files for a regular expression and return matching lines with file:line. Read-only.",
		func(ctx context.Context, in *searchCodeInput) (string, error) {
			asset := in.Path
			if asset == "" {
				asset = "."
			}
			spec := governor.ActionSpec{Tool: "search_code", Risk: policy.R1, Asset: asset, Summary: "search " + asset, RawArgs: jsonOf(in)}
			t := g.Begin(spec)
			defer t.EnsureEmitted(ctx)
			re, cerr := regexp.Compile(in.Pattern)
			if cerr != nil {
				// A bad pattern is a tool-argument error, not a governance
				// denial — classify it as an error so the audit trail is honest.
				t.FinishError(ctx, fmt.Errorf("invalid pattern"))
				return "ERROR: invalid regular expression: " + cerr.Error(), nil
			}
			if gate := t.Authorize(ctx, spec); !gate.Allowed {
				t.FinishDenied(ctx, gate.Reason)
				return "DENIED: " + gate.Reason, nil
			}
			start := in.Path
			if start == "" {
				start = "."
			}
			matches, serr := searchTree(g, start, re)
			if serr != nil {
				t.FinishError(ctx, serr)
				return "ERROR: " + serr.Error(), nil
			}
			t.Finish(ctx, "ok", fmt.Sprintf("%d matches", len(matches)), verify.NA("search observed directly"))
			if len(matches) == 0 {
				return "(no matches)", nil
			}
			result := evidence.Redact(strings.Join(matches, "\n"))
			if len(matches) >= maxSearchResults {
				result += fmt.Sprintf("\n...[truncated at %d matches — narrow the pattern or scope]", maxSearchResults)
			}
			return result, nil
		})
}

// searchTree walks the workspace FS (symlink-safe) from start and returns up to
// maxSearchResults "path:line: text" matches, skipping noise dirs, secret files,
// and binary/oversized files.
func searchTree(g *governor.Governor, start string, re *regexp.Regexp) ([]string, error) {
	var out []string
	fsys := g.WS.FS()
	// os.Root.FS uses unrooted, slash paths; "." is the root.
	walkStart := filepath.ToSlash(filepath.Clean(start))
	err := fs.WalkDir(fsys, walkStart, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".venv", "dist", "build":
				return fs.SkipDir
			}
			return nil
		}
		if len(out) >= maxSearchResults {
			return fs.SkipAll
		}
		if isSecretPath(path) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > maxReadBytes*8 {
			return nil
		}
		data, rerr := fs.ReadFile(fsys, path)
		if rerr != nil || isBinary(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
				if len(out) >= maxSearchResults {
					break
				}
			}
		}
		return nil
	})
	return out, err
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// --- run_command (R2) ------------------------------------------------------

type runCommandInput struct {
	Command string `json:"command" jsonschema_description:"Command line to run inside the workspace, e.g. 'go test ./...'. Only allowlisted programs are permitted; shell metacharacters are rejected."`
}

func newRunCommand(g *governor.Governor) (tool.InvokableTool, error) {
	return utils.InferTool("run_command",
		"Run an allowlisted command (such as the test suite) inside the workspace and return its output and exit code. Requires approval. Executed shell-free; metacharacters are rejected.",
		func(ctx context.Context, in *runCommandInput) (string, error) {
			program := firstToken(in.Command)
			// Asset/Summary carry the full (redacted) command so the approval
			// prompt and the evidence record are faithful, not just "git".
			redactedCmd := evidence.Redact(in.Command)
			spec := governor.ActionSpec{Tool: "run_command", Risk: policy.R2, Asset: redactedCmd, Summary: "run: " + redactedCmd, RawArgs: jsonOf(in)}
			t := g.Begin(spec)
			defer t.EnsureEmitted(ctx)

			argv, perr := splitCommand(in.Command)
			if perr != nil {
				t.FinishDenied(ctx, perr.Error())
				return "DENIED: " + perr.Error(), nil
			}
			if !g.Policy.CommandAllowed(in.Command) {
				t.FinishDenied(ctx, "command not on allowlist: "+program)
				return "DENIED: command '" + program + "' is not on the allowlist", nil
			}
			if ferr := checkDangerousFlags(argv); ferr != nil {
				t.FinishDenied(ctx, ferr.Error())
				return "DENIED: " + ferr.Error(), nil
			}
			if gate := t.Authorize(ctx, spec); !gate.Allowed {
				t.FinishDenied(ctx, gate.Reason)
				return "DENIED: " + gate.Reason, nil
			}

			cctx, cancel := context.WithTimeout(ctx, commandTimeout)
			defer cancel()
			// #nosec G204 -- program is allowlist-checked, args are shell-free
			// (no metacharacters, no `sh -c`), dangerous flags are rejected, and
			// execution is confined to the workspace directory with a neutralized
			// pager/config env. Running project code (go test/make) is the
			// approved intent of this tool.
			cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
			cmd.Dir = g.WS.Root()
			cmd.Env = neutralizedEnv()
			// Run in its own process group and kill the whole group on timeout so
			// backgrounded children are reaped, not orphaned.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error {
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return nil
			}

			output, exitCode, rerr := runBounded(cmd)
			if rerr != nil {
				t.FinishError(ctx, rerr)
				return "ERROR: " + rerr.Error(), nil
			}
			v := verify.CommandExit(exitCode, 0)
			t.Finish(ctx, "ok", fmt.Sprintf("exit=%d", exitCode), v)
			return fmt.Sprintf("exit_code=%d\n%s", exitCode, evidence.Redact(output)), nil
		})
}

// runBounded runs cmd, capturing at most maxCmdOutput bytes of combined output
// and returning the exit code. A process that never started yields an error
// rather than a nil-ProcessState panic.
func runBounded(cmd *exec.Cmd) (string, int, error) {
	var buf capBuffer
	buf.limit = maxCmdOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	if cmd.ProcessState == nil {
		if runErr != nil {
			return "", -1, runErr
		}
		return "", -1, fmt.Errorf("command did not start")
	}
	out := buf.String()
	if buf.truncated {
		out += "\n...[truncated]"
	}
	return out, cmd.ProcessState.ExitCode(), nil
}

// capBuffer is an io.Writer that stores at most limit bytes and notes truncation.
type capBuffer struct {
	buf       []byte
	limit     int
	truncated bool
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if len(c.buf) < c.limit {
		room := c.limit - len(c.buf)
		if room >= len(p) {
			c.buf = append(c.buf, p...)
		} else {
			c.buf = append(c.buf, p[:room]...)
			c.truncated = true
		}
	} else {
		c.truncated = true
	}
	return len(p), nil // always consume so the child never blocks on a full pipe
}

func (c *capBuffer) String() string { return string(c.buf) }

// neutralizedEnv returns the process environment with the pager and git config
// exec vectors neutralized, so an allowlisted program cannot be turned into a
// shell via its own configuration.
func neutralizedEnv() []string {
	env := append([]string(nil), os.Environ()...)
	env = append(env,
		"PAGER=cat",
		"GIT_PAGER=cat",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	return env
}

// dangerousFlags are argument tokens that let an allowlisted program run
// arbitrary code via configuration or a spawned helper (e.g. git -c
// core.pager=..., git --exec-path=/evil, go test -exec /evil, go build
// -toolexec /evil). The match is on the flag NAME only, so both the separate
// form ("--config" "x") and the equals form ("--config=x") are caught.
var dangerousFlags = map[string]bool{
	// git config / helper-path / receive-pack vectors
	"-c": true, "--config": true, "-C": true, "--exec-path": true,
	"--upload-pack": true, "--receive-pack": true, "-o": true,
	"--config-env": true, "--work-tree": true, "--git-dir": true,
	// go toolchain program-spawning flags
	"-exec": true, "-toolexec": true, "-ldflags": true, "-gcflags": true,
	"-asmflags": true, "-overlay": true,
}

func checkDangerousFlags(argv []string) error {
	for _, a := range argv[1:] {
		// Normalize the equals form ("--flag=value") to the flag name so the
		// value cannot smuggle a dangerous flag past an exact-token check.
		name := a
		if i := strings.IndexByte(a, '='); i >= 0 {
			name = a[:i]
		}
		if dangerousFlags[name] {
			return fmt.Errorf("argument %q is not allowed (arbitrary-exec vector)", a)
		}
	}
	return nil
}

// isDotGitPath reports whether a workspace-relative path targets the .git
// directory. Writing into .git (hooks, config) is an execution/tamper vector,
// so writes and patches refuse it even when writes are enabled.
func isDotGitPath(rel string) bool {
	clean := filepath.ToSlash(filepath.Clean(rel))
	return clean == ".git" || strings.HasPrefix(clean, ".git/")
}

// shellMeta is the set of shell control characters rejected before execution.
const shellMeta = ";|&$`><\n\r\\!(){}"

// splitCommand validates and tokenizes a command line for shell-free execution.
func splitCommand(command string) ([]string, error) {
	if strings.ContainsAny(command, shellMeta) {
		return nil, fmt.Errorf("command contains disallowed shell metacharacters")
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return fields, nil
}

func firstToken(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// --- write_file (R3) -------------------------------------------------------

type writeFileInput struct {
	Path    string `json:"path" jsonschema_description:"Workspace-relative path of the file to write."`
	Content string `json:"content" jsonschema_description:"Full new contents of the file."`
}

func newWriteFile(g *governor.Governor) (tool.InvokableTool, error) {
	return utils.InferTool("write_file",
		"Write (create or overwrite) a file inside the workspace. Disabled unless writes are enabled by policy; requires approval; the write is independently verified.",
		func(ctx context.Context, in *writeFileInput) (string, error) {
			want := verify.HashBytes([]byte(in.Content))
			// Summary shows the operator the path, size, and content hash — the
			// approval is over a specific, identified payload, not a blank write.
			summary := fmt.Sprintf("write %s (%d bytes, sha256:%s)", in.Path, len(in.Content), want[:16])
			spec := governor.ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: in.Path, Summary: summary, RawArgs: jsonOf(in)}
			t := g.Begin(spec)
			defer t.EnsureEmitted(ctx)
			if err := g.WS.Check(in.Path); err != nil {
				t.FinishDenied(ctx, err.Error())
				return "DENIED: " + err.Error(), nil
			}
			if isSecretPath(in.Path) {
				t.FinishDenied(ctx, "refused: looks like a secret file")
				return "DENIED: refusing to write a likely secret file", nil
			}
			if isDotGitPath(in.Path) {
				t.FinishDenied(ctx, "refused: writing into .git is not allowed")
				return "DENIED: refusing to write into the .git directory", nil
			}
			if len(in.Content) > maxWriteBytes {
				t.FinishDenied(ctx, "content exceeds size limit")
				return fmt.Sprintf("DENIED: content exceeds %d-byte limit", maxWriteBytes), nil
			}
			if gate := t.Authorize(ctx, spec); !gate.Allowed {
				t.FinishDenied(ctx, gate.Reason)
				return "DENIED: " + gate.Reason, nil
			}
			if werr := g.WS.WriteFile(in.Path, []byte(in.Content), 0o644); werr != nil {
				t.FinishError(ctx, werr)
				return "ERROR: " + werr.Error(), nil
			}
			// Independent verification: re-read from disk through the workspace
			// and compare hashes.
			got, _, rerr := g.WS.ReadFileLimited(in.Path, maxWriteBytes)
			v := verify.Verdict{Verified: rerr == nil && verify.HashBytes(got) == want}
			if v.Verified {
				v.Info = fmt.Sprintf("on-disk sha256 matches (%d bytes)", len(got))
			} else {
				v.Info = "on-disk content did not match intended write"
			}
			t.Finish(ctx, "ok", fmt.Sprintf("wrote %d bytes", len(in.Content)), v)
			if !v.Verified {
				return "WARNING: wrote file but verification failed: " + v.Info, nil
			}
			return fmt.Sprintf("wrote %s (%d bytes, verified)", in.Path, len(in.Content)), nil
		})
}
