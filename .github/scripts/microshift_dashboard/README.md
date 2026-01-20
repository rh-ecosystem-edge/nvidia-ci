# MicroShift Dashboard Workflow

This workflow generates dashboards for NVIDIA Device Plugin testing on Red Hat Device Edge (MicroShift). It fetches test results from OpenShift CI and updates an HTML dashboard showing the status of NVIDIA Device Plugin integration across different MicroShift versions.

## Overview

The MicroShift dashboard workflow:
- Fetches test results from OpenShift CI for MicroShift versions 4.14+
- Processes job results for NVIDIA Device Plugin validation tests
- Generates HTML dashboards showing test status across versions
- Updates GitHub Pages with the latest dashboard

## Running the Workflow

### Prerequisites

Install dependencies:

```console
pip install -r .github/scripts/microshift_dashboard/requirements.txt
```

### Manual Execution

Fetch job data:

```console
PYTHONPATH=.github/scripts python -m microshift_dashboard.microshift fetch-data --output-data microshift_results.json
```

Generate dashboard:

```console
PYTHONPATH=.github/scripts python -m microshift_dashboard.microshift generate-dashboard --input-data microshift_results.json --output-dashboard microshift.html
```

## GitHub Actions Integration

- **Scheduled**: Runs nightly to update the dashboard with the latest test results and deploy to GitHub Pages
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch