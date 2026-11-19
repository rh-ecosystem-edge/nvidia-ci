#!/usr/bin/env bash
set -eo pipefail
cd "$(dirname "$0")/.."

ACTION=${1:-apply}
MANIFEST_DIR=${MANIFEST_DIR:-manifests/nno}
WAIT_FOR_WORKER_MCP=${WAIT_FOR_WORKER_MCP:-false}
BATCH_WORKER_MCP=${BATCH_WORKER_MCP:-false}
WORKER_MCP_NAME=${WORKER_MCP_NAME:-worker}
WORKER_MCP_ROLLOUT_START_TIMEOUT=${WORKER_MCP_ROLLOUT_START_TIMEOUT:-2m}
WORKER_MCP_ROLLOUT_TIMEOUT=${WORKER_MCP_ROLLOUT_TIMEOUT:-40m}

fail() { echo "$*" >&2; exit 1; }
oc() { command oc --kubeconfig="${KUBECONFIG}" "$@"; }
pool() { oc get "mcp/${WORKER_MCP_NAME}" -o json; }
wait_pool() { oc wait "mcp/${WORKER_MCP_NAME}" --for="$1" --timeout="$2"; }

[[ $# -le 1 && ( ${ACTION} == apply || ${ACTION} == delete ) ]] || fail "Usage: $0 [apply|delete]"
[[ -d ${MANIFEST_DIR} ]] || fail "Manifest directory '${MANIFEST_DIR}' does not exist"
[[ -n ${KUBECONFIG:-} && ${KUBECONFIG} != *:* ]] || fail "KUBECONFIG must name a single file"
export KUBECONFIG
for tool in oc envsubst; do
    type -P "${tool}" > /dev/null || fail "Required command '${tool}' not found"
done

render() {
    if [[ -n ${MANIFEST_VARS:-} ]]; then
        envsubst -- "${MANIFEST_VARS}" < "$1"
    else
        envsubst < "$1"
    fi
}

check_required_vars() {
    local file=$1 name
    [[ -f ${file%.*}.env-required ]] || return 0
    while read -r name || [[ -n ${name} ]]; do
        [[ -z ${name} || ${name} == \#* ]] && continue
        [[ ${name} =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]] || fail "Invalid variable name in ${file%.*}.env-required"
        [[ -n ${!name:-} ]] || fail "Missing required variable ${name} for ${file}"
    done < "${file%.*}.env-required"
}

is_settled() {
    jq -e '
        (.spec.paused != true) and
        (.status.observedGeneration >= .metadata.generation) and
        (.spec.configuration.name != null and .spec.configuration.name != "") and
        (.spec.configuration.name == .status.configuration.name) and
        any(.status.conditions[]; .type == "Updated" and .status == "True") and
        all(.status.conditions[];
            (.type != "Updating" and .type != "Degraded" and .type != "RenderDegraded") or .status != "True")
    ' > /dev/null
}

pause_patch() {
    jq -c --arg owner "${pause_owner}" --argjson paused "$1" '[
        {op: "test", path: "/metadata/resourceVersion", value: .metadata.resourceVersion},
        {op: "add", path: "/metadata/annotations", value: ((.metadata.annotations // {}) + {"nvidia-ci/pause-owner": $owner})},
        {op: "add", path: "/spec/paused", value: $paused}
    ]'
}

resume_pool() {
    local state patch deadline=$((SECONDS + 60))
    while (( SECONDS < deadline )); do
        state=$(oc --request-timeout=10s get "mcp/${WORKER_MCP_NAME}" -o json) || continue
        if [[ $(jq -r '.metadata.annotations["nvidia-ci/pause-owner"] // ""' <<< "${state}") == "${pause_owner}" ]]; then
            patch=$(jq -cn --arg owner "${pause_owner}" '[
                {op: "test", path: "/metadata/annotations/nvidia-ci~1pause-owner", value: $owner},
                {op: "add", path: "/spec/paused", value: false},
                {op: "remove", path: "/metadata/annotations/nvidia-ci~1pause-owner"}
            ]')
            if oc --request-timeout=10s patch "mcp/${WORKER_MCP_NAME}" --type=json -p "${patch}"; then
                resume_needed=false
                return 0
            fi
        elif [[ $(jq -r '.metadata.resourceVersion' <<< "${state}") == "${pause_version}" ]]; then
            # Change the version to fence a delayed pause whose client reported failure.
            patch=$(pause_patch false <<< "${state}")
            oc --request-timeout=10s patch "mcp/${WORKER_MCP_NAME}" --type=json -p "${patch}" || true
        else
            [[ ${pause_confirmed} == false || $(jq -r '.spec.paused // false' <<< "${state}") == false ]] || return 1
            resume_needed=false
            return 0
        fi
    done
    return 1
}

cleanup() {
    local status=$?
    trap - EXIT
    trap '' HUP INT TERM
    if [[ ${resume_needed} == true ]] && ! resume_pool; then
        echo "Could not resume mcp/${WORKER_MCP_NAME}; check the pool before retrying." >&2
        status=1
    fi
    if [[ ${status} != 0 && ${BATCH_WORKER_MCP} == true && ${WAIT_FOR_WORKER_MCP} == true ]]; then
        oc --request-timeout=10s get "mcp/${WORKER_MCP_NAME}" -o json >&2 || true
    fi
    rm -rf "${workdir}"
    exit "${status}"
}

prepare_batch() {
    local file json name existing
    batch=false
    [[ ${BATCH_WORKER_MCP} == true && ${WAIT_FOR_WORKER_MCP} == true ]] || return 0
    for file in "${files[@]}"; do
        [[ ! -f ${file%.*}.sh ]] || return 0
    done
    command -v jq > /dev/null || fail "Required command 'jq' not found for batching"
    for file in "${files[@]}"; do
        json="${workdir}/${#prepared[@]}.json"
        render "${file}" > "${workdir}/rendered.yaml"
        oc create --dry-run=client --validate=false -f "${workdir}/rendered.yaml" -o json > "${json}"
        case $(jq -r '.apiVersion + "/" + .kind' "${json}") in
            machineconfiguration.openshift.io/v1/MachineConfig)
                name=$(jq -r '.metadata.name // ""' "${json}")
                [[ -n ${name} && " ${machineconfigs[*]} " != *" ${name} "* ]] || return 0
                existing=$(oc get "machineconfig/${name}" --ignore-not-found -o name)
                [[ -z ${existing} ]] || return 0
                machineconfigs+=("${name}")
                operations+=(create)
                ;;
            v1/ConfigMap|v1/ServiceAccount|rbac.authorization.k8s.io/v1/RoleBinding)
                operations+=(apply)
                ;;
            *) return 0 ;;
        esac
        prepared+=("${json}")
    done
    batch=true
}

pause_pool() {
    local state file selector name patch
    state=$(pool)
    [[ $(jq -r '.spec.paused // false' <<< "${state}") == false ]] || fail "mcp/${WORKER_MCP_NAME} is already paused"
    wait_pool condition=Updated=True "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    state=$(pool)
    is_settled <<< "${state}" || fail "mcp/${WORKER_MCP_NAME} is not settled; refusing to batch changes"
    selector=$(jq -c '.spec.machineConfigSelector' <<< "${state}")
    for file in "${prepared[@]}"; do
        [[ $(jq -r '.kind' "${file}") == MachineConfig ]] || continue
        jq -e --argjson selector "${selector}" '
            .metadata.labels as $labels |
            ($selector.matchLabels | length > 0) and
            (($selector.matchExpressions // []) | length == 0) and
            all($selector.matchLabels | to_entries[]; $labels[.key] == .value)
        ' "${file}" > /dev/null || fail "MachineConfig in ${file} is not selected by a supported matchLabels selector on mcp/${WORKER_MCP_NAME}"
    done
    for name in "${machineconfigs[@]}"; do
        jq -e --arg name "${name}" '
            all((.spec.configuration.source + .status.configuration.source)[]; .name != $name)
        ' <<< "${state}" > /dev/null || fail "MachineConfig/${name} is still referenced by mcp/${WORKER_MCP_NAME}"
    done
    pause_version=$(jq -r '.metadata.resourceVersion' <<< "${state}")
    patch=$(pause_patch true <<< "${state}")
    # Cleanup is armed before the request: a failed client may have committed the pause.
    resume_needed=true
    oc --request-timeout=30s patch "mcp/${WORKER_MCP_NAME}" --type=json -p "${patch}"
    pause_confirmed=true
}

wait_for_render() {
    local name
    for name in "${machineconfigs[@]}"; do
        wait_pool "jsonpath={.spec.configuration.source[?(@.name==\"${name}\")].name}=${name}" "${WORKER_MCP_ROLLOUT_START_TIMEOUT}"
    done
    pool > "${workdir}/target.json"
    target=$(jq -er '.spec.configuration.name | select(length > 0)' "${workdir}/target.json")
    check_sources "${workdir}/target.json" spec
}

check_sources() {
    local file=$1 section=$2 name
    for name in "${machineconfigs[@]}"; do
        jq -e --arg section "${section}" --arg name "${name}" \
            'any(.[$section].configuration.source[]; .name == $name)' "${file}" > /dev/null ||
            fail "mcp/${WORKER_MCP_NAME} ${section} is missing MachineConfig/${name}"
    done
}

wait_for_batch() {
    local generation condition
    generation=$(pool | jq -r '.metadata.generation')
    wait_pool "jsonpath={.status.configuration.name}=${target}" "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    wait_pool "jsonpath={.status.observedGeneration}=${generation}" "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    for condition in Updated=True Updating=False Degraded=False RenderDegraded=False; do
        wait_pool "condition=${condition}" "${WORKER_MCP_ROLLOUT_TIMEOUT}"
    done
    pool > "${workdir}/final.json"
    is_settled < "${workdir}/final.json" || fail "mcp/${WORKER_MCP_NAME} has not converged"
    [[ $(jq -r '.spec.configuration.name' "${workdir}/final.json") == "${target}" ]] || fail "mcp/${WORKER_MCP_NAME} rendered target changed"
    check_sources "${workdir}/final.json" spec
    check_sources "${workdir}/final.json" status
}

apply_batch() {
    local i
    [[ ${#machineconfigs[@]} == 0 ]] || pause_pool
    for ((i = 0; i < ${#prepared[@]}; i++)); do
        if [[ ${operations[$i]} == create ]]; then
            oc create --save-config -f "${prepared[$i]}"
        else
            oc apply -f "${prepared[$i]}"
        fi
    done
    if [[ ${#machineconfigs[@]} != 0 ]]; then
        wait_for_render
        resume_pool || fail "Could not resume mcp/${WORKER_MCP_NAME}"
        wait_for_batch
    fi
}

apply_sequentially() {
    local file hook
    for file in "${files[@]}"; do
        echo "Applying manifest: ${file}"
        render "${file}" | oc apply -f -
        if [[ ${WAIT_FOR_WORKER_MCP} == true ]]; then
            wait_pool condition=Updating=True "${WORKER_MCP_ROLLOUT_START_TIMEOUT}" > /dev/null 2>&1 || true
            wait_pool condition=Updated=True "${WORKER_MCP_ROLLOUT_TIMEOUT}"
        fi
        hook=${file%.*}.sh
        if [[ -f ${hook} ]]; then
            bash "${hook}"
        fi
    done
}

delete_manifests() {
    local i
    for ((i = ${#files[@]} - 1; i >= 0; i--)); do
        render "${files[$i]}" | oc delete --ignore-not-found=true -f -
    done
}

files=() prepared=() operations=() machineconfigs=()
while IFS= read -r file; do
    files+=("${file}")
done < <(find "${MANIFEST_DIR}" -maxdepth 1 -type f \( -name '*.yaml' -o -name '*.yml' \) | LC_ALL=C sort)
[[ ${#files[@]} != 0 ]] || fail "No YAML manifests found in '${MANIFEST_DIR}'"
workdir=$(mktemp -d)
resume_needed=false pause_confirmed=false pause_owner="$$-${RANDOM}-${RANDOM}"
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ ${ACTION} == delete ]]; then
    delete_manifests
else
    for file in "${files[@]}"; do
        check_required_vars "${file}"
    done
    prepare_batch
    if [[ ${batch} == true ]]; then
        apply_batch
    else
        echo "Using per-file apply and rollout waits."
        apply_sequentially
    fi
fi
