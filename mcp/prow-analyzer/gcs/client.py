"""GCS API client for fetching files and listing directories."""

import json
import urllib.parse
from typing import Any, Dict, List, Optional

import requests


def fetch_file(bucket: str, path: str) -> Optional[str]:
    """
    Fetch a file from GCS.

    Args:
        bucket: GCS bucket name
        path: File path within bucket

    Returns:
        File content as string, or None if not found
    """
    url = f"https://storage.googleapis.com/storage/v1/b/{bucket}/o/{urllib.parse.quote(path, safe='')}"

    try:
        response = requests.get(url, params={"alt": "media"}, timeout=30)
        response.raise_for_status()
        return response.text
    except Exception:
        return None


def fetch_file_with_metadata(bucket: str, path: str) -> Dict[str, Any]:
    """
    Fetch a file from GCS with metadata.

    Returns dict with content, size, and metadata.
    """
    content = fetch_file(bucket, path)

    if content is None:
        return {
            "path": path,
            "error": "File not found or could not be read",
            "content": None,
        }

    return {
        "path": path,
        "content": content,
        "size_bytes": len(content),
        "size_lines": len(content.split('\n')),
    }


def list_directories(bucket: str, prefix: str) -> List[str]:
    """
    List directories (common prefixes) under a GCS prefix.

    Args:
        bucket: GCS bucket name
        prefix: Path prefix to list

    Returns:
        List of directory names (without the prefix)
    """
    url = f"https://storage.googleapis.com/storage/v1/b/{bucket}/o"

    params = {
        "prefix": prefix,
        "delimiter": "/",
        "alt": "json",
    }

    try:
        response = requests.get(url, params=params, timeout=30)
        response.raise_for_status()
        data = response.json()

        # Extract directory names from prefixes
        prefixes = data.get("prefixes", [])
        # Remove the common prefix and trailing slash to get just the directory names
        directories = [p.rstrip('/').split('/')[-1] for p in prefixes]
        return directories
    except Exception as e:
        print(f"Error listing directories: {e}", flush=True)
        return []


def list_files_and_directories(bucket: str, path: str) -> Dict[str, Any]:
    """
    List both files and directories at a GCS path.

    Returns dict with 'files' and 'directories' lists.
    Files include name, size, and modified time.
    """
    url = f"https://storage.googleapis.com/storage/v1/b/{bucket}/o"

    # Normalize path - ensure it ends with / for directory listing
    if path and not path.endswith('/'):
        path = path + '/'

    params = {
        "prefix": path,
        "delimiter": "/",
        "alt": "json",
    }

    try:
        response = requests.get(url, params=params, timeout=30)
        response.raise_for_status()
        data = response.json()

        # Extract directories (prefixes)
        prefixes = data.get("prefixes", [])
        directories = []
        for p in prefixes:
            # Remove the common prefix and trailing slash to get just the directory name
            dir_name = p.rstrip('/').replace(path, '', 1)
            if dir_name:  # Skip empty
                directories.append(dir_name)

        # Extract files (items)
        items = data.get("items", [])
        files = []
        for item in items:
            file_name = item["name"].replace(path, '', 1)
            if file_name and file_name != path.rstrip('/'):  # Skip the directory itself
                files.append({
                    "name": file_name,
                    "size": int(item.get("size", 0)),
                    "updated": item.get("updated", ""),
                })

        return {
            "path": path.rstrip('/'),
            "directories": directories,
            "files": files,
            "total_directories": len(directories),
            "total_files": len(files),
        }
    except Exception as e:
        return {
            "path": path.rstrip('/'),
            "error": str(e),
            "directories": [],
            "files": [],
            "total_directories": 0,
            "total_files": 0,
        }

