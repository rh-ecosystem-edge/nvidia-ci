"""
GCS (Google Cloud Storage) utilities for fetching CI test artifacts.
Shared across GPU Operator and Network Operator dashboards.

Supports two backends:
- gcsweb proxy with Bearer token auth (when PROW_TOKEN env var is set)
- GCS JSON API without auth (legacy fallback for public buckets)
"""

import os
import re
import urllib.parse
from collections import deque
from typing import Dict, Any, List, Tuple

import requests

from common.utils import logger

# GCS API base URL for test-platform-results bucket
GCS_API_BASE_URL = "https://storage.googleapis.com/storage/v1/b/test-platform-results/o"

# Maximum number of results per GCS API request for pagination
GCS_MAX_RESULTS_PER_REQUEST = 1000

# gcsweb auth configuration (from environment)
_PROW_TOKEN = os.environ.get("PROW_TOKEN", "")
_GCSWEB_API_URL = os.environ.get(
    "PROW_GCSWEB_API_URL",
    "https://gcsweb-test-platform-results-ci.apps.ci.l2s4.p1.openshiftapps.com"
).rstrip("/")
if _PROW_TOKEN and not _GCSWEB_API_URL.startswith("https://"):
    raise ValueError(
        f"PROW_GCSWEB_API_URL must use HTTPS when PROW_TOKEN is set "
        f"(got {_GCSWEB_API_URL!r})"
    )
_GCS_BUCKET = "test-platform-results"
_CURATED_PREFIX = os.environ.get("PROW_CURATED_PREFIX", "curated/")

# Cache for recursive directory traversals (avoids re-crawling the same prefix)
_files_cache: Dict[str, List[str]] = {}


def _matches_gcs_glob(path: str, pattern: str) -> bool:
    """Match a path against a GCS matchGlob pattern.

    ** matches across directory boundaries, * matches within a single segment.
    """
    i = 0
    parts: list[str] = []
    while i < len(pattern):
        if i + 1 < len(pattern) and pattern[i] == "*" and pattern[i + 1] == "*":
            if (
                i + 2 < len(pattern)
                and pattern[i + 2] == "/"
                and (i == 0 or pattern[i - 1] == "/")
            ):
                parts.append("(?:.*/)?")
                i += 3
            else:
                parts.append(".*")
                i += 2
        elif pattern[i] == "*":
            parts.append("[^/]*")
            i += 1
        elif pattern[i] == "?":
            parts.append("[^/]")
            i += 1
        else:
            parts.append(re.escape(pattern[i]))
            i += 1
    return bool(re.fullmatch("".join(parts), path))


def _use_gcsweb() -> bool:
    return bool(_PROW_TOKEN)


def _get_auth_headers() -> Dict[str, str]:
    if _PROW_TOKEN:
        return {"Authorization": f"Bearer {_PROW_TOKEN}"}
    return {}


def _gcsweb_url(path: str) -> str:
    return f"{_GCSWEB_API_URL}/gcs/{_GCS_BUCKET}/{path}"


def _parse_gcsweb_listing(html: str, dir_path: str) -> Tuple[List[str], List[str]]:
    """Parse gcsweb HTML directory listing into child directory and file names.

    Only considers links whose href is a direct child of dir_path,
    filtering out navigation/breadcrumb links.
    """
    directories: List[str] = []
    files: List[str] = []
    seen: set = set()

    if dir_path and not dir_path.endswith("/"):
        dir_path = dir_path + "/"

    expected_prefix = f"/gcs/{_GCS_BUCKET}/{dir_path}"

    for match in re.finditer(r'href="([^"]*)"', html):
        href = match.group(1)

        if not href.startswith(expected_prefix):
            continue

        remainder = href[len(expected_prefix):]
        remainder_stripped = remainder.rstrip("/")
        if not remainder_stripped or "/" in remainder_stripped:
            continue

        name = remainder_stripped
        if name in seen:
            continue
        seen.add(name)

        if remainder.endswith("/"):
            directories.append(name)
        else:
            files.append(name)

    return directories, files


def _gcsweb_list_all_files(prefix: str) -> List[str]:
    """Recursively list all file paths under a gcsweb prefix (BFS).

    Returns full paths within the bucket (including the prefix).
    Results are cached per prefix to avoid repeated traversals.
    """
    if prefix in _files_cache:
        return _files_cache[prefix]

    all_files: List[str] = []
    dirs_to_visit: deque = deque([prefix])
    had_errors = False

    while dirs_to_visit:
        current = dirs_to_visit.popleft()
        try:
            response = requests.get(
                _gcsweb_url(current), headers=_get_auth_headers(), timeout=30
            )
            response.raise_for_status()
        except Exception as e:
            logger.warning(f"Failed to list {current}: {e}")
            had_errors = True
            continue

        directories, files = _parse_gcsweb_listing(response.text, current)

        for f in files:
            all_files.append(f"{current}{f}")

        for d in directories:
            dirs_to_visit.append(f"{current}{d}/")

    if not had_errors:
        _files_cache[prefix] = all_files
        logger.info(f"Cached {len(all_files)} files under {prefix}")
    else:
        logger.warning(f"Skipping cache for {prefix} due to errors during traversal")
    return all_files


def http_get_json(url: str, params: Dict[str, Any] | None = None, headers: Dict[str, str] | None = None) -> Dict[str, Any]:
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
    if _use_gcsweb():
        gcsweb_path = f"{_CURATED_PREFIX}{file_path}"
        logger.info(f"Fetching file content via gcsweb: {file_path}")
        response = requests.get(
            _gcsweb_url(gcsweb_path), headers=_get_auth_headers(), timeout=30
        )
        response.raise_for_status()
        # gcsweb returns 200 with HTML directory listing for non-existent files
        content_type = response.headers.get("Content-Type", "")
        if "text/html" in content_type and "<title>GCS browser:" in response.text[:1000]:
            raise requests.exceptions.HTTPError(
                f"File not found in curated view: {file_path}",
                response=response,
            )
        return response.text

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
    if _use_gcsweb():
        return f"{_GCSWEB_API_URL}/gcs/{_GCS_BUCKET}/{_CURATED_PREFIX}{directory_path}"
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
    if _use_gcsweb():
        return _gcsweb_fetch_filtered_files(pr_number, glob_pattern)

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


def _gcsweb_fetch_filtered_files(pr_number: str, glob_pattern: str) -> list[Dict[str, Any]]:
    """Fetch files matching a glob pattern using gcsweb recursive traversal.

    Glob patterns like "**/finished.json" are matched by suffix against all
    discovered files. Results are cached per PR prefix so repeated calls
    for different patterns reuse the same traversal.
    """
    all_items: list[Dict[str, Any]] = []

    for base_prefix in [
        f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_number}/",
        f"pr-logs/pull/openshift_release/{pr_number}/",
    ]:
        gcsweb_prefix = f"{_CURATED_PREFIX}{base_prefix}"
        all_files = _gcsweb_list_all_files(gcsweb_prefix)

        for file_path in all_files:
            if _matches_gcs_glob(file_path, glob_pattern):
                # Strip curated/ prefix so callers see the same paths as before
                original_path = file_path.removeprefix(_CURATED_PREFIX)
                all_items.append({"name": original_path})

    logger.info(f"Found {len(all_items)} files matching '{glob_pattern}' for PR #{pr_number} (via gcsweb)")
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
