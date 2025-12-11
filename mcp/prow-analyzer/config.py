"""Configuration management for Prow Analyzer MCP server."""

import yaml
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional


# Default configuration
DEFAULT_CONFIG = {
    "gcs_bucket": "test-platform-results",
    "gcsweb_base_url": "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs",
    "path_template": "pr-logs/pull/{org}_{repo}/{pr_number}",
    "repositories": [
        {
            "org": "rh-ecosystem-edge",
            "repo": "nvidia-ci",
        }
    ],
}


@dataclass
class RepositoryInfo:
    """Information about a configured repository."""
    org: str
    repo: str

    @property
    def full_name(self) -> str:
        """Get GitHub-style org/repo name."""
        return f"{self.org}/{self.repo}"

    @property
    def gcs_name(self) -> str:
        """Get GCS path format (org_repo with underscore)."""
        return f"{self.org}_{self.repo}"

    def __str__(self) -> str:
        return self.full_name


def load_config(config_path: Optional[str] = None) -> Dict[str, Any]:
    """
    Load configuration with priority: ENV vars > config.yaml > defaults.

    Environment variables (for MCP client configuration):
    - PROW_GCS_BUCKET: GCS bucket name
    - PROW_GCSWEB_BASE_URL: Base URL for GCSWeb UI (without trailing slash)
    - PROW_PATH_TEMPLATE: Path template string
    - PROW_REPOSITORIES: Comma-separated list of org/repo (e.g., "rh-ecosystem-edge/nvidia-ci,openshift/release")
    """
    import os

    # Start with defaults
    config = DEFAULT_CONFIG.copy()

    # Load from config.yaml if it exists (unless disabled)
    if os.environ.get("PROW_NO_CONFIG_FILE") != "1":
        if config_path is None:
            script_dir = Path(__file__).parent
            config_path = script_dir / "config.yaml"

        if isinstance(config_path, str):
            config_path = Path(config_path)

        if config_path.exists():
            try:
                with open(config_path, 'r') as f:
                    file_config = yaml.safe_load(f)
                    if file_config:
                        config.update(file_config)
            except Exception as e:
                print(f"Warning: Failed to load config from {config_path}: {e}", flush=True)

    # Override with environment variables if present
    if "PROW_GCS_BUCKET" in os.environ:
        config["gcs_bucket"] = os.environ["PROW_GCS_BUCKET"]

    if "PROW_GCSWEB_BASE_URL" in os.environ:
        config["gcsweb_base_url"] = os.environ["PROW_GCSWEB_BASE_URL"]

    if "PROW_PATH_TEMPLATE" in os.environ:
        config["path_template"] = os.environ["PROW_PATH_TEMPLATE"]

    if "PROW_REPOSITORIES" in os.environ:
        # Parse "org1/repo1,org2/repo2" format
        repos_str = os.environ["PROW_REPOSITORIES"]
        repos = []
        for repo_spec in repos_str.split(","):
            repo_spec = repo_spec.strip()
            if "/" in repo_spec:
                org, repo = repo_spec.split("/", 1)
                repos.append({"org": org.strip(), "repo": repo.strip()})
        if repos:
            config["repositories"] = repos

    # Normalize gcsweb_base_url to avoid trailing slash issues
    if "gcsweb_base_url" in config and isinstance(config["gcsweb_base_url"], str):
        config["gcsweb_base_url"] = config["gcsweb_base_url"].rstrip("/")

    return config


def build_repository_cache(config: Dict[str, Any]) -> Dict[str, Any]:
    """Build a cache mapping repository identifiers to RepositoryInfo objects."""
    cache = {}

    if not config or "repositories" not in config:
        return cache

    for repo_config in config["repositories"]:
        org = repo_config.get("org")
        repo = repo_config.get("repo")

        if not org or not repo:
            continue

        repo_info = RepositoryInfo(org=org, repo=repo)

        # Store multiple mappings for easy lookup
        if repo in cache:
            cache[repo] = "AMBIGUOUS"
        else:
            cache[repo] = repo_info

        cache[repo_info.full_name] = repo_info  # "org/repo"
        cache[repo_info.gcs_name] = repo_info   # "org_repo"

    return cache


def get_unique_repos(repo_cache: Dict[str, Any]) -> List[RepositoryInfo]:
    """Get list of unique repositories from cache."""
    repos = [r for r in repo_cache.values() if isinstance(r, RepositoryInfo)]
    return list({r.gcs_name: r for r in repos}.values())


def resolve_repository(repo_identifier: Optional[str], repo_cache: Dict[str, Any]) -> RepositoryInfo:
    """Resolve a repository identifier to a RepositoryInfo object."""
    # If no identifier provided, check if there's only one repository
    if not repo_identifier:
        unique_repos = get_unique_repos(repo_cache)

        if not unique_repos:
            raise ValueError("No repositories configured")

        if len(unique_repos) == 1:
            return unique_repos[0]

        available = [r.full_name for r in unique_repos]
        raise ValueError(
            f"Multiple repositories configured. Please specify which repository to use. "
            f"Available: {', '.join(available)}"
        )

    # Try to resolve the identifier
    if repo_identifier in repo_cache:
        result = repo_cache[repo_identifier]

        if result == "AMBIGUOUS":
            matches = [r for r in repo_cache.values()
                      if isinstance(r, RepositoryInfo) and r.repo == repo_identifier]
            match_names = [r.full_name for r in matches]
            raise ValueError(
                f"Repository name '{repo_identifier}' is ambiguous. "
                f"Please specify the full name (org/repo). Matches: {', '.join(match_names)}"
            )

        return result

    # Not found
    available = {r.full_name for r in repo_cache.values() if isinstance(r, RepositoryInfo)}
    raise ValueError(
        f"Repository '{repo_identifier}' not found. "
        f"Available: {', '.join(sorted(available))}"
    )

