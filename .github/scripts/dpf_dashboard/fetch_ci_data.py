#!/usr/bin/env python3
"""
Fetch DPF CI data from OpenShift CI (Prow) periodic jobs.

Reads job definitions from versions.json, fetches test results from Google
Cloud Storage, and merges them into an accumulated results file.
"""

import argparse
import json
import os
import sys
import tempfile
import urllib.parse
from typing import Dict, Any, List, Optional

import requests

from common.utils import logger

GCS_API_BASE_URL = (
    "https://storage.googleapis.com/storage/v1/b/test-platform-results-public/o"
)
PROW_BASE_URL = (
    "https://prow.ci.openshift.org/view/gs/test-platform-results-public"
)


def load_versions_config() -> Dict[str, Any]:
    script_dir = os.path.dirname(os.path.abspath(__file__))
    config_path = os.path.join(script_dir, "versions.json")
    with open(config_path, "r") as f:
        return json.load(f)


def gcs_list_dir(prefix: str) -> List[str]:
    all_prefixes: List[str] = []
    page_token = None
    while True:
        params: Dict[str, Any] = {"prefix": prefix, "delimiter": "/"}
        if page_token:
            params["pageToken"] = page_token
        resp = requests.get(GCS_API_BASE_URL, params=params, timeout=30)
        resp.raise_for_status()
        data = resp.json()
        all_prefixes.extend(data.get("prefixes", []))
        page_token = data.get("nextPageToken")
        if not page_token:
            break
    return all_prefixes


def gcs_get_file(path: str) -> Optional[str]:
    encoded = urllib.parse.quote_plus(path)
    resp = requests.get(
        f"{GCS_API_BASE_URL}/{encoded}",
        params={"alt": "media"},
        timeout=30,
    )
    if resp.status_code == 404:
        return None
    resp.raise_for_status()
    return resp.content.decode("UTF-8")


def parse_env_file(content: str) -> Dict[str, str]:
    env = {}
    for line in content.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" in line:
            key, _, value = line.partition("=")
            env[key.strip()] = value.strip()
    return env


def get_job_runs(job_name: str, limit: int) -> List[Dict[str, Any]]:
    prefix = f"logs/{job_name}/"
    prefixes = gcs_list_dir(prefix)
    runs = []
    for p in prefixes:
        build_id = p.rstrip("/").split("/")[-1]
        try:
            runs.append({"build_id": build_id, "num": int(build_id)})
        except ValueError:
            continue
    runs.sort(key=lambda r: r["num"])
    return runs[-limit:]


def build_hcp_provisioner_image(env: Dict[str, str]) -> str:
    repo = env.get("DPF_HCP_PROVISIONER_OPERATOR_IMAGE_REPO", "")
    version = env.get("DPF_HCP_PROVISIONER_OPERATOR_VERSION", "")
    tag = env.get("DPF_HCP_PROVISIONER_OPERATOR_IMAGE_TAG", "")
    if version:
        return f"{repo}:{version}"
    if tag:
        return f"{repo}:{tag}"
    return repo


def fetch_build_result(
    job_name: str, build_id: str, config: Dict[str, Any],
    step_prefix: str,
) -> Optional[Dict[str, Any]]:
    base_path = f"logs/{job_name}/{build_id}"
    env_step = config["env_step"]
    networking_step = config["networking_step"]

    finished_content = gcs_get_file(f"{base_path}/finished.json")
    if not finished_content:
        logger.warning(f"No finished.json for build {build_id}")
        return None

    try:
        finished = json.loads(finished_content)
    except json.JSONDecodeError:
        logger.warning(f"Invalid finished.json for build {build_id}")
        return None

    timestamp = finished.get("timestamp", 0)
    branch = finished.get("revision", "unknown")

    net_path = (
        f"{base_path}/artifacts/{step_prefix}/{networking_step}/finished.json"
    )
    net_content = gcs_get_file(net_path)
    if net_content:
        try:
            net_finished = json.loads(net_content)
            networking_status = (
                "SUCCESS" if net_finished.get("passed", False) else "FAILURE"
            )
        except json.JSONDecodeError:
            networking_status = "FAILURE"
    else:
        networking_status = "FAILURE"

    env_path = (
        f"{base_path}/artifacts/{step_prefix}/{env_step}/artifacts/.env"
    )
    env_content = gcs_get_file(env_path)
    if not env_content:
        logger.warning(f"No .env for build {build_id}, skipping")
        return None

    env = parse_env_file(env_content)

    ocp_version = env.get("OPENSHIFT_VERSION", "")
    if not ocp_version:
        logger.warning(f"No OPENSHIFT_VERSION in .env for build {build_id}")
        return None

    return {
        "ocp_version": ocp_version,
        "branch": branch,
        "dpf_version": env.get("DPF_VERSION", ""),
        "ovn_chart_version": env.get("OVN_CHART_VERSION", ""),
        "bluefield_ocp_image": env.get("BLUEFIELD_OCP_IMAGE", ""),
        "hcp_provisioner_image": build_hcp_provisioner_image(env),
        "networking_status": networking_status,
        "prow_job_url": f"{PROW_BASE_URL}/{base_path}",
        "job_timestamp": str(timestamp),
        "build_id": build_id,
    }


