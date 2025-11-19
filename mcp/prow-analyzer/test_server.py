#!/usr/bin/env python3
"""
Simple test script to verify the MCP server functions work correctly.

This script tests the core functions without requiring a full MCP client.
"""

import sys
import traceback
from typing import Optional, Tuple, List
from config import load_config, build_repository_cache, resolve_repository
from prow.jobs import get_failed_jobs_for_pr
from prow.logs import get_build_log
from must_gather.tools import _is_archive, ARCHIVE_EXTENSIONS

# Constants
SEPARATOR = "=" * 80
HEADER_SEPARATOR = "#" * 80


def print_test_header(test_name: str, repo_full_name: str, pr_number: str) -> None:
    """Print a formatted test header."""
    print(f"\n{SEPARATOR}")
    print(f"Testing: {test_name} for {repo_full_name} PR #{pr_number}")
    print(f"{SEPARATOR}\n")


def print_failed_job_details(job_name: str, build) -> None:
    """Print details of a failed job."""
    print(f"  Repository: {build.repository}")
    print(f"  Job: {job_name}")
    print(f"  Build ID: {build.build_id}")
    print(f"  Status: {build.status}")
    print(f"  URL: {build.prow_url}")
    print()


def print_log_preview(log_content: str, num_lines: int = 20) -> None:
    """Print a preview of the log content."""
    lines = log_content.split('\n')
    total_lines = len(lines)

    print(f"\n✓ Log preview (last {num_lines} lines of {total_lines} total):")
    print("  " + "-" * 76)

    preview_lines = lines[-num_lines:] if len(lines) > num_lines else lines
    for line in preview_lines:
        # Truncate very long lines
        display_line = line[:100] + "..." if len(line) > 100 else line
        print(f"  {display_line}")

    print("  " + "-" * 76)


def handle_test_error(error: Exception) -> None:
    """Print error information for a failed test."""
    print(f"✗ Error: {error}")
    traceback.print_exc()


def test_list_failed_jobs(config, repo_cache, repo_identifier: Optional[str], pr_number: str) -> bool:
    """Test listing failed jobs for a PR."""
    try:
        repo_info = resolve_repository(repo_identifier, repo_cache)
        print_test_header("List failed jobs", repo_info.full_name, pr_number)

        failed_jobs = get_failed_jobs_for_pr(config, repo_info, pr_number)

        if not failed_jobs:
            print(f"✓ No failed jobs found for {repo_info.full_name} PR #{pr_number}")
            return True

        print(f"✓ Found {len(failed_jobs)} failed job(s):\n")
        for job_name, build in failed_jobs.items():
            print_failed_job_details(job_name, build)

        return True

    except Exception as e:
        handle_test_error(e)
        return False


def fetch_and_display_log(config, repo_info, pr_number: str, job_name: str,
                          build_id: str) -> Optional[str]:
    """Fetch build log and display its size."""
    log_content = get_build_log(config, repo_info, pr_number, job_name, build_id)

    if not log_content:
        print(f"✗ Failed to fetch build log")
        return None

    num_lines = len(log_content.split('\n'))
    print(f"✓ Fetched build log ({len(log_content)} bytes, {num_lines} lines)")
    return log_content


def test_fetch_failed_job_log(config, repo_cache, repo_identifier: Optional[str], pr_number: str) -> bool:
    """Test fetching build logs for failed jobs."""
    try:
        repo_info = resolve_repository(repo_identifier, repo_cache)
        print_test_header("Fetch failed job logs", repo_info.full_name, pr_number)

        failed_jobs = get_failed_jobs_for_pr(config, repo_info, pr_number)

        if not failed_jobs:
            print(f"✓ No failed jobs found for {repo_info.full_name} PR #{pr_number}")
            return True

        # Fetch log for the first failed job
        job_name, build = list(failed_jobs.items())[0]

        print(f"Repository: {build.repository}")
        print(f"Fetching log for job: {job_name}")
        print(f"Build ID: {build.build_id}\n")

        log_content = fetch_and_display_log(config, repo_info, pr_number, job_name, build.build_id)
        if not log_content:
            return False

        # Show a preview of the log (LLM client will do actual analysis)
        print_log_preview(log_content, num_lines=20)

        return True

    except Exception as e:
        handle_test_error(e)
        return False


