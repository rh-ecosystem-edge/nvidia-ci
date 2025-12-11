"""MCP tool schema definitions."""

from typing import Any, Dict, List

from mcp.types import Tool

from config import get_unique_repos


def _get_repository_info(repo_cache: Dict[str, Any]) -> tuple[str, str, List[str], bool]:
    """Get repository configuration info for tool schemas."""
    unique_repos = get_unique_repos(repo_cache)
    repo_names = [r.full_name for r in unique_repos]
    repos_str = ", ".join(repo_names)
    repo_required = len(repo_names) > 1

    if not repo_names:
        repo_desc = "No repositories configured"
    elif len(repo_names) == 1:
        repo_desc = f"Optional. Defaults to {repo_names[0]} if not specified."
    else:
        repo_desc = f"Repository to analyze. Available: {repos_str}"

    return repo_desc, repos_str, ["pr_number"] if not repo_required else ["repository", "pr_number"], repo_required


def _build_base_properties(repo_desc: str) -> Dict[str, Any]:
    """Build base properties common to all tools."""
    return {
        "repository": {
            "type": "string",
            "description": repo_desc,
        },
        "pr_number": {
            "type": "string",
            "description": "PR number",
        },
    }


def _build_tool_schema(name: str, description: str, properties: Dict[str, Any], required: List[str]) -> Tool:
    """Build a tool schema with the given parameters."""
    return Tool(
        name=name,
        description=description,
        inputSchema={
            "type": "object",
            "properties": properties,
            "required": required,
        },
    )


