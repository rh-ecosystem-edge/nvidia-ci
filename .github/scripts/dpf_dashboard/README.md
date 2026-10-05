# Red Hat OpenShift DPF CI Dashboard

This module generates an HTML dashboard displaying CI test results for Red Hat OpenShift DPF (DOCA Platform Framework).

## Overview

The dashboard fetches results from periodic Prow jobs stored in Google Cloud Storage and generates an interactive HTML page showing:

- Test results organized by OpenShift version
- Version combinations: OCP, DPF, OVN chart, BlueField OCP image, HCP provisioner
- Deduplication: each unique version combination appears once (with SUCCESS if any run passed)
- History bar showing daily pass/fail results for the networking step
- Links to Prow job logs

## Data Flow

1. **Daily cron** (`--job-limit 1`): fetches the latest periodic job run
2. **Manual backfill** (`--job-limit 30`): fetches the last 30 runs (for initial setup)
3. Results accumulate in `dpf_matrix.json` on `gh-pages`, merged by `build_id`
4. HTML dashboard is regenerated when the JSON data changes

## Success Criteria

A job is considered successful if the `dpf-hypervisor-network-tests` step passes. This is checked via the step's `finished.json` (`passed: true`).

## Configuration

Job definitions are in `versions.json`:

```json
{
  "ocp_versions": {
    "4.22": {
      "status": "active",
      "jobs": [
        {
          "name": "periodic-ci-rh-ecosystem-edge-openshift-dpf-main-daily-e2e-doca4",
          "branch": "main"
        }
      ]
    }
  },
  "env_step": "dpf-hypervisor-deploy-cluster",
  "networking_step": "dpf-hypervisor-network-tests",
  "step_prefix": "daily-e2e-doca4"
}
```

To add a new OCP version or branch, add an entry to `ocp_versions`.

## Scripts

### fetch_ci_data.py

Fetches periodic job results from GCS.

```bash
# Daily run (fetch latest result)
PYTHONPATH=.github/scripts python -m dpf_dashboard.fetch_ci_data \
  --output-data output/dpf_matrix.json

# Initial backfill (fetch last 30 runs)
PYTHONPATH=.github/scripts python -m dpf_dashboard.fetch_ci_data \
  --output-data output/dpf_matrix.json \
  --job-limit 30
```

For each job run, it fetches:
- `finished.json` (timestamp, branch)
- `dpf-hypervisor-network-tests/finished.json` (networking pass/fail)
- `dpf-hypervisor-deploy-cluster/artifacts/.env` (version info)

### generate_ci_dashboard.py

Generates HTML dashboard from JSON data.

```bash
PYTHONPATH=.github/scripts python -m dpf_dashboard.generate_ci_dashboard \
  --input-data output/dpf_matrix.json \
  --output-html output/dpf_matrix.html
```

### Local Preview

```bash
git show upstream/gh-pages:styles.css > /tmp/styles.css
PYTHONPATH=.github/scripts python -m dpf_dashboard.fetch_ci_data \
  --output-data /tmp/dpf_matrix.json --job-limit 30
PYTHONPATH=.github/scripts python -m dpf_dashboard.generate_ci_dashboard \
  --input-data /tmp/dpf_matrix.json --output-html /tmp/dpf_matrix.html
```

## Data Structure

The accumulated results file (`dpf_matrix.json`) has this structure:

```json
{
  "4.22": {
    "results": [
      {
        "ocp_version": "4.22.7",
        "branch": "main",
        "dpf_version": "v26.4.1",
        "ovn_chart_version": "v26.4-ocp-beta4-kata",
        "bluefield_ocp_image": "quay.io/edge-infrastructure/bluefield-ocp:4.22.7",
        "hcp_provisioner_image": "quay.io/.../dpf-hcp-provisioner-rhel10-operator:latest",
        "networking_status": "SUCCESS",
        "prow_job_url": "https://prow.ci.openshift.org/view/gs/...",
        "job_timestamp": "1791182801",
        "build_id": "2106905001866563584"
      }
    ]
  }
}
```

## Integration

This module is called by `.github/workflows/generate_dpf_dashboard.yaml` (daily cron at 5:30 AM UTC).

## See Also

- [GPU Operator Dashboard](../gpu_operator_dashboard/)
- [Network Operator Dashboard](../nno_dashboard/)
- [Common Utilities](../common/)
