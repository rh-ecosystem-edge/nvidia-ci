#!/usr/bin/env python3
"""
Fetch Network Operator CI data from OpenShift CI (Prow) and store results.

This script fetches test results for NVIDIA Network Operator from Google Cloud Storage
where OpenShift CI stores Prow job artifacts.
"""

import argparse
import json
import re
from typing import Dict, Any, List, Tuple

from workflows.common import (
    logger,
    fetch_gcs_file_content,
    fetch_filtered_files,
    build_prow_job_url,
    build_job_history_url,
    TestResult,
    OCP_FULL_VERSION,
    OPERATOR_VERSION,
    STATUS_SUCCESS,
    STATUS_FAILURE,
    STATUS_ABORTED,
    is_valid_ocp_version,
)


# Regular expression to match Network Operator test result paths
# Example: pr-logs/pull/openshift_release/67673/rehearse-67673-pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca4-nvidia-network-operator-legacy-sriov-rdma/1961127149603655680
NNO_TEST_PATH_REGEX = re.compile(
    r"pr-logs/pull/(?P<repo>[^/]+)/(?P<pr_number>\d+)/"
    r"(?P<job_name>(?:rehearse-\d+-)?pull-ci-rh-ecosystem-edge-nvidia-ci-main-"
    r"(?P<infrastructure>[^-]+)-nvidia-network-operator-(?P<test_type>.+))/"
    r"(?P<build_id>[^/]+)"
)


def extract_test_flavor_from_job_name(job_name: str) -> str:
    """
    Extract test flavor from Network Operator job name.
    
    Test flavors combine:
    - Infrastructure: DOCA4, Bare Metal, Hosted
    - Test type: Legacy SR-IOV RDMA, Shared Device RDMA, E2E, etc.
    - GPU presence: with GPU or without
    
    Examples:
        - "pull-ci-...-doca4-nvidia-network-operator-legacy-sriov-rdma" -> "DOCA4 - RDMA Legacy SR-IOV"
        - "pull-ci-...-bare-metal-nvidia-network-operator-bare-metal-e2e-doca4-latest" -> "Bare Metal - E2E"
        - "pull-ci-...-hosted-nvidia-network-operator-sriov-rdma-gpu" -> "Hosted - RDMA SR-IOV with GPU"
    
    Args:
        job_name: Full job name from Prow
        
    Returns:
        Human-readable test flavor string
    """
    job_lower = job_name.lower()
    
    # Identify infrastructure type
    infrastructure = None
    if "doca4" in job_lower and "bare-metal" not in job_lower:
        infrastructure = "DOCA4"
    elif "bare-metal" in job_lower:
        infrastructure = "Bare Metal"
    elif "hosted" in job_lower:
        infrastructure = "Hosted"
    
    # Identify RDMA/test type
    rdma_type = None
    if "legacy-sriov-rdma" in job_lower or "rdma-legacy-sriov" in job_lower:
        rdma_type = "RDMA Legacy SR-IOV"
    elif "shared-device-rdma" in job_lower or "rdma-shared-dev" in job_lower:
        rdma_type = "RDMA Shared Device"
    elif "sriov" in job_lower and "rdma" in job_lower:
        rdma_type = "RDMA SR-IOV"
    elif "rdma" in job_lower:
        rdma_type = "RDMA"
    
    # Identify test type if not RDMA
    test_type = None
    if not rdma_type:
        if "bare-metal-e2e" in job_lower:
            test_type = "E2E"
        elif "nvidia-network-operator-e2e" in job_lower or "-e2e" in job_lower:
            test_type = "E2E"
    
    # Check for GPU
    has_gpu = "gpu" in job_lower or "gpudirect" in job_lower
    
    # Build flavor string
    parts = []
    
    if infrastructure:
        parts.append(infrastructure)
    
    if rdma_type:
        if has_gpu:
            parts.append(f"{rdma_type} with GPU")
        else:
            parts.append(rdma_type)
    elif test_type:
        if has_gpu:
            parts.append(f"{test_type} with GPU")
        else:
            parts.append(test_type)
    elif has_gpu:
        parts.append("with GPU")
    
    if not parts:
        if infrastructure:
            return infrastructure
        return "Standard"
    
    return " - ".join(parts)


