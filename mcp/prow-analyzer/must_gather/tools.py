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

    Uses a single GCS API call to list all objects under artifacts/, then filters
    for must-gather directories and archives. Much more efficient than recursive traversal.

    Returns list containing:
    - Extracted directories (type='extracted'): Can be analyzed via list_must_gather_files
    - Archives (type='archive'): Require local download for analysis with tools like 'omc'

    Each archive includes a download_url for easy access.
    """
    bucket = config["gcs_bucket"]
    path_template = config["path_template"]
    gcsweb_base = config["gcsweb_base_url"]
    artifacts_prefix = build_artifacts_path(
        repo_info, pr_number, job_name, build_id, path_template
    ).rstrip('/')

    # Get all objects under artifacts/ in one API call
    all_objects = gcs_client.list_all_objects(bucket, artifacts_prefix)

    results = []
    found_dirs = set()  # Track directories we've already added

    def _is_must_gather_name(name: str) -> bool:
        """Check if a name contains must-gather or must_gather."""
        name_lower = name.lower()
        return "must-gather" in name_lower or "must_gather" in name_lower

    # Process all objects to find must-gather directories and archives
    for obj in all_objects:
        relative_path = obj["name"]
        full_path = obj["full_path"]

        # Check if this is a must-gather archive file (check filename only, not path)
        filename = relative_path.split('/')[-1]
        if _is_must_gather_name(filename) and _is_archive(filename):
            results.append({
                "filename": filename,
                "path": relative_path,
                "full_path": full_path,
                "size_bytes": obj["size"],
                "type": "archive",
                "download_url": f"{gcsweb_base}/{bucket}/{full_path}",
            })
            continue

        # Check each directory component in the path for must-gather (exclude filename)
        path_parts = relative_path.split('/')
        # Skip the last element (filename) - only check directory components
        for i, part in enumerate(path_parts[:-1]):
            if _is_must_gather_name(part):
                # This is a must-gather directory
                dir_path = '/'.join(path_parts[:i+1])

                # Skip if we've already added this directory
                if dir_path in found_dirs:
                    continue

                found_dirs.add(dir_path)
                results.append({
                    "path": dir_path,
                    "full_path": f"{artifacts_prefix}/{dir_path}",
                    "type": "extracted",
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

