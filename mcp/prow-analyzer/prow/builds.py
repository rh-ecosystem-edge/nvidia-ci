"""Prow build step operations."""

from typing import Any, Dict, List

from config import RepositoryInfo
from gcs import client as gcs_client
from gcs.paths import build_artifacts_path


def _check_build_log_exists(bucket: str, artifacts_prefix: str, path: str) -> bool:
    """Check if a build-log.txt exists at the given path without downloading it."""
    dir_path = f"{artifacts_prefix}{path}/"
    listing = gcs_client.list_files_and_directories(bucket, dir_path)
    return any(f["name"] == "build-log.txt" for f in listing.get("files", []))


def _process_step_directory(bucket: str, artifacts_prefix: str, top_dir: str) -> List[Dict[str, Any]]:
    """Process a single step directory and return its steps."""
    has_top_level_log = _check_build_log_exists(bucket, artifacts_prefix, top_dir)

    # When a top-level build-log.txt exists, we return only the top-level step and skip
    # enumerating nested sub-steps. This is the expected behavior: top-level logs indicate
    # a monolithic build step, while absence of a top-level log suggests sub-steps.
    if has_top_level_log:
        return [{"path": top_dir, "has_build_log": True}]

    # Check one level deeper for sub-steps
    sub_dirs = gcs_client.list_directories(bucket, f"{artifacts_prefix}{top_dir}/")
    if not sub_dirs:
        return [{"path": top_dir, "has_build_log": False}]

    # Process sub-directories
    steps = []
    for sub_dir in sub_dirs:
        sub_path = f"{top_dir}/{sub_dir}"
        has_sub_log = _check_build_log_exists(bucket, artifacts_prefix, sub_path)
        steps.append({"path": sub_path, "has_build_log": has_sub_log})

    return steps


def list_build_steps(config: Dict[str, Any], repo_info: RepositoryInfo, pr_number: str,
                    job_name: str, build_id: str) -> List[Dict[str, Any]]:
    """
    List available steps/artifacts in a build with their nested structure.

    Returns a list of dicts with 'path' and 'has_build_log' keys.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]

    artifacts_prefix = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template)

    # List top-level directories under artifacts/
    top_level_dirs = gcs_client.list_directories(bucket, artifacts_prefix)

    # Process each directory and flatten results
    steps = []
    for top_dir in top_level_dirs:
        steps.extend(_process_step_directory(bucket, artifacts_prefix, top_dir))

    return steps

