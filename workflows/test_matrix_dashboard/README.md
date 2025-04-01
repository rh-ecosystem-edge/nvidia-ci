#ignore this line adding it to test an issue regarding updating the upstream main
# NVIDIA GPU Operator Matrix – Dashboard

This directory contains the scripts, data, and supporting files used to **generate** and **deploy** a test matrix for the NVIDIA GPU Operator on Red Hat OpenShift.

> **Note:** The JSON output file (e.g. `output/ocp_data.json`) and HTML file names are not fixed. You can specify custom filenames using the command-line parameters (`--old_data_file`, `--new_data_file`, `--data_file`, and `--output_file`) when running the scripts.

> **Important:** Before running the data generation script, **create the old data file** (e.g., `old.json`) in your output directory and initialize it with an empty JSON object `{}`. If this file does not exist or is not properly formatted, the code will crash.

---

## Contents

1. **Data JSON File**
   - **Default Example:** `output/ocp_data.json`
   - **Purpose:** Stores summarized test results for various OCP versions and GPU Operator runs (including “bundle”/“master” runs and stable releases).
   - **Customization:** The file name and location are customizable via the terminal parameters.

2. **`generate_test_matrix_data.py`**
   - A Python script that **fetches** the latest test data and **merges** it with existing data.
   - **New Requirement:** You must now provide both `--old_data_file` (for existing data) and `--new_data_file` (for the updated JSON).
   - **PR Parameter:** The `--pr` parameter accepts either a specific PR number (e.g., `"105"`) or `"all"` to retrieve data for all closed PRs.
   - May be triggered by a GitHub Actions workflow whenever a pull request is merged.

3. **`generate_test_matrix_ui.py`**
   - Reads the JSON data file (e.g. `new.json`) and **generates** an HTML dashboard summarizing pass/fail statuses across OCP and GPU Operator versions.
   - **New Requirement:** You must now supply `--data_file` (the JSON filename) and `--output_file` (the HTML filename) along with the output directory.

4. **`requirements.txt`**
   - Lists Python dependencies required by the above scripts (e.g., `requests`).
   - Install them with:
     ```bash
     pip install -r requirements.txt
     ```

---

## How to Use Locally

1. **Install Dependencies**
   ```bash
   pip install -r workflows/test_matrix_dashboard/requirements.txt
   ```
   *(Adjust the path if your `requirements.txt` is in a different location.)*

2. **Prepare the Old Data File**
   Ensure that the old data file exists before running the script. For example, if you plan to use `old.json` as your old data file, create it in your output directory with the following content:
   ```json
   {}
   ```
   This step is crucial—if the file does not exist or does not contain valid JSON, the script will crash.

3. **Update Test Data**
   Run the following command to update the test data. This command fetches new results, merges them with the existing data from `old.json`, and writes the merged results to `new.json` (you can choose any file name).
   **Note:** The `--pr` parameter accepts either a specific PR number (e.g., `"105"`) or `"all"` to retrieve test data for all closed PRs.
   ```bash
   python workflows/test_matrix_dashboard/generate_test_matrix_data.py \
     --pr "all" \
     --output_dir "workflows/test_matrix_dashboard/output" \
     --old_data_file "old.json" \
     --new_data_file "new.json"
   ```

4. **Generate the UI**
   After updating the data, generate the HTML dashboard by running:
   ```bash
   python workflows/test_matrix_dashboard/generate_test_matrix_ui.py \
     --output_dir "workflows/test_matrix_dashboard/output" \
     --data_file "new.json" \
     --output_file "x.html"
   ```
   This creates the HTML dashboard (`x.html`) in the specified output folder.
   *(You can customize the file names as needed.)*

5. **Open the Dashboard**
   To view the generated dashboard:
   ```bash
   xdg-open workflows/test_matrix_dashboard/output/x.html
   ```
   *(Replace `xdg-open` with the appropriate command on your OS, such as `open` on macOS.)*

---

## Local Workflow Example

1. **Navigate** to the directory:
   ```bash
   cd workflows/test_matrix_dashboard
   ```

2. **Install** Python dependencies:
   ```bash
   pip install -r requirements.txt
   ```

