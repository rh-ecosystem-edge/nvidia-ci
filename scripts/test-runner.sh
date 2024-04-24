#!/usr/bin/env bash

GOPATH="${GOPATH:-~/go}"
PATH=$PATH:$GOPATH/bin
TEST_DIR="./tests"
REPORTS_DUMP_DIR="${ARTIFACT_DIR:-/tmp/reports}"

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
cmd="ginkgo -timeout=24h --keep-going --require-suite -r"

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
