# 002-DR-STND — Operating rules for iam-bob-eino

- **Status:** ACTIVE
- **Type:** Standard (DR-STND)

Lean rules for the Go / Eino runtime of Bob. The discipline lives in the tests
and the governor, not in process ceremony.

## P1 — Provider-neutral, BYOK-only
No provider is hardcoded. The only model path is the OpenAI-compatible gateway
in `internal/provider`, selecting provider + model at runtime from env/config.
**Anti-pattern:** importing a provider SDK directly in agent code.

## P2 — Zero Google by default
Google is not in the provider registry; selecting `google`/`gemini`/`vertex` is
an explicit error. `make run-local` with any non-Google key yields a working Bob,
no GCP. **Anti-pattern:** any code path that requires GOOGLE_*/GCP.

## P3 — Eino owns the loop; Bob owns governance
Use Eino's ADK agent machinery for the ReAct loop and tool dispatch. Bob adds
persona, tools, policy, approval, verification, evidence, and seams.
**Anti-pattern:** reimplementing an agent loop or a framework.

## P4 — One governance boundary
Every side effect routes through `internal/governor` (policy → approval →
execution seam → evidence). Exactly one evidence record per action, on every
path. **Anti-pattern:** a tool performing file I/O or exec without a passing
`Authorize()`, or file I/O outside `workspace`'s symlink-safe methods.

## P5 — Evidence is content-safe and tamper-evident
Evidence records carry hashes, workspace-relative paths, and short summaries —
never secrets or file contents. Records are hash-chained and written outside the
workspace. **Anti-pattern:** logging raw file contents or secrets; writing the
evidence log where a tool can reach it.

## P6 — Least authority by default
Reads/search are allowed; execution and writes require approval; writes need
`--allow-writes`; destructive actions are refused. Commands run shell-free with a
program allowlist. **Anti-pattern:** `sh -c`, a "run anything" tool, or writes on
by default.

## P7 — Don't claim what the tools didn't confirm
Say "typed, not signed" until signing exists. Verification is independent
(re-read + hash writes; inspect command exit), and the persona forbids claiming
attestation that did not happen. **Anti-pattern:** "verified/attested" language
without a real check.

## P8 — Consume the kernel through a seam (when authorized)
Governance kernel / AGP / Mission Control integration is confined to
`internal/seams` and (later) a single kernel-adapter boundary. **Anti-pattern:**
scattering kernel or estate calls across the codebase, or wiring them before the
HQ kernel-consumption decision (see `001-AT-DECR` D7).

These rules are enforced by CI (fmt, vet, test/race) and the test suite —
enforcement travels with the code.
