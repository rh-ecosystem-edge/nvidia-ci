"""JUnit XML parsing utilities."""

import xml.etree.ElementTree as ET
from typing import Any, Dict, List

from config import RepositoryInfo
from gcs import client as gcs_client
from gcs.paths import build_artifacts_path


def _is_junit_file(file_info: Dict[str, Any]) -> bool:
    """Check if a file is a JUnit XML file."""
    name = file_info["name"]
    return name.startswith("junit") and name.endswith(".xml")


def find_junit_files_in_build(config: Dict[str, Any], repo_info: RepositoryInfo,
                              pr_number: str, job_name: str, build_id: str) -> List[Dict[str, Any]]:
    """
    Find all JUnit XML files in a build's artifacts.

    Returns list of dicts with file paths and sizes.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    artifacts_prefix = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template)

    # List top-level directories
    top_dirs = gcs_client.list_directories(bucket, artifacts_prefix)

    junit_files = []

    def add_junit_files_from_dir(dir_path: str, step_prefix: str):
        """Helper to add JUnit files from a directory."""
        files_data = gcs_client.list_files_and_directories(bucket, dir_path)
        for file_info in files_data.get("files", []):
            if _is_junit_file(file_info):
                junit_files.append({
                    "path": f"{step_prefix}{file_info['name']}" if step_prefix else file_info['name'],
                    "full_path": f"{dir_path.rstrip('/')}/{file_info['name']}",
                    "size": file_info["size"],
                    "step": step_prefix.split('/')[0] if step_prefix else "artifacts_root",
                })

    # Check for JUnit files directly at artifacts root (before checking subdirs)
    add_junit_files_from_dir(artifacts_prefix, "")

    # Search in each top-level directory and one level down
    for top_dir in top_dirs:
        dir_path = f"{artifacts_prefix}{top_dir}/"

        # Check top level for junit files
        add_junit_files_from_dir(dir_path, f"{top_dir}/")

        # Check one level down (in artifacts/ subdirectory)
        files_data = gcs_client.list_files_and_directories(bucket, dir_path)
        if "artifacts" in files_data.get("directories", []):
            subdir_path = f"{dir_path}artifacts/"
            add_junit_files_from_dir(subdir_path, f"{top_dir}/artifacts/")

    return junit_files


def parse_junit_xml(xml_content: str) -> Dict[str, Any]:
    """
    Parse JUnit XML and extract test results.

    Returns dict with test counts, failures, and error details.
    """
    try:
        root = ET.fromstring(xml_content)

        # Get test suite info
        tests = int(root.get("tests", 0))
        failures = int(root.get("failures", 0))
        errors = int(root.get("errors", 0))
        skipped = int(root.get("skipped", 0))
        time = float(root.get("time", 0.0))

        # Extract failed test cases
        failed_tests = []
        for testcase in root.findall(".//testcase"):
            failure = testcase.find("failure")
            error = testcase.find("error")

            if failure is not None or error is not None:
                test_info = {
                    "name": testcase.get("name", ""),
                    "classname": testcase.get("classname", ""),
                    "time": float(testcase.get("time", 0.0)),
                }

                if failure is not None:
                    test_info["type"] = "failure"
                    test_info["message"] = failure.get("message", "")
                    test_info["details"] = failure.text or ""
                elif error is not None:
                    test_info["type"] = "error"
                    test_info["message"] = error.get("message", "")
                    test_info["details"] = error.text or ""

                failed_tests.append(test_info)

        return {
            "summary": {
                "total_tests": tests,
                "failures": failures,
                "errors": errors,
                "skipped": skipped,
                "passed": tests - failures - errors - skipped,
                "duration_seconds": time,
            },
            "failed_tests": failed_tests,
            "success": failures == 0 and errors == 0,
        }
    except ET.ParseError as e:
        return {
            "error": f"Failed to parse JUnit XML: {str(e)}",
            "summary": {},
            "failed_tests": [],
        }


def get_junit_results(config: Dict[str, Any], repo_info: RepositoryInfo, pr_number: str,
                     job_name: str, build_id: str, junit_path: str) -> Dict[str, Any]:
    """
    Fetch and parse a JUnit XML file.

    Returns parsed test results with failure details.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    full_path = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template, junit_path).rstrip('/')

    xml_content = gcs_client.fetch_file(bucket, full_path)
    if not xml_content:
        return {
            "repository": repo_info.full_name,
            "pr_number": pr_number,
            "job_name": job_name,
            "build_id": build_id,
            "junit_path": junit_path,
            "error": "JUnit file not found",
        }

    parsed = parse_junit_xml(xml_content)

    return {
        "repository": repo_info.full_name,
        "pr_number": pr_number,
        "job_name": job_name,
        "build_id": build_id,
        "junit_path": junit_path,
        **parsed,
    }

