"""MCP tool call handlers."""

import json
from typing import Any, Dict

from mcp.types import TextContent

from config import resolve_repository
from gcs import client as gcs_client
from gcs.paths import build_pr_path
from must_gather import tools as must_gather
from parsers import junit, metadata
from prow import builds, jobs, logs


def _handle_error(error: Exception) -> list[TextContent]:
    """Create error response for tool calls."""
    return [TextContent(
        type="text",
        text=json.dumps({"error": str(error)}, indent=2),
    )]


def _handle_success(data: Any) -> list[TextContent]:
    """Create success response for tool calls."""
    return [TextContent(
        type="text",
        text=json.dumps(data, indent=2),
    )]


def _create_base_result(repo_info, pr_number: str, **kwargs) -> Dict[str, Any]:
    """Create base result dictionary with repository and PR info."""
    return {
        "repository": repo_info.full_name,
        "pr_number": pr_number,
        **kwargs
    }


def _add_log_metadata(result: Dict[str, Any], log_content: str) -> None:
    """Add log size metadata to result dictionary (modifies in place)."""
    result["log_size_bytes"] = len(log_content)
    result["log_size_lines"] = len(log_content.split('\n'))


def _with_repo_resolution(config, repo_cache, handler_func):
    """Decorator to handle repository resolution and error handling."""
    def wrapper(arguments: dict) -> list[TextContent]:
        try:
            repo_info = resolve_repository(arguments.get("repository"), repo_cache)
            return handler_func(config, repo_info, arguments)
        except Exception as e:
            # Log server-side for debugging, still return a clean error to the client
            import traceback
            import sys
            print("Tool handler error:", file=sys.stderr, flush=True)
            traceback.print_exc(file=sys.stderr)
            return _handle_error(e)
    return wrapper


