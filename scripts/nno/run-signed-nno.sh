#!/bin/bash
set -euo pipefail

case "${1:-}" in
  deploy) label=deploy; gpudirect=false ;;
  gpudirect) label=rdma-shared-dev; gpudirect=true ;;
  *) echo "Usage: $0 deploy | gpudirect" >&2; exit 1 ;;
esac

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
profile="${CLUSTER_PROFILE_DIR}/nno-e2e-env"
if [[ ! -f "${profile}" ]]; then
  echo "Missing cluster-profile file nno-e2e-env" >&2
  exit 1
fi
set -o allexport
source "${profile}"
set +o allexport

# The validator must have published a request before provisioning the lab.
for file in dpf-openshift-version ofed-pullspec nno-doca2-signed-request.json; do
  if [[ ! -s "${SHARED_DIR}/${file}" ]]; then
    echo "Missing or empty signed request handoff ${file}" >&2
    exit 1
  fi
done
if [[ -z "$(tr -d '\n' < "${SHARED_DIR}/dpf-openshift-version")" ]]; then
  echo "Signed request has an empty dpf-openshift-version" >&2
  exit 1
fi
export NVIDIANETWORK_OFED_DRIVER_PULLSPEC="$(tr -d '\n' < "${SHARED_DIR}/ofed-pullspec")"
if [[ -z "${NVIDIANETWORK_OFED_DRIVER_PULLSPEC}" ]]; then
  echo "Signed request has an empty ofed-pullspec" >&2
  exit 1
fi
# Apply the explicit image after the profile so its defaults cannot replace it.
unset NVIDIANETWORK_OFED_DRIVER_VERSION NVIDIANETWORK_OFED_REPOSITORY
export NVIDIANETWORK_USE_PRECOMPILED_OFED=true
export TEST_FEATURES=nvidianetwork
export TEST_LABELS="${label}"
export NVIDIANETWORK_CLEANUP=false
export NVIDIANETWORK_RDMA_LINK_TYPE=ethernet
export NVIDIANETWORK_RDMA_NETWORK_TYPE=shared-device
export NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE=default
export NVIDIANETWORK_RDMA_GPUDIRECT="${gpudirect}"
export NNO_STEP_NAME="network-operator-e2e-signed-${1}"
export NNO_STEP_ATTEMPT="${BUILD_ID:-local}-$(date +%s)-$$"
make run-tests
python3 scripts/nno/verify-signed-step-result.py
