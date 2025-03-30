import argparse
import json
import os
import re
import urllib.parse
from dataclasses import dataclass
from typing import Any, Dict, List, Tuple

import requests
from logger import logger

BASE_URL = "https://storage.googleapis.com/storage/v1/b/test-platform-results/o"

# Regular expression to match test result paths.
TEST_PATTERN = re.compile(
    r"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/\d+/pull-ci-rh-ecosystem-edge-nvidia-ci-main-"
    r"(?P<ocp_version>\d+\.\d+)-stable-nvidia-gpu-operator-e2e-(?P<gpu_version>\d+-\d+-x|master)/"
)

# --- Helper Functions ---

def raise_error(message: str) -> None:
    logger.error(message)
    raise Exception(message)

def make_request(url: str, params: Dict[str, Any] = None, headers: Dict[str, str] = None) -> Dict[str, Any]:
    """Send an HTTP GET request and return the JSON response."""
    response = requests.get(url, params=params, headers=headers)
    response.raise_for_status()
    return response.json()


def fetch_file_content(file_path: str) -> str:
    """Fetch the raw text content from a file in GCS."""
    logger.info(f"Fetching file content for {file_path}")
    response = requests.get(
        url=f"{BASE_URL}/{urllib.parse.quote_plus(file_path)}",
        params={"alt": "media"},
    )
    response.raise_for_status()
    return response.content.decode("UTF-8")


def gpu_suffix_to_version(gpu: str) -> str:
    """Convert GPU suffix to a version string, e.g., '14-9-x' -> '14.9'."""
    return gpu if gpu == "master" else gpu[:-2].replace("-", ".")

def get_job_url(pr_id: str, ocp_minor: str, gpu_suffix: str, job_id: str) -> str:
    """Build the Prow job URL for the given PR, OCP version, GPU suffix, and job ID."""
    return (
        f"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/"
        f"pull/rh-ecosystem-edge_nvidia-ci/{pr_id}/pull-ci-rh-ecosystem-edge-nvidia-ci-main-"
        f"{ocp_minor}-stable-nvidia-gpu-operator-e2e-{gpu_suffix}/{job_id}"
    )

def get_versions(prefix: str, build_id: str, gpu_version_suffix: str) -> Tuple[str, str]:
    """Fetch the exact OCP and GPU versions for a given build."""
    logger.info(f"Fetching versions for build {build_id}")
    ocp_version_file = (
        f"{prefix}{build_id}/artifacts/nvidia-gpu-operator-e2e-{gpu_version_suffix}/"
        "gpu-operator-e2e/artifacts/ocp.version"
    )
    gpu_version_file = (
        f"{prefix}{build_id}/artifacts/nvidia-gpu-operator-e2e-{gpu_version_suffix}/"
        "gpu-operator-e2e/artifacts/operator.version"
    )
    ocp_version = fetch_file_content(ocp_version_file)
    gpu_version = fetch_file_content(gpu_version_file)
    return ocp_version, gpu_version


def get_status_and_time(prefix: str, latest_build_id: str) -> Tuple[str, Any]:
    """Fetch the status (SUCCESS/FAILURE/UNKNOWN) and timestamp for a given build."""
    logger.info(f"Fetching status for build {latest_build_id}")
    finished_file = f"{prefix}{latest_build_id}/finished.json"
    url = f"{BASE_URL}/{urllib.parse.quote_plus(finished_file)}"
    data = make_request(url, params={"alt": "media"})
    status = data.get("result", "UNKNOWN")
    timestamp = data.get("timestamp", None)
    return status, timestamp

# --- Domain Model ---

@dataclass
class TestResults:
    """Represents a single test run result."""
    ocp_version: str
    gpu_version: str
    status: str
    link: str
    timestamp: str

    def to_dict(self) -> Dict[str, Any]:
        """Convert TestResults object to a dictionary for JSON serialization."""
        return {
            "ocp": self.ocp_version,
            "gpu": self.gpu_version,
            "status": self.status,
            "link": self.link,
            "timestamp": self.timestamp,
        }

# --- Core Functions ---

