#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/.."

scratch=$(mktemp -d)
trap 'rm -rf "${scratch}"' EXIT
case_name=setup
fail_test() {
    echo "FAIL ${case_name}: $*" >&2
    for diagnostic in output events pool.json; do
        if [[ -f ${scratch}/${diagnostic} ]]; then
            echo "${diagnostic}:" >&2
            cat "${scratch}/${diagnostic}" >&2
        fi
    done
    exit 1
}
trap 'fail_test "Unexpected failure on line ${LINENO}: ${BASH_COMMAND}"' ERR

mkdir -p "${scratch}/bin" "${scratch}/manifests"
export FAKE_OC_DIR=${scratch} MANIFEST_DIR=${scratch}/manifests
export PATH="${scratch}/bin:${PATH}" KUBECONFIG=/selected/config
export WAIT_FOR_WORKER_MCP=true BATCH_WORKER_MCP=true WORKER_MCP_NAME=worker MANIFEST_VARS=''

# Record batch phases and model the pause patch and rendered source names.
# Fixtures use JSON, which is also valid YAML.
cat > "${scratch}/bin/oc" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == --kubeconfig=/selected/config ]]
shift
if [[ $1 == --request-timeout=* ]]; then
    shift
fi
oc_command=$1 resource=${2:-}
shift
manifest_file='' patch='' condition='' dry_run=false
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
        --for=*) condition=${1#--for=} ;;
        --dry-run=client) dry_run=true ;;
    esac
    shift
