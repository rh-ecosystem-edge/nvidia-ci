# GPU Operator Versions Workflow

This workflow automates the process of checking for new versions of OpenShift and NVIDIA GPU Operator, updating version matrices, and triggering CI jobs.

## Overview

The version update automation:
- Fetches the latest OpenShift release versions from the official API
- Retrieves NVIDIA GPU Operator versions from container registries
- Validates GPU operator availability in OpenShift catalogs before triggering tests
- Updates the version matrix in `versions.json`
- Generates test commands based on the support matrix in `settings.json`
- Creates pull requests with version updates

## Catalog Availability Checking

New GPU operator images may not immediately appear in all OpenShift operator catalogs. The workflow verifies catalog availability before tracking new versions.

### Behavior

- New GPU versions available in at least one active OCP catalog are tracked
- Versions not yet in any catalog are skipped and rechecked on the next run
- Tests are scheduled for all OCP versions with warnings where the operator is missing
- OCP version updates proceed independently of GPU catalog status

### Configuration

Environment variable `CHECK_CATALOG_AVAILABILITY`:
- `false` (default): Disable catalog checking - all new versions are tracked immediately
- `true`: Enable catalog checking - only versions available in OCP catalogs are tracked

**Note:** The automated CI workflow (`.github/workflows/update-versions.yaml`) sets this to `true` to ensure only catalog-available versions trigger tests.

### Manual Check

```bash
PYTHONPATH=.github/scripts python -m gpu_operator_versions.catalog_checker 25.10.1 4.20 4.19
```

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
pip install -r .github/scripts/gpu_operator_versions/requirements.txt
```

### Manual Execution

Run the version update process:

```console
PYTHONPATH=.github/scripts python -m gpu_operator_versions.update_versions
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
PYTHONPATH=.github/scripts python -m pytest .github/scripts/gpu_operator_versions/tests/ -v
```

## GitHub Actions Integration

- **Scheduled**: Runs nightly to check for new versions and creates pull requests when updates are detected
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch