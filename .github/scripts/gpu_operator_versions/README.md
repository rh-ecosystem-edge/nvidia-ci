# GPU Operator Versions Workflow

This workflow automates the process of checking for new versions of OpenShift and NVIDIA GPU Operator, updating version matrices, and triggering CI jobs.

## Overview

The version update automation:
- Fetches the latest OpenShift release versions from the official API once per run
- Retrieves NVIDIA GPU Operator versions from container registries once per run
- Dispatches the shared upstream snapshot to configured profiles
- Validates GPU operator availability in OpenShift catalogs before triggering tests
- Updates the regular version matrix in `versions.json`
- Updates the signed/precompiled version matrix in `signed_versions.json`
- Generates test commands from each profile’s support matrix
- Creates one pull request containing all active profile updates

## Catalog Availability Checking

New GPU operator images may not immediately appear in all OpenShift operator catalogs. The workflow verifies catalog availability before tracking new versions.

### Behavior

- New GPU versions available in at least one active OCP catalog are tracked
- Versions not yet in any catalog are skipped and rechecked on the next run
- Tests are scheduled for all OCP versions with warnings where the operator is missing
- OCP version updates proceed independently of GPU catalog status

### Configuration

Catalog checking is configured per profile in `profiles.json`:
- `false`: Disable catalog checking - all new versions are tracked immediately
- `true`: Enable catalog checking - only versions available in OCP catalogs are tracked

**Note:** Both production profiles set this to `true` to ensure only catalog-available versions trigger tests.

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

## Profiles and Signed/Precompiled Driver Testing

Version updates are organized as profiles in `profiles.json`. The workflow discovers upstream versions once and passes the same snapshot to every active profile.

### Profiles

- **regular**: uses `versions.json`, `settings.json`, and the regular Prow command template
- **signed**: uses `signed_versions.json`, `signed_settings.json`, and the signed Prow command template

For example, the profile command templates are:

```json
"test_command_template": "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}"
```

and:

```json
"test_command_template": "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}-signed"
```

The loader validates that each template contains exactly the supported `{ocp_version}` and `{gpu_version}` placeholders.

The signed profile also sets:
- `minimum_gpu_channel` in `signed_settings.json`: minimum GPU operator channel for signed tests (for example, `"26.3"`)
- A stricter `ignored_versions_regex` to limit OCP versions to 4.20+ (only versions with signed CI jobs in the release repo)

### Schedules

The workflow uses the union of all profile schedules and resolves the active profiles from each profile’s five-field cron expression:

- regular: `0 0 * * 0-5` (Sunday through Friday)
- signed: `0 0 * * 2-4` (Tuesday through Thursday)

On Sunday, Monday, and Friday, only the regular profile runs. On Tuesday through Thursday, both profiles run from one upstream discovery pass. The workflow does not run on Saturday.

When the signed profile is inactive in `auto` mode, `signed_versions.json` is not updated and signed test commands are not generated. The unchanged signed tracking file remains the pending state for the next scheduled signed run.

### Manual profile selection

The GitHub workflow accepts a `profiles` input:

- `auto`: use each profile’s cron schedule
- `all`: run every profile
- `regular`: run only the regular profile
- `signed`: run only the signed profile
- A comma-separated list, such as `regular,signed`

## Data Sources

- **OpenShift versions**: Retrieved from OpenShift CI release streams API
- **GPU Operator versions**: Fetched from NVIDIA Container Registry and GitHub Container Registry
- **Version matrix**: Stored in `versions.json` (regular) and `signed_versions.json` (signed) and updated automatically
- **Support matrix**: Configured in `settings.json` (regular) and `signed_settings.json` (signed)

## Running the Workflow

### Prerequisites

Install dependencies:

```console
pip install -r .github/scripts/gpu_operator_versions/requirements.txt
```

### Manual Execution

Run all profiles selected by the current UTC weekday:

```console
PYTHONPATH=.github/scripts python -m gpu_operator_versions.update_versions
```

Run a specific profile selection:

```console
PYTHONPATH=.github/scripts python -m gpu_operator_versions.update_versions --profiles signed
```

Use a custom profile manifest:

```console
PYTHONPATH=.github/scripts python -m gpu_operator_versions.update_versions \
  --profiles all \
  --manifest path/to/profiles.json
```

### Environment Variables

The profile manifest defines each profile’s version file, settings file, output file, complete `/test` command template, schedule, and catalog-check setting. The remaining environment variables are:
- `REQUEST_TIMEOUT_SECONDS` - Request timeout in seconds (Optional, defaults to the manifest value or `30`)
- `OCP_IGNORED_VERSIONS_REGEX` - Fallback OpenShift ignore regex when a settings file omits one
- `CHECK_CATALOG_AVAILABILITY` - Fallback catalog-check setting when a profile omits one

### Running Tests

First, make sure `pytest` is installed. Then, run:

```console
PYTHONPATH=.github/scripts python -m pytest .github/scripts/gpu_operator_versions/tests/ -v
```

## GitHub Actions Integration

- **Scheduled**: Runs nightly to check for new versions and creates pull requests when updates are detected
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch