#!/usr/bin/env python
import argparse
import json
import re
import urllib.parse
from dataclasses import dataclass
from typing import Any, Dict, List, Optional, Tuple, Set

import requests
from pydantic import BaseModel
import semver

from workflows.common.utils import logger


# Constants for version field names
OCP_FULL_VERSION = "ocp_full_version"
GPU_OPERATOR_VERSION = "gpu_operator_version"


# =============================================================================
# Constants
# =============================================================================

GCS_API_BASE_URL = "https://storage.googleapis.com/storage/v1/b/test-platform-results/o"

# Regular expression to match test result paths.
TEST_RESULT_PATH_REGEX = re.compile(
    r"pr-logs/pull/(?P<repo>[^/]+)/(?P<pr_number>\d+)/"
    r"(?P<job_name>(?:rehearse-\d+-)?pull-ci-rh-ecosystem-edge-nvidia-ci-main-"
    r"(?P<ocp_version>\d+\.\d+)-stable-nvidia-gpu-operator-e2e-(?P<gpu_version>\d+-\d+-x|master))/"
    r"(?P<build_id>[^/]+)"
)

# Maximum number of results per GCS API request for pagination
GCS_MAX_RESULTS_PER_REQUEST = 1000


# =============================================================================
# Data Fetching & JSON Update Functions
# =============================================================================

def http_get_json(url: str, params: Dict[str, Any] = None, headers: Dict[str, str] = None) -> Dict[str, Any]:
    """Send an HTTP GET request and return the JSON response."""
    response = requests.get(url, params=params, headers=headers, timeout=30)
    response.raise_for_status()
    return response.json()


def fetch_gcs_file_content(file_path: str) -> str:
    """Fetch the raw text content from a file in GCS."""
    logger.info(f"Fetching file content for {file_path}")
    response = requests.get(
        url=f"{GCS_API_BASE_URL}/{urllib.parse.quote_plus(file_path)}",
        params={"alt": "media"},
        timeout=30,
    )
    response.raise_for_status()
    return response.content.decode("UTF-8")


def build_prow_job_url(finished_json_path: str) -> str:
    directory_path = finished_json_path[:-len('/finished.json')]
    return f"https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/{directory_path}"


# --- Pydantic Model and Domain Model for Test Results ---


class TestResultKey(BaseModel):
    ocp_full_version: str
    gpu_operator_version: str
    test_status: str
    pr_number: str
    job_name: str
    build_id: str

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
            OCP_FULL_VERSION: self.ocp_full_version,
            GPU_OPERATOR_VERSION: self.gpu_operator_version,
            "test_status": self.test_status,
            "prow_job_url": self.prow_job_url,
            "job_timestamp": self.job_timestamp,
        }

    def composite_key(self) -> TestResultKey:
        repo, pr_number, job_name, build_id = extract_build_components(self.prow_job_url)
        return TestResultKey(
            ocp_full_version=self.ocp_full_version,
            gpu_operator_version=self.gpu_operator_version,
            test_status=self.test_status,
            pr_number=pr_number,
            job_name=job_name,
            build_id=build_id
        )

    def build_key(self) -> Tuple[str, str, str]:
        """Get the PR number, job name and build ID for deduplication purposes."""
        repo, pr_number, job_name, build_id = extract_build_components(self.prow_job_url)
        return (pr_number, job_name, build_id)

    def has_exact_versions(self) -> bool:
        """Check if this result has exact semantic versions (not base versions from URL)."""
        try:
            ocp = self.ocp_full_version
            gpu = self.gpu_operator_version.split("(")[0].strip()
            semver.VersionInfo.parse(ocp)
            semver.VersionInfo.parse(gpu)
        except (ValueError, TypeError):
            return False
        else:
            return True


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

        response_data = http_get_json(
            GCS_API_BASE_URL, params=params, headers=headers)
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
    ocp_version_files = fetch_filtered_files(
        pr_number, "**/gpu-operator-e2e/artifacts/ocp.version")
    gpu_version_files = fetch_filtered_files(
        pr_number, "**/gpu-operator-e2e/artifacts/operator.version")

    return all_finished_files, ocp_version_files, gpu_version_files


