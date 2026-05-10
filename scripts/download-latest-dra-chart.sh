#!/usr/bin/env bash
#
# download-latest-dra-chart.sh - Fetch the latest DRA driver Helm chart from GitHub Actions
#
# TEMPORARY: This script downloads charts from GitHub Actions artifacts (90-day retention).
# Once chart publishing is implemented, charts will be available at:
# us-central1-docker.pkg.dev/k8s-staging-images/dra-driver-nvidia/charts
# At that point, this script can be removed and the original get-latest-dra-chart.sh updated.
#
# REQUIREMENTS:
#   - Environment variable: READ_PACKAGES_GITHUB_TOKEN_FILE
#     Must point to a file containing a GitHub Personal Access Token (classic)
#     with 'public_repo' scope (sufficient for public repositories)
#     or 'repo' scope (for private repositories)
#     and expiration <= 366 days.
#
#   - jq must be installed for JSON parsing
#   - curl must be installed for API requests
#   - unzip must be installed for extraction
#
# USAGE:
#   export READ_PACKAGES_GITHUB_TOKEN_FILE="/path/to/read-packages-gh-token"
#   ./scripts/download-latest-dra-chart.sh
#
# OUTPUT:
#   - stderr: Human-readable message with chart version and artifact info
#   - stdout: Path to the .tgz file (for easy variable capture)

set -euo pipefail

echo "NOTE: Fetching chart from GitHub Actions (temporary until published to GCP Artifact Registry)" >&2

# Read GitHub PAT from file (never print this!)
GITHUB_TOKEN=$(cat "${READ_PACKAGES_GITHUB_TOKEN_FILE}" | tr -d '[:space:]')

REPO="kubernetes-sigs/dra-driver-nvidia-gpu"
OUTPUT_DIR="${1:-./charts}"
API_BASE="https://api.github.com/repos/${REPO}"
AUTH_HEADERS=(-H "Accept: application/vnd.github+json" -H "Authorization: Bearer ${GITHUB_TOKEN}" -H "X-GitHub-Api-Version: 2022-11-28")

# Get the most recent successful workflow run
response=$(curl --fail-with-body -sSL "${AUTH_HEADERS[@]}" \
    "${API_BASE}/actions/workflows/ci.yaml/runs?per_page=5&branch=main&status=completed")

# Find the first successful run and extract its details directly
read -r run_id commit_sha created_at < <(echo "$response" | jq -r '.workflow_runs[] | select(.conclusion == "success") | "\(.id) \(.head_sha) \(.created_at)"' | head -1)

if [ -z "$run_id" ]; then
    echo "Error: Failed to find successful workflow run" >&2
    echo "API response:" >&2
    echo "$response" >&2
    exit 1
fi

# Get the helm-chart artifact
artifacts_response=$(curl --fail-with-body -sSL "${AUTH_HEADERS[@]}" \
    "${API_BASE}/actions/runs/${run_id}/artifacts")

# Let jq errors go to stderr (don't capture them)
artifact_id=$(echo "$artifacts_response" | jq -r '.artifacts[] | select(.name == "helm-chart") | .id')

if [ -z "$artifact_id" ] || [ "$artifact_id" = "null" ]; then
    echo "Error: No 'helm-chart' artifact found in run ${run_id}" >&2
    echo "API response:" >&2
    echo "$artifacts_response" >&2
    exit 1
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Download the artifact (it's a .zip file)
temp_zip="${OUTPUT_DIR}/helm-chart-temp.zip"
curl --fail-with-body -sSL "${AUTH_HEADERS[@]}" \
    -o "${temp_zip}" \
    "${API_BASE}/actions/artifacts/${artifact_id}/zip"

# Extract the .tgz from the .zip
unzip -q -o "${temp_zip}" -d "${OUTPUT_DIR}"
rm -f "${temp_zip}"

# Find the extracted .tgz file
chart_tgz=$(find "${OUTPUT_DIR}" -name "dra-driver-nvidia-gpu-*.tgz" -type f -printf '%T@ %p\n' | sort -rn | head -1 | cut -d' ' -f2-)

if [ -z "$chart_tgz" ]; then
    echo "Error: No .tgz file found after extraction" >&2
    echo "Contents of ${OUTPUT_DIR}:" >&2
    ls -la "${OUTPUT_DIR}" >&2
    exit 1
fi

# Output to stderr for human readability
artifact_url="https://github.com/${REPO}/actions/runs/${run_id}/artifacts/${artifact_id}"
echo "Found latest Helm chart: $(basename "$chart_tgz") (created: ${created_at}, artifact: ${artifact_url})" >&2

# Output only the chart .tgz path to stdout for easy variable assignment
echo "${chart_tgz}"
