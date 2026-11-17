#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if command -v apply-manifests > /dev/null; then
    binary=apply-manifests
else
    go build -o .bin/apply-manifests ./cmd/apply-manifests
    binary=.bin/apply-manifests
fi

args=("${binary}")
if [[ $# -gt 0 && $1 != -* ]]; then
    if [[ -n $1 ]]; then
        args+=(--action="$1")
    fi
    shift
fi
for setting in \
    MANIFEST_DIR:manifest-dir \
    KUBECONFIG:kubeconfig \
    MANIFEST_VARS:manifest-vars \
    WAIT_FOR_WORKER_MCP:wait-for-worker-mcp \
    BATCH_WORKER_MCP:batch-worker-mcp \
    WORKER_MCP_NAME:worker-mcp-name \
    WORKER_MCP_ROLLOUT_START_TIMEOUT:worker-mcp-rollout-start-timeout \
    WORKER_MCP_ROLLOUT_TIMEOUT:worker-mcp-rollout-timeout; do
    name=${setting%%:*}
    value=${!name:-}
    if [[ -n ${value} ]]; then
        args+=("--${setting#*:}=${value}")
    fi
done

exec "${args[@]}" "$@"
