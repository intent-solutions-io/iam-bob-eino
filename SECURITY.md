# Security model — iam-bob-eino

Bob is a coding agent that reads, searches, runs commands, and writes files on
behalf of an LLM. Its safety rests on four boundaries, all in `internal/`:

1. **Workspace confinement** (`workspace`) — every file operation goes through a
   Go `os.Root`, which refuses any path that resolves outside the workspace root,
   **including through a symlink inside the workspace**. Absolute paths and `../`
   traversal are rejected lexically before I/O as well.
2. **Policy boundary** (`policy`) — each tool declares an R0–R4 risk class. Reads
   and search (R0/R1) are always allowed; command execution (R2) and writes (R3)
   require approval; writes are additionally gated by `--allow-writes`; R4 is
   refused.
3. **Approval boundary** (`approval`) — R2/R3 actions require an approver. The
   default is deny-by-default (non-interactive) or an interactive prompt that
   shows the **full command** or the **file path + content hash** being
   authorized. `--yes` pre-authorizes a session.
4. **Evidence boundary** (`evidence`) — every action emits one content-safe,
   hash-chained record to an append-only log **outside the workspace**, so the
   audited agent cannot read, rewrite, or delete its own audit trail.

## Hardening implemented

- **No shell.** `run_command` never uses `sh -c`. Commands are tokenized,
  shell metacharacters (`;`, `|`, `&`, `$`, backtick, `()`, redirects, …) are
  rejected, and the program is exec'd directly. First-token allowlisting is a
  real control because a second command cannot be smuggled in.
- **Config-exec vectors neutralized.** Arguments like `git -c` are refused, and
  the command environment sets `GIT_PAGER=cat`, `PAGER=cat`,
  `GIT_CONFIG_NOSYSTEM=1`, so an allowlisted program cannot be turned into a
  shell via its own configuration.
- **Process-group reaping.** Commands run in their own process group and the
  whole group is killed on the 60s timeout, so backgrounded children are not
  orphaned.
- **Secret files refused.** `read_file`/`write_file` refuse `.env`, `secrets.*`,
  `id_rsa`, `*.pem`, `*.key`, `.npmrc`, `age.key`, etc., and `search_code` skips
  them.
- **Redaction everywhere output leaves the process.** Tool results, the terminal
  trace, and evidence records are passed through a credential scrubber
  (OpenAI/GitHub/AWS/Slack/Google keys, JWTs, PEM blocks, bearer/`key=value`).
- **Bounded I/O.** Reads, command output, and writes are size-capped and read
  incrementally, so a huge file or chatty command cannot exhaust memory.
- **Tamper-evident evidence.** Records are sha256 hash-chained; editing or
  deleting any record breaks the chain for all later records (`VerifyChain`).

## Residual risks (accepted, documented)

- **`run_command` runs project-defined code by design.** `go test`, `make`,
  `npm`, etc. execute code from the workspace — that is the tool's purpose. The
  control is *approval with the full command shown*, not sandboxing. For
  untrusted workspaces, run Bob inside a container/VM. A future hardening is to
  route execution through a real sandbox (bubblewrap/nsjail) via the AGP seam.
- **Read content egresses to the model provider.** Any file Bob reads is sent to
  the configured LLM provider (BYOK). Scope keys and workspaces accordingly; the
  secret-file denylist reduces but does not eliminate this.
- **The environment is not fully scrubbed** for run_command (HOME/PATH/GOCACHE
  are needed by go/git). Secrets present in the environment are visible to an
  approved command. Prefer a scrubbed shell/session when running Bob.
- **The AGP execution seam is a local no-op in this slice.** Until a real AGP
  adapter lands, the gating boundaries that actually enforce are policy +
  approval; the execution seam does not add sandboxing yet.

## Reporting a vulnerability

Do not open a public issue for a security problem. Email
`jeremy@intentsolutions.io` with details and a reproduction. This is
pre-release software; expect rapid iteration.
