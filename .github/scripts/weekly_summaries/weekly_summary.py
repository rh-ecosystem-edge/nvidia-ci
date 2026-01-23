#!/usr/bin/env python3
"""Generate weekly summary of tested versions from git history."""

import argparse
import json
import os
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Dict, Set

from common.utils import logger

VERSION_GPU_MAIN_LATEST = "gpu-main-latest"
VERSION_GPU_OPERATOR = "gpu-operator"
VERSION_OCP = "ocp"


def get_commits_in_range(file_path: str, since: datetime, until: datetime) -> list[tuple[str, str]]:
    """Get commits that modified the file in the date range, following renames.

    Returns list of (commit_hash, file_path_at_commit) tuples, oldest first.
    """
    result = subprocess.run(
        [
            "git", "log",
            "--follow",
            "-n50",
            "--format=%H %ct",
            "--name-only",
            "-z",
            "--",
            file_path
        ],
        capture_output=True,
        text=True,
        check=True
    )

    since_ts, until_ts = int(since.timestamp()), int(until.timestamp())
    commits = []
    seen_commits = set()

    parts = [p.strip() for p in result.stdout.split("\0") if p.strip()]
    current_header = None
    for part in parts:
        tokens = part.split()
        if len(tokens) >= 2 and len(tokens[0]) == 40 and tokens[1].isdigit():
            current_header = (tokens[0], int(tokens[1]))
        elif current_header and current_header[0] not in seen_commits:
            commit_hash, timestamp = current_header
            if since_ts <= timestamp < until_ts:  # exclusive end
                commits.append((commit_hash, part))
                seen_commits.add(commit_hash)

    commits.reverse()
    return commits


def get_file_at_commit(commit: str, file_path: str) -> dict:
    """Get the JSON content of a file at a specific commit."""
    result = subprocess.run(
        ["git", "show", f"{commit}:{file_path}"],
        capture_output=True,
        text=True,
        check=True
    )
    return json.loads(result.stdout)


def extract_versions_from_dict(old_dict: dict, new_dict: dict) -> Set[str]:
    """Extract version strings that were added or updated between two dicts."""
    versions = set()
    for key, new_value in new_dict.items():
        old_value = old_dict.get(key)
        if old_value != new_value:
            if isinstance(new_value, str):
                versions.add(new_value)
            elif isinstance(new_value, dict):
                old_nested = old_dict.get(key, {})
                if isinstance(old_nested, dict):
                    versions.update(extract_versions_from_dict(old_nested, new_value))
    return versions


def collect_tested_versions(commit_path_pairs: list[tuple[str, str]]) -> Dict[str, Set[str]]:
    """Collect tested versions by comparing consecutive commits."""
    tested_versions = {
        VERSION_GPU_MAIN_LATEST: set(),
        VERSION_GPU_OPERATOR: set(),
        VERSION_OCP: set(),
    }

    if not commit_path_pairs:
        return tested_versions

    # Get initial state from parent of first commit
    first_commit, first_path = commit_path_pairs[0]
    try:
        prev_content = get_file_at_commit(f"{first_commit}^", first_path)
    except subprocess.CalledProcessError:
        prev_content = {}

    for commit, path in commit_path_pairs:
        current_content = get_file_at_commit(commit, path)

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


def generate_markdown_summary(tested_versions: Dict[str, Set[str]], start_date: datetime, end_date: datetime) -> str:
    """Generate markdown summary of tested versions."""
    start_str = start_date.strftime("%Y-%m-%d")
    # end_date is exclusive (midnight), so use previous day for display
    end_str = (end_date - timedelta(days=1)).strftime("%Y-%m-%d")

    lines = [f"# Weekly test summary: {start_str} to {end_str}", ""]

    sections = [
        ("GPU Operator Development Versions", VERSION_GPU_MAIN_LATEST),
        ("GPU Operator Releases", VERSION_GPU_OPERATOR),
        ("OpenShift Releases", VERSION_OCP),
    ]
    for title, key in sections:
        lines.append(f"## {title}")
        versions = tested_versions[key]
        if versions:
            lines.extend(f"- {v}" for v in sorted(versions))
        else:
            lines.append("- (none)")
        lines.append("")

    # Build summary line with link, omitting zero entries
    pr_url = (
        f"https://github.com/rh-ecosystem-edge/nvidia-ci/pulls?q=is%3Apr+is%3Amerged+"
        f"merged%3A{start_str}..{end_str}+%22%5BAutomatic%5D+Update%22+in%3Atitle"
    )
    parts = []
    dev_count = len(tested_versions[VERSION_GPU_MAIN_LATEST])
    rel_count = len(tested_versions[VERSION_GPU_OPERATOR])
    ocp_count = len(tested_versions[VERSION_OCP])

    if dev_count:
        parts.append(f"{dev_count} new development versions of the NVIDIA GPU Operator")
    if rel_count:
        parts.append(f"{rel_count} new release versions of the NVIDIA GPU Operator")
    if ocp_count:
        parts.append(f"{ocp_count} new OpenShift versions with the release versions of the NVIDIA GPU Operator")

    if parts:
        lines.append("## Summary")
        if len(parts) == 1:
            summary_text = parts[0]
        elif len(parts) == 2:
            summary_text = f"{parts[0]} and {parts[1]}"
        else:
            summary_text = f"{parts[0]}, {parts[1]}, and {parts[2]}"
        lines.append(f"[Tested]({pr_url}) {summary_text}.")

    return "\n".join(lines)


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Generate weekly summary of tested versions")
    parser.add_argument("--since", help="Start date (YYYY-MM-DD), default: 7 days ago")
    parser.add_argument("--until", help="End date (YYYY-MM-DD), default: today")
    args = parser.parse_args()

    file_path = os.environ["VERSION_FILE_PATH"]

    # End date is midnight (start of day) - exclusive boundary
    # Start date is 7 days before - inclusive boundary
    if args.until:
        end_date = datetime.strptime(args.until, "%Y-%m-%d").replace(tzinfo=timezone.utc)
    else:
        end_date = datetime.now(timezone.utc).replace(hour=0, minute=0, second=0, microsecond=0)

    if args.since:
        start_date = datetime.strptime(args.since, "%Y-%m-%d").replace(tzinfo=timezone.utc)
    else:
        start_date = end_date - timedelta(days=7)

    logger.info(f"Collecting tested versions from {start_date.date()} to {end_date.date()}")

    commit_path_pairs = get_commits_in_range(file_path, start_date, end_date)
    logger.info(f"Found {len(commit_path_pairs)} commits")

    tested_versions = collect_tested_versions(commit_path_pairs)
    summary = generate_markdown_summary(tested_versions, start_date, end_date)

    print(summary)

    output_file = Path("weekly-summary.md")
    output_file.write_text(summary)
    logger.info(f"Summary written to {output_file}")


if __name__ == "__main__":
    main()
