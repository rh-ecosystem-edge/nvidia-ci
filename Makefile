# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
GO_PACKAGES=$(shell go list ./... | grep -v vendor)
.PHONY: lint \
        deps-update \
        vet

.PHONY: mockgen
mockgen: ## Install mockgen locally.
	go install go.uber.org/mock/mockgen@v0.3.0

.PHONY: generate
generate: mockgen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	go generate ./...

vet:
	go vet ${GO_PACKAGES}

lint:
	@echo "Running go lint"
	scripts/golangci-lint.sh

deps-update:
	go mod tidy && \
	go mod vendor

install-ginkgo:
	scripts/install-ginkgo.sh

build-container-image:
	@echo "Building container image"
	podman build -t nvidiagpu:latest -f Containerfile

install: deps-update install-ginkgo
	@echo "Installing needed dependencies"

TEST ?= ...

.PHONY: unit-test
unit-test:
	go test github.com/rh-ecosystem-edge/nvidia-ci/$(TEST)

get-gpu-operator-must-gather:
	test -s gpu-operator-must-gather.sh || (\
    		SCRIPT_URL="https://raw.githubusercontent.com/NVIDIA/gpu-operator/v23.9.1/hack/must-gather.sh" && \
    		if ! curl -SsL -o gpu-operator-must-gather.sh -L $$SCRIPT_URL; then \
    			echo "Failed to download must-gather script" >&2; \
    			exit 1; \
    		fi && \
    		chmod +x gpu-operator-must-gather.sh \
    	)

run-tests: get-gpu-operator-must-gather
	@echo "Executing nvidiagpu test-runner script"
	scripts/test-runner.sh

test-bm-arm-deployment:
	/bin/bash tests/gpu-operator-arm-bm/uninstall-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/install-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/areweok.sh
