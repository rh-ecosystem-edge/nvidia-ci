#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/.."

scratch=$(mktemp -d)
trap 'rm -rf "${scratch}"' EXIT
test_name=setup

fail_test() {
    echo "FAIL ${test_name}: $*" >&2
    local diagnostic_file
    for diagnostic_file in output events pool.json; do
        if [[ -f ${scratch}/${diagnostic_file} ]]; then
            echo "${diagnostic_file}:" >&2
            cat "${scratch}/${diagnostic_file}" >&2
        fi
    done
    exit 1
}
trap 'fail_test "Unexpected failure on line ${LINENO}: ${BASH_COMMAND}"' ERR

mkdir -p "${scratch}/bin" "${scratch}/manifests"
cp scripts/testdata/apply-manifests/fake-oc.sh "${scratch}/bin/oc"
chmod +x "${scratch}/bin/oc"
export FAKE_OC_DIR=${scratch}
export PATH="${scratch}/bin:${PATH}"
export KUBECONFIG=/selected/config
export MANIFEST_DIR=${scratch}/manifests

write_machineconfig() {
    local name=$1
    jq -n --arg name "${name}" '{
        apiVersion: "machineconfiguration.openshift.io/v1",
        kind: "MachineConfig",
        metadata: {name: $name, labels: {role: "worker"}}
    }' > "${MANIFEST_DIR}/${name}.yaml"
}

set_pool_state() {
    jq "$1" "${scratch}/pool.json" > "${scratch}/pool.json.tmp"
    mv "${scratch}/pool.json.tmp" "${scratch}/pool.json"
}

