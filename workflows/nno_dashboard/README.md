# NVIDIA Network Operator Dashboard Workflow

This workflow generates an HTML dashboard showing NVIDIA Network Operator test results across different operator versions and OpenShift versions. It fetches test data from CI systems and creates visual reports for tracking test status over time.

## Overview

The dashboard workflow:
- Fetches test results from Google Cloud Storage based on pull request data
- Supports various network operator test patterns including:
  - `nvidia-network-operator-legacy-sriov-rdma`
  - `nvidia-network-operator-e2e`
  - DOCA-based tests (e.g., `doca4-nvidia-network-operator-*`)
- Merges new results with existing baseline data
- Generates HTML dashboard reports
- Automatically deploys updates to GitHub Pages

## Architecture

This dashboard **reuses** the GPU Operator Dashboard code and only overrides the operator-specific parts:
- ✅ Imports all core logic from `workflows.gpu_operator_dashboard.fetch_ci_data`
- ✅ Overrides only Network Operator specific:
  - Regex patterns to match network operator job names
  - Artifact paths (`network-operator-e2e/artifacts/`)
  - Version field names (`network_operator_version` vs `gpu_operator_version`)
- ✅ Maintains a clean, DRY codebase with minimal duplication

This design makes maintenance easier - bug fixes in the core logic automatically benefit both dashboards.

## Supported Test Patterns

The dashboard recognizes the following test job patterns:
- `pull-ci-rh-ecosystem-edge-nvidia-ci-main-{version}-nvidia-network-operator-legacy-sriov-rdma`
- `pull-ci-rh-ecosystem-edge-nvidia-ci-main-{version}-nvidia-network-operator-e2e`
- `rehearse-{id}-pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca4-nvidia-network-operator-*`

Example URL that will be processed:
```
https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/openshift_release/67673/rehearse-67673-pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca4-nvidia-network-operator-legacy-sriov-rdma/1961127149603655680/
```

## Usage

### Prerequisites

```console
pip install -r workflows/nno_dashboard/requirements.txt
```

**Important:** Before running fetch_ci_data.py, create the baseline data file and initialize it with an empty JSON object if it doesn't exist:

```console
echo '{}' > nno_data.json
```

### Fetch CI Data

```console
# Process a specific PR
python -m workflows.nno_dashboard.fetch_ci_data --pr_number "123" --baseline_data_filepath nno_data.json --merged_data_filepath nno_data.json

# Process all merged PRs - limited to 100 most recent (default)
python -m workflows.nno_dashboard.fetch_ci_data --pr_number "all" --baseline_data_filepath nno_data.json --merged_data_filepath nno_data.json

# Process with bundle result limit (keep only last 50 bundle tests per version)
python -m workflows.nno_dashboard.fetch_ci_data --pr_number "all" --baseline_data_filepath nno_data.json --merged_data_filepath nno_data.json --bundle_result_limit 50
```

### Generate Dashboard

```console
python -m workflows.nno_dashboard.generate_ci_dashboard --dashboard_data_filepath nno_data.json --dashboard_html_filepath nno_dashboard.html
```

The dashboard generator also **reuses** the GPU Operator dashboard code:
- Imports all HTML generation logic from `workflows.gpu_operator_dashboard.generate_ci_dashboard`
- Uses Network Operator specific templates (in `templates/` directory)
- Only aliases `NETWORK_OPERATOR_VERSION` as `GPU_OPERATOR_VERSION` for compatibility

### Running Tests

First, make sure `pytest` is installed. Then, run:

```console
python -m pytest workflows/nno_dashboard/tests/ -v
```

## GitHub Actions Integration

- **Automatic**: Processes merged pull requests to update the dashboard with new test results and deploys to GitHub Pages
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch

## Data Structure

The fetched data follows this structure:

```json
{
  "doca4": {
    "notes": [],
    "bundle_tests": [
      {
        "ocp_full_version": "4.16.0",
        "network_operator_version": "24.10.0",
        "test_status": "SUCCESS",
        "prow_job_url": "https://...",
        "job_timestamp": "1234567890"
      }
    ],
    "release_tests": [...],
    "job_history_links": [
      "https://prow.ci.openshift.org/job-history/gs/test-platform-results/pr-logs/directory/..."
    ]
  }
}
```

## Troubleshooting

### No data being fetched

1. Verify the PR number exists and has network operator test runs
2. Check that the job names match the expected patterns (see regex in fetch_ci_data.py line 36-40)
3. Ensure the test artifacts contain the required files:
   - `finished.json`
   - `network-operator-e2e/artifacts/ocp.version`
   - `network-operator-e2e/artifacts/operator.version`

### Regex pattern not matching

The regex pattern is designed to match:
- Repository: `rh-ecosystem-edge_nvidia-ci` or `openshift_release` (for rehearse jobs)
- OCP version prefix: Can be `doca4`, `nno1`, or other custom prefixes
- Job suffix: Must contain `nvidia-network-operator` followed by test type

If your job names don't match, you may need to adjust the `TEST_RESULT_PATH_REGEX` pattern in `fetch_ci_data.py`.

