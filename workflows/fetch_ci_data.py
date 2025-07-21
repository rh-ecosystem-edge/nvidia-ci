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
# Constants
# =============================================================================

# Expected number of slashes for top-level GPU operator E2E finished.json paths
# Format: pr-logs/pull/org/pr/job/build/finished.json (6 slashes)
EXPECTED_FINISHED_JSON_SLASH_COUNT = 6

# Maximum number of results per GCS API request for pagination
GCS_MAX_RESULTS_PER_REQUEST = 1000


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



def fetch_filtered_files(pr_number: str, glob_pattern: str) -> List[Dict[str, Any]]:
    """Fetch files matching a specific glob pattern for a PR."""
    logger.info(f"Fetching files matching pattern: {glob_pattern}")

    params = {
        "prefix": f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_number}/",
        "alt": "json",
        "matchGlob": glob_pattern,
        "maxResults": str(GCS_MAX_RESULTS_PER_REQUEST),
        "projection": "noAcl",
    }
    headers = {"Accept": "application/json"}

    all_items = []
    next_page_token = None

    # Handle pagination
    while True:
        if next_page_token:
            params["pageToken"] = next_page_token

        response_data = http_get_json(GCS_API_BASE_URL, params=params, headers=headers)
        items = response_data.get("items", [])
        all_items.extend(items)

        next_page_token = response_data.get("nextPageToken")
        if not next_page_token:
            break

    logger.info(f"Found {len(all_items)} files matching {glob_pattern}")
    return all_items

def fetch_pr_files(pr_number: str) -> Tuple[List[Dict[str, Any]], List[Dict[str, Any]], List[Dict[str, Any]]]:
    """Fetch all required file types for a PR using targeted filtering."""
    logger.info(f"Fetching files for PR #{pr_number}")

    # Fetch the 3 file types we need using glob patterns
    all_finished_files = fetch_filtered_files(pr_number, "**/finished.json")
    ocp_version_files = fetch_filtered_files(pr_number, "**/gpu-operator-e2e/artifacts/ocp.version")
    gpu_version_files = fetch_filtered_files(pr_number, "**/gpu-operator-e2e/artifacts/operator.version")

    return all_finished_files, ocp_version_files, gpu_version_files