reset_fixture() {
    rm -f "${MANIFEST_DIR}"/*
    : > "${scratch}/events"
    : > "${scratch}/output"
    export WAIT_FOR_WORKER_MCP=true BATCH_WORKER_MCP=true WORKER_MCP_NAME=worker
    export MANIFEST_VARS=''
    export FAKE_EXISTING_MACHINECONFIG=''
    export FAKE_PAUSE_RESULT=success
    export FAKE_FAILED_CREATE=''
    export FAKE_MISSING_RENDER_SOURCE=''
    export FAKE_STALL_ROLLOUT=false
    export FAKE_FINAL_POOL_FILTER='.'
    write_machineconfig first
    write_machineconfig second
    cat > "${scratch}/pool.json" <<'JSON'
{
    "metadata": {"resourceVersion": "1", "generation": 1},
    "spec": {
        "paused": false,
        "machineConfigSelector": {"matchLabels": {"role": "worker"}},
        "configuration": {"name": "rendered-old", "source": []}
    },
    "status": {
        "observedGeneration": 1,
        "configuration": {"name": "rendered-old", "source": []},
        "conditions": [
            {"type": "Updated", "status": "True"},
            {"type": "Updating", "status": "False"},
            {"type": "Degraded", "status": "False"},
            {"type": "RenderDegraded", "status": "False"}
        ]
    }
}
JSON
}

run_script() {
    local action=${1:-apply}
    if bash scripts/apply-manifests.sh "${action}" > "${scratch}/output" 2>&1; then
        run_status=0
    else
        run_status=$?
    fi
}

assert_success() {
    if (( run_status != 0 )); then
        fail_test "Expected success, got exit status ${run_status}"
    fi
}

assert_failure() {
    if (( run_status == 0 )); then
        fail_test "Expected a nonzero exit status"
    fi
    assert_output_contains "$1"
}

assert_output_contains() {
    local expected=$1 output
    output=$(cat "${scratch}/output")
    if [[ ${output} != *"${expected}"* ]]; then
        fail_test "Expected output to include: ${expected}"
    fi
}

assert_operations() {
    local expected actual
    expected=$(printf '%s\n' "$@")
    # Keep resource changes and hooks; waits are checked separately where relevant.
    actual=$(grep -v '^wait:' "${scratch}/events" || true)
    if [[ ${actual} != "${expected}" ]]; then
        fail_test "Expected operations in this order:
${expected}"
    fi
}

assert_events_in_order() {
    local expected actual
    expected=$(printf '%s\n' "$@")
    printf '%s\n' "$@" > "${scratch}/expected-events"
    # Ignore unrelated events while preserving the order of the requested ones.
    actual=$(grep -Fxf "${scratch}/expected-events" "${scratch}/events" || true)
    if [[ ${actual} != "${expected}" ]]; then
        fail_test "Expected events in this order:
${expected}"
    fi
}

assert_pool() {
    if ! jq -e "$1" "${scratch}/pool.json" > /dev/null; then
        fail_test "$2"
    fi
}

assert_pool_resumed() {
    assert_pool '.spec.paused == false' "Expected the worker pool to be resumed"
}

test_batches_new_machineconfigs() {
    run_script
    assert_success
    assert_operations pause create:first create:second resume
    assert_events_in_order \
        pause create:first create:second \
        'wait:jsonpath={.spec.configuration.source[?(@.name=="first")].name}=first' \
        'wait:jsonpath={.spec.configuration.source[?(@.name=="second")].name}=second' \
        resume \
        'wait:jsonpath={.status.configuration.name}=rendered-new'
    assert_pool_resumed
}

test_existing_machineconfig_uses_sequential_apply() {
    export FAKE_EXISTING_MACHINECONFIG=first
    run_script
    assert_success
    assert_output_contains 'MachineConfig/first already exists'
    assert_operations apply:first apply:second
}

test_hook_runs_between_sequential_applies() {
    cat > "${MANIFEST_DIR}/first.sh" <<'HOOK'
if [[ ${KUBECONFIG} != /selected/config ]]; then
    exit 1
fi
echo hook:first >> "${FAKE_OC_DIR}/events"
HOOK
    run_script
    assert_success
    assert_output_contains 'has a post-apply hook'
    assert_operations apply:first hook:first apply:second
}

test_unsupported_kind_uses_sequential_apply() {
    echo '{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"KubeletConfig","metadata":{"name":"other"}}' > "${MANIFEST_DIR}/00-other.yaml"
    run_script
    assert_success
    assert_output_contains 'not a single resource of a supported kind'
    assert_operations apply:other apply:first apply:second
}

test_duplicate_machineconfig_uses_sequential_apply() {
    cp "${MANIFEST_DIR}/first.yaml" "${MANIFEST_DIR}/third.yaml"
    run_script
    assert_success
    assert_output_contains 'MachineConfig/first appears more than once'
    assert_operations apply:first apply:second apply:first
}

test_batching_can_be_disabled() {
    export BATCH_WORKER_MCP=false
    run_script
    assert_success
    assert_output_contains 'batching is disabled'
    assert_operations apply:first apply:second
}

test_batching_requires_pool_waiting() {
    export WAIT_FOR_WORKER_MCP=false
    run_script
    assert_success
    assert_output_contains 'worker pool waiting is disabled'
    assert_operations apply:first apply:second
    if grep -q '^wait:' "${scratch}/events"; then
        fail_test "Pool waiting was disabled, but the script waited on the pool"
    fi
}

test_already_paused_pool_is_left_untouched() {
    set_pool_state '.spec.paused = true'
    run_script
    assert_failure 'is already paused'
    assert_operations
    assert_pool '.spec.paused == true' "Expected the existing pause to remain"
}

test_cleanup_resumes_a_committed_pause_after_client_error() {
    export FAKE_PAUSE_RESULT=committed-error
    run_script
    assert_failure 'Pause committed, but the client reported an error'
    assert_operations pause resume
    assert_pool_resumed
}

test_cleanup_fences_a_rejected_pause() {
    export FAKE_PAUSE_RESULT=rejected
    run_script
    assert_failure 'Pause request rejected before committing'
    assert_operations pause fence resume
    assert_pool_resumed
}

test_cleanup_resumes_after_create_failure() {
    export FAKE_FAILED_CREATE=second
    run_script
    assert_failure 'Failed to create MachineConfig/second'
    assert_operations pause create:first create:second resume
    assert_pool_resumed
}

test_render_timeout_resumes_the_pool_and_fails() {
    export FAKE_MISSING_RENDER_SOURCE=second
    run_script
    assert_failure 'Timed out waiting for jsonpath={.spec.configuration.source[?(@.name=="second")].name}=second'
    assert_operations pause create:first create:second resume
    assert_pool_resumed
}

test_rollout_timeout_fails_after_resuming_the_pool() {
    export FAKE_STALL_ROLLOUT=true
    run_script
    assert_failure 'Timed out waiting for jsonpath={.status.configuration.name}=rendered-new'
    assert_operations pause create:first create:second resume
    assert_pool_resumed
}

test_final_pool_status_must_include_all_machineconfigs() {
    export FAKE_FINAL_POOL_FILTER='.status.configuration.source = []'
    run_script
    assert_failure 'status is missing MachineConfig/first'
    assert_pool_resumed
}

test_final_rendered_target_must_match() {
    export FAKE_FINAL_POOL_FILTER='.spec.configuration.name = "rendered-other" | .status.configuration.name = "rendered-other"'
    run_script
    assert_failure 'rendered target changed'
    assert_pool_resumed
}

test_delete_uses_reverse_filename_order() {
    run_script delete
    assert_success
    assert_operations delete:second delete:first
}

run_test() {
    test_name=$1
    reset_fixture
    "${test_name}"
    assert_pool '.metadata.annotations["nvidia-ci/pause-owner"] == null' "Expected pause ownership to be cleared"
    echo "PASS ${test_name}"
}

run_test test_batches_new_machineconfigs
run_test test_existing_machineconfig_uses_sequential_apply
run_test test_hook_runs_between_sequential_applies
run_test test_unsupported_kind_uses_sequential_apply
run_test test_duplicate_machineconfig_uses_sequential_apply
run_test test_batching_can_be_disabled
run_test test_batching_requires_pool_waiting
run_test test_already_paused_pool_is_left_untouched
run_test test_cleanup_resumes_a_committed_pause_after_client_error
run_test test_cleanup_fences_a_rejected_pause
run_test test_cleanup_resumes_after_create_failure
run_test test_render_timeout_resumes_the_pool_and_fails
run_test test_rollout_timeout_fails_after_resuming_the_pool
run_test test_final_pool_status_must_include_all_machineconfigs
run_test test_final_rendered_target_must_match
run_test test_delete_uses_reverse_filename_order