def get_ocp_major_minor(version: str) -> str:
    parts = version.split(".")
    if len(parts) >= 2:
        return f"{parts[0]}.{parts[1]}"
    return version


def fetch_all_results(
    config: Dict[str, Any], job_limit: int
) -> Dict[str, Dict[str, Any]]:
    results_by_ocp: Dict[str, Dict[str, Any]] = {}

    for ocp_config in config.get("ocp_versions", {}).values():
        if ocp_config.get("status") != "active":
            continue

        for job in ocp_config.get("jobs", []):
            job_name = job["name"]
            step_prefix = job["step_prefix"]
            logger.info(f"Fetching last {job_limit} runs for {job_name}")

            runs = get_job_runs(job_name, job_limit)
            logger.info(f"Found {len(runs)} runs")

            for run in runs:
                result = fetch_build_result(
                    job_name, run["build_id"], config, step_prefix,
                )
                if not result:
                    continue

                ocp_mm = get_ocp_major_minor(result["ocp_version"])
                if ocp_mm not in results_by_ocp:
                    results_by_ocp[ocp_mm] = {"results": []}

                results_by_ocp[ocp_mm]["results"].append(result)

    return results_by_ocp


def merge_results(
    new_results: Dict[str, Dict[str, Any]],
    existing_results: Dict[str, Dict[str, Any]],
) -> Dict[str, Dict[str, Any]]:
    merged: Dict[str, Dict[str, Any]] = {}

    for ocp_version, data in existing_results.items():
        merged[ocp_version] = {"results": list(data.get("results", []))}

    for ocp_version, data in new_results.items():
        if ocp_version not in merged:
            merged[ocp_version] = {"results": []}

        existing_ids = {
            r["build_id"] for r in merged[ocp_version]["results"]
        }

        for result in data.get("results", []):
            if result["build_id"] not in existing_ids:
                merged[ocp_version]["results"].append(result)

    return merged


def main():
    parser = argparse.ArgumentParser(
        description="Fetch DPF CI data from periodic jobs"
    )
    parser.add_argument(
        "--output-data",
        required=True,
        help="Path to save/merge results JSON",
    )
    parser.add_argument(
        "--job-limit",
        type=int,
        default=1,
        help="Number of recent job runs to fetch per job (default: 1)",
    )
    args = parser.parse_args()

    config = load_versions_config()

    existing_data: Dict[str, Dict[str, Any]] = {}
    if os.path.exists(args.output_data):
        try:
            with open(args.output_data, "r") as f:
                existing_data = json.load(f)
            logger.info(f"Loaded existing data from {args.output_data}")
        except (json.JSONDecodeError, OSError) as e:
            logger.error(f"Failed to load existing data: {e}")
            sys.exit(1)

    new_data = fetch_all_results(config, args.job_limit)

    merged = merge_results(new_data, existing_data)

    out_dir = os.path.dirname(os.path.abspath(args.output_data))
    fd, tmp_path = tempfile.mkstemp(dir=out_dir, suffix=".json")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(merged, f, indent=2)
        os.replace(tmp_path, args.output_data)
    except BaseException:
        os.unlink(tmp_path)
        raise

    total = sum(len(d["results"]) for d in merged.values())
    logger.info(f"Saved {total} results to {args.output_data}")


if __name__ == "__main__":
    main()
