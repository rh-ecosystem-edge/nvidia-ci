#!/usr/bin/env bash

GOPATH="${GOPATH:-~/go}"
PATH=$PATH:$GOPATH/bin
TEST_DIR="./tests"

# Override REPORTS_DUMP_DIR if ARTIFACT_DIR is set
if [[ -n "${ARTIFACT_DIR}" ]]; then
    # `export` passes the variable down to a child process,
    # which is needed because of the `eval`
    export REPORTS_DUMP_DIR=${ARTIFACT_DIR}
fi

# Check that TEST_FEATURES environment variable has been set
if [[ -z "${TEST_FEATURES}" ]]; then
    echo "TEST_FEATURES environment variable is undefined"
    exit 1
fi

# The signed NNO workflow publishes this handoff before its GPU installation step.
# Its workload uses DMA-BUF, so override the release step's legacy RDMA patch
# before creating ClusterPolicy and starting the driver build.
if [[ "${TEST_FEATURES}" == "nvidiagpu" && -n "${SHARED_DIR:-}" &&
      -s "${SHARED_DIR}/nno-doca2-signed-request.json" ]]; then
    if ! signed_gpu_patch=$(python3 - <<'PY'
import json
import os
import sys

try:
    patch = json.loads(os.environ.get("NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH") or "[]")
    if not isinstance(patch, list):
        raise ValueError("ClusterPolicy patch must be a JSON array")
except ValueError as error:
    print("signed NNO GPU policy error: " + str(error), file=sys.stderr)
    sys.exit(1)

patch.extend([
    {"op": "add", "path": "/spec/driver/rdma", "value": {"enabled": False}},
    {"op": "add", "path": "/spec/driver/kernelModuleType", "value": "open"},
])
print(json.dumps(patch))
PY
    ); then
        exit 1
    fi
    export NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH="${signed_gpu_patch}"
    echo "Signed NNO GPU installation uses open kernel modules and DMA-BUF (driver.rdma.enabled=false)"
fi

# Set feature_dirs to top-level test directory when "all" feature provided
if [[ "${TEST_FEATURES}" == "all" ]]; then
    feature_dirs=${TEST_DIR}
else
    # Find all test directories matching provided features
    for feature in ${TEST_FEATURES}; do
        discovered_features=$(find $TEST_DIR -depth -name "${feature}" -not -path '*/internal/*' 2> /dev/null)
        if [[ ! -z $discovered_features ]]; then
            feature_dirs+=" "$discovered_features
        else
            if [[ "${VERBOSE_SCRIPT}" == "true" ]]; then
                echo "Could not find any feature directories matching ${feature}"
            fi
        fi
    done

    if [[ -z "${feature_dirs}" ]]; then
        echo "Could not find any feature directories for provided features: ${TEST_FEATURES}"
        exit 1
    fi

    if [[ "${VERBOSE_SCRIPT}" == "true" ]]; then
        echo "Found feature directories:"
        for directory in $feature_dirs; do printf "$directory\n"; done
    fi
fi


# Build ginkgo command
cmd="PATH_TO_MUST_GATHER_SCRIPT=$(pwd)/scripts/gpu-operator-tests-must-gather.sh ginkgo -timeout=24h --keep-going --require-suite -r"

if [[ "${TEST_VERBOSE}" == "true" ]]; then
    cmd+=" -vv"
fi

if [[ "${TEST_TRACE}" == "true" ]]; then
    cmd+=" --trace"
fi

if [[ ! -z "${TEST_LABELS}" ]]; then
    cmd+=" --label-filter=\"${TEST_LABELS}\""
fi
cmd+=" "$feature_dirs" $@"   # + user args --xxx=yyy...


# Execute ginkgo command
echo $cmd
eval $cmd
