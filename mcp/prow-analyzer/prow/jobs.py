"""Prow job discovery and management."""

from collections import defaultdict
from dataclasses import dataclass
from typing import Any, Dict, List

from config import RepositoryInfo
from gcs import client as gcs_client
from gcs.paths import build_pr_path, build_prow_url
from prow.logs import analyze_log_for_failure, get_build_log
from prow.statuses import STATUS_SUCCESS, STATUS_FAILURE, STATUS_UNKNOWN


@dataclass
class JobBuild:
    """Represents a single job build."""
    repository: str
    pr_number: str
    job_name: str
    build_id: str
    status: str
    prow_url: str

    def to_dict(self) -> Dict[str, Any]:
        return {
            "repository": self.repository,
            "pr_number": self.pr_number,
            "job_name": self.job_name,
            "build_id": self.build_id,
            "status": self.status,
            "prow_url": self.prow_url,
        }


def get_latest_build_id(bucket: str, path_template: str, repo_info: RepositoryInfo,
                       pr_number: str, job_name: str) -> str | None:
    """Get the latest build ID for a job."""
    pr_path = build_pr_path(repo_info, pr_number, path_template)
    latest_build_path = f"{pr_path}/{job_name}/latest-build.txt"

    content = gcs_client.fetch_file(bucket, latest_build_path)
    return content.strip() if content else None


def get_all_jobs_for_pr(config: Dict[str, Any], repo_info: RepositoryInfo, pr_number: str) -> List[JobBuild]:
    """Get all jobs and their latest builds for a PR."""
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    pr_path = build_pr_path(repo_info, pr_number, path_template)

    # List all job directories
    job_names = gcs_client.list_directories(bucket, pr_path + "/")

    builds = []
    for job_name in job_names:
        # Get latest build ID
        build_id = get_latest_build_id(bucket, path_template, repo_info, pr_number, job_name)
        if not build_id:
            continue

        # Fetch build log to determine status
        log_content = get_build_log(config, repo_info, pr_number, job_name, build_id)
        status = analyze_log_for_failure(log_content) if log_content else STATUS_UNKNOWN

        prow_url = build_prow_url(
            repo_info, pr_number, job_name, build_id,
            path_template, bucket, config["gcsweb_base_url"]
        )

        build = JobBuild(
            repository=repo_info.full_name,
            pr_number=pr_number,
            job_name=job_name,
            build_id=build_id,
            status=status,
            prow_url=prow_url,
        )
        builds.append(build)

    return builds


def get_failed_jobs_for_pr(config: Dict[str, Any], repo_info: RepositoryInfo, pr_number: str) -> Dict[str, JobBuild]:
    """Get all jobs where the latest build failed."""
    all_builds = get_all_jobs_for_pr(config, repo_info, pr_number)

    failed_jobs = {}
    for build in all_builds:
        if build.status == STATUS_FAILURE:
            failed_jobs[build.job_name] = build

    return failed_jobs


def get_pr_jobs_overview(config: Dict[str, Any], repo_info: RepositoryInfo, pr_number: str) -> Dict[str, Any]:
    """Get comprehensive overview of all jobs in a PR, including status and statistics."""
    all_builds = get_all_jobs_for_pr(config, repo_info, pr_number)

    # Count by status
    status_counts = defaultdict(int)
    for build in all_builds:
        status_counts[build.status] += 1

    total_jobs = len(all_builds)
    success_count = status_counts[STATUS_SUCCESS]
    failure_count = status_counts[STATUS_FAILURE]
    unknown_count = status_counts[STATUS_UNKNOWN]

    # Calculate success rate
    success_rate = (success_count / total_jobs * 100) if total_jobs > 0 else 0

    # Group jobs by status
    jobs_by_status = {
        "success": [build.to_dict() for build in all_builds if build.status == STATUS_SUCCESS],
        "failure": [build.to_dict() for build in all_builds if build.status == STATUS_FAILURE],
        "unknown": [build.to_dict() for build in all_builds if build.status == STATUS_UNKNOWN],
    }

    return {
        "repository": repo_info.full_name,
        "pr_number": pr_number,
        "total_jobs": total_jobs,
        "statistics": {
            "success_count": success_count,
            "failure_count": failure_count,
            "unknown_count": unknown_count,
            "success_rate_percent": round(success_rate, 2),
        },
        "jobs_by_status": jobs_by_status,
        "summary": f"{total_jobs} total jobs: {success_count} passed, {failure_count} failed, {unknown_count} unknown ({success_rate:.1f}% success rate)",
    }

