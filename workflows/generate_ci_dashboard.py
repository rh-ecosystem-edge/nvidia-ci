#!/usr/bin/env python
import argparse
import json
import os
import urllib.parse
from datetime import datetime, timezone
from dataclasses import dataclass
from typing import Any, Dict, List, Tuple

import semver
import requests

from pydantic import BaseModel
from utils import logger, GCS_API_BASE_URL, TEST_RESULT_PATH_REGEX, version2suffix

# =============================================================================
# HTML Report Generation Functions
# =============================================================================

def load_template(filename: str) -> str:
    """
    Load and return the contents of a template file.
    Uses an absolute path based on the script's location.
    """
    script_dir = os.path.dirname(os.path.abspath(__file__))
    file_path = os.path.join(script_dir, "templates", filename)
    with open(file_path, 'r', encoding='utf-8') as f:
        return f.read()

def build_catalog_table_rows(regular_results: List[Dict[str, Any]]) -> str:
    """
    Build the <tr> rows for the table, grouped by the full OCP version.
    For each OCP version group, deduplicate by GPU version (keeping only the entry with the latest timestamp)
    and create clickable GPU version links.
    """
    grouped = {}
    for result in regular_results:
        # Group using the new field name.
        ocp_full = result["ocp_full_version"]
        grouped.setdefault(ocp_full, []).append(result)

    rows_html = ""
    for ocp_full in sorted(grouped.keys(), reverse=True):
        rows = grouped[ocp_full]
        deduped = {}
        for row in rows:
            gpu = row["gpu_operator_version"]
            # Use the new key for the timestamp.
            if gpu not in deduped or row["job_timestamp"] > deduped[gpu]["job_timestamp"]:
                deduped[gpu] = row

        deduped_rows = list(deduped.values())
        sorted_rows = sorted(
            deduped_rows,
            key=lambda r: semver.VersionInfo.parse(r["gpu_operator_version"].split("(")[0]),
            reverse=True
        )
        gpu_links = ", ".join(
            f'<a href="{r["prow_job_url"]}" target="_blank">{r["gpu_operator_version"]}</a>'
            for r in sorted_rows
        )
        rows_html += f"""
        <tr>
          <td style="min-width:150px; white-space:nowrap;">{ocp_full}</td>
          <td>{gpu_links}</td>
        </tr>
        """
    return rows_html

