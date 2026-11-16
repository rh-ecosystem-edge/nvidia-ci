# GPU Operator Dashboard Workflow

This workflow generates an HTML dashboard showing NVIDIA GPU Operator test results across different operator versions and OpenShift versions. It fetches test data from CI systems and creates visual reports for tracking test status over time.

## Overview

The dashboard workflow:
- Fetches test results from Google Cloud Storage based on pull request data
- Merges new results with existing baseline data
- Generates HTML dashboard reports
- Automatically deploys updates to GitHub Pages

## Usage

### Prerequisites

```console
pip install -r .github/scripts/gpu_operator_dashboard/requirements.txt
```

**Important:** Before running fetch_ci_data.py, create the baseline data file and initialize it with an empty JSON object if it doesn't exist:

```console
echo '{}' > data.json
```

### Fetch CI Data

```console
# Process a specific PR
PYTHONPATH=.github/scripts python -m gpu_operator_dashboard.fetch_ci_data --pr_number "123" --baseline_data_filepath data.json --merged_data_filepath data.json

# Process all merged PRs - limited to 100 most recent (default)
PYTHONPATH=.github/scripts python -m gpu_operator_dashboard.fetch_ci_data --pr_number "all" --baseline_data_filepath data.json --merged_data_filepath data.json
```

### Generate Dashboard

```console
PYTHONPATH=.github/scripts python -m gpu_operator_dashboard.generate_ci_dashboard --dashboard_data_filepath data.json --dashboard_html_filepath dashboard.html
```

### Running Tests

First, make sure `pytest` is installed. Then, run:

```console
PYTHONPATH=.github/scripts python -m pytest .github/scripts/gpu_operator_dashboard/tests/ -v
```

## Manual Notes

Manual notes are stored in `manual_notes.json` next to the dashboard package. Each key is an OpenShift major.minor version, and its value is a list of note strings.
Notes support a safe Markdown-like subset for `[label](https://example.com)` links, `**bold**`, and `*italic*` text. Raw HTML is escaped.

During data collection, manual notes replace any notes carried in the baseline JSON before the HTML dashboard is rendered. This creates a section for a version even when no test results exist yet and keeps `manual_notes.json` as the single source of truth for manually maintained notes.

## GitHub Actions Integration

- **Automatic**: Processes merged pull requests to update the dashboard with new test results and deploys to GitHub Pages
- **Manual**: Can be triggered manually via GitHub Actions workflow dispatch
