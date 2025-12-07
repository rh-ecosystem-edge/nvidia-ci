"""
Common utilities shared across NVIDIA CI workflows.
"""

from workflows.common.utils import get_logger, logger
from workflows.common.templates import load_template
from workflows.common.data_structures import (
    TestResult,
    OCP_FULL_VERSION,
    OPERATOR_VERSION,
    GPU_OPERATOR_VERSION,
    STATUS_SUCCESS,
    STATUS_FAILURE,
    STATUS_ABORTED,
)
from workflows.common.gcs_utils import (
    http_get_json,
    fetch_gcs_file_content,
    build_prow_job_url,
    fetch_filtered_files,
    build_job_history_url,
    GCS_API_BASE_URL,
    GCS_MAX_RESULTS_PER_REQUEST,
)
from workflows.common.html_builders import (
    build_toc,
    build_notes,
    build_history_bar,
    build_last_updated_footer,
    sanitize_id,
)
from workflows.common.validation import (
    is_valid_ocp_version,
    has_valid_semantic_versions,
    is_infrastructure_type,
)
from workflows.common.data_fetching import (
    build_version_lookups,
    build_finished_lookup,
    extract_test_status,
    extract_timestamp,
    determine_repo_from_job_name,
    convert_sets_to_lists_recursive,
    merge_job_history_links,
    int_or_none,
)

__all__ = [
    # Utils
    "get_logger",
    "logger",
    "load_template",
    
    # Data structures
    "TestResult",
    "OCP_FULL_VERSION",
    "OPERATOR_VERSION",
    "GPU_OPERATOR_VERSION",
    "STATUS_SUCCESS",
    "STATUS_FAILURE",
    "STATUS_ABORTED",
    
    # GCS utilities
    "http_get_json",
    "fetch_gcs_file_content",
    "build_prow_job_url",
    "fetch_filtered_files",
    "build_job_history_url",
    "GCS_API_BASE_URL",
    "GCS_MAX_RESULTS_PER_REQUEST",
    
    # HTML builders
    "build_toc",
    "build_notes",
    "build_history_bar",
    "build_last_updated_footer",
    "sanitize_id",
    
    # Validation
    "is_valid_ocp_version",
    "has_valid_semantic_versions",
    "is_infrastructure_type",
    
    # Data fetching
    "build_version_lookups",
    "build_finished_lookup",
    "extract_test_status",
    "extract_timestamp",
    "determine_repo_from_job_name",
    "convert_sets_to_lists_recursive",
    "merge_job_history_links",
    "int_or_none",
]

