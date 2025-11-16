"""Must-gather artifact analysis tools."""

from fnmatch import fnmatch
from typing import Any, Callable, Dict, List

from config import RepositoryInfo
from gcs import client as gcs_client
from gcs.paths import build_artifacts_path

# Archive extensions to filter out (we can't usefully read these as text)
ARCHIVE_EXTENSIONS = ('.tar', '.gz', '.tgz', '.tar.gz', '.zip', '.bz2', '.tar.bz2', '.xz', '.tar.xz')


def _is_archive(filename: str) -> bool:
    """Check if a filename is an archive based on extension."""
    return filename.lower().endswith(ARCHIVE_EXTENSIONS)


def _search_directory_recursive(bucket: str, base_path: str, filter_fn: Callable[[Dict[str, Any]], bool],
                                max_depth: int = 5) -> List[Dict[str, Any]]:
    """
    Recursively search a directory structure and collect files matching a filter.

    Args:
        bucket: GCS bucket name
        base_path: GCS base path to start search
        filter_fn: Function that takes file_info dict and returns True if file should be included
        max_depth: Maximum recursion depth

    Returns:
        List of matching files with path, name, size, and full_path
    """
    results = []

    def search_directory(dir_path: str, relative_path: str = ""):
        """Inner recursive function."""
        data = gcs_client.list_files_and_directories(bucket, dir_path + "/")

        # Check files in current directory
        for file_info in data.get("files", []):
            if filter_fn(file_info):
                results.append({
                    "name": file_info["name"],
                    "path": f"{relative_path}/{file_info['name']}" if relative_path else file_info["name"],
                    "full_path": f"{dir_path}/{file_info['name']}",
                    "size": file_info["size"],
                })

        # Recursively search subdirectories (with depth limit)
        if relative_path.count('/') < max_depth:
            for subdir in data.get("directories", []):
                new_relative = f"{relative_path}/{subdir}" if relative_path else subdir
                search_directory(f"{dir_path}/{subdir}", new_relative)

    search_directory(base_path)
    return results


def find_must_gather_dirs(config: Dict[str, Any], repo_info: RepositoryInfo,
                         pr_number: str, job_name: str, build_id: str) -> List[Dict[str, Any]]:
    """
    Find all must-gather data (both extracted directories and archives).

    Returns list containing:
    - Extracted directories (type='extracted'): Can be analyzed via list_must_gather_files
    - Archives (type='archive'): Require local download for analysis with tools like 'omc'

    Each archive includes a download_url for easy access.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    gcsweb_base = config["gcsweb_base_url"]
    artifacts_prefix = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template)

    top_dirs = gcs_client.list_directories(bucket, artifacts_prefix)
    results = []

    # Search for must-gather directories and archives at multiple levels
    for top_dir in top_dirs:
        dir_path = f"{artifacts_prefix}{top_dir}"

        # Check if this directory itself is a must-gather
        if "must-gather" in top_dir.lower():
            results.append({
                "path": top_dir,
                "full_path": dir_path,
                "level": "step",
                "type": "extracted",
            })

        # Check one level deeper
        subdirs_data = gcs_client.list_files_and_directories(bucket, dir_path + "/")

        # Check for extracted directories
        for subdir in subdirs_data.get("directories", []):
            if "must-gather" in subdir.lower():
                subdir_full_path = f"{dir_path}/{subdir}"
                subdir_contents = gcs_client.list_files_and_directories(bucket, subdir_full_path + "/")

                # Only include if it has directories (meaning it's extracted)
                if subdir_contents.get("directories"):
                    results.append({
                        "path": f"{top_dir}/{subdir}",
                        "full_path": subdir_full_path,
                        "level": "nested",
                        "type": "extracted",
                    })

        # Check for archive files
        for file_info in subdirs_data.get("files", []):
            filename = file_info["name"]
            if "must-gather" in filename.lower() and _is_archive(filename):
                file_path = f"{dir_path}/{filename}"
                results.append({
                    "filename": filename,
                    "path": f"{top_dir}/{filename}",
                    "full_path": file_path,
                    "size_bytes": file_info["size"],
                    "type": "archive",
                    "download_url": f"{gcsweb_base}/{bucket}/{file_path}",
                })

    return results


def list_must_gather_files(config: Dict[str, Any], repo_info: RepositoryInfo,
                           pr_number: str, job_name: str, build_id: str,
                           must_gather_path: str, include_archives: bool = False,
                           pattern: str = "*") -> List[Dict[str, Any]]:
    """
    List files in a must-gather directory (excluding archives by default).

    Args:
        config: Configuration dictionary
        repo_info: Repository information
        pr_number: PR number
        job_name: Job name
        build_id: Build ID
        must_gather_path: Path to must-gather directory
        include_archives: If True, include archive files (.tar, .gz, etc.)
        pattern: Optional pattern to filter files (e.g., '*.log', '*.yaml'). Default: '*' (all files)

    Returns:
        List of files with their paths and sizes.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    mg_base_path = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template, must_gather_path)

    # Build filter function
    def filter_fn(f):
        # Check pattern match
        if not fnmatch(f["name"].lower(), pattern.lower()):
            return False
        # Check archive filtering
        if include_archives:
            return True
        return not _is_archive(f["name"])

    return _search_directory_recursive(
        bucket,
        mg_base_path.rstrip('/'),
        filter_fn=filter_fn
    )