def test_archive_filtering() -> bool:
    """Test that archive filtering works correctly."""
    try:
        print_test_header("Archive filtering", "N/A", "N/A")

        # Test cases: (filename, should_be_archive)
        test_cases = [
            ("event-filter.html", False),
            ("pod.log", False),
            ("config.yaml", False),
            ("events.json", False),
            ("must-gather.tar", True),
            ("must-gather.tar.gz", True),
            ("data.tgz", True),
            ("backup.zip", True),
            ("archive.bz2", True),
            ("file.tar.bz2", True),
            ("data.xz", True),
            ("file.tar.xz", True),
            ("FILE.TAR.GZ", True),  # Test case insensitivity
        ]

        print(f"Testing {len(test_cases)} filenames against {len(ARCHIVE_EXTENSIONS)} archive extensions...")
        print(f"Archive extensions: {', '.join(ARCHIVE_EXTENSIONS)}\n")

        all_passed = True
        for filename, expected_is_archive in test_cases:
            result = _is_archive(filename)
            status = "✓" if result == expected_is_archive else "✗"
            expected_str = "archive" if expected_is_archive else "non-archive"
            result_str = "archive" if result else "non-archive"

            print(f"  {status} {filename:25s} expected: {expected_str:12s} got: {result_str:12s}")

            if result != expected_is_archive:
                all_passed = False

        print()
        if all_passed:
            print("✓ All archive filtering tests passed!")
        else:
            print("✗ Some archive filtering tests failed")

        return all_passed

    except Exception as e:
        handle_test_error(e)
        return False


def parse_arguments() -> Tuple[str, Optional[str]]:
    """Parse command line arguments and return PR number and repository identifier."""
    if len(sys.argv) < 2:
        print("Usage: python test_server.py <pr_number> [repository]")
        print("\nExamples:")
        print("  python test_server.py 123                    # Uses default repo if only one configured")
        print("  python test_server.py 123 nvidia-ci          # Just repo name (if unambiguous)")
        print("  python test_server.py 123 rh-ecosystem-edge/nvidia-ci  # Full name")
        sys.exit(1)

    pr_number = sys.argv[1]
    repo_identifier = sys.argv[2] if len(sys.argv) >= 3 else None

    if repo_identifier is None:
        print("No repository specified - will auto-detect from config...")

    return pr_number, repo_identifier


def initialize_config():
    """Load configuration and build repository cache."""
    config = load_config()
    repo_cache = build_repository_cache(config)
    return config, repo_cache


def print_main_header(repo_full_name: str, pr_number: str) -> None:
    """Print the main test suite header."""
    print(f"\n{HEADER_SEPARATOR}")
    print(f"# Testing MCP Server Functions")
    print(f"# Repository: {repo_full_name}")
    print(f"# PR: #{pr_number}")
    print(f"{HEADER_SEPARATOR}")


def run_tests(config, repo_cache, repo_identifier: Optional[str], pr_number: str) -> List[Tuple[str, bool]]:
    """Run all tests and return results."""
    return [
        ("Archive Filtering", test_archive_filtering()),
        ("List Failed Jobs", test_list_failed_jobs(config, repo_cache, repo_identifier, pr_number)),
        ("Fetch Failed Job Logs", test_fetch_failed_job_log(config, repo_cache, repo_identifier, pr_number)),
    ]


def print_test_summary(results: List[Tuple[str, bool]]) -> bool:
    """Print test summary and return whether all tests passed."""
    print(f"\n{SEPARATOR}")
    print("Test Summary")
    print(f"{SEPARATOR}\n")

    for test_name, passed in results:
        status = "✓ PASSED" if passed else "✗ FAILED"
        print(f"{status}: {test_name}")

    all_passed = all(passed for _, passed in results)

    print()
    if all_passed:
        print("✓ All tests passed!")
    else:
        print("✗ Some tests failed")

    return all_passed


def main() -> None:
    """Run the test suite."""
    pr_number, repo_identifier = parse_arguments()
    config, repo_cache = initialize_config()

    try:
        repo_info = resolve_repository(repo_identifier, repo_cache)
        print_main_header(repo_info.full_name, pr_number)

        results = run_tests(config, repo_cache, repo_identifier, pr_number)
        all_passed = print_test_summary(results)

        sys.exit(0 if all_passed else 1)

    except Exception as e:
        print(f"\n✗ Error: {e}")
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