done
pool_file=${FAKE_OC_DIR}/pool.json
case ${oc_command} in
    get)
        if [[ ${resource} == mcp/* ]]; then
            cat "${pool_file}"
        elif [[ ${resource} == "machineconfig/${FAKE_EXISTING_MC:-}" ]]; then
            echo "${resource}"
        fi
        ;;
    patch)
        operation=$(jq -r '
            if .[-1].value == true then "pause"
            elif .[-1].op == "remove" then "resume" else "fence" end
        ' <<< "${patch}")
        echo "${operation}" >> "${FAKE_OC_DIR}/events"
        if [[ ${operation} == pause && ${FAKE_PAUSE_ERROR:-} == rejected ]]; then
            echo 'Simulated pause rejection' >&2
            exit 1
        fi
        jq --argjson patch "${patch}" '
            reduce $patch[] as $op (.;
                ($op.path | split("/")[1:] | map(gsub("~1"; "/"))) as $path |
                if $op.op == "test" then
                    if getpath($path) == $op.value then . else error("patch precondition failed") end
                elif $op.op == "remove" then delpaths([$path])
                else setpath($path; $op.value) end) |
            .metadata.resourceVersion |= (tonumber + 1 | tostring)
        ' "${pool_file}" > "${pool_file}.tmp"
        mv "${pool_file}.tmp" "${pool_file}"
        if [[ ${operation} == pause && ${FAKE_PAUSE_ERROR:-} == committed ]]; then
            echo 'Simulated client error after pause committed' >&2
            exit 1
        fi
        ;;
    wait)
        case ${condition} in
            'jsonpath={.spec.configuration.source['*)
                name=${condition##*=}
                echo "render:${name}" >> "${FAKE_OC_DIR}/events"
                if ! jq -e --arg name "${name}" '
                    any(.spec.configuration.source[]; .name == $name)
                ' "${pool_file}" > /dev/null; then
                    echo "Simulated render timeout for ${name}" >&2
                    exit 1
                fi
                ;;
            'jsonpath={.status.configuration.name}='*)
                echo rollout >> "${FAKE_OC_DIR}/events"
                jq '.status.configuration = .spec.configuration' "${pool_file}" > "${pool_file}.tmp"
                mv "${pool_file}.tmp" "${pool_file}"
                ;;
            condition=Updating=True) exit 1 ;;
        esac
        ;;
    apply|create)
        input=$(cat "${manifest_file}")
        if [[ ${dry_run} == true ]]; then
            echo "${input}"
            exit 0
        fi
        name=$(jq -r '.metadata.name' <<< "${input}")
        echo "${oc_command}:${name}" >> "${FAKE_OC_DIR}/events"
        if [[ ${oc_command} == create ]]; then
            if [[ ${name} == "${FAKE_FAILED_CREATE:-}" ]]; then
                echo "Simulated create failure for ${name}" >&2
                exit 1
            fi
            if [[ ${name} != "${FAKE_MISSING_SOURCE:-}" ]]; then
                jq --arg name "${name}" '.spec.configuration.name = "rendered-new" |
                    .spec.configuration.source += [{name: $name}]' "${pool_file}" > "${pool_file}.tmp"
                mv "${pool_file}.tmp" "${pool_file}"
            fi
        fi
        ;;
    *)
        echo "Unexpected oc command: ${oc_command}" >&2
        exit 1
        ;;
esac
MOCK
chmod +x "${scratch}/bin/oc"

# Each case names the behavior, exit status, expected error, and phase order.
run_case() {
    case_name=$1
    local expected_status=$2 expected_error=$3 actual_status=0 expected_events actual_events
    shift 3
    rm -f "${MANIFEST_DIR}"/*
    : > "${scratch}/events"
    for name in first second; do
        jq -n --arg name "${name}" '{
            apiVersion: "machineconfiguration.openshift.io/v1", kind: "MachineConfig",
            metadata: {name: $name, labels: {role: "worker"}}
        }' > "${MANIFEST_DIR}/${name}.yaml"
    done
    jq -n --argjson paused "${POOL_ALREADY_PAUSED:-false}" '{
        metadata: {resourceVersion: "1", generation: 1},
        spec: {paused: $paused, machineConfigSelector: {matchLabels: {role: "worker"}},
            configuration: {name: "rendered-old", source: []}},
        status: {observedGeneration: 1, configuration: {name: "rendered-old", source: []},
            conditions: [{type: "Updated", status: "True"}, {type: "Updating", status: "False"},
                {type: "Degraded", status: "False"}, {type: "RenderDegraded", status: "False"}]}
    }' > "${scratch}/pool.json"
    if [[ ${WITH_HOOK:-false} == true ]]; then
        echo 'echo hook:first >> "${FAKE_OC_DIR}/events"' > "${MANIFEST_DIR}/first.sh"
    fi

    bash scripts/apply-manifests.sh apply > "${scratch}/output" 2>&1 || actual_status=$?
    if (( actual_status != expected_status )); then
        fail_test "Expected exit ${expected_status}, got ${actual_status}"
    fi
    if ! grep -Fq -- "${expected_error}" "${scratch}/output"; then
        fail_test "Expected output to include: ${expected_error}"
    fi
    expected_events=$(printf '%s\n' "$@")
    actual_events=$(cat "${scratch}/events")
    if [[ ${actual_events} != "${expected_events}" ]]; then
        fail_test "Expected phases in this order:
${expected_events}"
    fi
    if ! jq -e --argjson paused "${POOL_ALREADY_PAUSED:-false}" '
        .spec.paused == $paused and .metadata.annotations["nvidia-ci/pause-owner"] == null
    ' "${scratch}/pool.json" > /dev/null; then
        fail_test "Expected the original pause state and no leftover pause owner"
    fi
    echo "PASS ${case_name}"
}

run_case 'batch new MachineConfigs in one rollout' 0 '' \
    pause create:first create:second render:first render:second resume rollout
FAKE_EXISTING_MC=first run_case 'existing MachineConfig uses sequential apply' 0 'already exists' \
    apply:first apply:second
WITH_HOOK=true run_case 'hook runs between sequential applies' 0 'has a post-apply hook' \
    apply:first hook:first apply:second
POOL_ALREADY_PAUSED=true run_case 'leave an existing pause untouched' 1 'is already paused'
FAKE_PAUSE_ERROR=committed run_case 'resume after a lost pause response' 1 'Simulated client error' \
    pause resume
FAKE_PAUSE_ERROR=rejected run_case 'fence a rejected pause request' 1 'Simulated pause rejection' \
    pause fence resume
FAKE_FAILED_CREATE=second run_case 'resume after a create failure' 1 'Simulated create failure' \
    pause create:first create:second resume
FAKE_MISSING_SOURCE=second run_case 'resume after a render timeout' 1 'Simulated render timeout' \
    pause create:first create:second render:first render:second resume