def get_job_results(
    pr_id: str,
    prefix: str,
    ocp_version: str,
    gpu_version_suffix: str,
    ocp_data: Dict[str, List[Dict[str, Any]]]
) -> None:
    """Fetch and store results for a specific job prefix."""
    logger.info(f"Processing job results for {prefix}")
    params = {
        "prefix": prefix,
        "alt": "json",
        "delimiter": "/",
        "includeFoldersAsPrefixes": "True",
        "maxResults": "1000",
        "projection": "noAcl",
    }
    headers = {"Accept": "application/json"}
    response_data = make_request(BASE_URL, params=params, headers=headers)
    latest_build = fetch_file_content(response_data["items"][0]["name"])
    logger.info(f"Latest build for {prefix}: {latest_build}")

    status, timestamp = get_status_and_time(prefix, latest_build)
    job_url = get_job_url(pr_id, ocp_version, gpu_version_suffix, latest_build)

    if status == "SUCCESS":
        exact_ocp, exact_gpu_version = get_versions(prefix, latest_build, gpu_version_suffix)
        result = TestResults(exact_ocp, exact_gpu_version, status, job_url, timestamp)
    else:
        converted_gpu = gpu_suffix_to_version(gpu_version_suffix)
        result = TestResults(ocp_version, converted_gpu, status, job_url, timestamp)
    ocp_data.setdefault(ocp_version, []).append(result.to_dict())

def get_all_pr_tests(pr_num: str, ocp_data: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all jobs under a single PR."""
    logger.info(f"Fetching tests for PR #{pr_num}")
    params = {
        "prefix": f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_num}/",
        "alt": "json",
        "delimiter": "/",
        "includeFoldersAsPrefixes": "True",
        "maxResults": "1000",
        "projection": "noAcl",
    }
    headers = {"Accept": "application/json"}
    response_data = make_request(BASE_URL, params=params, headers=headers)
    prefixes = response_data.get("prefixes", [])

    for job in prefixes:
        match = TEST_PATTERN.match(job)
        if not match:
            continue
        ocp = match.group("ocp_version")
        gpu_suffix = match.group("gpu_version")
        get_job_results(pr_num, job, ocp, gpu_suffix, ocp_data)
    
def retrieve_all_prs(ocp_data: Dict[str, List[Dict[str, Any]]]) -> None:
    """Retrieve and store test results for all closed PRs against the main branch."""
    logger.info("Retrieving PR history...")
    url = "https://api.github.com/repos/rh-ecosystem-edge/nvidia-ci/pulls"
    params = {"state": "closed", "base": "main", "per_page": "100", "page": "1"}
    headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28"
    }
    response_data = make_request(url, params=params, headers=headers)
    for pr in response_data:
        pr_num = str(pr["number"])
        logger.info(f"Processing PR #{pr_num}")
        get_all_pr_tests(pr_num, ocp_data)
   
def save_to_json(new_data: Dict[str, List[Dict[str, Any]]],
                 output_dir: str,
                 new_data_file: str,
                 existing_data: Dict[str, List[Dict[str, Any]]] = None) -> None:
    """
    Merge new_data into existing_data (if provided) and write to JSON.
    This ensures old entries remain and new ones are appended.
    """
    file_path = os.path.join(output_dir, new_data_file)
    logger.info(f"Saving JSON to {file_path}")
    merged_data = existing_data.copy() if existing_data else {}
    for key, new_values in new_data.items():
        merged_data.setdefault(key, []).extend(new_values)
    with open(file_path, "w") as f:
        json.dump(merged_data, f, indent=4)
    logger.info(f"Data successfully saved to {file_path}")

# --- Main Function ---

def main() -> None:
    parser = argparse.ArgumentParser(description="Generate test matrix data")
    parser.add_argument("--pr", default="all", help="PR number to process; use 'all' for full history")
    parser.add_argument("--output_dir", required=True, help="Directory to store the output JSON")
    parser.add_argument("--old_data_file", required=True, help="Name of the existing json file")
    parser.add_argument("--new_data_file", required=True, help="Name of the generated json file")
    args = parser.parse_args()

    # Combine the output_dir with the old_data_file name
    old_data_path = os.path.join(args.output_dir, args.old_data_file)
    with open(old_data_path, "r") as f:
        old_data: Dict[str, List[Dict[str, Any]]] = json.load(f)
    logger.info(f"Loaded old data from: {old_data_path} with keys: {list(old_data.keys())}")

    # local_ocp_data will hold ONLY new data collected during this run.
    local_ocp_data: Dict[str, List[Dict[str, Any]]] = {}

    if args.pr.lower() == "all":
        retrieve_all_prs(local_ocp_data)
    else:
        get_all_pr_tests(args.pr, local_ocp_data)

    # Merge old data with new data and save
    save_to_json(local_ocp_data, args.output_dir, args.new_data_file, existing_data=old_data)

if __name__ == "__main__":
    main()
