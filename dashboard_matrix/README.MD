
# NVIDIA GPU Operator Matrix – Dashboard Directory

This **`dashboard_matrix/`** directory contains the scripts, data, and supporting files used to **generate** and **deploy** a test matrix for the NVIDIA GPU Operator on Red Hat OpenShift.

## Contents

1. **`ocp_data.json`**  
   - Stores the summarized test results for various OCP versions and GPU Operator runs (including “bundle”/“master” runs and stable releases).
   - Gets updated by the `generate_test_matrix_data.py` script, which fetches results from web API responses.

2. **`generate_test_matrix_data.py`**  
   - A Python script that **fetches** the latest test data and **updates** `ocp_data.json`.
   - May be triggered by a GitHub Actions workflow whenever a pull request is opened or merged.

3. **`generate_test_matrix_ui.py`**  
   - Reads `ocp_data.json` and **generates** an HTML dashboard, summarizing pass/fail statuses across OCP versions, GPU Operator versions, etc.
   - Outputs an **`index.html`** file (by default in `dashboard_matrix/output/index.html`).

4. **`requirements.txt`**  
   - Lists Python dependencies required by the above scripts (e.g. `requests`).
   - Install them with:
     ```bash
     pip install -r requirements.txt
     ```

5. **`output/`**  
   - Receives the generated HTML (`index.html`), so it can be deployed (e.g., to GitHub Pages).

6. **`store_data.py`**
   - Contains the data structures (`TestResults` dataclass) and helper functions for storing results in memory (`store_ocp_data()`) and saving them to JSON (`save_to_json()`).
   - Used by `generate_test_matrix_data.py` to aggregate and persist new test findings.

---

## How to Use Locally

1. **Navigate** to this directory:
   ```bash
   cd dashboard_matrix
   ```
2. **Install** Python dependencies:
   ```bash
   pip install -r requirements.txt
   ```
3. **Update test data** by running:
   ```bash
   python generate_test_matrix_data.py
   ```
   - This updates **`ocp_data.json`** with fresh results.
4. **Generate the HTML** dashboard:
   ```bash
   python generate_test_matrix_ui.py
   ```
   - Writes **`index.html`** to **`dashboard_matrix/output/index.html`**.
5. **Open**(from terminal) the generated `index.html` in any browser:
   ```bash
   xdg-open output/index.html
   ```
   (`xdg-open` is for linux – replace it with the relevant command on your system.)

---

## GitHub Actions Workflow (in CI/CD)

A typical GitHub Actions process here might look like this:

1. **Check out** the PR branch.  
2. **Run** `generate_test_matrix_data.py` to fetch new test results and update `ocp_data.json`.  
3. **Copy** the updated `ocp_data.json` somewhere safe (e.g., `/tmp/`) to avoid losing it when switching branches.  
4. **Check out** the `main` branch and pull the latest changes.  
5. **Overwrite** `ocp_data.json` on `main` with the updated copy from `/tmp/`, then commit and push.  
6. **Re-pull** `main` so the local workspace is fully up-to-date.  
7. **Generate** the HTML (`index.html`) by running `generate_test_matrix_ui.py` – output goes to `dashboard_matrix/output/`.  
8. **Deploy** whatever’s in `dashboard_matrix/output/` (including `index.html`) to a static host or GitHub Pages (e.g., via [JamesIves/github-pages-deploy-action](https://github.com/JamesIves/github-pages-deploy-action)).

This approach ensures each PR’s changes to `ocp_data.json` end up in `main`, and the final HTML page is updated automatically.

---

## Customizing

- **Paths**: If you want to generate HTML somewhere else, edit the scripts to use a different output folder.
- **Filtering**: For instance, if you only want to display SUCCESS runs in the main table, see where the script filters out statuses.
- **Styling**: CSS is inline in `generate_test_matrix_ui.py` for convenience—feel free to replace it or reference an external stylesheet if you prefer.

