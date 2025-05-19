#!/usr/bin/env python
import argparse
import json
import urllib.parse
from dataclasses import dataclass
from typing import Any, Dict, List, Tuple

import requests
from pydantic import BaseModel

from utils import logger, GCS_API_BASE_URL, TEST_RESULT_PATH_REGEX, version2suffix


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
    output_file: str,
    existing_results: Dict[str, List[Dict[str, Any]]] = None
) -> None:
    file_path = output_file
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
# Main Workflow: Update JSON
# =============================================================================

def main() -> None:
    parser = argparse.ArgumentParser(description="Test Matrix Utility")
    parser.add_argument("--pr_number", default="all",
                        help="PR number to process; use 'all' for full history")
    parser.add_argument("--baseline_data_filepath", required=True,
                        help="Path to the baseline data file")
    parser.add_argument("--merged_data_filepath", required=True,
                        help="Path to the updated (merged) data file")
    args = parser.parse_args()

    # Update JSON data.
    with open(args.baseline_data_filepath, "r") as f:
        existing_results: Dict[str, List[Dict[str, Any]]] = json.load(f)
    logger.info(f"Loaded baseline data from: {args.baseline_data_filepath} with keys: {list(existing_results.keys())}")

    local_results: Dict[str, List[Dict[str, Any]]] = {}
    if args.pr_number.lower() == "all":
        process_closed_prs(local_results)
    else:
        process_tests_for_pr(args.pr_number, local_results)
    merge_and_save_results(local_results, args.merged_data_filepath, existing_results=existing_results)

if __name__ == "__main__":
    main()
