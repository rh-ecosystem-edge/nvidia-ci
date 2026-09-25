#!/usr/bin/env bash
#
# Applies (or deletes) a directory of YAML manifests to/from an existing OpenShift cluster.
#
# Files are processed in lexical filename order, so prefix files with numbers
# (e.g. 00-namespace.yaml, 01-serviceaccount.yaml) to control ordering when one
# manifest depends on another (e.g. a namespace must exist before objects in it).
#
# Manifests may contain ${VAR} / $VAR placeholders, substituted from the current
# environment via envsubst before being applied. By default all variables present
# in the environment are eligible for substitution; set MANIFEST_VARS to a
# space-separated list of '$VAR' tokens (envsubst's own filter syntax) to restrict
# substitution to only those variables, e.g. to avoid clobbering an unrelated '$'
# character embedded in a manifest (such as inside a shell script in a ConfigMap).
#
# When WAIT_FOR_WORKER_MCP=true and the action is "apply", the script waits for the
# named MachineConfigPool (WORKER_MCP_NAME, default "worker") to finish rolling out
# after each manifest is applied, before moving on to the next one. Applying a
# MachineConfig makes the Machine Config Operator cordon/drain/reboot/uncordon every
# node in the pool, so this is how later manifests can rely on that having finished.
# Manifests that don't touch worker MachineConfigs are unaffected: the pool is already
# "Updated", so the wait returns quickly.
#
# After a manifest is applied (and any MCP wait completes), if a sibling script with
# the same base name and a .sh extension exists next to it (e.g. 20-foo.yaml ->
# 20-foo.sh), it is executed. This is how manifest-specific follow-up commands (like
# granting an SCC to a freshly created ServiceAccount) are attached without hardcoding
# them into this generic script.
#
# Before a manifest is rendered, if a sibling file with the same base name and a
# .env-required extension exists (e.g. 10-foo.yaml -> 10-foo.env-required, one
# variable name per line, blank lines and #-comments ignored), the script verifies
# every listed variable is set and non-empty, failing fast with a clear error
# instead of letting envsubst silently substitute an empty string for a variable
# nobody remembered to export.
#
# Usage:
#   MANIFEST_DIR=manifests KUBECONFIG=/path/to/kubeconfig scripts/apply-manifests.sh [apply|delete]

set -e

. "$(dirname "$0")"/common.sh

ACTION="${1:-apply}"
MANIFEST_DIR="${MANIFEST_DIR:-manifests}"
WAIT_FOR_WORKER_MCP="${WAIT_FOR_WORKER_MCP:-false}"
WORKER_MCP_NAME="${WORKER_MCP_NAME:-worker}"
WORKER_MCP_ROLLOUT_START_TIMEOUT="${WORKER_MCP_ROLLOUT_START_TIMEOUT:-2m}"
WORKER_MCP_ROLLOUT_TIMEOUT="${WORKER_MCP_ROLLOUT_TIMEOUT:-40m}"

if [[ "${ACTION}" != "apply" && "${ACTION}" != "delete" ]]; then
    echo "Usage: $0 [apply|delete]"
    exit 1
fi

if [[ ! -d "${MANIFEST_DIR}" ]]; then
    echo "Manifest directory '${MANIFEST_DIR}' does not exist"
    exit 1
fi

if ! command -v oc &> /dev/null; then
    echo "'oc' binary not found on PATH"
    exit 1
fi

if ! command -v envsubst &> /dev/null; then
    echo "'envsubst' binary not found on PATH (part of the 'gettext' package)"
    exit 1
fi

render() {
    if [[ -n "${MANIFEST_VARS:-}" ]]; then
        envsubst "${MANIFEST_VARS}" < "$1"
    else
        envsubst < "$1"
    fi
}

# check_required_vars verifies that every variable name listed in
# "<manifest-file-without-extension>.env-required" (if that file exists) is set and
# non-empty in the current environment.
check_required_vars() {
    local manifest_file="$1"
    local vars_file="${manifest_file%.*}.env-required"

    if [[ ! -f "${vars_file}" ]]; then
        return 0
    fi

    local missing=()
    local var_name
    while IFS= read -r var_name; do
        [[ -z "${var_name}" || "${var_name}" == \#* ]] && continue
        if [[ -z "${!var_name:-}" ]]; then
            missing+=("${var_name}")
        fi
    done < "${vars_file}"

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "Missing required environment variable(s) for ${manifest_file}: ${missing[*]}"
        return 1
    fi
}

# wait_for_worker_mcp waits for the named MachineConfigPool to finish rolling out.
# Nodes in the pool reboot as part of this rollout, so this also waits out the reboot.
wait_for_worker_mcp() {
    echo "Waiting up to ${WORKER_MCP_ROLLOUT_START_TIMEOUT} for MachineConfigPool/${WORKER_MCP_NAME}" \
        "to start updating (returns immediately if this manifest didn't change its MachineConfigs)..."
    oc wait "mcp/${WORKER_MCP_NAME}" --for="condition=Updating=True" \
        --timeout="${WORKER_MCP_ROLLOUT_START_TIMEOUT}" &> /dev/null || true

    echo "Waiting up to ${WORKER_MCP_ROLLOUT_TIMEOUT} for MachineConfigPool/${WORKER_MCP_NAME}" \
        "to finish updating (nodes reboot during this step)..."
    if ! oc wait "mcp/${WORKER_MCP_NAME}" --for="condition=Updated=True" --timeout="${WORKER_MCP_ROLLOUT_TIMEOUT}"; then
        echo "MachineConfigPool/${WORKER_MCP_NAME} did not become Updated in time. Current status:"
        oc get "mcp/${WORKER_MCP_NAME}"
        return 1
    fi
}

# run_post_apply_hook executes "<manifest-file-without-extension>.sh" if it exists next to
# the manifest that was just applied.
run_post_apply_hook() {
    local manifest_file="$1"
    local hook_file="${manifest_file%.*}.sh"

    if [[ -f "${hook_file}" ]]; then
        echo "Running post-apply hook: ${hook_file}"
        bash "${hook_file}"
    fi
}

manifest_files=()
while IFS= read -r file; do
    manifest_files+=("${file}")
done < <(find "${MANIFEST_DIR}" -maxdepth 1 -type f \( -name '*.yaml' -o -name '*.yml' \) | sort)

if [[ ${#manifest_files[@]} -eq 0 ]]; then
    echo "No YAML manifests found in '${MANIFEST_DIR}'"
    exit 1
fi

if [[ "${ACTION}" == "delete" ]]; then
    # Reverse order on delete so dependents are removed before what they depend on.
    for ((i = ${#manifest_files[@]} - 1; i >= 0; i--)); do
        file="${manifest_files[$i]}"
        echo "Deleting manifest: ${file}"
        render "${file}" | oc delete --ignore-not-found=true -f -
    done
else
    for file in "${manifest_files[@]}"; do
        check_required_vars "${file}"

        echo "Applying manifest: ${file}"
        render "${file}" | oc apply -f -

        if [[ "${WAIT_FOR_WORKER_MCP}" == "true" ]]; then
            wait_for_worker_mcp
        fi

        run_post_apply_hook "${file}"
    done
fi