def build_tool_list(repo_cache: Dict[str, Any]) -> list[Tool]:
    """Build the complete list of MCP tools."""
    repo_desc, repos_str, base_required, _ = _get_repository_info(repo_cache)
    base_props = _build_base_properties(repo_desc)

    # ========== HIGH-LEVEL TOOLS: Convenient, focused workflows ==========

    # Overview & Status tools
    tools = [
        _build_tool_schema(
            "get_pr_jobs_overview",
            f"Get comprehensive overview of all jobs in a PR including their status, counts, and details. Use this first to understand the state of a PR's CI jobs. Configured repositories: {repos_str}",
            base_props,
            base_required
        ),
        _build_tool_schema(
            "list_failed_jobs",
            f"List all Prow jobs where the latest build failed. Configured repositories: {repos_str}",
            base_props,
            base_required
        ),
    ]

    # Build-level tools
    job_build_props = {**base_props,
        "job_name": {"type": "string", "description": "Job name"},
        "build_id": {"type": "string", "description": "Build ID"},
    }

    tools.extend([
        _build_tool_schema(
            "get_build_log",
            "Fetch build log for a specific job build.",
            job_build_props,
            [*base_required, "job_name", "build_id"]
        ),
        _build_tool_schema(
            "list_build_steps",
            "List available steps/artifacts in a build. Useful for identifying which steps failed.",
            job_build_props,
            [*base_required, "job_name", "build_id"]
        ),
    ])

    # Step-level tools
    step_props = {**job_build_props,
        "step_name": {"type": "string", "description": "Step/artifact name"},
    }
    tools.extend([
        _build_tool_schema(
            "get_step_build_log",
            "Fetch build log for a specific step/artifact within a job build. Use list_build_steps first to see available steps.",
            step_props,
            [*base_required, "job_name", "build_id", "step_name"]
        ),
        _build_tool_schema(
            "get_step_metadata",
            "Get parsed metadata (finished.json, started.json) for a specific step. Returns timing, status, and duration information.",
            step_props,
            [*base_required, "job_name", "build_id", "step_name"]
        ),
    ])

    # JUnit test result tools
    tools.extend([
        _build_tool_schema(
            "find_junit_files",
            "Find all JUnit XML test result files in a build. Returns paths to all junit*.xml files found in artifacts.",
            job_build_props,
            [*base_required, "job_name", "build_id"]
        ),
        _build_tool_schema(
            "get_junit_results",
            "Parse a JUnit XML file and extract test results. Returns test counts, failures, and detailed error messages for failed tests.",
            {**job_build_props, "junit_path": {"type": "string", "description": "Path to JUnit XML file (from find_junit_files)"}},
            [*base_required, "job_name", "build_id", "junit_path"]
        ),
    ])

    # Must-gather tools (OpenShift debugging)
    tools.extend([
        _build_tool_schema(
            "find_must_gather_directories",
            "Find all must-gather data in a build (both extracted directories and archives). Returns two types: (1) type='extracted' - can be analyzed directly via list_must_gather_files/get_must_gather_file, (2) type='archive' - requires local download for analysis with specialized tools. If root cause cannot be determined from available data, suggest user download archives from the provided download_url for deeper analysis with tools like 'omc' (OpenShift Must Gather CLI) or local LLM file analysis.",
            job_build_props,
            [*base_required, "job_name", "build_id"]
        ),
        _build_tool_schema(
            "list_must_gather_files",
            "List files in a must-gather directory. Returns all non-archive files by default (logs, YAML, HTML, JSON, etc.). Supports optional pattern filtering.",
            {**job_build_props,
             "must_gather_path": {"type": "string", "description": "Path to must-gather directory (from find_must_gather_directories)"},
             "pattern": {"type": "string", "description": "Optional file pattern with wildcards (e.g., '*.log', '*.yaml', '*events*'). Default: '*' (all files)"},
             "include_archives": {"type": "boolean", "description": "If true, include archive files (.tar, .gz, etc.) in results. Default: false"}},
            [*base_required, "job_name", "build_id", "must_gather_path"]
        ),
        _build_tool_schema(
            "get_must_gather_file",
            "Fetch any file from a must-gather directory (logs, YAML, HTML, JSON, event files, etc.). Use list_must_gather_files or search_must_gather_files to find available files first.",
            {**job_build_props,
             "must_gather_path": {"type": "string", "description": "Path to must-gather directory"},
             "file_path": {"type": "string", "description": "Path to file within must-gather"}},
            [*base_required, "job_name", "build_id", "must_gather_path", "file_path"]
        ),
        _build_tool_schema(
            "search_must_gather_files",
            "Search for files matching a pattern in a must-gather directory. Supports wildcards (e.g., '*.yaml', '*events*', 'cluster_policy*'). Archives are excluded by default. Use get_must_gather_file to read found files.",
            {**job_build_props,
             "must_gather_path": {"type": "string", "description": "Path to must-gather directory"},
             "pattern": {"type": "string", "description": "File pattern with wildcards (e.g., '*.yaml', '*events*', '*.html')"},
             "include_archives": {"type": "boolean", "description": "If true, include archive files in results. Default: false"}},
            [*base_required, "job_name", "build_id", "must_gather_path", "pattern"]
        ),
    ])

    # ========== LOW-LEVEL TOOLS: Maximum flexibility for exploration ==========

    tools.extend([
        _build_tool_schema(
            "list_directory",
            "List files and directories at any GCS path. Use this for exploration when high-level tools don't cover your needs. Returns directories and files with sizes.",
            {"path": {"type": "string", "description": "GCS path to list (e.g., 'pr-logs/pull/org_repo/123/job/build/artifacts/')"}},
            ["path"]
        ),
        _build_tool_schema(
            "fetch_file",
            "Fetch any file content from GCS by path. Use this when you need a specific file not covered by other tools. Returns file content with size metadata.",
            {"path": {"type": "string", "description": "Full GCS path to file"}},
            ["path"]
        ),
        _build_tool_schema(
            "get_pr_base_path",
            f"Get the base GCS path for a PR. Useful for constructing custom paths for list_directory or fetch_file. Configured repositories: {repos_str}",
            base_props,
            base_required
        ),
    ])

    return tools

