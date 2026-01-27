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
#### user arguments can be e.g.:
# --single.mig.profile=0  #### usually 0-5 are valid
# --mixed.mig.instances='2,0,1,1'  #### defaults are for A100 GPU [2,0,1,1,0,0]
# --mixed.mig.pod-delay=10  #### delay in seconds between pod creation on mixed-mig testcase
# --no-color  #### disable color output on mig testcases
#### make run-tests ARGS="-- --pod-delay=10 --single.mig.profile=0 --mixed.mig.instances='2,0,1,1' --no-color=true"

# Execute ginkgo command
echo $cmd
eval $cmd
