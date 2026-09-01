# AGENTS.md

This file provides guidance to AI tools when working with code in this repository.

## Project overview

Golang + Ginkgo automation/CI framework for testing NVIDIA GPU Operator and Network Operator on OpenShift Container Platform (OCP >= 4.12). Tests run against a pre-installed OCP cluster via `KUBECONFIG`.

## Common commands

```bash
# Lint (downloads golangci-lint v2 if needed)
make lint

# Vet
make vet

# Both lint + vet
make verify

# Unit tests (all packages)
make unit-test

# Unit tests (specific package)
make unit-test TEST=pkg/nodes

# Run integration/e2e tests (requires OCP cluster + env vars)
make run-tests              # uses scripts/test-runner.sh
make run-mig-tests          # MIG-specific tests

# Update dependencies
make deps-update            # go mod tidy && go mod vendor

# Generate mocks
make generate               # runs mockgen + go generate

# Build container image
make build-container-image  # podman build
```

## Running tests

Tests are Ginkgo suites under `tests/`. The test-runner script (`scripts/test-runner.sh`) wraps ginkgo with env-var-driven configuration:

- **`TEST_FEATURES`** (required): comma-separated subdirectory names under `tests/` (e.g. `nvidiagpu`, `mps`, `mig`, `nvidianetwork`, `timeslicing`, `dra`)
- **`TEST_LABELS`**: Ginkgo label filter (e.g. `'nvidia-ci,gpu'`, `'deploy || rdma-legacy-sriov'`)
- **`KUBECONFIG`**: path to cluster kubeconfig
- **`NVIDIAGPU_CLEANUP`**: set to `false` when chaining test suites (e.g. deploy GPU Operator first, then run MPS/MIG/timeslicing tests on the same cluster)

Pass ginkgo CLI parameters via `ARGS`: `make run-tests ARGS="-- --single.mig.profile=1"`

## Architecture

### Package layout

- **`internal/inittools/`**: Package-level `init()` that creates the API client (`APIClient`) and loads config (`GeneralConfig`). Import with dot-import to get these globals. All test files under `tests/` import this.
- **`internal/config/`**: `GeneralConfig` struct, populated from `default.yaml` + env vars via `envconfig`. Controls report paths, verbosity, dry-run, node labels.
- **`internal/tsparams/`**: Test suite parameters: labels, namespaces to dump, CRDs to dump. Separate vars files per feature (GPU, network, MPS, MIG, timeslicing).
- **`internal/gpuparams/`, `internal/networkparams/`**: Label constants and feature-specific env-var-driven config structs.
- **`internal/`** (other packages): Test helpers for specific features: `deploy/`, `check/`, `get/`, `wait/`, `gpuburn/`, `mps/`, `rdma/`, `timeslicing/`, `dra/`, `helm/`, `testworkloads/`, `reporter/`.
- **`pkg/`**: Reusable Kubernetes resource wrappers: `clients/`, `pod/`, `namespace/`, `deployment/`, `nodes/`, `machine/`, `configmap/`, `olm/`, `nfd/`, `nvidiagpu/`, `nvidianetwork/`, `mig/`, `operatorconfig/`.
- **`tests/`**: Ginkgo test suites. Each subdirectory has a `*_suite_test.go` (test runner + reporter setup) and `*_test.go` files. Test suites: `nvidiagpu`, `nvidianetwork`, `mps`, `mig`, `timeslicing`, `dra/` (with `gpuallocation` and `computedomain` sub-suites), `dummy`.
- **`mcp/prow-analyzer/`**: Python MCP server for analyzing Prow CI job results (separate from the Go codebase).

### Key patterns

- **API client**: `pkg/clients.Settings` wraps multiple Kubernetes/OpenShift typed clients plus a controller-runtime client. Custom CRD schemes (GPU Operator, Network Operator, DRA, NFD) are registered in `SetScheme()`.
- **Config via env vars**: Test parameters are loaded from environment variables using `envconfig` tags (see `internal/nvidiagpuconfig/`, `internal/nvidianetworkconfig/`, `internal/nfd/`).
- **Test labels**: Tests use Ginkgo labels for selective execution. Common labels: `nvidia-ci`, `gpu`, `nno`, `mps`, `mig`, `single-mig`, `mixed-mig`, `timeslicing`, `deploy`, `cleanup`, `operator-upgrade`, `rdma-legacy-sriov`, `rdma-shared-dev`.
- **Reporter**: `internal/reporter/` uses k8sreporter to dump namespace logs and CRDs on test failure. Controlled by `DUMP_FAILED_TESTS` and `REPORTS_DUMP_DIR` env vars.

### Linting

golangci-lint v2 with config in `.golangci.yml`. Enabled linters beyond defaults: `decorder`, `exhaustive`, `goconst`, `importas`, `loggercheck`, `wastedassign`, `nolintlint`, `revive`. File naming enforced by revive's `filename-format` rule: `^[a-z][a-z0-9]*(_suite_test|_test|_linux|_darwin|_windows|_amd64|_arm64)?\.go$`.

### Vendoring

Dependencies are vendored (`vendor/`). After adding or updating deps, run `make deps-update`.
