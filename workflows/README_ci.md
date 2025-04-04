# NVIDIA GPU Operator Matrix – Dashboard

This repository contains scripts, data, and supporting files used to **generate** and **deploy** a test matrix dashboard for the NVIDIA GPU Operator on Red Hat OpenShift. The dashboard is built by merging test results into a JSON file and then transforming that data into an HTML report.

> **Note:**
> The output JSON and HTML file names are configurable. In the examples below, we use the names:
> - **Baseline JSON data file:** `nvidia_gpu_operator_data.json`
> - **HTML dashboard file:** `nvidia_gpu_operator_dashboard.html`
>
> You can choose other names by specifying the command-line parameters when running the script.

> **Important:**
> Before running the data generation script, **create the baseline data file** in your output directory and initialize it with an empty JSON object `{}`. If this file is missing or contains invalid JSON, the script will crash.

---

## File Structure

```text
workflows/
├── generate_ci_dashboard.py      # Runs both JSON merging and HTML generation
├── logger.py                     # Logging configuration and functions
├── dashboard_requirements.txt              # Python dependencies (e.g., requests)
├── README.md                     # This documentation file
├── output/                       # Directory where generated JSON and HTML files are stored
├── templates/                    # HTML template files used by the dashboard generator
│   ├── footer.html
│   ├── header.html
│   └── main_table.html
└── tests/                        # Unit tests for the project
    ├── test_generate_data.py     # Tests for data extraction and merging logic
    └── test_generate_ui.py       # Tests for the HTML generation logic
```

---

## Contents

1. **JSON Data Files**
   - **Baseline Data File (Input):**
     **Example Name:** `nvidia_gpu_operator_data.json`
     **Purpose:** Stores previously merged test results from various OpenShift versions and NVIDIA GPU Operator test runs. This file must be created and initialized with an empty JSON object (`{}`) before running the script.
   - **Merged Data File (Output):**
     This file will be updated with newly merged test results and typically uses the same filename as the baseline file.
   - **Customization:** The file names and location are customizable via the command-line parameters when running the dashboard generation script.

2. **`generate_ci_dashboard.py`**
   - A Python script that:
     - **Fetches** new test results.
     - **Merges** them with the baseline JSON data.
     - **Generates** an HTML dashboard report.
   - **Command-line Parameters:**
     - `--pr_number`:
       Specifies which pull request’s test data to process. Accepts a specific PR number (e.g., `"105"`) or `"all"` to retrieve test data for all closed PRs.
     - `--baseline_data_file`:
       The filename of the baseline JSON data file (inside `output_dir`). This file must exist and contain valid JSON (e.g., `{}` for an empty file).
     - `--merged_data_file`:
       The filename for the updated (merged) JSON data file (inside `output_dir`).
     - `--dashboard_file`:
       The filename for the generated HTML dashboard (inside `output_dir`).
     - `--output_dir`:
       The directory where the baseline data file, updated JSON data file, and HTML dashboard file will be stored.

3. **`dashboard_requirements.txt`**
   - Lists the Python dependencies required by the project (e.g., `requests`).
   - **Installation:**
     ```bash
     pip install -r dashboard_requirements.txt
     ```

4. **Tests**
   - **Data Merge Tests:**
     Located in `tests/test_generate_data.py`, these tests verify that new test data is merged correctly with the baseline data while preventing duplicates.
   - **UI Generation Tests:**
     Located in `tests/test_generate_ui.py`, these tests ensure that the HTML dashboard correctly sorts bundle entries, formats timestamps, and applies the proper CSS classes.

---

## How to Use Locally

### 1. Install Dependencies

Install the required dependencies:
```bash
pip install -r requirements.txt
```

### 2. Prepare the Baseline Data File

Create your baseline data file in your output directory (for example, `output/nvidia_gpu_operator_data.json`) and initialize it with an empty JSON object:
```json
{}
```
This step is required; if the file is missing or contains invalid JSON, the script will crash.

### 3. Run the Tests

To run all tests, use the following command from the repository root:
```bash
PYTHONPATH=/ python -m unittest discover -s tests -p "test*.py"
```
You should see output similar to:
```
2025-04-02 19:41:47,059 [INFO] Saving JSON to /tmp/tmp2ebutk14/test_data.json
2025-04-02 19:41:47,059 [INFO] Data successfully saved to /tmp/tmp2ebutk14/test_data.json
.2025-04-02 19:41:47,060 [INFO] Saving JSON to /tmp/tmpkfzax0dw/test_data.json
...
----------------------------------------------------------------------
Ran 13 tests in 0.004s

OK
```

### 4. Run the Dashboard Generation Script

The `generate_ci_dashboard.py` script performs both JSON data merging and HTML generation. Run it using the following command:
```bash
py generate_ci_dashboard.py --pr_number "all" \
  --baseline_data_file "nvidia_gpu_operator_data.json" \
  --merged_data_file "nvidia_gpu_operator_data.json" \
  --dashboard_file "nvidia_gpu_operator_dashboard.html" \
  --output_dir "output"
```
This command will:
- **Fetch and merge new test results** with the baseline data from `nvidia_gpu_operator_data.json`.
- **Write the updated (merged) JSON** back to `nvidia_gpu_operator_data.json`.
- **Generate an HTML dashboard** (`nvidia_gpu_operator_dashboard.html`) in the specified output directory.

### 5. Open the Dashboard

After running the script, open the generated HTML file to view your dashboard:
```bash
xdg-open output/nvidia_gpu_operator_dashboard.html
```
*(On macOS, use `open` instead of `xdg-open`.)*

---

## GitHub Actions Workflow (CI/CD)

The GitHub Actions workflow automates the following steps:

1. **Determining the PR Number:**
   Uses the merged pull request’s number (or a manual input of `"all"`) to process test results.

2. **Code Checkout:**
   Checks out the code and the baseline data from the `gh-pages` branch.

3. **Preparing the Baseline Data:**
   Renames and moves the baseline JSON file so it can be used as the existing data file.

4. **Setting Up the Python Environment:**
   Uses the appropriate Python version and installs dependencies.

5. **Running the Dashboard Generation Script:**
   Executes `generate_ci_dashboard.py` with the proper parameters to update JSON data and generate the HTML dashboard.

6. **Deploying to GitHub Pages:**
   Publishes the updated HTML dashboard and JSON data to the `gh-pages` branch using the [JamesIves/github-pages-deploy-action](https://github.com/JamesIves/github-pages-deploy-action).

For more details, refer to the workflow file in the repository.

---
