# NVIDIA Network Operator CI Dashboard

This module generates an HTML dashboard displaying CI test results for NVIDIA Network Operator on Red Hat OpenShift.

## Overview

The dashboard fetches test results from OpenShift CI (Prow) stored in Google Cloud Storage and generates an interactive HTML page showing:

- Test results organized by OpenShift version
- Multiple test flavors (infrastructure types, RDMA configurations, GPU tests)
- Success/failure status with links to detailed test logs
- Historical test data

## Test Flavors

Network Operator tests run across multiple configurations:

### Infrastructure Types
- **DOCA4**: Tests on DOCA4 infrastructure
- **Bare Metal**: Tests on bare metal servers
- **Hosted**: Tests in hosted environments

### Test Types
- **RDMA Legacy SR-IOV**: Legacy SR-IOV RDMA testing
- **RDMA Shared Device**: Shared device RDMA testing  
- **RDMA SR-IOV**: SR-IOV with RDMA
- **E2E**: End-to-end integration tests

### GPU Support
Tests can run with or without GPU:
- **with GPU**: Tests including GPU/GPUDirect functionality
- **(no suffix)**: Tests without GPU

### Example Flavors
- `DOCA4 - RDMA Legacy SR-IOV`
- `Bare Metal - E2E`
- `Hosted - RDMA SR-IOV with GPU`
- `DOCA4 - RDMA Shared Device`

## Data Structure

The dashboard uses a JSON file with this structure:

```json
{
  "4.17.16": {
    "notes": [],
    "bundle_tests": [],
    "release_tests": [],
    "job_history_links": [...],
    "test_flavors": {
      "DOCA4 - RDMA Legacy SR-IOV": {
        "results": [
          {
            "ocp_full_version": "4.17.16",
            "operator_version": "25.4.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://...",
            "job_timestamp": 1756406663,
            "test_flavor": "DOCA4 - RDMA Legacy SR-IOV"
          }
        ],
        "job_history_links": [...]
      }
    }
  }
}
```

## Scripts

### fetch_ci_data.py

Fetches test results from GCS for a specific PR.

```bash
python -m workflows.nno_dashboard.fetch_ci_data \
  --pr_number 67673 \
  --baseline_data_filepath output/network_operator_matrix.json \
  --merged_data_filepath output/network_operator_matrix.json
```

**What it does:**
1. Fetches `finished.json`, `ocp.version`, and `operator.version` files from GCS
2. Extracts test flavor from job name (infrastructure + test type + GPU)
3. Validates OpenShift versions (filters out infrastructure types)
4. Organizes results by OCP version and test flavor
5. Merges with existing data

### generate_ci_dashboard.py

Generates HTML dashboard from JSON data.

```bash
python -m workflows.nno_dashboard.generate_ci_dashboard \
  --dashboard_data_filepath output/network_operator_matrix.json \
  --dashboard_html_filepath output/network_operator_matrix.html
```

**What it does:**
1. Loads JSON data
2. Filters valid OCP versions
3. Builds HTML sections for each test flavor
4. Groups results by OCP and operator versions
5. Creates clickable links with success/failure styling

## Shared Utilities

This module uses shared utilities from `workflows/common/`:

- **gcs_utils**: GCS API access (fetch files, build URLs)
- **html_builders**: HTML generation (TOC, notes, footers)
- **data_structures**: `TestResult` dataclass and constants
- **templates**: Template loading utilities
- **utils**: Logging

## Example Job Names

Network Operator CI jobs follow this pattern:

```
pull-ci-rh-ecosystem-edge-nvidia-ci-main-{infrastructure}-nvidia-network-operator-{test-type}
```

Examples:
- `pull-ci-...-doca4-nvidia-network-operator-legacy-sriov-rdma`
- `pull-ci-...-bare-metal-nvidia-network-operator-bare-metal-e2e-doca4-latest`
- `pull-ci-...-hosted-nvidia-network-operator-sriov-rdma-gpu`

## Dashboard Output

The generated HTML includes:

- **Table of Contents**: Quick navigation to OCP versions
- **OCP Version Sections**: One per OpenShift version
- **Test Flavor Tables**: One table per flavor showing:
  - OpenShift version
  - Network Operator version (clickable to test logs)
  - Color-coded success/failure status

## Development

### Adding New Test Flavors

1. Update `extract_test_flavor_from_job_name()` in `fetch_ci_data.py`
2. Add pattern matching for the new flavor
3. Test with actual PR data

### Modifying HTML Layout

1. Edit templates in `templates/`:
   - `header.html`: Page header and scripts
   - `main_table.html`: OCP version container
   - `test_flavor_section.html`: Individual flavor tables
2. Use `{placeholders}` for dynamic content
3. Regenerate dashboard to test changes

## Integration

This module is called by the GitHub Actions workflow in `.github/workflows/generate_matrix_page.yaml`:

```yaml
- name: Fetch Network Operator CI Data
  run: |
    python -m workflows.nno_dashboard.fetch_ci_data \
      --pr_number "${{ steps.determine_pr.outputs.PR_NUMBER }}" \
      --baseline_data_filepath "${{ env.NNO_DASHBOARD_DATA_FILEPATH }}" \
      --merged_data_filepath "${{ env.NNO_DASHBOARD_DATA_FILEPATH }}"

- name: Generate Network Operator HTML Dashboard
  run: |
    python -m workflows.nno_dashboard.generate_ci_dashboard \
      --dashboard_data_filepath "${{ env.NNO_DASHBOARD_DATA_FILEPATH }}" \
      --dashboard_html_filepath "${{ env.NNO_DASHBOARD_HTML_FILEPATH }}"
```

## See Also

- [GPU Operator Dashboard](../gpu_operator_dashboard/) - Similar dashboard for GPU Operator
- [Common Utilities](../common/) - Shared code across dashboards
- [GitHub Actions Workflow](../../.github/workflows/generate_matrix_page.yaml) - CI automation

