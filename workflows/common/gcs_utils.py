"""
GCS (Google Cloud Storage) utilities for fetching CI test artifacts.
Shared across GPU Operator and Network Operator dashboards.
"""

import re
import urllib.parse
from typing import Dict, Any

import requests

from workflows.common.utils import logger

# GCS API base URL for test-platform-results bucket
GCS_API_BASE_URL = "https://storage.googleapis.com/storage/v1/b/test-platform-results/o"

# Maximum number of results per GCS API request for pagination
GCS_MAX_RESULTS_PER_REQUEST = 1000


def http_get_json(url: str, params: Dict[str, Any] = None, headers: Dict[str, str] = None) -> Dict[str, Any]:
    """
    Send an HTTP GET request and return the JSON response.
    
    Args:
        url: URL to fetch
        params: Optional query parameters
        headers: Optional HTTP headers
        
    Returns:
        Parsed JSON response
        
    Raises:
        requests.HTTPError: If the request fails
    """
    response = requests.get(url, params=params, headers=headers, timeout=30)
    response.raise_for_status()
    return response.json()


def fetch_gcs_file_content(file_path: str) -> str:
    """
    Fetch the raw text content from a file in GCS.
    
    Args:
        file_path: Path to the file in GCS (e.g., "pr-logs/pull/...")
        
    Returns:
        File content as string
        
    Raises:
        requests.HTTPError: If the file cannot be fetched
    """
    logger.info(f"Fetching file content for {file_path}")
    response = requests.get(
        url=f"{GCS_API_BASE_URL}/{urllib.parse.quote_plus(file_path)}",
        params={"alt": "media"},
        timeout=30,
    )
    response.raise_for_status()
    return response.content.decode("UTF-8")


def build_prow_job_url(finished_json_path: str) -> str:
    """
    Build a Prow job URL from a finished.json file path.
    
    Args:
        finished_json_path: Path to finished.json file (e.g., "pr-logs/pull/.../finished.json")
        
    Returns:
        Full URL to the Prow job artifacts page
    """
    directory_path = finished_json_path[:-len('/finished.json')]
    return f"https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/{directory_path}"


def fetch_filtered_files(pr_number: str, glob_pattern: str) -> list[Dict[str, Any]]:
    """
    Fetch files from GCS matching a specific pattern for a PR.
    
    Args:
        pr_number: Pull request number
        glob_pattern: Glob pattern to match files (e.g., "*/finished.json", "*/ocp.version")
        
    Returns:
        List of file metadata dictionaries from GCS API
    """
    all_items = []
    
    # Search in both possible PR locations
    for prefix in [
        f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_number}/",
        f"pr-logs/pull/openshift_release/{pr_number}/"
    ]:
        page_token = None  # Reset pagination token for each prefix
        while True:
            params = {
                "prefix": prefix,
                "delimiter": "",
                "matchGlob": glob_pattern,
                "maxResults": GCS_MAX_RESULTS_PER_REQUEST,
            }
            if page_token:
                params["pageToken"] = page_token
                
            data = http_get_json(GCS_API_BASE_URL, params=params)
            items = data.get("items", [])
            all_items.extend(items)
            
            page_token = data.get("nextPageToken")
            if not page_token:
                break
                
    logger.info(f"Found {len(all_items)} files matching pattern '{glob_pattern}' for PR #{pr_number}")
    return all_items


def build_job_history_url(job_name: str) -> str:
    """
    Build a Prow job history URL for a given job name.
    
    Args:
        job_name: Name of the CI job
        
    Returns:
        Full URL to the job history page
    """
    return f"https://prow.ci.openshift.org/job-history/gs/test-platform-results/pr-logs/directory/{job_name}"

