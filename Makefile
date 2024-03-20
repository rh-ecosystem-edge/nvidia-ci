# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
GO_PACKAGES=$(shell go list ./... | grep -v vendor)
.PHONY: lint \
        deps-update \
        vet
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
	podman build -t nvidiagpu:latest -f Dockerfile

install: deps-update install-ginkgo
	@echo "Installing needed dependencies"

run-tests:
	@echo "Executing nvidiagpu test-runner script"
	scripts/test-runner.sh

test-bm-arm-deployment:
	/bin/bash tests/gpu-operator-arm-bm/uninstall-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/install-gpu-operator.sh
	/bin/bash tests/gpu-operator-arm-bm/areweok.sh