def extract_build_components(path: str) -> Tuple[str, str, str, str]:
    """Extract build components (repo, pr_number, job_name, build_id) from URL or file path.

    Args:
        path: File path or URL (e.g., "pr-logs/.../build-id/..." or "pr-logs/.../build-id/artifacts/...")

    Returns:
        Tuple of (repo, pr_number, job_name, build_id)

    Raises:
        ValueError: If path doesn't match expected pattern
    """
    # For nested files, get base path by removing everything after build_id
    original_path = path
    if '/artifacts/' in path:
        path = path.split('/artifacts/')[0] + '/'

    # Search for our pattern (works with both paths and full URLs)
    match = TEST_RESULT_PATH_REGEX.search(path)
    if not match:
        msg = "GPU operator path regex mismatch" if "nvidia-gpu-operator-e2e" in original_path else "Unexpected path format"
        raise ValueError(msg)

    # Extract values directly from regex groups
    repo = match.group("repo")
    pr_number = match.group("pr_number")
    job_name = match.group("job_name")
    build_id = match.group("build_id")

    return (repo, pr_number, job_name, build_id)


def filter_gpu_finished_files(all_finished_files: List[Dict[str, Any]]) -> Tuple[List[Dict[str, Any]], Dict[Tuple[str, str, str], Dict[str, Dict[str, Any]]]]:
    """Filter GPU operator E2E finished.json files, preferring nested when available.

    For each build, returns the preferred finished.json file:
    - If nested finished.json exists (artifacts/nvidia-gpu-operator-e2e-{gpu_suffix}/gpu-operator-e2e/finished.json), use it
    - Otherwise, use top-level finished.json

    The prow job URL is derived directly from the returned file path, eliminating the need for separate metadata.

    Returns:
        Tuple of (preferred_files, dual_builds_info)
        - preferred_files: List of preferred finished.json file items for each build
        - dual_builds_info: Dict mapping build_key to {'nested': file_item, 'top_level': file_item}
                           for builds that have both nested and top-level finished.json files
    """
    preferred_files = {}  # {build_key: (file_item, is_nested)}
    all_build_files = {}  # {build_key: {'nested': file_item, 'top_level': file_item}}

    for file_item in all_finished_files:
        path = file_item.get("name", "")

        # Check if it's a GPU operator E2E finished.json file
        if not ("nvidia-gpu-operator-e2e" in path and path.endswith('/finished.json')):
            continue

        # Determine file type and extract build key
        is_nested = '/artifacts/nvidia-gpu-operator-e2e-' in path and '/gpu-operator-e2e/finished.json' in path
        is_top_level = not is_nested and '/artifacts/' not in path

        if not (is_nested or is_top_level):
            continue

        try:
            repo, pr_number, job_name, build_id = extract_build_components(path)
            build_key = (pr_number, job_name, build_id)
        except ValueError:
            continue

        # Track all files for each build
        if build_key not in all_build_files:
            all_build_files[build_key] = {}

        if is_nested:
            all_build_files[build_key]['nested'] = file_item
        else:
            all_build_files[build_key]['top_level'] = file_item

        # Store file, preferring nested over top-level
        if build_key not in preferred_files or is_nested:
            preferred_files[build_key] = (file_item, is_nested)

    # Extract file items and find builds with both nested and top-level files
    result = [file_item for file_item, _ in preferred_files.values()]
    dual_builds = {k: v for k, v in all_build_files.items()
                   if 'nested' in v and 'top_level' in v}

    return result, dual_builds