3. **Prepare the Old Data File**
   Create `old.json` in your `output` folder with the content:
   ```json
   {}
   ```

4. **Update Test Data**:
   ```bash
   python generate_test_matrix_data.py \
     --pr "all" \
     --old_data_file "old.json" \
     --new_data_file "new.json" \
     --output_dir "output"
   ```
   - This updates **`new.json`** with fresh results merged with the old data.

5. **Generate the HTML Dashboard**:
   ```bash
   python generate_test_matrix_ui.py \
     --output_dir "output" \
     --data_file "new.json" \
     --output_file "x.html"
   ```
   - This writes **`x.html`** to **`output/x.html`**.

6. **Open the Dashboard**:
   ```bash
   xdg-open output/x.html
   ```
   *(Replace `xdg-open` with the appropriate command on your OS, such as `open` on macOS.)*

---

## GitHub Actions Workflow (CI/CD)

The GitHub Actions workflow automates the process of updating test data, generating the dashboard UI, and deploying the updated pages to GitHub Pages. Below is an explanation of each step in the updated workflow.

### Workflow Explanation

**Workflow Name:** *Generate test matrices pages*

**Trigger Conditions:**
- **Pull Request (Closed on Main):**
  The workflow is triggered when a pull request targeting the `main` branch is closed. In this case, the PR number of the merged pull request is used.
- **Manual Dispatch:**
  The workflow can also be manually triggered via `workflow_dispatch`. When triggered this way, you must provide inputs for the branch to check out and the `pr_number`. The `pr_number` input can be either a specific PR number (e.g., `"105"`) or `"all"` to process all closed PRs.

**Job: generate-matrix**

1. **Determine PR Number:**
   - *Purpose:* Sets the PR number to process.
   - *Details:*
     - If triggered by a pull request closure, the workflow automatically extracts the merged pull request’s number.
     - If triggered manually, the workflow uses the provided `pr_number` input—which may be a specific PR number or `"all"`.

2. **Checkout Code:**
   - *Purpose:* Retrieves the code for the branch specified (either the triggering branch or the branch provided via inputs).
   - *Details:* Uses the `actions/checkout@v4` action to pull the code from the appropriate branch.

3. **Checkout Baseline (gh-pages):**
   - *Purpose:* Checks out the `gh-pages` branch to obtain the current version of the JSON data file.
   - *Details:* Uses the `actions/checkout@v4` action with a separate `path` (named `baseline`) to fetch the baseline file efficiently.

4. **Rename and Move File:**
   - *Purpose:* Prepares the baseline data by renaming the JSON data file (e.g., from `ocp_data.json`) to the name expected by the data generation script (e.g., `old_ocp_data.json`).
   - *Details:* Copies the file from the `baseline` directory into the output directory, ensuring that the extraction script can use it as the old data file.

5. **Set Up Python Environment:**
   - *Purpose:* Installs the required Python version (3.13).
   - *Details:* Uses the `actions/setup-python@v5` action to set up the Python environment.

6. **Install Dependencies:**
   - *Purpose:* Installs all necessary Python libraries.
   - *Details:* Runs `pip install -r workflows/test_matrix_dashboard/requirements.txt`.

7. **Run Extraction Script:**
   - *Purpose:* Extracts test data and generates an updated JSON file.
   - *Details:* Runs `generate_test_matrix_data.py` with the following parameters:
     - The PR number determined earlier (via `--pr`),
     - The output directory,
     - The old data file (e.g., `old_ocp_data.json`),
     - And the new data file (e.g., `ocp_data.json`).

8. **Generate UI:**
   - *Purpose:* Generates an updated HTML dashboard using the new JSON data.
   - *Details:* Runs `generate_test_matrix_ui.py` with parameters for the output directory, the JSON file (specified via `--data_file`), and the output HTML filename (specified via `--output_file`).

9. **Deploy HTML to GitHub Pages:**
   - *Purpose:* Publishes the generated HTML and updated data to GitHub Pages.
   - *Details:* Uses the [JamesIves/github-pages-deploy-action](https://github.com/JamesIves/github-pages-deploy-action) to deploy the contents of the output directory to the `gh-pages` branch.
