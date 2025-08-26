# GPU Operator Versions Workflow

This workflow automates the process of checking for new versions of OpenShift and NVIDIA GPU Operator, updating version matrices, and triggering CI jobs.

## Overview

The version update automation:
- Fetches the latest OpenShift release versions from the official API
- Retrieves NVIDIA GPU Operator versions from container registries
- Updates the version matrix in `versions.json`
- Generates test commands for new version combinations
- Creates pull requests with version updates

## Data Sources

- **OpenShift versions**: Retrieved from OpenShift CI release streams API
- **GPU Operator versions**: Fetched from NVIDIA Container Registry and GitHub Container Registry
- **Version matrix**: Stored in `versions.json` and updated automatically

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
- `GH_AUTH_TOKEN` - GitHub authentication token (Optional)
- `OCP_IGNORED_VERSIONS_REGEX` - Regex to exclude specific OpenShift versions (Optional)
- `REQUEST_TIMEOUT_SECONDS` - Request timeout in seconds (Optional, defaults to 30)

### Running Tests

First, make sure `pytest` is installed. Then, run:

```console
python -m pytest workflows/gpu_operator_versions/tests/ -v
```

## GitHub Actions Integration

- **Scheduled**: Runs nightly to check for new versions and creates pull requests when updates are detected
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch