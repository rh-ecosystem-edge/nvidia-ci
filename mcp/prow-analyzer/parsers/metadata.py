"""Metadata parsing utilities for Prow artifacts."""

import json
from typing import Any, Dict

from config import RepositoryInfo
from gcs import client as gcs_client
from gcs.paths import build_artifacts_path


def get_step_metadata(config: Dict[str, Any], repo_info: RepositoryInfo, pr_number: str,
                     job_name: str, build_id: str, step_name: str) -> Dict[str, Any]:
    """
    Get metadata from finished.json, started.json for a specific step.

    Returns parsed JSON data with timing and status information.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    step_base = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template, step_name).rstrip('/')

    result = {
        "repository": repo_info.full_name,
        "pr_number": pr_number,
        "job_name": job_name,
        "build_id": build_id,
        "step_name": step_name,
    }

    # Try to fetch finished.json
    finished_path = f"{step_base}/finished.json"
    finished_content = gcs_client.fetch_file(bucket, finished_path)
    if finished_content:
        try:
            result["finished"] = json.loads(finished_content)
        except json.JSONDecodeError:
            result["finished"] = {"error": "Invalid JSON"}

    # Try to fetch started.json
    started_path = f"{step_base}/started.json"
    started_content = gcs_client.fetch_file(bucket, started_path)
    if started_content:
        try:
            result["started"] = json.loads(started_content)
        except json.JSONDecodeError:
            result["started"] = {"error": "Invalid JSON"}

    # Calculate duration if both timestamps present
    if "finished" in result and "started" in result:
        finished_ts = result["finished"].get("timestamp")
        started_ts = result["started"].get("timestamp")
        if finished_ts is not None and started_ts is not None:
            result["duration_seconds"] = finished_ts - started_ts

    return result

