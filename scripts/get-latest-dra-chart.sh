#!/usr/bin/env bash
#
# get-latest-dra-chart.sh - Fetch the latest DRA driver Helm chart tag from ghcr.io
#
# REQUIREMENTS:
#   - Environment variable: READ_PACKAGES_GITHUB_TOKEN_FILE
#     Must point to a file containing a GitHub Personal Access Token (classic)
#     with 'read:packages' scope and expiration <= 366 days.
#
#   - jq must be installed for JSON parsing
#   - curl must be installed for API requests
#
# USAGE:
#   export READ_PACKAGES_GITHUB_TOKEN_FILE="/path/to/read-packages-gh-token"
#   ./scripts/get-latest-dra-chart.sh
#
# OUTPUT:
#   - stderr: Human-readable message with tag and creation date
#   - stdout: Just the tag (for easy variable capture)

set -euo pipefail

# Read GitHub PAT from file (never print this!)
GITHUB_TOKEN=$(cat "${READ_PACKAGES_GITHUB_TOKEN_FILE}" | tr -d '[:space:]')

API_URL='https://api.github.com/orgs/NVIDIA/packages/container/k8s-dra-driver-gpu/versions'

# Fetch first 100 versions (API returns sorted by created_at DESC, newest first)
set +e
response=$(curl --fail-with-body -sSL \
    -H "Accept: application/vnd.github+json" \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${API_URL}?per_page=100&state=active")
curl_exit=$?
set -e

# If curl failed, print its output and exit
if [ $curl_exit -ne 0 ]; then
    echo "$response" >&2
    exit $curl_exit
fi

# Find first version with a tag ending in "-chart"
# Since API returns newest first, the first match is the latest chart
# Extract tag and created_at directly to avoid JSON parsing issues
# Use first() to avoid SIGPIPE issues with head -1
result=$(echo "$response" | jq -r '
    first(
        .[] |
        select(.metadata.container.tags[]? | endswith("-chart")) |
        "\(.metadata.container.tags[] | select(endswith("-chart")))\t\(.created_at)"
    ) // empty
')

if [ -z "$result" ]; then
    echo "Error: No Helm chart versions found in the latest 100 versions" >&2
    exit 1
fi

# Parse the tab-separated output
tag=$(echo "$result" | cut -f1)
created_at=$(echo "$result" | cut -f2)

# Output to stderr for human readability
echo "Found latest Helm chart: ${tag} (created: ${created_at})" >&2

# Output only the tag to stdout for easy variable assignment
echo "${tag}"