def build_bundle_info(bundle_results: List[Dict[str, Any]]) -> str:
    """
    Build a small HTML snippet that displays info about GPU bundle statuses
    (shown in a 'history-bar' with colored squares).
    """
    if not bundle_results:
        return ""
    sorted_bundles = sorted(bundle_results, key=lambda r: r["job_timestamp"], reverse=True)
    leftmost_bundle = sorted_bundles[0]
    last_bundle_date = datetime.fromtimestamp(int(leftmost_bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    bundle_html = f"""
  <div style="margin-top: 20px; font-size: 0.9em; color: #009688; padding: 10px; border-radius: 4px;">
    <strong>From main branch (OLM bundle)</strong>
  </div>
  <div class="history-bar" style="opacity: 0.7;">
    <div style="margin-top: 5px;">
      <strong>Last Bundle Job Date:</strong> {last_bundle_date}
    </div>
    """
    for bundle in sorted_bundles:
        status = bundle.get("test_status", "Unknown").upper()
        if status == "SUCCESS":
            status_class = "history-success"
        elif status == "FAILURE":
            status_class = "history-failure"
        else:
            status_class = "history-aborted"
        bundle_timestamp = datetime.fromtimestamp(int(bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        bundle_html += f"""
    <div class='history-square {status_class}'
         onclick='window.open("{bundle["prow_job_url"]}", "_blank")'
         title='Status: {status} | Timestamp: {bundle_timestamp}'>
    </div>
        """
    bundle_html += "</div>"
    return bundle_html

def generate_test_matrix(ocp_data: Dict[str, List[Dict[str, Any]]]) -> str:
    """
    Build the final HTML report by:
      1. Reading the header template,
      2. Generating the table blocks for each OCP version,
      3. Reading the footer template and injecting the last-updated time.
    """
    header_template = load_template("header.html")
    html_content = header_template

    main_table_template = load_template("main_table.html")
    sorted_ocp_keys = sorted(ocp_data.keys(), reverse=True)
    for ocp_key in sorted_ocp_keys:
        results = ocp_data[ocp_key]
        regular_results = [
            r for r in results
            if ("bundle" not in r["gpu_operator_version"].lower())
               and ("master" not in r["gpu_operator_version"].lower())
               and (r.get("test_status") == "SUCCESS")
        ]
        bundle_results = [r for r in results if r not in regular_results]
        table_rows_html = build_catalog_table_rows(regular_results)
        bundle_info_html = build_bundle_info(bundle_results)
        table_block = main_table_template
        table_block = table_block.replace("{ocp_key}", ocp_key)
        table_block = table_block.replace("{table_rows}", table_rows_html)
        table_block = table_block.replace("{bundle_info}", bundle_info_html)
        html_content += table_block

    footer_template = load_template("footer.html")
    now_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    footer_template = footer_template.replace("{LAST_UPDATED}", now_str)
    html_content += footer_template
    return html_content

# =============================================================================
# Data Fetching & JSON Update Functions
# =============================================================================

def http_get_json(url: str, params: Dict[str, Any] = None, headers: Dict[str, str] = None) -> Dict[str, Any]:
    """Send an HTTP GET request and return the JSON response."""
    response = requests.get(url, params=params, headers=headers)
    response.raise_for_status()
    return response.json()

def fetch_gcs_file_content(file_path: str) -> str:
    """Fetch the raw text content from a file in GCS."""
    logger.info(f"Fetching file content for {file_path}")
    response = requests.get(
        url=f"{GCS_API_BASE_URL}/{urllib.parse.quote_plus(file_path)}",
        params={"alt": "media"},
    )
    response.raise_for_status()
    return response.content.decode("UTF-8")

def build_prow_job_url(pr_number: str, ocp_minor: str, gpu_suffix: str, job_id: str) -> str:
    """Build the Prow job URL for the given PR, OCP version, GPU suffix, and job ID."""
    return (
        f"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/"
        f"pull/rh-ecosystem-edge_nvidia-ci/{pr_number}/pull-ci-rh-ecosystem-edge-nvidia-ci-main-"
        f"{ocp_minor}-stable-nvidia-gpu-operator-e2e-{gpu_suffix}/{job_id}"
    )

def fetch_build_versions(job_prefix: str, build_id: str, gpu_version_suffix: str) -> Tuple[str, str]:
    """Fetch the exact OCP and GPU versions for a given build."""
    logger.info(f"Fetching versions for build {build_id}")
    ocp_version_file = (
        f"{job_prefix}{build_id}/artifacts/nvidia-gpu-operator-e2e-{gpu_version_suffix}/"
        "gpu-operator-e2e/artifacts/ocp.version"
    )
    gpu_version_file = (
        f"{job_prefix}{build_id}/artifacts/nvidia-gpu-operator-e2e-{gpu_version_suffix}/"
        "gpu-operator-e2e/artifacts/operator.version"
    )
    ocp_version = fetch_gcs_file_content(ocp_version_file)
    gpu_version = fetch_gcs_file_content(gpu_version_file)
    return ocp_version, gpu_version

def fetch_build_status_and_timestamp(job_prefix: str, build_id: str) -> Tuple[str, Any]:
    """Fetch the status (SUCCESS/FAILURE/UNKNOWN) and timestamp for a given build."""
    logger.info(f"Fetching status for build {build_id}")
    finished_file = f"{job_prefix}{build_id}/finished.json"
    url = f"{GCS_API_BASE_URL}/{urllib.parse.quote_plus(finished_file)}"
    data = http_get_json(url, params={"alt": "media"})
    status = data.get("result", "UNKNOWN")
    timestamp = data.get("timestamp", None)
    return status, timestamp

# --- Pydantic Model and Domain Model for Test Results ---

class TestResultKey(BaseModel):
    ocp_full_version: str
    gpu_operator_version: str
    test_status: str
    prow_job_url: str
    job_timestamp: str
    class Config:
        frozen = True

@dataclass(frozen=True)
class TestResult:
    """Represents a single test run result."""
    ocp_full_version: str
    gpu_operator_version: str
    test_status: str
    prow_job_url: str
    job_timestamp: str

    def to_dict(self) -> Dict[str, Any]:
        return {
            "ocp_full_version": self.ocp_full_version,
            "gpu_operator_version": self.gpu_operator_version,
            "test_status": self.test_status,
            "prow_job_url": self.prow_job_url,
            "job_timestamp": self.job_timestamp,
        }

    def composite_key(self) -> TestResultKey:
        return TestResultKey(
            ocp_full_version=self.ocp_full_version,
            gpu_operator_version=self.gpu_operator_version,
            test_status=self.test_status,
            prow_job_url=self.prow_job_url,
            job_timestamp=str(self.job_timestamp)
        )

def process_job_result(
    pr_number: str,
    job_prefix: str,
    ocp_full_version: str,
    gpu_version_suffix: str,
    results_by_ocp: Dict[str, List[Dict[str, Any]]]
) -> None:
    """Fetch and store results for a specific job prefix."""
    logger.info(f"Processing job results for {job_prefix}")
    params = {
        "prefix": job_prefix,
        "alt": "json",
        "delimiter": "/",
        "includeFoldersAsPrefixes": "True",
        "maxResults": "1000",
        "projection": "noAcl",
    }
    headers = {"Accept": "application/json"}
    response_data = http_get_json(GCS_API_BASE_URL, params=params, headers=headers)
    latest_build = fetch_gcs_file_content(response_data["items"][0]["name"])
    logger.info(f"Latest build for {job_prefix}: {latest_build}")

    status, timestamp = fetch_build_status_and_timestamp(job_prefix, latest_build)
    job_url = build_prow_job_url(pr_number, ocp_full_version, gpu_version_suffix, latest_build)

    if status == "SUCCESS":
        exact_ocp, exact_gpu_version = fetch_build_versions(job_prefix, latest_build, gpu_version_suffix)
        result = TestResult(exact_ocp, exact_gpu_version, status, job_url, timestamp)
    else:
        converted_gpu = version2suffix(gpu_version_suffix)
        result = TestResult(ocp_full_version, converted_gpu, status, job_url, timestamp)
    results_by_ocp.setdefault(ocp_full_version, []).append(result.to_dict())

def process_tests_for_pr(pr_number: str, results_by_ocp: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all jobs under a single PR."""
    logger.info(f"Fetching tests for PR #{pr_number}")
    params = {
        "prefix": f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_number}/",
        "alt": "json",
        "delimiter": "/",
        "includeFoldersAsPrefixes": "True",
        "maxResults": "1000",
        "projection": "noAcl",
    }
    headers = {"Accept": "application/json"}
    response_data = http_get_json(GCS_API_BASE_URL, params=params, headers=headers)
    job_prefixes = response_data.get("prefixes", [])
    for job_prefix in job_prefixes:
        match = TEST_RESULT_PATH_REGEX.match(job_prefix)
        if not match:
            continue
        ocp_full = match.group("ocp_version")
        gpu_suffix = match.group("gpu_version")
        process_job_result(pr_number, job_prefix, ocp_full, gpu_suffix, results_by_ocp)

def process_closed_prs(results_by_ocp: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all closed PRs against the main branch."""
    logger.info("Retrieving PR history...")
    url = "https://api.github.com/repos/rh-ecosystem-edge/nvidia-ci/pulls"
    params = {"state": "closed", "base": "main", "per_page": "100", "page": "1"}
    headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28"
    }
    response_data = http_get_json(url, params=params, headers=headers)
    for pr in response_data:
        pr_number = str(pr["number"])
        logger.info(f"Processing PR #{pr_number}")
        process_tests_for_pr(pr_number, results_by_ocp)

def merge_and_save_results(
    new_results: Dict[str, List[Dict[str, Any]]],
    output_directory: str,
    output_file: str,
    existing_results: Dict[str, List[Dict[str, Any]]] = None
) -> None:
    file_path = os.path.join(output_directory, output_file)
    logger.info(f"Saving JSON to {file_path}")
    merged_results = existing_results.copy() if existing_results else {}
    for key, new_values in new_results.items():
        merged_results.setdefault(key, [])
        seen_keys = set(TestResult(**item).composite_key() for item in merged_results[key])
        for item in new_values:
            key_instance = TestResult(**item).composite_key()
            if key_instance not in seen_keys:
                merged_results[key].append(item)
                seen_keys.add(key_instance)
    with open(file_path, "w") as f:
        json.dump(merged_results, f, indent=4)
    logger.info(f"Data successfully saved to {file_path}")

# =============================================================================
# Main Workflow: Update JSON and Generate HTML
# =============================================================================

def main() -> None:
    parser = argparse.ArgumentParser(description="Test Matrix Utility")
    parser.add_argument("--pr_number", default="all",
                        help="PR number to process; use 'all' for full history")
    parser.add_argument("--baseline_data_file", required=True,
                        help="Name of the baseline JSON file (inside output_dir)")
    parser.add_argument("--merged_data_file", required=True,
                        help="Name of the updated (merged) JSON file (inside output_dir)")
    parser.add_argument("--dashboard_file", required=True,
                        help="Name of the generated HTML dashboard file (inside output_dir)")
    parser.add_argument("--output_dir", required=True,
                        help="Directory to store the JSON and HTML files")
    args = parser.parse_args()

    # Update JSON data.
    baseline_data_path = os.path.join(args.output_dir, args.baseline_data_file)
    with open(baseline_data_path, "r") as f:
        existing_results: Dict[str, List[Dict[str, Any]]] = json.load(f)
    logger.info(f"Loaded baseline data from: {baseline_data_path} with keys: {list(existing_results.keys())}")

    local_results: Dict[str, List[Dict[str, Any]]] = {}
    if args.pr_number.lower() == "all":
        process_closed_prs(local_results)
    else:
        process_tests_for_pr(args.pr_number, local_results)
    merge_and_save_results(local_results, args.output_dir, args.merged_data_file, existing_results=existing_results)

    # Generate HTML report from updated JSON data.
    merged_data_path = os.path.join(args.output_dir, args.merged_data_file)
    with open(merged_data_path, "r") as f:
        ocp_data = json.load(f)
    logger.info(f"Loaded JSON data with keys: {list(ocp_data.keys())} from {merged_data_path}")

    html_content = generate_test_matrix(ocp_data)
    dashboard_path = os.path.join(args.output_dir, args.dashboard_file)
    with open(dashboard_path, "w", encoding="utf-8") as f:
        f.write(html_content)
    logger.info(f"Matrix dashboard generated: {dashboard_path}")

if __name__ == "__main__":
    main()
