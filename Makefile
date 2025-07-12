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
	test -s scripts/gpu-operator-must-gather.sh || (\
    		SCRIPT_URL="https://raw.githubusercontent.com/NVIDIA/gpu-operator/v23.9.1/hack/must-gather.sh" && \
    		if ! curl -SsL -o scripts/gpu-operator-must-gather.sh -L $$SCRIPT_URL; then \
    			echo "Failed to download must-gather script" >&2; \
    			exit 1; \
    		fi && \
    		chmod +x scripts/gpu-operator-must-gather.sh \
    	)

get-nfd-must-gather:
	# TODO: change it to use release branch when it will be released
	test -s scripts/nfd-must-gather.sh || (\
		SCRIPT_URL="https://raw.githubusercontent.com/openshift/cluster-nfd-operator/c93914db38232ebf2abf62fa8028c23a154a5048/must-gather/gather" && \
		if ! curl -SsL -o scripts/nfd-must-gather.sh -L $$SCRIPT_URL; then \
			echo "Failed to download NFD must-gather script" >&2; \
			exit 1; \
		fi && \
		chmod +x scripts/nfd-must-gather.sh \
	)

run-tests: get-gpu-operator-must-gather get-nfd-must-gather
	@echo "Executing nvidiagpu test-runner script"
	scripts/test-runner.sh

test-bm-arm-deployment:
	/bin/bash tests/gpu-operator-arm-bm/uninstall-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/install-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/areweok.sh