def build_files_lookup(
    finished_files: List[Dict[str, Any]],
    ocp_version_files: List[Dict[str, Any]],
    gpu_version_files: List[Dict[str, Any]]
) -> Tuple[Dict[Tuple[str, str, str], Dict[str, Dict[str, Any]]], Set[Tuple[str, str, str]]]:
    """Build a single lookup dictionary mapping build keys to all their related files.

    Returns a dictionary where each key (pr_number, job_name, build_id) maps to a structure containing
    all related files: {finished: file, ocp: file, gpu: file}

    Much cleaner than maintaining three separate lookup dictionaries.
    """
    build_files = {}  # {(pr_number, job_name, build_id): {finished: file, ocp: file, gpu: file}}
    all_builds = set()

    # Combine all files into a single list with their file type
    all_files_with_type = []
    for file_item in finished_files:
        all_files_with_type.append((file_item, 'finished'))
    for file_item in ocp_version_files:
        all_files_with_type.append((file_item, 'ocp'))
    for file_item in gpu_version_files:
        all_files_with_type.append((file_item, 'gpu'))

    # Process all files in a single pass - parse each path only once
    for file_item, file_type in all_files_with_type:
        path = file_item.get("name", "")

        # Skip non-GPU operator paths early
        try:
            repo, pr_number, job_name, build_id = extract_build_components(path)
        except ValueError:
            continue

        if build_id in ['latest-build.txt', 'latest-build']:
            continue

        # Build key from extracted components
        key = (pr_number, job_name, build_id)

        # Ensure the build entry exists
        if key not in build_files:
            build_files[key] = {}

        # Store file in the appropriate slot
        build_files[key][file_type] = file_item
        all_builds.add(key)

    return build_files, all_builds


def process_single_build(
    pr_number_arg: str,
    job_name: str,
    build_id: str,
    ocp_version: str,
    gpu_suffix: str,
    build_files: Dict[Tuple[str, str, str], Dict[str, Dict[str, Any]]],
    dual_builds_info: Optional[Dict[Tuple[str, str, str], Dict[str, Dict[str, Any]]]] = None
) -> TestResult:
    """Process a single build and return its test result."""
    # No need to reconstruct path - versions already extracted by caller

    # Get all files for this build
    key = (pr_number_arg, job_name, build_id)
    build_file_set = build_files[key]

    # Get build status and timestamp from finished.json
    finished_file = build_file_set['finished']
    finished_content = fetch_gcs_file_content(finished_file['name'])
    finished_data = json.loads(finished_content)
    status = finished_data["result"]
    timestamp = finished_data["timestamp"]

    # Check for mismatch between nested GPU operator test and top-level build result
    if dual_builds_info and key in dual_builds_info:
        dual_files = dual_builds_info[key]
        if 'nested' in dual_files and 'top_level' in dual_files:
            # Fetch both statuses for comparison
            nested_content = fetch_gcs_file_content(dual_files['nested']['name'])
            nested_data = json.loads(nested_content)
            nested_status = nested_data["result"]

            top_level_content = fetch_gcs_file_content(dual_files['top_level']['name'])
            top_level_data = json.loads(top_level_content)
            top_level_status = top_level_data["result"]

            # Warn if GPU operator succeeded but overall build failed
            if nested_status == "SUCCESS" and top_level_status == "FAILURE":
                logger.warning(
                    f"Build {build_id}: GPU operator tests SUCCEEDED but overall build FAILED. "
                    f"This indicates GPU tests passed but the full CI pipeline (i.e. post-steps) failed."
                )

    # Build prow job URL directly from the finished.json file path
    job_url = build_prow_job_url(finished_file['name'])

    logger.info(f"Built prow job URL for build {build_id} from path {finished_file['name']}: {job_url}")

    # Get exact versions if files exist (regardless of build status)
    ocp_version_file = build_file_set.get('ocp')
    gpu_version_file = build_file_set.get('gpu')

    if ocp_version_file and gpu_version_file:
        exact_ocp = fetch_gcs_file_content(ocp_version_file['name']).strip()
        exact_gpu_version = fetch_gcs_file_content(
            gpu_version_file['name']).strip()
        result = TestResult(exact_ocp, exact_gpu_version,
                            status, job_url, timestamp)
    else:
        # Use base versions
        result = TestResult(ocp_version, gpu_suffix,
                            status, job_url, timestamp)

    return result