def create_handlers(config: Dict[str, Any], repo_cache: Dict[str, Any]) -> Dict[str, Any]:
    """Create all tool handlers with config and repo_cache bound."""

    def _handle_get_pr_jobs_overview(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_pr_jobs_overview tool call."""
        overview = jobs.get_pr_jobs_overview(cfg, repo_info, arguments["pr_number"])
        return _handle_success(overview)

    def _handle_list_failed_jobs(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle list_failed_jobs tool call."""
        pr_number = arguments["pr_number"]
        failed_jobs_dict = jobs.get_failed_jobs_for_pr(cfg, repo_info, pr_number)

        if not failed_jobs_dict:
            result = _create_base_result(
                repo_info, pr_number,
                message=f"No failed jobs found for {repo_info.full_name} PR #{pr_number}",
                failed_jobs_count=0,
            )
        else:
            result = _create_base_result(
                repo_info, pr_number,
                failed_jobs_count=len(failed_jobs_dict),
                failed_jobs=[build.to_dict() for build in failed_jobs_dict.values()],
            )

        return _handle_success(result)

    def _handle_get_build_log(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_build_log tool call."""
        pr_number = arguments["pr_number"]
        job_name = arguments["job_name"]
        build_id = arguments["build_id"]

        log_content = logs.get_build_log(cfg, repo_info, pr_number, job_name, build_id)
        if not log_content:
            return _handle_error(ValueError("Build log not found"))

        result = _create_base_result(
            repo_info, pr_number,
            job_name=job_name,
            build_id=build_id,
            log_content=log_content,
        )
        _add_log_metadata(result, log_content)

        return _handle_success(result)

    def _handle_list_build_steps(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle list_build_steps tool call."""
        pr_number = arguments["pr_number"]
        job_name = arguments["job_name"]
        build_id = arguments["build_id"]

        steps = builds.list_build_steps(cfg, repo_info, pr_number, job_name, build_id)
        steps_with_logs = [s for s in steps if s.get("has_build_log")]

        result = _create_base_result(
            repo_info, pr_number,
            job_name=job_name,
            build_id=build_id,
            total_steps=len(steps),
            steps_with_build_logs=len(steps_with_logs),
            steps=steps,
            summary=f"Found {len(steps)} steps, {len(steps_with_logs)} have build logs available"
        )

        return _handle_success(result)

    def _handle_get_step_build_log(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_step_build_log tool call."""
        pr_number = arguments["pr_number"]
        job_name = arguments["job_name"]
        build_id = arguments["build_id"]
        step_name = arguments["step_name"]

        log_content = logs.get_step_build_log(cfg, repo_info, pr_number, job_name, build_id, step_name)
        if not log_content:
            return _handle_error(ValueError(f"Build log not found for step '{step_name}'"))

        result = _create_base_result(
            repo_info, pr_number,
            job_name=job_name,
            build_id=build_id,
            step_name=step_name,
            log_content=log_content,
        )
        _add_log_metadata(result, log_content)

        return _handle_success(result)

    def _handle_get_step_metadata(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_step_metadata tool call."""
        meta = metadata.get_step_metadata(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"],
            arguments["step_name"]
        )
        return _handle_success(meta)

    def _handle_find_junit_files(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle find_junit_files tool call."""
        junit_files = junit.find_junit_files_in_build(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"]
        )

        result = _create_base_result(
            repo_info, arguments["pr_number"],
            job_name=arguments["job_name"],
            build_id=arguments["build_id"],
            junit_files=junit_files,
            total_files=len(junit_files),
        )
        return _handle_success(result)

    def _handle_get_junit_results(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_junit_results tool call."""
        results = junit.get_junit_results(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"],
            arguments["junit_path"]
        )
        return _handle_success(results)

    def _handle_find_must_gather_directories(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle find_must_gather_directories tool call."""
        must_gather_items = must_gather.find_must_gather_dirs(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"]
        )

        # Separate extracted directories from archives
        extracted = [item for item in must_gather_items if item.get("type") == "extracted"]
        archives = [item for item in must_gather_items if item.get("type") == "archive"]

        result = _create_base_result(
            repo_info, arguments["pr_number"],
            job_name=arguments["job_name"],
            build_id=arguments["build_id"],
            extracted_directories=extracted,
            archives=archives,
            total_extracted=len(extracted),
            total_archives=len(archives),
            summary=f"Found {len(extracted)} extracted director{'y' if len(extracted) == 1 else 'ies'} and {len(archives)} archive{'s' if len(archives) != 1 else ''}",
            note="Extracted directories can be analyzed via list_must_gather_files/get_must_gather_file. "
                 "Archives require local download for analysis with tools like 'omc' (https://github.com/gmeghnag/omc) or local LLM analysis.",
        )
        return _handle_success(result)

    def _handle_list_must_gather_files(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle list_must_gather_files tool call."""
        include_archives = arguments.get("include_archives", False)
        pattern = arguments.get("pattern", "*")
        files = must_gather.list_must_gather_files(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"],
            arguments["must_gather_path"],
            include_archives=include_archives,
            pattern=pattern
        )

        result = _create_base_result(
            repo_info, arguments["pr_number"],
            job_name=arguments["job_name"],
            build_id=arguments["build_id"],
            must_gather_path=arguments["must_gather_path"],
            pattern=pattern,
            files=files,
            total_files=len(files),
            archives_included=include_archives,
        )
        return _handle_success(result)

    def _handle_get_must_gather_file(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_must_gather_file tool call."""
        result = must_gather.get_must_gather_file(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"],
            arguments["must_gather_path"],
            arguments["file_path"]
        )

        if "error" in result:
            return _handle_error(ValueError(result["error"]))

        return _handle_success(result)

    def _handle_search_must_gather_files(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle search_must_gather_files tool call."""
        include_archives = arguments.get("include_archives", False)
        matching_files = must_gather.search_must_gather_files(
            cfg, repo_info,
            arguments["pr_number"],
            arguments["job_name"],
            arguments["build_id"],
            arguments["must_gather_path"],
            arguments["pattern"],
            include_archives=include_archives
        )

        result = _create_base_result(
            repo_info, arguments["pr_number"],
            job_name=arguments["job_name"],
            build_id=arguments["build_id"],
            must_gather_path=arguments["must_gather_path"],
            pattern=arguments["pattern"],
            matching_files=matching_files,
            total_matches=len(matching_files),
            archives_included=include_archives,
        )
        return _handle_success(result)

    def _handle_list_directory(arguments: dict) -> list[TextContent]:
        """Handle list_directory tool call."""
        try:
            result = gcs_client.list_files_and_directories(config["gcs_bucket"], arguments["path"])
            return _handle_success(result)
        except Exception as e:
            return _handle_error(e)

    def _handle_fetch_file(arguments: dict) -> list[TextContent]:
        """Handle fetch_file tool call."""
        try:
            result = gcs_client.fetch_file_with_metadata(config["gcs_bucket"], arguments["path"])
            if "error" in result:
                return _handle_error(ValueError(result["error"]))
            return _handle_success(result)
        except Exception as e:
            return _handle_error(e)

    def _handle_get_pr_base_path(cfg, repo_info, arguments: dict) -> list[TextContent]:
        """Handle get_pr_base_path tool call."""
        pr_path = build_pr_path(repo_info, arguments["pr_number"], cfg["path_template"])
        bucket = cfg["gcs_bucket"]
        gcsweb_url = f"{cfg['gcsweb_base_url']}/{bucket}/{pr_path}"

        result = {
            "repository": repo_info.full_name,
            "pr_number": arguments["pr_number"],
            "gcs_path": pr_path,
            "gcs_bucket": bucket,
            "full_gcs_path": f"gs://{bucket}/{pr_path}",
            "gcsweb_url": gcsweb_url,
        }
        return _handle_success(result)

    # Create wrapped handlers with config and repo_cache
    wrapped = _with_repo_resolution

    return {
        # High-level tools
        "get_pr_jobs_overview": wrapped(config, repo_cache, _handle_get_pr_jobs_overview),
        "list_failed_jobs": wrapped(config, repo_cache, _handle_list_failed_jobs),
        "get_build_log": wrapped(config, repo_cache, _handle_get_build_log),
        "list_build_steps": wrapped(config, repo_cache, _handle_list_build_steps),
        "get_step_build_log": wrapped(config, repo_cache, _handle_get_step_build_log),
        "get_step_metadata": wrapped(config, repo_cache, _handle_get_step_metadata),

        # JUnit tools
        "find_junit_files": wrapped(config, repo_cache, _handle_find_junit_files),
        "get_junit_results": wrapped(config, repo_cache, _handle_get_junit_results),

        # Must-gather tools
        "find_must_gather_directories": wrapped(config, repo_cache, _handle_find_must_gather_directories),
        "list_must_gather_files": wrapped(config, repo_cache, _handle_list_must_gather_files),
        "get_must_gather_file": wrapped(config, repo_cache, _handle_get_must_gather_file),
        "search_must_gather_files": wrapped(config, repo_cache, _handle_search_must_gather_files),

        # Low-level tools
        "list_directory": _handle_list_directory,
        "fetch_file": _handle_fetch_file,
        "get_pr_base_path": wrapped(config, repo_cache, _handle_get_pr_base_path),
    }