def process_single_nno_build(
    pr_number: str,
    job_name: str,
    build_id: str,
    finished_file: Dict[str, Any],
    ocp_file: str,
    operator_file: str
) -> TestResult:
    """
    Process a single Network Operator build and create a TestResult.
    
    Args:
        pr_number: PR number
        job_name: Job name
        build_id: Build ID
        finished_file: Parsed finished.json dict
        ocp_file: ocp.version content string
        operator_file: operator.version content string
        
    Returns:
        TestResult object containing the build information
    """
    from workflows.common.data_fetching import extract_test_status, extract_timestamp, determine_repo_from_job_name
    
    # Extract test flavor from job name
    test_flavor = extract_test_flavor_from_job_name(job_name)
    
    # Get OCP version
    ocp_version = ocp_file if ocp_file else "Unknown"
    
    # Get operator version
    operator_version = operator_file if operator_file else "Unknown"
    
    # Get test status using common function
    test_status = extract_test_status(finished_file, STATUS_SUCCESS, STATUS_FAILURE, STATUS_ABORTED)
    
    # Get timestamp using common function
    timestamp = extract_timestamp(finished_file)
    
    # Build Prow URL (construct the finished.json path)
    repo = determine_repo_from_job_name(job_name)
    finished_path = f"pr-logs/pull/{repo}/{pr_number}/{job_name}/{build_id}/finished.json"
    prow_url = build_prow_job_url(finished_path)
    
    return TestResult(
        ocp_full_version=ocp_version,
        operator_version=operator_version,
        test_status=test_status,
        prow_job_url=prow_url,
        job_timestamp=str(timestamp),
        test_flavor=test_flavor
    )


