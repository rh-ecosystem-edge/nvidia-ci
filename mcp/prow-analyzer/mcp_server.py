#!/usr/bin/env python3
"""
MCP Server for analyzing failed Prow CI jobs in any GitHub repository using OpenShift CI.

This is the main entry point that coordinates between MCP protocol and the analysis tools.
"""

import json
import sys

from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import TextContent

from config import load_config, build_repository_cache, get_unique_repos
from tools.handlers import create_handlers
from tools.schemas import build_tool_list


# Create MCP server
app = Server("prow-analyzer")

# Global state (loaded at startup)
CONFIG = None
REPO_CACHE = None
TOOL_HANDLERS = None


@app.list_tools()
async def list_tools() -> list:
    """List available MCP tools."""
    if REPO_CACHE is None:
        raise RuntimeError("Server not initialized: REPO_CACHE is None. Call main() to initialize.")
    return build_tool_list(REPO_CACHE)


@app.call_tool()
async def call_tool(name: str, arguments: dict) -> list[TextContent]:
    """Handle tool calls by dispatching to appropriate handler."""
    if TOOL_HANDLERS is None:
        raise RuntimeError("Server not initialized: TOOL_HANDLERS is None. Call main() to initialize.")

    handler = TOOL_HANDLERS.get(name)

    if handler:
        return handler(arguments)

    # Unknown tool error
    return [TextContent(
        type="text",
        text=json.dumps({"error": f"Unknown tool: {name}"}, indent=2),
    )]


async def main():
    """Run the MCP server."""
    global CONFIG, REPO_CACHE, TOOL_HANDLERS

    # Load configuration at startup
    CONFIG = load_config()

    # Build repository cache
    REPO_CACHE = build_repository_cache(CONFIG)

    # Create tool handlers with bound config and cache
    TOOL_HANDLERS = create_handlers(CONFIG, REPO_CACHE)

    # Log configuration
    unique_repos = get_unique_repos(REPO_CACHE)
    repo_names = [r.full_name for r in unique_repos]

    print(f"Loaded configuration: {len(unique_repos)} repositories configured", file=sys.stderr, flush=True)
    print(f"Repositories: {', '.join(repo_names)}", file=sys.stderr, flush=True)
    print(f"GCS Bucket: {CONFIG.get('gcs_bucket')}", file=sys.stderr, flush=True)

    async with stdio_server() as (read_stream, write_stream):
        await app.run(read_stream, write_stream, app.create_initialization_options())


if __name__ == "__main__":
    import asyncio
    asyncio.run(main())