def get_must_gather_file(config: Dict[str, Any], repo_info: RepositoryInfo,
                         pr_number: str, job_name: str, build_id: str,
                         must_gather_path: str, file_path: str) -> Dict[str, Any]:
    """
    Fetch any file from a must-gather directory (logs, YAML, HTML, JSON, etc.).

    Args:
        config: Configuration dictionary
        repo_info: Repository information
        pr_number: PR number
        job_name: Job name
        build_id: Build ID
        must_gather_path: Path to must-gather directory
        file_path: Path to file within must-gather

    Returns:
        Dictionary with file content and metadata, or error if not found.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    full_path = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template, must_gather_path, file_path).rstrip('/')

    file_content = gcs_client.fetch_file(bucket, full_path)
    if file_content is None:
        return {
            "repository": repo_info.full_name,
            "pr_number": pr_number,
            "job_name": job_name,
            "build_id": build_id,
            "must_gather_path": must_gather_path,
            "file_path": file_path,
            "error": "File not found",
        }

    return {
        "repository": repo_info.full_name,
        "pr_number": pr_number,
        "job_name": job_name,
        "build_id": build_id,
        "must_gather_path": must_gather_path,
        "file_path": file_path,
        "content": file_content,
        "size_bytes": len(file_content),
        "size_lines": len(file_content.split('\n')),
    }


def search_must_gather_files(config: Dict[str, Any], repo_info: RepositoryInfo,
                            pr_number: str, job_name: str, build_id: str,
                            must_gather_path: str, pattern: str,
                            include_archives: bool = False) -> List[Dict[str, Any]]:
    """
    Search for files matching a pattern in a must-gather directory.

    Pattern supports wildcards: *.yaml, *events*, etc.
    Archives (.tar, .gz, etc.) are excluded by default.

    Use get_must_gather_file to read the content of found files.

    Args:
        config: Configuration dictionary
        repo_info: Repository information
        pr_number: PR number
        job_name: Job name
        build_id: Build ID
        must_gather_path: Path to must-gather directory
        pattern: File pattern with wildcards (e.g., '*.yaml', '*events*')
        include_archives: If True, include archive files in results

    Returns:
        List of matching files with their paths and sizes.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    mg_base_path = build_artifacts_path(repo_info, pr_number, job_name, build_id, path_template, must_gather_path)

    # Search for files matching pattern (case-insensitive), excluding archives unless requested
    def filter_fn(f):
        matches_pattern = fnmatch(f["name"].lower(), pattern.lower())
        if not matches_pattern:
            return False
        if include_archives:
            return True
        return not _is_archive(f["name"])

    return _search_directory_recursive(
        bucket,
        mg_base_path.rstrip('/'),
        filter_fn=filter_fn
    )

