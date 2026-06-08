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
testacc: ## Run acceptance tests against the local platform (ADR-042). Override the target with SMPLKIT_API_URL.
	: "$${SMPLKIT_API_URL:=http://localhost}"; \
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
