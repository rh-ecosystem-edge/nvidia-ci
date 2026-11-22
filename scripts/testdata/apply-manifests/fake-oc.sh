#!/usr/bin/env bash
# Minimal oc fixture for apply-manifests-test.sh. Manifests use JSON, which is valid YAML.
set -euo pipefail

if [[ $1 != --kubeconfig=/selected/config ]]; then
    echo "Expected the explicitly selected kubeconfig, got: $1" >&2
    exit 1
fi
shift
if [[ $1 == --request-timeout=* ]]; then
    shift
fi
oc_command=$1
shift
resource=${1:-}
manifest_file=''
patch=''
wait_condition=''
dry_run=false
save_config=false
while (( $# > 0 )); do
    case $1 in
        -f)
            manifest_file=$2
            shift
            ;;
        -p)
            patch=$2
            shift
            ;;
        --for=*) wait_condition=${1#--for=} ;;
        --dry-run=client) dry_run=true ;;
        --save-config) save_config=true ;;
    esac
    shift
done
pool_file=${FAKE_OC_DIR}/pool.json

record_event() {
    echo "$1" >> "${FAKE_OC_DIR}/events"
}

update_pool() {
    local filter=$1
    shift
    jq "$@" "${filter}" "${pool_file}" > "${pool_file}.tmp"
    mv "${pool_file}.tmp" "${pool_file}"
}

get_resource() {
    case ${resource} in
        mcp/*)
            cat "${pool_file}"
            ;;
        machineconfig/*)
            if [[ ${resource} == "machineconfig/${FAKE_EXISTING_MACHINECONFIG}" ]]; then
                echo "${resource}"
            fi
            ;;
        *)
            echo "Unsupported get: ${resource}" >&2
            return 1
            ;;
    esac
}

patch_pool() {
    local operation
    operation=$(jq -r '
        if .[-1].value == true then "pause"
        elif .[-1].op == "remove" then "resume"
        else "fence" end
    ' <<< "${patch}")
    record_event "${operation}"
    if [[ ${operation} == pause && ${FAKE_PAUSE_RESULT} == rejected ]]; then
        echo "Pause request rejected before committing" >&2
        return 1
    fi

    # Apply the actual JSON patch, including its ownership and version preconditions.
    update_pool '
        . as $original |
        reduce $patch[] as $op (.;
            ($op.path | split("/")[1:] | map(gsub("~1"; "/"))) as $path |
            if $op.op == "test" then
                if getpath($path) == $op.value then . else error("patch test failed") end
            elif $op.op == "remove" then delpaths([$path])
            else setpath($path; $op.value) end) |
        .metadata.resourceVersion |= (tonumber + 1 | tostring) |
        if .spec != $original.spec then .metadata.generation += 1 else . end
    ' --argjson patch "${patch}"
    if [[ ${operation} == pause && ${FAKE_PAUSE_RESULT} == committed-error ]]; then
        echo "Pause committed, but the client reported an error" >&2
        return 1
    fi
}

wait_condition_matches() {
    local expected_value condition_name condition_status expected_source_wait
    expected_value=${wait_condition##*=}
    case ${wait_condition} in
        condition=*)
            condition_name=${wait_condition#condition=}
            condition_name=${condition_name%=*}
            condition_status=${expected_value}
            jq -e --arg type "${condition_name}" --arg status "${condition_status}" '
                any(.status.conditions[]; .type == $type and .status == $status)
            ' "${pool_file}" > /dev/null
            ;;
        'jsonpath={.spec.configuration.source['*)
            # Accept the source lookup used by the script; reject malformed lookups.
            expected_source_wait="jsonpath={.spec.configuration.source[?(@.name==\"${expected_value}\")].name}=${expected_value}"
            if [[ ${wait_condition} != "${expected_source_wait}" ]]; then
                return 1
            fi
            jq -e --arg name "${expected_value}" '
                any(.spec.configuration.source[]; .name == $name)
            ' "${pool_file}" > /dev/null
            ;;
        'jsonpath={.status.configuration.name}='*)
            jq -e --arg expected "${expected_value}" '
                .status.configuration.name == $expected
            ' "${pool_file}" > /dev/null
            ;;
        'jsonpath={.status.observedGeneration}='*)
            jq -e --arg expected "${expected_value}" '
                (.status.observedGeneration | tostring) == $expected
            ' "${pool_file}" > /dev/null
            ;;
        *)
            echo "Unsupported wait: ${wait_condition}" >&2
            return 1
            ;;
    esac
}

wait_for_pool() {
    record_event "wait:${wait_condition}"
    if [[ ${FAKE_STALL_ROLLOUT} == false ]] && jq -e '.spec.paused != true' "${pool_file}" > /dev/null; then
        update_pool '.status.configuration = .spec.configuration |
            .status.observedGeneration = .metadata.generation'
    fi
    if ! wait_condition_matches; then
        # Simulate a timeout immediately, without slowing down the test suite.
        echo "Timed out waiting for ${wait_condition}" >&2
        return 1
    fi
    if [[ ${wait_condition} == condition=RenderDegraded=False ]]; then
        update_pool "${FAKE_FINAL_POOL_FILTER}"
    fi
}

change_manifest() {
    local input name
    input=$(cat "${manifest_file}")
    if [[ ${dry_run} == true ]]; then
        echo "${input}"
        return 0
    fi
    name=$(jq -r '.metadata.name' <<< "${input}")
    record_event "${oc_command}:${name}"
    if [[ ${oc_command} != create ]]; then
        return 0
    fi
    if [[ ${save_config} != true ]]; then
        echo "MachineConfig creation must preserve the last-applied configuration" >&2
        return 1
    fi
    if [[ ${name} == "${FAKE_FAILED_CREATE}" ]]; then
        echo "Failed to create MachineConfig/${name}" >&2
        return 1
    fi
    if [[ ${name} != "${FAKE_MISSING_RENDER_SOURCE}" ]]; then
        update_pool '.spec.configuration.name = "rendered-new" |
            .spec.configuration.source += [{name: $name}] |
            .metadata.generation += 1' --arg name "${name}"
    fi
}

case ${oc_command} in
    get) get_resource ;;
    patch) patch_pool ;;
    wait) wait_for_pool ;;
    apply|create|delete) change_manifest ;;
    *)
        echo "Unsupported oc command: ${oc_command}" >&2
        exit 1
        ;;
esac
