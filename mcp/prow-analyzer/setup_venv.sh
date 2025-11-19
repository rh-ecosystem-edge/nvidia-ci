#!/bin/bash
# Script to create a clean Python venv outside of Cursor's AppImage environment
#
# WHY THIS IS NEEDED:
# When running from Cursor's integrated terminal, the environment is polluted with
# AppImage-specific variables (APPIMAGE, APPDIR, LD_LIBRARY_PATH, etc.) and PATH
# entries pointing to the mounted AppImage. This causes Python's venv module to
# incorrectly record the AppImage as the Python interpreter's home, resulting in
# symlinks like venv/bin/python -> cursor.appimage instead of the system Python.
#
# This script cleans the environment before creating the venv to ensure proper
# Python interpreter references.

set -euo pipefail  # Exit on error, undefined vars, and pipe failures

cd "$(dirname "$0")"

# Clean up AppImage pollution from environment
unset APPIMAGE
unset APPDIR
unset LD_LIBRARY_PATH
unset PERLLIB
unset QT_PLUGIN_PATH
unset GSETTINGS_SCHEMA_DIR

# Reset PATH to standard system directories only
# This removes any AppImage mount points that may be in PATH
export PATH=/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin

# Add user bin directories if they exist
[ -d "$HOME/.local/bin" ] && export PATH="$PATH:$HOME/.local/bin"
[ -d "$HOME/bin" ] && export PATH="$PATH:$HOME/bin"

# Optionally unset XDG_DATA_DIRS if it contains AppImage paths
if [[ "${XDG_DATA_DIRS:-}" == *"/tmp/.mount_"* ]]; then
    unset XDG_DATA_DIRS
fi

# Remove old venv if it exists
rm -rf venv

# Create fresh venv with real Python
# Use absolute path to ensure we get the system Python, not an AppImage wrapper
/usr/bin/python3 -m venv venv

# Install dependencies
venv/bin/pip install --upgrade pip
venv/bin/pip install -r requirements.txt

echo ""
echo "Virtual environment created successfully!"
echo "Python executable: $(venv/bin/python -c 'import sys; print(sys.executable)')"
echo ""
echo "Note: The executable path above may still show an AppImage path if you're"
echo "running this from Cursor's terminal, but the venv configuration is correct."

