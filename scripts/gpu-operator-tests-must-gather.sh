#!/usr/bin/env bash

# Get the directory where this script is located
SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Default to must-gather dir
ARTIFACT_DIR="${ARTIFACT_DIR:-must-gather}"


# Collect NFD operator must-gather
NFD_ARTIFACT_DIR="${ARTIFACT_DIR}/nfd-must-gather"
mkdir -p "${NFD_ARTIFACT_DIR}"
echo "Collecting NFD operator must-gather in ${NFD_ARTIFACT_DIR}"
OUTPUT_DIR="${NFD_ARTIFACT_DIR}" "$SCRIPTS_DIR/nfd-must-gather.sh"


# Collect GPU operator must-gather 
GPU_ARTIFACT_DIR="${ARTIFACT_DIR}/gpu-must-gather"
mkdir -p "${GPU_ARTIFACT_DIR}"
echo "Collecting GPU operator must-gather in ${GPU_ARTIFACT_DIR}"
ARTIFACT_DIR="${GPU_ARTIFACT_DIR}" "$SCRIPTS_DIR/gpu-operator-must-gather.sh"
