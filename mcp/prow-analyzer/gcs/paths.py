"""GCS path construction utilities."""

from typing import Any, Dict

from config import RepositoryInfo


def build_pr_path(repo_info: RepositoryInfo, pr_number: str, path_template: str) -> str:
    """
    Build the GCS path for a PR.

    Args:
        repo_info: Repository information
        pr_number: PR number
        path_template: Path template string with {org}, {repo}, {org_repo}, {pr_number} placeholders

    Returns:
        GCS path for the PR
    """
    return path_template.format(
        org=repo_info.org,
        repo=repo_info.repo,
        org_repo=repo_info.gcs_name,
        pr_number=pr_number
    )


def build_artifacts_path(repo_info: RepositoryInfo, pr_number: str, job_name: str,
                        build_id: str, path_template: str, *sub_paths: str) -> str:
    """
    Build a GCS path to artifacts directory with optional sub-paths.

    Args:
        repo_info: Repository information
        pr_number: PR number
        job_name: Job name
        build_id: Build ID
        path_template: Path template string
        *sub_paths: Optional sub-paths to append

    Returns:
        GCS path to artifacts (or sub-path within artifacts)

    Example:
        build_artifacts_path(repo, "123", "job", "456", template) -> "path/to/artifacts/"
        build_artifacts_path(repo, "123", "job", "456", template, "step", "file.txt") -> "path/to/artifacts/step/file.txt"
    """
    pr_path = build_pr_path(repo_info, pr_number, path_template)
    base = f"{pr_path}/{job_name}/{build_id}/artifacts"

    if sub_paths:
        return f"{base}/{'/'.join(sub_paths)}"
    return f"{base}/"


def build_prow_url(repo_info: RepositoryInfo, pr_number: str, job_name: str, build_id: str,
                  path_template: str, gcs_bucket: str, gcsweb_base_url: str) -> str:
    """
    Build web UI URL for a job build using configured GCSWeb base URL.

    Args:
        repo_info: Repository information
        pr_number: PR number
        job_name: Job name
        build_id: Build ID
        path_template: Path template string
        gcs_bucket: GCS bucket name
        gcsweb_base_url: GCSWeb base URL

    Returns:
        URL to view the build in GCSWeb
    """
    pr_path = build_pr_path(repo_info, pr_number, path_template)
    return f"{gcsweb_base_url}/{gcs_bucket}/{pr_path}/{job_name}/{build_id}"