def filter_gpu_finished_files(all_finished_files: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Filter to get only top-level finished.json files from GPU operator E2E builds."""
    finished_files = []
    for file_item in all_finished_files:
        path = file_item.get("name", "")
        # Must be a GPU operator E2E test and a top-level finished.json (not nested in artifacts)
        if ("nvidia-gpu-operator-e2e" in path and
            path.count('/') == EXPECTED_FINISHED_JSON_SLASH_COUNT and
            path.endswith('/finished.json')):
            finished_files.append(file_item)
    return finished_files

def build_single_file_lookup(
    file_items: List[Dict[str, Any]],
    all_builds: set
) -> Dict[Tuple[str, str], Dict[str, Any]]:
    """Build a lookup dictionary for a single file type.

    Args:
        file_items: List of file items from GCS API
        all_builds: Set that will be populated with (job_path, build_id) tuples as a side effect

    Returns:
        Dictionary mapping (job_path, build_id) to file item
    """
    lookup = {}

    for file_item in file_items:
        path = file_item.get("name", "")
        match = TEST_RESULT_PATH_REGEX.match(path)
        if not match:
            continue

        path_parts = path.split('/')
        if len(path_parts) < 6:
            continue

        build_id = path_parts[5]
        if build_id in ['latest-build.txt', 'latest-build']:
            continue

        job_path = '/'.join(path_parts[:5]) + '/'
        key = (job_path, build_id)
        lookup[key] = file_item
        all_builds.add(key)

    return lookup

def build_file_lookups(
    finished_files: List[Dict[str, Any]],
    ocp_version_files: List[Dict[str, Any]],
    gpu_version_files: List[Dict[str, Any]]
) -> Tuple[Dict[Tuple[str, str], Dict[str, Any]], Dict[Tuple[str, str], Dict[str, Any]], Dict[Tuple[str, str], Dict[str, Any]], set]:
    """Build lookup dictionaries for fast file access by (job_path, build_id)."""
    all_builds = set()

    finished_lookup = build_single_file_lookup(finished_files, all_builds)
    ocp_version_lookup = build_single_file_lookup(ocp_version_files, all_builds)
    gpu_version_lookup = build_single_file_lookup(gpu_version_files, all_builds)

    return finished_lookup, ocp_version_lookup, gpu_version_lookup, all_builds

def process_single_build(
    pr_number: str,
    job_path: str,
    build_id: str,
    finished_lookup: Dict[Tuple[str, str], Dict[str, Any]],
    ocp_version_lookup: Dict[Tuple[str, str], Dict[str, Any]],
    gpu_version_lookup: Dict[Tuple[str, str], Dict[str, Any]]
) -> TestResult:
    """Process a single build and return its test result."""
    # Extract OCP and GPU versions from job path
    match = TEST_RESULT_PATH_REGEX.match(job_path)
    if not match:
        raise ValueError(f"Invalid job path format: {job_path}")

    ocp_version = match.group("ocp_version")
    gpu_suffix = match.group("gpu_version")

    # Get build status and timestamp from finished.json
    key = (job_path, build_id)
    finished_file = finished_lookup[key]

    finished_content = fetch_gcs_file_content(finished_file['name'])
    finished_data = json.loads(finished_content)
    status = finished_data["result"]
    timestamp = finished_data["timestamp"]

    # Build prow job URL
    job_url = build_prow_job_url(pr_number, ocp_version, gpu_suffix, build_id)

    # Get exact versions if SUCCESS and files exist
    ocp_version_file = ocp_version_lookup.get(key)
    gpu_version_file = gpu_version_lookup.get(key)

    if status == "SUCCESS" and ocp_version_file and gpu_version_file:
        exact_ocp = fetch_gcs_file_content(ocp_version_file['name']).strip()
        exact_gpu_version = fetch_gcs_file_content(gpu_version_file['name']).strip()
        result = TestResult(exact_ocp, exact_gpu_version, status, job_url, timestamp)
    else:
        # Use base versions
        result = TestResult(ocp_version, gpu_suffix, status, job_url, timestamp)

    return result

def process_tests_for_pr(pr_number: str, results_by_ocp: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all jobs under a single PR using targeted file filtering."""
    logger.info(f"Fetching targeted test data for PR #{pr_number} using filtered requests")

    # Step 1: Fetch all required files
    all_finished_files, ocp_version_files, gpu_version_files = fetch_pr_files(pr_number)

    # Step 2: Filter to get only GPU operator E2E finished.json files
    finished_files = filter_gpu_finished_files(all_finished_files)

    # Step 3: Build lookup dictionaries for fast access
    finished_lookup, ocp_version_lookup, gpu_version_lookup, all_builds = build_file_lookups(
        finished_files, ocp_version_files, gpu_version_files)

    logger.info(f"Found {len(all_builds)} unique job/build combinations from filtered files")

    # Step 4: Process each job/build combination
    for job_path, build_id in sorted(all_builds):
        # Extract OCP version for logging
        match = TEST_RESULT_PATH_REGEX.match(job_path)
        ocp_version = match.group("ocp_version")
        gpu_suffix = match.group("gpu_version")

        logger.info(f"Processing build {build_id} for {ocp_version} + {gpu_suffix}")

        result = process_single_build(
            pr_number, job_path, build_id,
            finished_lookup, ocp_version_lookup, gpu_version_lookup)

        results_by_ocp.setdefault(ocp_version, []).append(result.to_dict())
        logger.info(f"Added result for build {build_id}: {result.test_status}")

    logger.info(f"Successfully processed {len(all_builds)} builds using targeted filtering")

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
    existing_results: Dict[str, Dict[str, Any]] = None
) -> None:
    file_path = output_file
    logger.info(f"Saving JSON to {file_path}")
    merged_results = existing_results.copy() if existing_results else {}
    for key, new_values in new_results.items():
        merged_results.setdefault(key, {"notes": [], "tests": []})
        merged_results[key].setdefault("tests", [])
        seen_keys = set(TestResult(**item).composite_key() for item in merged_results[key]["tests"])
        for item in new_values:
            key_instance = TestResult(**item).composite_key()
            if key_instance not in seen_keys:
                merged_results[key]["tests"].append(item)
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
        existing_results: Dict[str, Dict[str, Any]] = json.load(f)
    logger.info(f"Loaded baseline data from: {args.baseline_data_filepath} with keys: {list(existing_results.keys())}")

    local_results: Dict[str, List[Dict[str, Any]]] = {}
    if args.pr_number.lower() == "all":
        process_closed_prs(local_results)
    else:
        process_tests_for_pr(args.pr_number, local_results)
    merge_and_save_results(local_results, args.merged_data_filepath, existing_results=existing_results)

if __name__ == "__main__":
    main()
