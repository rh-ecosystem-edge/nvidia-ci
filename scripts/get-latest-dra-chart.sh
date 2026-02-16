#!/usr/bin/env bash

set -euo pipefail

# Read GitHub PAT from file (never print this!)
GITHUB_TOKEN=$(cat "${READ_PACKAGES_GITHUB_TOKEN_FILE}" | tr -d '[:space:]')

API_URL='https://api.github.com/orgs/NVIDIA/packages/container/k8s-dra-driver-gpu/versions'

# Fetch first 100 versions (API returns sorted by created_at DESC, newest first)
response=$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${API_URL}?per_page=100&state=active")

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
