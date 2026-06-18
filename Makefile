SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# Repository-wide knobs.
COVERPROFILE := coverage.out

##@ Build

.PHONY: build
build: ## Build the provider binary.
	go build -o terraform-provider-smplkit .

.PHONY: install
install: ## Install the provider binary into $GOBIN.
	go install .

##@ Test

.PHONY: test
test: ## Run unit tests.
	go test ./... -count=1

.PHONY: testacc
testacc: ## Run acceptance tests. Destructive e2e — point ONLY at an ephemeral prod account (ADR-052 §2.8). SMPLKIT_API_URL is required.
	@if [ -z "$${SMPLKIT_API_URL:-}" ]; then \
		echo "ERROR: SMPLKIT_API_URL is required." >&2; \
		echo "  These acceptance tests are destructive e2e tests: they create and delete real" >&2; \
		echo "  environments and delete the seeded 'development' environment to free a slot." >&2; \
		echo "  Per ADR-052 §2.8 they must run only against an ephemeral production account — never the" >&2; \
		echo "  local dev platform. Run them via the e2e suite" >&2; \
		echo "  (e2e/tests/tools/test_terraform_acceptance.py), or set SMPLKIT_API_URL explicitly" >&2; \
		echo "  to a throwaway-account endpoint (e.g. https://app.smplkit.com)." >&2; \
		exit 1; \
	fi; \
		TF_ACC=1 SMPLKIT_API_URL="$$SMPLKIT_API_URL" \
		go test ./internal/provider/... -v -timeout 30m -count=1

.PHONY: cover
cover: ## Run unit tests with coverage.
	go test ./... -coverprofile=$(COVERPROFILE) -covermode=atomic
	go tool cover -func=$(COVERPROFILE) | tail -1

##@ Lint

.PHONY: vet
vet: ## go vet.
	go vet ./...

.PHONY: lint
lint: ## golangci-lint.
	golangci-lint run ./...

##@ Docs

.PHONY: docs
docs: ## Regenerate docs/ via tfplugindocs.
	go tool tfplugindocs generate --provider-name smplkit

.PHONY: docs-check
docs-check: ## Fail if committed docs differ from what tfplugindocs would generate.
	go tool tfplugindocs generate --provider-name smplkit
	@if ! git diff --exit-code -- docs/; then \
		echo "docs/ are out of date — run \`make docs\` and commit the result"; \
		exit 1; \
	fi

##@ Help

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n"} \
	/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } \
	/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
