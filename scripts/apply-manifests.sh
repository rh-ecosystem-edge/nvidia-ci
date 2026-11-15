#!/usr/bin/env bash
set -e
exec python3 -u "$(dirname "$0")/apply_manifests.py" "$@"