def process_tests_for_pr(pr_number: str, results_by_ocp: Dict[str, Dict[str, Any]]) -> None:
    from workflows.common.data_fetching import build_version_lookups
    
    logger.info(f"Fetching Network Operator test data for PR #{pr_number}")
    
    # Fetch all finished.json files, then filter for network operator jobs
    # Network operator finished.json files are at the build root, so we need to:
    # 1. Fetch all finished.json files
    # 2. Filter for paths containing "nvidia-network-operator" in the job name
    all_finished_files = fetch_filtered_files(pr_number, "**/finished.json")
    
    # Filter for Network Operator jobs by checking if job name contains "nvidia-network-operator"
    finished_files = []
    for file_item in all_finished_files:
        path = file_item.get("name", "")
        # Check if this is a network operator job by looking for the pattern in the path
        if "nvidia-network-operator" in path and path.endswith("/finished.json"):
            # Additional check: must be at build root, not nested in artifacts
            # Path should look like: .../rehearse-X-...-nvidia-network-operator-.../BUILD_ID/finished.json
            if path.count("/finished.json") == 1 and "/artifacts/" not in path.split("/finished.json")[0]:
                finished_files.append(file_item)
    
    # Now fetch version files - try multiple possible artifact paths
    # Network Operator artifacts can be in different locations depending on test type
    ocp_version_files = []
    operator_version_files = []
    
    # Try common artifact patterns
    version_patterns = [
        "**/network-operator-e2e/ocp.version",
        "**/artifacts/ocp.version",
        "**/nvidia-network-operator*/ocp.version",
    ]
    
    for pattern in version_patterns:
        files = fetch_filtered_files(pr_number, pattern)
        for file_item in files:
            path = file_item.get("name", "")
            # Only include if it's part of a network operator job
            if "nvidia-network-operator" in path:
                ocp_version_files.append(file_item)
    
    for pattern in version_patterns:
        operator_pattern = pattern.replace("ocp.version", "operator.version")
        files = fetch_filtered_files(pr_number, operator_pattern)
        for file_item in files:
            path = file_item.get("name", "")
            if "nvidia-network-operator" in path:
                operator_version_files.append(file_item)
    
    logger.info(f"Found {len(finished_files)} finished.json files")
    
    # Build lookup dictionaries by BUILD_ID (not by parent directory)
    # Network Operator version files are deeply nested, so we need to extract the build ID from the path
    ocp_lookup = {}
    operator_lookup = {}
    
    for file_item in ocp_version_files:
        path = file_item["name"]
        # Extract build ID from path using regex
        match = NNO_TEST_PATH_REGEX.search(path)
        if match:
            build_id = match.group("build_id")
            pr_num = match.group("pr_number")
            job_name = match.group("job_name")
            # Use the full build directory path as key
            build_dir_key = f"pr-logs/pull/{match.group('repo')}/{pr_num}/{job_name}/{build_id}"
            try:
                content = fetch_gcs_file_content(path)
                ocp_lookup[build_dir_key] = content.strip()
            except Exception as e:
                logger.warning(f"Failed to fetch OCP version from {path}: {e}")
    
    for file_item in operator_version_files:
        path = file_item["name"]
        match = NNO_TEST_PATH_REGEX.search(path)
        if match:
            build_id = match.group("build_id")
            pr_num = match.group("pr_number")
            job_name = match.group("job_name")
            build_dir_key = f"pr-logs/pull/{match.group('repo')}/{pr_num}/{job_name}/{build_id}"
            try:
                content = fetch_gcs_file_content(path)
                operator_lookup[build_dir_key] = content.strip()
            except Exception as e:
                logger.warning(f"Failed to fetch operator version from {path}: {e}")
    
    # Process each finished.json file
    processed_count = 0
    for finished_item in finished_files:
        finished_path = finished_item["name"]
        
        # Parse the path to extract job information
        match = NNO_TEST_PATH_REGEX.search(finished_path)
        if not match:
            logger.warning(f"Could not parse path: {finished_path}")
            continue
        
        job_name = match.group("job_name")
        build_id = match.group("build_id")
        pr_num = match.group("pr_number")
        repo = match.group("repo")
        
        # Build the lookup key (same format as we used when building the lookup dictionaries)
        build_dir = f"pr-logs/pull/{repo}/{pr_num}/{job_name}/{build_id}"
        
        logger.info(f"Processing build {build_id} for job {job_name}")
        
        # Fetch finished.json content
        try:
            finished_content_str = fetch_gcs_file_content(finished_path)
            finished_json = json.loads(finished_content_str)
        except Exception as e:
            logger.warning(f"Failed to fetch/parse finished.json from {finished_path}: {e}")
            continue
        
        # Get OCP and operator versions using the build directory key
        ocp_content = ocp_lookup.get(build_dir)
        operator_content = operator_lookup.get(build_dir)
        
        if not ocp_content:
            logger.warning(f"Missing ocp.version for {build_dir}")
            continue
        if not operator_content:
            logger.warning(f"Missing operator.version for {build_dir}")
            continue
        
        # Create TestResult
        try:
            result = process_single_nno_build(
                pr_number,
                job_name,
                build_id,
                finished_json,
                ocp_content,
                operator_content
            )
        except Exception as e:
            logger.error(f"Failed to process build {build_id}: {e}")
            continue
        
        # Validate OCP version
        if not is_valid_ocp_version(result.ocp_full_version):
            logger.warning(f"Skipping result - invalid OCP version '{result.ocp_full_version}' for job {job_name}")
            continue
        
        # Initialize OCP version structure if needed
        ocp_version = result.ocp_full_version
        if ocp_version not in results_by_ocp:
            results_by_ocp[ocp_version] = {
                "notes": [],
                "bundle_tests": [],
                "release_tests": [],
                "job_history_links": set(),
                "test_flavors": {}
            }
        
        # Add job history link
        job_history_url = build_job_history_url(job_name)
        results_by_ocp[ocp_version]["job_history_links"].add(job_history_url)
        
        # Add result to appropriate test flavor
        test_flavor = result.test_flavor
        if test_flavor not in results_by_ocp[ocp_version]["test_flavors"]:
            results_by_ocp[ocp_version]["test_flavors"][test_flavor] = {
                "results": [],
                "job_history_links": set()
            }
        
        results_by_ocp[ocp_version]["test_flavors"][test_flavor]["results"].append(result.to_dict())
        results_by_ocp[ocp_version]["test_flavors"][test_flavor]["job_history_links"].add(job_history_url)
        
        processed_count += 1
    
    logger.info(f"Processed {processed_count} builds for PR #{pr_number}")


