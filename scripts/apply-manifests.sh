#!/usr/bin/env bash
# Apply day-2 manifests in filename order, or delete them in reverse order.
# Eligible new MachineConfigs can share one rollout by pausing the worker pool.
set -eo pipefail
cd "$(dirname "$0")/.."

ACTION=${1:-apply}
MANIFEST_DIR=${MANIFEST_DIR:-manifests/nno}
WAIT_FOR_WORKER_MCP=${WAIT_FOR_WORKER_MCP:-false}
BATCH_WORKER_MCP=${BATCH_WORKER_MCP:-false}
WORKER_MCP_NAME=${WORKER_MCP_NAME:-worker}
WORKER_MCP_ROLLOUT_START_TIMEOUT=${WORKER_MCP_ROLLOUT_START_TIMEOUT:-2m}
WORKER_MCP_ROLLOUT_TIMEOUT=${WORKER_MCP_ROLLOUT_TIMEOUT:-40m}

fail() {
    echo "$*" >&2
    exit 1
}

oc() {
    command oc --kubeconfig="${KUBECONFIG}" "$@"
}

get_worker_pool() {
    oc --request-timeout=10s get "mcp/${WORKER_MCP_NAME}" -o json
}

wait_for_worker_pool() {
    local condition=$1 timeout=$2
    oc wait "mcp/${WORKER_MCP_NAME}" --for="${condition}" --timeout="${timeout}"
}

