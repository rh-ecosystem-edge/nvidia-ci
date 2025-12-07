"""
Common data fetching patterns for CI dashboards.
Shared logic for building file lookups and processing builds.
"""

import json
from typing import Dict, Any, List, Tuple, Optional
from workflows.common.utils import logger
from workflows.common.gcs_utils import fetch_gcs_file_content


def build_version_lookups(
    version_files_list: List[Tuple[str, List[Dict[str, Any]]]]
) -> Dict[str, Dict[str, str]]:
    """
    Build lookup dictionaries for version files organized by build directory.
    
    Args:
        version_files_list: List of tuples (file_type, file_items)
                           e.g., [("ocp", ocp_files), ("operator", operator_files)]
    
    Returns:
        Dict mapping file_type to {build_dir: content}
        e.g., {"ocp": {build_dir: "4.17.16"}, "operator": {build_dir: "25.4.0"}}
    """
    version_lookups = {}
    
    for file_type, file_items in version_files_list:
        lookup = {}
        for file_item in file_items:
            path = file_item["name"]
            build_dir = path.rsplit("/", 1)[0]
            try:
                content = fetch_gcs_file_content(path)
                lookup[build_dir] = content.strip()
            except Exception as e:
                logger.warning(f"Failed to fetch {file_type} from {path}: {e}")
        version_lookups[file_type] = lookup
    
    return version_lookups


def build_finished_lookup(
    finished_files: List[Dict[str, Any]]
) -> Dict[str, Dict[str, Any]]:
    """
    Build lookup dictionary for finished.json files by build directory.
    
    Args:
        finished_files: List of finished.json file items from GCS
    
    Returns:
        Dict mapping build_dir to parsed finished.json content
    """
    finished_lookup = {}
    
    for finished_item in finished_files:
        finished_path = finished_item["name"]
        build_dir = finished_path.rsplit("/", 1)[0]
        try:
            content = fetch_gcs_file_content(finished_path)
            finished_lookup[build_dir] = json.loads(content)
        except Exception as e:
            logger.warning(f"Failed to fetch/parse finished.json from {finished_path}: {e}")
    
    return finished_lookup


def extract_test_status(
    finished_json: Dict[str, Any],
    status_success: str,
    status_failure: str,
    status_aborted: str
) -> str:
    """
    Extract and normalize test status from finished.json.
    
    Args:
        finished_json: Parsed finished.json content
        status_success: String constant for success status
        status_failure: String constant for failure status
        status_aborted: String constant for aborted status
        
    Returns:
        Normalized test status string
    """
    result_str = finished_json.get("result", "UNKNOWN").upper()
    if result_str in [status_success, status_failure, status_aborted]:
        return result_str
    return status_failure


def extract_timestamp(finished_json: Dict[str, Any]) -> int:
    """
    Extract timestamp from finished.json.
    
    Args:
        finished_json: Parsed finished.json content
        
    Returns:
        Unix timestamp (defaults to 0 if not found)
    """
    return finished_json.get("timestamp", 0)


def determine_repo_from_job_name(job_name: str) -> str:
    """
    Determine repository from job name pattern.
    
    Args:
        job_name: Job name string
        
    Returns:
        Repository identifier ('openshift_release' or 'rh-ecosystem-edge_nvidia-ci')
    """
    return "openshift_release" if job_name.startswith("rehearse-") else "rh-ecosystem-edge_nvidia-ci"


def convert_sets_to_lists_recursive(data: Any) -> Any:
    """
    Recursively convert sets to sorted lists for JSON serialization.
    
    Args:
        data: Any data structure that may contain sets
        
    Returns:
        Data structure with sets converted to sorted lists
    """
    if isinstance(data, set):
        return sorted(list(data))
    elif isinstance(data, dict):
        return {k: convert_sets_to_lists_recursive(v) for k, v in data.items()}
    elif isinstance(data, list):
        return [convert_sets_to_lists_recursive(item) for item in data]
    else:
        return data


def merge_job_history_links(
    new_links: Any,
    existing_links: Any
) -> List[str]:
    """
    Merge and deduplicate job history links.
    
    Args:
        new_links: New links (can be set or list)
        existing_links: Existing links (can be set or list)
        
    Returns:
        Sorted list of unique links
    """
    # Convert both to sets
    new_set = set(new_links) if isinstance(new_links, (set, list)) else set()
    existing_set = set(existing_links) if isinstance(existing_links, (set, list)) else set()
    
    # Merge and return sorted list
    all_links = new_set | existing_set
    return sorted(list(all_links))


def int_or_none(value: Optional[str]) -> Optional[int]:
    """
    Convert string to int or None for unlimited.
    
    Args:
        value: String value to convert
        
    Returns:
        Integer or None
    """
    if value is None:
        return None
    if value.lower() in ('none', 'unlimited'):
        return None
    return int(value)

