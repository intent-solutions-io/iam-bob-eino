# iam-bob-eino — build + quality gates
# Enforcement travels with the code: these targets are what CI runs.
#
# Canonical binary: bob-eino (component intent-bob-eino). The bare `bob` name
# collides on PATH with iam-bob-intendant; build it only as the legacy alias.

GO         ?= go
BIN        ?= bob-eino
LEGACY_BIN ?= bob
PKG        := ./...

.PHONY: all build build-legacy test test-race vet fmt fmtcheck tidy lint hooks run-local ci clean

all: build

build: ## Compile the canonical bob-eino binary
	$(GO) build -o $(BIN) ./cmd/bob-eino

build-legacy: ## Compile the legacy `bob` compatibility alias (same internal/cli)
	$(GO) build -o $(LEGACY_BIN) ./cmd/bob

test: ## Run the full test suite
	$(GO) test $(PKG)

test-race: ## Run tests with the race detector
	$(GO) test -race $(PKG)

vet: ## go vet
	$(GO) vet $(PKG)

fmt: ## Format all Go files
	gofmt -w .

fmtcheck: ## Fail if any file is unformatted
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

lint: ## golangci-lint if installed, else vet
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run $(PKG); else echo "golangci-lint not installed; running go vet"; $(GO) vet $(PKG); fi

hooks: ## Install the repo git hooks (L1 enforcement)
	@mkdir -p .git/hooks
	@cp scripts/hooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "installed .git/hooks/pre-commit"

# run-local: BYOK, zero GCP. Set a non-Google provider key first, e.g.
#   export DEEPSEEK_API_KEY=...   (or OPENAI_API_KEY / GROQ_API_KEY / ZHIPU_API_KEY)
# Then:  make run-local TASK='list the Go files and summarize the governance model'
TASK ?= list the files in this repo and describe what Bob is
run-local: build ## Run Bob against this repo (read-only by default)
	./$(BIN) -workspace . $(TASK)

ci: fmtcheck vet test ## The required CI gate

clean:
	rm -f $(BIN) $(LEGACY_BIN)
	rm -rf dist