if [[ $# -gt 1 || ( ${ACTION} != apply && ${ACTION} != delete ) ]]; then
    fail "Usage: $0 [apply|delete]"
fi
if [[ ! -d ${MANIFEST_DIR} ]]; then
    fail "Manifest directory '${MANIFEST_DIR}' does not exist"
fi
if [[ -z ${KUBECONFIG:-} || ${KUBECONFIG} == *:* ]]; then
    fail "KUBECONFIG must name a single file"
fi
export KUBECONFIG
for tool in oc envsubst; do
    if ! type -P "${tool}" > /dev/null; then
        fail "Required command '${tool}' not found"
    fi
done

render_manifest() {
    if [[ -n ${MANIFEST_VARS:-} ]]; then
        envsubst -- "${MANIFEST_VARS}" < "$1"
    else
        envsubst < "$1"
    fi
}

check_required_vars() {
    local manifest_file=$1 variable_name
    local required_vars_file=${manifest_file%.*}.env-required

    if [[ ! -f ${required_vars_file} ]]; then
        return 0
    fi
    while read -r variable_name || [[ -n ${variable_name} ]]; do
        if [[ -z ${variable_name} || ${variable_name} == \#* ]]; then
            continue
        fi
        if [[ ! ${variable_name} =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
            fail "Invalid variable name in ${required_vars_file}"
        fi
        # Indirect expansion reads the value of the variable named in the file.
        if [[ -z ${!variable_name:-} ]]; then
            fail "Missing required variable ${variable_name} for ${manifest_file}"
        fi
    done < "${required_vars_file}"
}

pool_is_settled() {
    # Updated alone is insufficient: the controller must also have observed this spec.
    jq -e '
        (.spec.paused != true) and
        (.status.observedGeneration >= .metadata.generation) and
        (.spec.configuration.name | length > 0) and
        (.spec.configuration.name == .status.configuration.name) and
        any(.status.conditions[]; .type == "Updated" and .status == "True") and
        all(.status.conditions[] |
            select(.type == "Updating" or .type == "Degraded" or .type == "RenderDegraded");
            .status != "True")
    ' > /dev/null
}

build_pause_patch() {
    local paused=$1
    jq -c --arg owner "${pause_owner}" --argjson paused "${paused}" '[
        {op: "test", path: "/metadata/resourceVersion", value: .metadata.resourceVersion},
        {op: "add", path: "/metadata/annotations", value: ((.metadata.annotations // {}) + {"nvidia-ci/pause-owner": $owner})},
        {op: "add", path: "/spec/paused", value: $paused}
    ]'
}

resume_worker_pool() {
    local pool_state current_owner current_version pool_paused patch
    local deadline=$((SECONDS + 60))

    while (( SECONDS < deadline )); do
        if ! pool_state=$(get_worker_pool); then
            continue
        fi
        current_owner=$(jq -r '.metadata.annotations["nvidia-ci/pause-owner"] // ""' <<< "${pool_state}")
        current_version=$(jq -r '.metadata.resourceVersion' <<< "${pool_state}")
        pool_paused=$(jq -r '.spec.paused // false' <<< "${pool_state}")

        if [[ ${current_owner} == "${pause_owner}" ]]; then
            # Remove only the pause owned by this invocation.
            patch=$(jq -cn --arg owner "${pause_owner}" '[
                {op: "test", path: "/metadata/annotations/nvidia-ci~1pause-owner", value: $owner},
                {op: "add", path: "/spec/paused", value: false},
                {op: "remove", path: "/metadata/annotations/nvidia-ci~1pause-owner"}
            ]')
            if oc --request-timeout=10s patch "mcp/${WORKER_MCP_NAME}" --type=json -p "${patch}"; then
                pending_pause_version=''
                return 0
            fi
        elif [[ ${current_version} == "${pending_pause_version}" ]]; then
            # The pause request may still arrive after its client reported failure.
            # Advance the version so that delayed request fails its version check.
            patch=$(build_pause_patch false <<< "${pool_state}")
            if ! oc --request-timeout=10s patch "mcp/${WORKER_MCP_NAME}" --type=json -p "${patch}"; then
                continue
            fi
        else
            # Our original request can no longer apply. Do not undo someone else's pause.
            if [[ ${pool_paused} != false ]]; then
                return 1
            fi
            pending_pause_version=''
            return 0
        fi
    done
    return 1
}

cleanup() {
    local exit_status=$?
    trap - EXIT
    trap '' HUP INT TERM

    if [[ -n ${pending_pause_version} ]]; then
        if ! resume_worker_pool; then
            echo "Could not resume mcp/${WORKER_MCP_NAME}; check the pool before retrying." >&2
            exit_status=1
        fi
    fi
    if [[ ${exit_status} != 0 && ${BATCH_WORKER_MCP} == true && ${WAIT_FOR_WORKER_MCP} == true ]]; then
        get_worker_pool >&2 || true
    fi
    rm -rf "${work_dir}"
    exit "${exit_status}"
}

prepare_batch() {
    local manifest_file prepared_file resource_type name previous_name existing
    local candidate_manifests=() candidate_machineconfigs=()

    if [[ ${BATCH_WORKER_MCP} != true ]]; then
        batch_skip_reason="batching is disabled"
        return 0
    fi
    if [[ ${WAIT_FOR_WORKER_MCP} != true ]]; then
        batch_skip_reason="worker pool waiting is disabled"
        return 0
    fi
    for manifest_file in "${manifest_files[@]}"; do
        if [[ -f ${manifest_file%.*}.sh ]]; then
            batch_skip_reason="${manifest_file} has a post-apply hook"
            return 0
        fi
    done
    if ! type -P jq > /dev/null; then
        fail "Required command 'jq' not found for batching"
    fi

    for manifest_file in "${manifest_files[@]}"; do
        prepared_file="${work_dir}/${manifest_file##*/}.json"
        # oc flattens Lists; eligibility depends on the resulting single resource.
        render_manifest "${manifest_file}" |
            oc create --dry-run=client --validate=false -f - -o json > "${prepared_file}"
        resource_type=$(jq -r '.apiVersion + "/" + .kind' "${prepared_file}")

        case ${resource_type} in
            machineconfiguration.openshift.io/v1/MachineConfig)
                name=$(jq -r '.metadata.name // ""' "${prepared_file}")
                if [[ -z ${name} ]]; then
                    batch_skip_reason="${manifest_file} has no MachineConfig name"
                    return 0
                fi
                for previous_name in "${candidate_machineconfigs[@]}"; do
                    if [[ ${previous_name} == "${name}" ]]; then
                        batch_skip_reason="MachineConfig/${name} appears more than once"
                        return 0
                    fi
                done
                existing=$(oc get "machineconfig/${name}" --ignore-not-found -o name)
                if [[ -n ${existing} ]]; then
                    batch_skip_reason="MachineConfig/${name} already exists"
                    return 0
                fi
                candidate_machineconfigs+=("${name}")
                ;;
            v1/ConfigMap|v1/ServiceAccount|rbac.authorization.k8s.io/v1/RoleBinding)
                # These resources can be applied without waiting for a worker rollout.
                ;;
            *)
                batch_skip_reason="${manifest_file} is not a single resource of a supported kind (${resource_type})"
                return 0
                ;;
        esac
        candidate_manifests+=("${prepared_file}")
    done

    # Publish the batch only after every file passes preflight, in manifest_files order.
    prepared_manifests=("${candidate_manifests[@]}")
    batch_machineconfigs=("${candidate_machineconfigs[@]}")
    batch_ready=true
}

pause_worker_pool() {
    local pool_state prepared_file selector name patch
    pool_state=$(get_worker_pool)
    if [[ $(jq -r '.spec.paused // false' <<< "${pool_state}") != false ]]; then
        fail "mcp/${WORKER_MCP_NAME} is already paused"
    fi
    wait_for_worker_pool condition=Updated=True "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    pool_state=$(get_worker_pool)
    if ! pool_is_settled <<< "${pool_state}"; then
        fail "mcp/${WORKER_MCP_NAME} is not settled; refusing to batch changes"
    fi

    selector=$(jq -c '.spec.machineConfigSelector' <<< "${pool_state}")
    for prepared_file in "${prepared_manifests[@]}"; do
        if [[ $(jq -r '.kind' "${prepared_file}") != MachineConfig ]]; then
            continue
        fi
        if ! jq -e --argjson selector "${selector}" '
            .metadata.labels as $labels |
            ($selector.matchLabels | length > 0) and
            (($selector.matchExpressions // []) | length == 0) and
            all($selector.matchLabels | to_entries[]; $labels[.key] == .value)
        ' "${prepared_file}" > /dev/null; then
            fail "MachineConfig in ${prepared_file} does not match a supported matchLabels selector on mcp/${WORKER_MCP_NAME}"
        fi
    done
    for name in "${batch_machineconfigs[@]}"; do
        if ! jq -e --arg name "${name}" '
            all((.spec.configuration.source + .status.configuration.source)[]; .name != $name)
        ' <<< "${pool_state}" > /dev/null; then
            fail "MachineConfig/${name} is still referenced by mcp/${WORKER_MCP_NAME}"
        fi
    done

    patch=$(build_pause_patch true <<< "${pool_state}")
    # Arm cleanup before sending the request: a failed client may have committed it.
    pending_pause_version=$(jq -r '.metadata.resourceVersion' <<< "${pool_state}")
    echo "Pausing mcp/${WORKER_MCP_NAME} to batch new MachineConfigs."
    oc --request-timeout=30s patch "mcp/${WORKER_MCP_NAME}" --type=json -p "${patch}"
}

wait_for_batch_render() {
    local name render_condition
    echo "Waiting for mcp/${WORKER_MCP_NAME} to render all new MachineConfigs."
    for name in "${batch_machineconfigs[@]}"; do
        render_condition="jsonpath={.spec.configuration.source[?(@.name==\"${name}\")].name}=${name}"
        wait_for_worker_pool "${render_condition}" "${WORKER_MCP_ROLLOUT_START_TIMEOUT}"
    done
}

check_batch_sources() {
    local section=$1 name pool_state
    pool_state=$(cat)
    for name in "${batch_machineconfigs[@]}"; do
        if ! jq -e --arg section "${section}" --arg name "${name}" '
            any(.[$section].configuration.source[]; .name == $name)
        ' <<< "${pool_state}" > /dev/null; then
            fail "mcp/${WORKER_MCP_NAME} ${section} is missing MachineConfig/${name}"
        fi
    done
}

wait_for_batch_rollout() {
    local target_config=$1 condition pool_state
    echo "Waiting for mcp/${WORKER_MCP_NAME} to finish rolling out ${target_config}."
    wait_for_worker_pool "jsonpath={.status.configuration.name}=${target_config}" "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    # oc condition waits already require observedGeneration >= metadata.generation.
    for condition in Updated=True Updating=False Degraded=False RenderDegraded=False; do
        wait_for_worker_pool "condition=${condition}" "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    done

    pool_state=$(get_worker_pool)
    if ! pool_is_settled <<< "${pool_state}"; then
        fail "mcp/${WORKER_MCP_NAME} has not converged"
    fi
    if [[ $(jq -r '.spec.configuration.name' <<< "${pool_state}") != "${target_config}" ]]; then
        fail "mcp/${WORKER_MCP_NAME} rendered target changed"
    fi
    check_batch_sources spec <<< "${pool_state}"
    check_batch_sources status <<< "${pool_state}"
}

apply_batch() {
    local manifest_index prepared_file pool_state target_config
    if (( ${#batch_machineconfigs[@]} > 0 )); then
        pause_worker_pool
    fi
    for manifest_index in "${!prepared_manifests[@]}"; do
        prepared_file=${prepared_manifests[$manifest_index]}
        echo "Applying manifest: ${manifest_files[$manifest_index]}"
        if [[ $(jq -r '.kind' "${prepared_file}") == MachineConfig ]]; then
            oc create --save-config -f "${prepared_file}"
        else
            oc apply -f "${prepared_file}"
        fi
    done
    if (( ${#batch_machineconfigs[@]} == 0 )); then
        return 0
    fi

    wait_for_batch_render
    pool_state=$(get_worker_pool)
    check_batch_sources spec <<< "${pool_state}"
    target_config=$(jq -er '.spec.configuration.name | select(length > 0)' <<< "${pool_state}")
    if ! resume_worker_pool; then
        fail "Could not resume mcp/${WORKER_MCP_NAME}"
    fi
    wait_for_batch_rollout "${target_config}"
}

apply_sequentially() {
    local manifest_file hook_file
    for manifest_file in "${manifest_files[@]}"; do
        echo "Applying manifest: ${manifest_file}"
        render_manifest "${manifest_file}" | oc apply -f -
        if [[ ${WAIT_FOR_WORKER_MCP} == true ]]; then
            # Non-MachineConfig resources may never start a rollout.
            wait_for_worker_pool condition=Updating=True "${WORKER_MCP_ROLLOUT_START_TIMEOUT}" > /dev/null 2>&1 || true
            wait_for_worker_pool condition=Updated=True "${WORKER_MCP_ROLLOUT_TIMEOUT}"
        fi
        hook_file=${manifest_file%.*}.sh
        if [[ -f ${hook_file} ]]; then
            echo "Running post-apply hook: ${hook_file}"
            bash "${hook_file}"
        fi
    done
}

delete_manifests() {
    local manifest_index manifest_file
    for ((manifest_index = ${#manifest_files[@]} - 1; manifest_index >= 0; manifest_index--)); do
        manifest_file=${manifest_files[$manifest_index]}
        echo "Deleting manifest: ${manifest_file}"
        render_manifest "${manifest_file}" | oc delete --ignore-not-found=true -f -
    done
}

manifest_files=()
prepared_manifests=()
batch_machineconfigs=()
batch_ready=false
batch_skip_reason=''
while IFS= read -r manifest_file; do
    manifest_files+=("${manifest_file}")
done < <(
    find "${MANIFEST_DIR}" -maxdepth 1 -type f \( -name '*.yaml' -o -name '*.yml' \) |
        LC_ALL=C sort
)
if (( ${#manifest_files[@]} == 0 )); then
    fail "No YAML manifests found in '${MANIFEST_DIR}'"
fi

work_dir=$(mktemp -d)
pause_owner="$$-${RANDOM}-${RANDOM}"
# A nonempty version means a pause was attempted and EXIT cleanup must resolve it.
# Clear it only after our pause has been removed or its delayed request cannot apply.
pending_pause_version=''
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ ${ACTION} == delete ]]; then
    delete_manifests
else
    for manifest_file in "${manifest_files[@]}"; do
        check_required_vars "${manifest_file}"
    done
    prepare_batch
    if [[ ${batch_ready} == true ]]; then
        apply_batch
    else
        echo "Using per-file apply: ${batch_skip_reason}."
        apply_sequentially
    fi
fi
