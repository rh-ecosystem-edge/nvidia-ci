# GPU Operator Versions Workflow

This workflow automates the process of checking for new versions of OpenShift and NVIDIA GPU Operator, updating version matrices, and triggering CI jobs.

## Overview

The version update automation:
- Fetches the latest OpenShift release versions from the official API
- Retrieves NVIDIA GPU Operator versions from container registries
- Updates the version matrix in `versions.json`
- Generates test commands based on the support matrix in `settings.json`
- Creates pull requests with version updates

## Configuration

The workflow requires a `settings.json` file that defines which OpenShift versions are in maintenance mode:

```json
{
  "openshift_support": {
    "4.12": {
      "status": "maintenance",
      "pinned_gpu_operator": ["25.3"]
    },
    "4.13": {
      "status": "maintenance",
      "pinned_gpu_operator": ["25.3"]
    }
  },
  "defaults": {
    "unlisted_versions": {
      "status": "active"
    }
  },
  "ignored_versions_regex": "^4.[0-9]$|^4.1[0-1]$"
}
```

**Key behavior:**
- **Active versions** (unlisted or `status: "active"`):
  - Test with new GPU operator releases
  - Test with GPU operator patch updates (if in latest 2)
  - Test with master bundle changes
- **Maintenance versions** (`status: "maintenance"`):
  - Only test when OpenShift gets a new patch (with their pinned GPU operators)
  - Do NOT test when GPU operators change (frozen configuration)
  - Do NOT test with master bundle changes
- New OpenShift versions automatically default to "active"

To move a version to maintenance mode, add it to `openshift_support` with status `"maintenance"` and specify which GPU operator versions to pin.

## Data Sources

- **OpenShift versions**: Retrieved from OpenShift CI release streams API
- **GPU Operator versions**: Fetched from NVIDIA Container Registry and GitHub Container Registry
- **Version matrix**: Stored in `versions.json` and updated automatically
- **Support matrix**: Configured in `settings.json`

## Running the Workflow

### Prerequisites

Install dependencies:

```console
pip install -r workflows/gpu_operator_versions/requirements.txt
```

### Manual Execution

Run the version update process:

```console
python -m workflows.gpu_operator_versions.update_versions
```

### Environment Variables

The workflow supports several environment variables:
- `VERSION_FILE_PATH` - Path to versions.json file (Required)
- `TEST_TO_TRIGGER_FILE_PATH` - Path to generated test commands file (Required)
- `SETTINGS_FILE_PATH` - Path to settings.json (Optional, defaults to same directory)
- `GH_AUTH_TOKEN` - GitHub authentication token (Optional)
- `REQUEST_TIMEOUT_SECONDS` - Request timeout in seconds (Optional, defaults to 30)

### Running Tests

First, make sure `pytest` is installed. Then, run:

```console
python -m pytest workflows/gpu_operator_versions/tests/ -v
```

## GitHub Actions Integration

- **Scheduled**: Runs nightly to check for new versions and creates pull requests when updates are detected
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch