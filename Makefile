# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
GO_PACKAGES=$(shell go list ./... | grep -v vendor)
.PHONY: lint \
        deps-update \
        vet \
        verify

.PHONY: help
help: ## Show available make targets
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: mockgen
mockgen: ## Install mockgen locally.
	go install go.uber.org/mock/mockgen@v0.6.0

.PHONY: generate
generate: mockgen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	go generate ./...

vet: ## Run go vet on all packages
	go vet ${GO_PACKAGES}

lint: ## Run golangci-lint
	@echo "Running go lint"
	scripts/golangci-lint.sh

verify: lint vet ## Verify code quality
	@echo "All quality checks passed"

deps-update: ## Update dependencies (go mod tidy and vendor)
	go mod tidy && \
	go mod vendor

install-ginkgo: ## Install ginkgo test framework
	scripts/install-ginkgo.sh

build-container-image: ## Build container image
	@echo "Building container image"
	podman build -t nvidiagpu:latest -f Containerfile

install: deps-update install-ginkgo ## Install dependencies
	@echo "Installing needed dependencies"

TEST ?= ...

.PHONY: unit-test
unit-test: ## Run unit tests (use TEST=path to specify)
	go test github.com/rh-ecosystem-edge/nvidia-ci/$(TEST)

get-gpu-operator-must-gather: ## Download GPU operator must-gather script
	test -s scripts/gpu-operator-must-gather.sh || (\
    	SCRIPT_URL="https://raw.githubusercontent.com/NVIDIA/gpu-operator/v25.10.1/hack/must-gather.sh" && \
    	if ! curl -SsLf -o scripts/gpu-operator-must-gather.sh $$SCRIPT_URL; then \
    		echo "Failed to download must-gather script" >&2; \
    		exit 1; \
    	fi && \
    	chmod +x scripts/gpu-operator-must-gather.sh \
    )

get-nfd-must-gather: ## Download NFD must-gather script
	test -s scripts/nfd-must-gather.sh || (\
		SCRIPT_URL="https://raw.githubusercontent.com/openshift/cluster-nfd-operator/refs/heads/release-4.22/must-gather/gather" && \
		if ! curl -SsLf -o scripts/nfd-must-gather.sh $$SCRIPT_URL; then \
			echo "Failed to download NFD must-gather script" >&2; \
			exit 1; \
		fi && \
		chmod +x scripts/nfd-must-gather.sh \
	)

run-tests: get-gpu-operator-must-gather get-nfd-must-gather ## Run test suite
	@echo "Executing nvidiagpu test-runner script"
	scripts/test-runner.sh $(ARGS)

run-mig-tests: get-gpu-operator-must-gather get-nfd-must-gather
	@echo "Executing mig-tests runner script"
	scripts/mig-tests.sh $(ARGS)

test-bm-arm-deployment: ## Test bare-metal ARM deployment
	/bin/bash tests/gpu-operator-arm-bm/uninstall-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/install-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/areweok.sh