def merge_and_save_results(
    new_results: Dict[str, Dict[str, Any]],
    output_filepath: str,
    existing_results: Dict[str, Dict[str, Any]] = None
) -> None:
    """
    Merge new results with existing results and save to file.
    
    Args:
        new_results: New test results to add
        output_filepath: Path to save merged results
        existing_results: Existing results to merge with (optional)
    """
    from workflows.common.data_fetching import merge_job_history_links, convert_sets_to_lists_recursive
    
    if existing_results is None:
        existing_results = {}
    
    # Merge results
    merged = dict(existing_results)
    for ocp_version, version_data in new_results.items():
        if ocp_version not in merged:
            merged[ocp_version] = version_data
            # Convert sets to lists for JSON serialization
            merged[ocp_version]["job_history_links"] = merge_job_history_links(
                version_data.get("job_history_links", set()), []
            )
            for flavor, flavor_data in merged[ocp_version].get("test_flavors", {}).items():
                flavor_data["job_history_links"] = merge_job_history_links(
                    flavor_data.get("job_history_links", set()), []
                )
        else:
            # Merge test flavors
            existing_flavors = merged[ocp_version].get("test_flavors", {})
            new_flavors = version_data.get("test_flavors", {})
            
            for flavor, flavor_data in new_flavors.items():
                if flavor not in existing_flavors:
                    existing_flavors[flavor] = flavor_data
                    existing_flavors[flavor]["job_history_links"] = merge_job_history_links(
                        flavor_data.get("job_history_links", set()), []
                    )
                else:
                    # Merge results
                    existing_flavors[flavor]["results"].extend(flavor_data["results"])
                    
                    # Merge job history links using common function
                    existing_flavors[flavor]["job_history_links"] = merge_job_history_links(
                        flavor_data.get("job_history_links", set()),
                        existing_flavors[flavor].get("job_history_links", [])
                    )
            
            merged[ocp_version]["test_flavors"] = existing_flavors
            
            # Merge global job history links using common function
            merged[ocp_version]["job_history_links"] = merge_job_history_links(
                version_data.get("job_history_links", set()),
                merged[ocp_version].get("job_history_links", [])
            )
    
    # Save to file
    with open(output_filepath, 'w') as f:
        json.dump(merged, f, indent=2)
    
    logger.info(f"Saved merged results to {output_filepath}")


def main() -> None:
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Fetch Network Operator CI data")
    parser.add_argument("--pr_number", required=True, help="PR number to process")
    parser.add_argument("--baseline_data_filepath", help="Path to existing JSON data")
    parser.add_argument("--merged_data_filepath", required=True, help="Path to save merged data")
    
    args = parser.parse_args()
    
    # Load existing data if available
    existing_data = {}
    if args.baseline_data_filepath:
        try:
            with open(args.baseline_data_filepath, 'r') as f:
                existing_data = json.load(f)
            logger.info(f"Loaded existing data from {args.baseline_data_filepath}")
        except FileNotFoundError:
            logger.info("No existing data file found, starting fresh")
        except json.JSONDecodeError as e:
            logger.warning(f"Failed to parse existing data: {e}, starting fresh")
    
    # Fetch new data
    new_data = {}
    process_tests_for_pr(args.pr_number, new_data)
    
    # Merge and save
    merge_and_save_results(new_data, args.merged_data_filepath, existing_data)


if __name__ == "__main__":
    main()

