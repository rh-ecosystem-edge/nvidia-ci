#!/usr/bin/env python3
"""Generate weekly summary of tested versions from git history."""

import argparse
import json
import os
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Dict, Set

from workflows.common.utils import logger

# Version type constants (matching update_versions.py)
VERSION_GPU_MAIN_LATEST = "gpu-main-latest"
VERSION_GPU_OPERATOR = "gpu-operator"
VERSION_OCP = "ocp"


def get_commits_in_range(file_path: str, since: datetime, until: datetime) -> list[str]:
    """Get all commit hashes that modified the file in the given date range (oldest first)."""
    result = subprocess.run(
        [
            "git", "log",
            "--reverse",
            f"--since={since.isoformat()}",
            f"--until={until.isoformat()}",
            "--format=%H",
            "--",
            file_path
        ],
        capture_output=True,
        text=True,
        check=True
    )
    commits = [line.strip() for line in result.stdout.strip().split("\n") if line.strip()]
    return commits


def file_exists_at_commit(commit: str, file_path: str) -> bool:
    """Check if a file exists at a specific commit."""
    result = subprocess.run(
        ["git", "cat-file", "-e", f"{commit}:{file_path}"],
        capture_output=True,
    )
    return result.returncode == 0


def get_file_at_commit(commit: str, file_path: str) -> dict | None:
    """Get the content of a file at a specific commit. Returns None if file doesn't exist."""
    if not file_exists_at_commit(commit, file_path):
        return None
    result = subprocess.run(
        ["git", "show", f"{commit}:{file_path}"],
        capture_output=True,
        text=True,
        check=True
    )
    return json.loads(result.stdout)


def extract_versions_from_dict(old_dict: dict, new_dict: dict) -> Set[str]:
    """Extract all version strings that were added or updated between two dicts."""
    versions = set()

    # For nested dicts (gpu-operator and ocp), the values are the patch versions we want
    # Check for new keys or updated values
    for key, new_value in new_dict.items():
        old_value = old_dict.get(key)
        if old_value != new_value:
            # Value changed or was added
            if isinstance(new_value, str):
                # This is a patch version (e.g., "25.3.5", "4.20.11")
                versions.add(new_value)
            elif isinstance(new_value, dict):
                # Recursively check nested dicts
                old_nested = old_dict.get(key, {})
                if isinstance(old_nested, dict):
                    versions.update(extract_versions_from_dict(old_nested, new_value))

    return versions


def collect_tested_versions(commits: list[str], file_path: str) -> Dict[str, Set[str]]:
    """Collect all tested versions from git history."""
    tested_versions = {
        VERSION_GPU_MAIN_LATEST: set(),
        VERSION_GPU_OPERATOR: set(),
        VERSION_OCP: set(),
    }

    if not commits:
        return tested_versions

    first_commit = commits[0]
    prev_content = get_file_at_commit(f"{first_commit}^", file_path)

    for commit in commits:
        current_content = get_file_at_commit(commit, file_path)
        if current_content is None:
            continue

        for key in tested_versions:
            if key not in current_content:
                continue
            new_val = current_content[key]
            old_val = prev_content.get(key)
            if isinstance(new_val, dict):
                tested_versions[key].update(extract_versions_from_dict(old_val or {}, new_val))
            elif new_val != old_val:
                tested_versions[key].add(new_val)

        prev_content = current_content

    return tested_versions


def format_version_section(title: str, versions: Set[str]) -> list[str]:
    """Format a section with title and version list."""
    lines = [f"## {title}"]
    if versions:
        lines.extend(f"- {v}" for v in sorted(versions))
    else:
        lines.append("- (none)")
    lines.append("")
    return lines


def generate_markdown_summary(tested_versions: Dict[str, Set[str]], start_date: datetime, end_date: datetime) -> str:
    """Generate markdown summary of tested versions."""
    start_str = start_date.strftime("%Y-%m-%d")
    end_str = end_date.strftime("%Y-%m-%d")

    lines = [f"# Weekly test summary: {start_str} to {end_str}", ""]

    sections = [
        ("GPU Operator Development Versions", VERSION_GPU_MAIN_LATEST),
        ("GPU Operator Releases", VERSION_GPU_OPERATOR),
        ("OpenShift Releases", VERSION_OCP),
    ]
    for title, key in sections:
        lines.extend(format_version_section(title, tested_versions[key]))

    lines.append("## Summary")
    lines.append(f"- Development versions of GPU operator: {len(tested_versions[VERSION_GPU_MAIN_LATEST])}")
    lines.append(f"- Releases of GPU operator: {len(tested_versions[VERSION_GPU_OPERATOR])}")
    lines.append(f"- Releases of OpenShift: {len(tested_versions[VERSION_OCP])}")

    return "\n".join(lines)


def parse_date(date_str: str) -> datetime:
    """Parse date string (YYYY-MM-DD) to datetime at midnight UTC."""
    return datetime.strptime(date_str, "%Y-%m-%d").replace(tzinfo=timezone.utc)


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Generate weekly summary of tested versions")
    parser.add_argument("--since", help="Start date (YYYY-MM-DD), default: 7 days ago")
    parser.add_argument("--until", help="End date (YYYY-MM-DD), default: today")
    args = parser.parse_args()

    file_path = os.environ["VERSION_FILE_PATH"]

    if args.until:
        end_date = parse_date(args.until)
    else:
        end_date = datetime.now(timezone.utc)

    if args.since:
        start_date = parse_date(args.since)
    else:
        start_date = end_date - timedelta(days=7)

    logger.info(f"Collecting tested versions from {start_date.date()} to {end_date.date()}")

    commits = get_commits_in_range(file_path, start_date, end_date)
    logger.info(f"Found {len(commits)} commits to {file_path}")

    if not commits:
        logger.info("No commits found in the date range")

    tested_versions = collect_tested_versions(commits, file_path)
    summary = generate_markdown_summary(tested_versions, start_date, end_date)

    print(summary)

    output_file = Path("weekly-summary.md")
    output_file.write_text(summary)
    logger.info(f"Summary written to {output_file}")


if __name__ == "__main__":
    main()
