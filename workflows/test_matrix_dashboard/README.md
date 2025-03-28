
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
   This publishes the `output` folder to the `gh-pages` branch on GitHub 