def process_tests_for_pr(pr_number: str, results_by_ocp: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all jobs under a single PR using targeted file filtering."""
    logger.info(
        f"Fetching targeted test data for PR #{pr_number} using filtered requests")

    # Step 1: Fetch all required files
    all_finished_files, ocp_version_files, gpu_version_files = fetch_pr_files(
        pr_number)

    # Step 2: Filter to get the preferred finished.json files (nested when available, otherwise top-level)
    finished_files, dual_builds_info = filter_gpu_finished_files(all_finished_files)

    # Step 3: Build single unified lookup for all file types
    build_files, all_builds = build_files_lookup(
        finished_files, ocp_version_files, gpu_version_files)

    logger.info(
        f"Found {len(all_builds)} unique job/build combinations from filtered files")

    # Step 4: Process each job/build combination (already unique from all_builds set)
    processed_count = 0

    for pr_num, job_name, build_id in sorted(all_builds):
        # Determine repository from job name pattern
        if job_name.startswith("rehearse-"):
            repo = "openshift_release"
        else:
            repo = "rh-ecosystem-edge_nvidia-ci"

        # Extract OCP version for logging
        job_path = f"pr-logs/pull/{repo}/{pr_num}/{job_name}/"
        full_path = f"{job_path}{build_id}"
        match = TEST_RESULT_PATH_REGEX.search(full_path)
        if not match:
            logger.warning(f"Could not parse versions from components: {pr_num}, {job_name}, {build_id}")
            continue
        ocp_version = match.group("ocp_version")
        gpu_suffix = match.group("gpu_version")

        logger.info(
            f"Processing build {build_id} for {ocp_version} + {gpu_suffix}")

        result = process_single_build(
            pr_num, job_name, build_id, ocp_version, gpu_suffix, build_files, dual_builds_info)

        results_by_ocp.setdefault(ocp_version, []).append(result.to_dict())
        logger.info(f"Added result for build {build_id}: {result.test_status}")
        processed_count += 1

    logger.info(
        f"Successfully processed {processed_count} unique builds using targeted filtering")


def process_closed_prs(results_by_ocp: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all closed PRs against the main branch."""
    logger.info("Retrieving PR history...")
    url = "https://api.github.com/repos/rh-ecosystem-edge/nvidia-ci/pulls"
    params = {"state": "closed", "base": "main",
              "per_page": "100", "page": "1"}
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

        # Build a map of existing entries by build key (pr_number, job_name, build_id)
        existing_by_build = {}
        for item in merged_results[key]["tests"]:
            result = TestResult(**item)
            build_key = result.build_key()
            existing_by_build[build_key] = item

        # Process new values and merge with existing
        for item in new_values:
            result = TestResult(**item)
            build_key = result.build_key()

            if build_key in existing_by_build:
                # Choose the better entry between existing and new
                existing_result = TestResult(**existing_by_build[build_key])
                better_entry = _choose_better_entry(existing_result, result)
                existing_by_build[build_key] = better_entry.to_dict()
            else:
                existing_by_build[build_key] = item

        # Update the tests list with deduplicated entries
        merged_results[key]["tests"] = list(existing_by_build.values())

    with open(file_path, "w") as f:
        json.dump(merged_results, f, indent=4)
    logger.info(f"Data successfully saved to {file_path}")


def _choose_better_entry(existing: TestResult, new: TestResult) -> TestResult:
    """Choose the better entry between two results for the same build.

    Priority order:
    1. Entry with exact versions over base versions
    2. Entry with SUCCESS status over FAILURE
    3. Entry with later timestamp
    """
    existing_has_exact = existing.has_exact_versions()
    new_has_exact = new.has_exact_versions()

    # Prefer exact versions over base versions
    if new_has_exact and not existing_has_exact:
        return new
    elif existing_has_exact and not new_has_exact:
        return existing

    # If both have same version quality, prefer SUCCESS over FAILURE
    if existing.test_status != new.test_status:
        if new.test_status == "SUCCESS":
            return new
        elif existing.test_status == "SUCCESS":
            return existing

    # If same status, prefer later timestamp
    if int(new.job_timestamp) > int(existing.job_timestamp):
        return new

    return existing

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
    logger.info(
        f"Loaded baseline data from: {args.baseline_data_filepath} with keys: {list(existing_results.keys())}")

    local_results: Dict[str, List[Dict[str, Any]]] = {}
    if args.pr_number.lower() == "all":
        process_closed_prs(local_results)
    else:
        process_tests_for_pr(args.pr_number, local_results)
    merge_and_save_results(
        local_results, args.merged_data_filepath, existing_results=existing_results)


if __name__ == "__main__":
    main()
