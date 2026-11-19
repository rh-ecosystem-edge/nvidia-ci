#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

scratch=$(mktemp -d)
trap 'rm -rf "${scratch}"' EXIT
mkdir -p "${scratch}/bin" "${scratch}/manifests"
export FAKE_DIR=${scratch}
export PATH="${scratch}/bin:${PATH}" KUBECONFIG=/selected/config
export MANIFEST_DIR=${scratch}/manifests WAIT_FOR_WORKER_MCP=true BATCH_WORKER_MCP=true

cat > "${scratch}/bin/oc" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == --kubeconfig=/selected/config ]]
shift
[[ $1 != --request-timeout=* ]] || shift
command=$1
shift
file= patch= condition=
args=" $* "
while [[ $# != 0 ]]; do
    case $1 in
        -f) file=$2; shift ;;
        -p) patch=$2; shift ;;
        --for=*) condition=${1#--for=} ;;
    esac
    shift
done
state=${FAKE_DIR}/pool.json
case ${command} in
    get)
        if [[ ${args} == *' mcp/'* ]]; then cat "${state}"
        elif [[ ${MODE} == existing ]]; then echo machineconfig/first
        fi
        ;;
    patch)
        operation=$(jq -r 'if .[-1].value == true then "pause" elif .[-1].op == "remove" then "resume" else "fence" end' <<< "${patch}")
        echo "${operation}" >> "${FAKE_DIR}/events"
        if [[ ${operation} == pause && ${MODE} == rejected-pause ]]; then exit 1; fi
        jq --argjson patch "${patch}" '
            reduce $patch[] as $op (.;
                ($op.path | split("/")[1:] | map(gsub("~1"; "/"))) as $path |
                if $op.op == "test" then
                    if getpath($path) == $op.value then . else error("patch test failed") end
                elif $op.op == "remove" then delpaths([$path])
                else setpath($path; $op.value) end) |
            .metadata.resourceVersion |= (tonumber + 1 | tostring) |
            .metadata.generation += 1
        ' "${state}" > "${state}.tmp"
        mv "${state}.tmp" "${state}"
        if [[ ${operation} == pause && ${MODE} == committed-pause ]]; then exit 1; fi
        ;;
    wait)
        echo "wait:${condition}" >> "${FAKE_DIR}/events"
        jq 'if .spec.paused then . else
            .status.configuration = .spec.configuration | .status.observedGeneration = .metadata.generation
            end' "${state}" > "${state}.tmp"
        mv "${state}.tmp" "${state}"
        ;;
    apply|create|delete)
        input=$(cat "${file}")
        if [[ ${args} == *' --dry-run=client '* ]]; then echo "${input}"; exit; fi
        name=$(jq -r '.metadata.name' <<< "${input}")
        echo "${command}:${name}" >> "${FAKE_DIR}/events"
        if [[ ${command} == create ]]; then
            [[ ${args} == *' --save-config '* ]]
            if [[ ${MODE} == failed-create && ${name} == second ]]; then exit 1; fi
            jq --arg name "${name}" '.spec.configuration.name = "rendered-new" |
                .spec.configuration.source += [{name: $name}]' "${state}" > "${state}.tmp"
            mv "${state}.tmp" "${state}"
        fi
        ;;
    *) exit 1 ;;
esac
MOCK
chmod +x "${scratch}/bin/oc"

for name in first second; do
    jq -n --arg name "${name}" '{apiVersion:"machineconfiguration.openshift.io/v1",kind:"MachineConfig",
        metadata:{name:$name,labels:{role:"worker"}}}' > "${MANIFEST_DIR}/${name}.yaml"
done

for MODE in batch existing hook unsupported paused committed-pause rejected-pause failed-create delete; do
    export MODE
    : > "${scratch}/events"
    rm -f "${MANIFEST_DIR}/first.sh" "${MANIFEST_DIR}/00-other.yaml"
    jq -n --arg mode "${MODE}" '{metadata:{resourceVersion:"1",generation:1},
        spec:{paused:($mode == "paused"),machineConfigSelector:{matchLabels:{role:"worker"}},
            configuration:{name:"rendered-old",source:[]}},
        status:{observedGeneration:1,configuration:{name:"rendered-old",source:[]},conditions:[
            {type:"Updated",status:"True"},{type:"Updating",status:"False"},
            {type:"Degraded",status:"False"},{type:"RenderDegraded",status:"False"}]}}' > "${scratch}/pool.json"
    if [[ ${MODE} == hook ]]; then
        cat > "${MANIFEST_DIR}/first.sh" <<'HOOK'
test "$KUBECONFIG" = /selected/config
HOOK
    elif [[ ${MODE} == unsupported ]]; then
        echo '{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"KubeletConfig","metadata":{"name":"other"}}' > "${MANIFEST_DIR}/00-other.yaml"
    fi
    action=apply status=0
    [[ ${MODE} != delete ]] || action=delete
    bash scripts/apply-manifests.sh "${action}" > "${scratch}/output" 2>&1 || status=$?
    case ${MODE} in
        paused|committed-pause|rejected-pause|failed-create) [[ ${status} != 0 ]] ;;
        *) [[ ${status} == 0 ]] || { cat "${scratch}/output"; exit 1; } ;;
    esac
    jq -e --arg mode "${MODE}" '.spec.paused == ($mode == "paused") and
        .metadata.annotations["nvidia-ci/pause-owner"] == null' "${scratch}/pool.json" > /dev/null
    events=$(cat "${scratch}/events")
    case ${MODE} in
        batch)
            [[ ${events} == *$'pause\ncreate:first\ncreate:second'* ]]
            [[ ${events} == *'spec.configuration.source[?(@.name=="second")]'*$'\nresume\n'* ]]
            [[ ${events} != *'condition=Updating=True'* ]]
            ;;
        existing|hook|unsupported) [[ ${events} != *pause* && ${events} == *apply:first* ]] ;;
        paused) [[ -z ${events} ]] ;;
        committed-pause) [[ ${events} == $'wait:condition=Updated=True\npause\nresume' ]] ;;
        rejected-pause) [[ ${events} == $'wait:condition=Updated=True\npause\nfence\nresume' ]] ;;
        failed-create) [[ ${events} == *$'create:first\ncreate:second\nresume' ]] ;;
        delete) [[ ${events} == $'delete:second\ndelete:first' ]] ;;
    esac
    echo "PASS ${MODE}"
done
