
# NVIDIA GPU Operator Matrix – Dashboard

This directory contains the scripts, data, and supporting files used to **generate** and **deploy** a test matrix for the NVIDIA GPU Operator on Red Hat OpenShift.

---

## Contents
ignore this comment
1. **`output/ocp_data.json`**  
   - Stores summarized test results for various OCP versions and GPU Operator runs (including “bundle”/“master” runs and stable releases).  
   - Gets updated by the **`generate_test_matrix_data.py`** script, which fetches results from web APIs.

2. **`generate_test_matrix_data.py`**  
   - A Python script that **fetches** the latest test data and **generates** `ocp_data.json`.  
   - May be triggered by a GitHub Actions workflow whenever a pull request is merged.

3. **`generate_test_matrix_ui.py`**  
   - Reads `ocp_data.json` and **generates** an HTML dashboard summarizing pass/fail statuses across OCP versions, GPU Operator versions, etc.  
   - Outputs an **`index.html`** file (by default in `workflows/test_matrix_dashboard/output/index.html`).

4. **`requirements.txt`**  
   - Lists Python dependencies required by the above scripts (e.g., `requests`).  

   - Install them with:
     ```bash
     pip install -r requirements.txt
     ```


## How to Use Locally

1. **Install Dependencies**  
   ```bash
   pip install -r workflows/test_matrix_dashboard/requirements.txt
   ```
   *(Adjust the path if your `requirements.txt` is in a different location.)*

2. **First Run (No Previous Data)**  
   If you don’t have an existing `ocp_data.json`, you can pass any placeholder name to the `--old_data_file` parameter (even if it doesn’t exist yet):
   ```bash
   python workflows/test_matrix_dashboard/generate_test_matrix_data.py \
     --pr "95" \
     --output_dir "workflows/test_matrix_dashboard/output" \
     --old_data_file "old_ocp_data.json"
   ```

3. **Subsequent Runs (With Existing Data)**  
   If you already have data in `old_ocp_data.json` (for example, from a previous run):
   ```bash
   # Ensure the old data file is in the output directory:
   cp ocp_data.json workflows/test_matrix_dashboard/output/old_ocp_data.json

   # Then run:
   python workflows/test_matrix_dashboard/generate_test_matrix_data.py \
     --pr "105" \
     --output_dir "workflows/test_matrix_dashboard/output" \
     --old_data_file "old_ocp_data.json"
   ```

4. **Generate the UI**  
   After you have an updated `ocp_data.json`, generate the HTML dashboard:
   ```bash
   python workflows/test_matrix_dashboard/generate_test_matrix_ui.py \
     --output_dir "workflows/test_matrix_dashboard/output"
   ```
   This creates an `index.html` inside the specified output folder.

5. **Deploy**  
   If you use [gh-pages](https://www.npmjs.com/package/gh-pages) for deployment:
   ```bash
   gh-pages -d workflows/test_matrix_dashboard/output
   ```
   This publishes the `output` folder to the `gh-pages` branch on GitHub (or whichever branch you configure for GitHub Pages).

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

3. **Update Test Data**:
   ```bash
   python generate_test_matrix_data.py \
     --pr "105" \
     --old_data_file "old_ocp_data.json" \
     --output_dir "output"
   ```
   - This updates **`ocp_data.json`** with fresh results.
4. **Generate the HTML Dashboard**:
   ```bash
   python generate_test_matrix_ui.py --output_dir "output"
   ```
   - This writes **`index.html`** to **`output/index.html`**.
5. **Open the Dashboard**:
   ```bash
   xdg-open output/index.html
   ```
   *(Replace `xdg-open` with the appropriate command on your OS, such as `open` on macOS.)*

---

## GitHub Actions Workflow (CI/CD)

The following GitHub Actions workflow automates the process of updating test data, generating the dashboard UI, and deploying the updated pages to GitHub Pages. Below is an explanation of each step in the workflow.

### Workflow Explanation

**Workflow Name:** *Generate test matrices pages*

**Trigger Conditions:**
- **Pull Request (Closed on Main):** Runs when a pull request targeting the `main` branch is closed.
- **Manual Dispatch:** Can also be triggered manually with inputs for the branch to check out and the PR number to process.

**Job: generate-matrix**

1. **Determine PR Number:**  
   - *Purpose:* Sets the PR number to process.  
   - *Details:*  
     - If triggered by a pull request closure, the workflow sets `PR_NUMBER=all` to process all PR history.  
     - If manually triggered, it expects a valid PR number (or "all"). If none is provided, it errors out.

2. **Checkout Code:**  
   - *Purpose:* Retrieves the code for the triggering branch (or a specified branch).  
   - *Details:* Uses the `actions/checkout@v4` action to pull the code.

3. **Checkout Baseline (gh-pages):**  
   - *Purpose:* Checks out the `gh-pages` branch to obtain the current version of `ocp_data.json`.  
   - *Details:* Uses sparse checkout to retrieve only the `ocp_data.json` file, saving time and bandwidth.

4. **Rename and Move File:**  
   - *Purpose:* Prepares the baseline data by renaming `ocp_data.json` to `old_ocp_data.json`.  
   - *Details:* Copies the file from the baseline folder into the workspace with the new name.

5. **Set Up Python Environment:**  
   - *Purpose:* Ensures the correct Python version (3.13) is installed.  
   - *Details:* Uses the `actions/setup-python@v5` action.

6. **Install Dependencies:**  
   - *Purpose:* Installs all necessary Python libraries.  
   - *Details:* Runs `pip install -r workflows/test_matrix_dashboard/requirements.txt`.

7. **Run Extraction Script:**  
   - *Purpose:* Executes the script to extract test data and generate an updated `ocp_data.json`.  
   - *Details:* Runs `generate_test_matrix_data.py` with parameters including the PR number, output directory, and the old data file.

8. **Archive Old Data:**  
   - *Purpose:* Archives the old data file (`old_ocp_data.json`) by copying it into the output directory.  
   - *Details:* Ensures that the previous data is saved alongside the new data.

9. **Generate UI:**  
   - *Purpose:* Generates an updated HTML dashboard using the new `ocp_data.json`.  
   - *Details:* Executes `generate_test_matrix_ui.py` to create an `index.html` file.

10. **Deploy HTML to GitHub Pages:**  
    - *Purpose:* Publishes the generated HTML and updated data to GitHub Pages.  
    - *Details:* Uses the [JamesIves/github-pages-deploy-action](https://github.com/JamesIves/github-pages-deploy-action) to deploy the contents of the output directory to the `gh-pages` branch.
