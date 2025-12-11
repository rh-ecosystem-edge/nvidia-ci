#!/usr/bin/env python3
"""
Check if GPU operator versions exist in OpenShift catalog using Red Hat Catalog API.
"""

import requests
from workflows.common.utils import logger

# Red Hat Catalog API base URL
CATALOG_API_BASE = "https://catalog.redhat.com/api/containers/v1"


def get_operator_channel(version: str) -> str:
    """
    Extract operator channel from version.
    Channel has 'v' prefix and is major.minor (e.g., "v25.10" from "25.10.1")
    """
    parts = version.lstrip('v').split('.')
    if len(parts) >= 2:
        return f"v{parts[0]}.{parts[1]}"
    return f"v{version.lstrip('v')}"


def build_catalog_filter(
    operator_package: str,
    gpu_versions: set[str],
    channels: set[str],
    ocp_versions: set[str]
) -> str:
    """Build optimized API filter query for catalog entries."""
    field_expressions = []

    # Package field (always present)
    field_expressions.append(f'package=="{operator_package}"')

    # GPU versions field (skip if empty)
    if gpu_versions:
        if len(gpu_versions) == 1:
            field_expressions.append(f'version=="{next(iter(gpu_versions))}"')
        else:
            version_ors = ' or '.join(f'version=="{v}"' for v in sorted(gpu_versions))
            field_expressions.append(f'({version_ors})')

    # Channel names field (skip if empty)
    if channels:
        if len(channels) == 1:
            field_expressions.append(f'channel_name=="{next(iter(channels))}"')
        else:
            channel_ors = ' or '.join(f'channel_name=="{c}"' for c in sorted(channels))
            field_expressions.append(f'({channel_ors})')

    # OCP versions field (skip if empty)
    if ocp_versions:
        if len(ocp_versions) == 1:
            field_expressions.append(f'ocp_version=="{next(iter(ocp_versions))}"')
        else:
            ocp_ors = ' or '.join(f'ocp_version=="{ocp}"' for ocp in sorted(ocp_versions))
            field_expressions.append(f'({ocp_ors})')

    return ' and '.join(field_expressions)


def should_stop_pagination(
    found_combinations: set,
    expected_combinations: int,
    fetched_count: int,
    total_count: int
) -> bool:
    """Determine if we should stop paginating through API results."""
    if len(found_combinations) == expected_combinations:
        logger.debug(f"Found all {expected_combinations} combinations, stopping pagination")
        return True

    return fetched_count >= total_count


def fetch_catalog_entries(
    filter_query: str,
    normalized_gpu_versions: set[str],
    ocp_versions_set: set[str]
) -> list[dict]:
    """Fetch operator catalog entries from Red Hat Catalog API with smart pagination."""
    url = f"{CATALOG_API_BASE}/operators/bundles"
    page = 0
    page_size = 100
    all_entries = []
    expected_combinations = len(normalized_gpu_versions) * len(ocp_versions_set)

    while True:
        params = {"filter": filter_query, "page_size": page_size, "page": page}

        response = requests.get(url, params=params, timeout=30)
        response.raise_for_status()

        data = response.json()
        entries = data.get('data', [])

        if not entries:
            break

        all_entries.extend(entries)

        # Check if we've found all needed combinations (early termination)
        found_combinations = {
            (e.get('version', '').lstrip('v'), e.get('ocp_version'))
            for e in all_entries
            if e.get('version', '').lstrip('v') in normalized_gpu_versions
            and e.get('ocp_version') in ocp_versions_set
        }

        fetched_count = len(all_entries)

        if should_stop_pagination(found_combinations, expected_combinations,
                                 fetched_count, data.get('total', 0)):
            break

        page += 1

    logger.debug(f"Fetched {len(all_entries)} catalog entries")
    return all_entries


def is_available_in_catalog_entries(
    catalog_entries: list[dict],
    gpu_version: str,
    ocp_version: str
) -> bool:
    """
    Check if specific GPU version is available for specific OCP version in catalog entries.

    Args:
        catalog_entries: List of catalog entry dicts from Red Hat Catalog API
        gpu_version: GPU operator version (e.g., "25.10.1")
        ocp_version: OpenShift version (e.g., "4.20")

    Returns:
        True if the combination exists, False otherwise
    """
    gpu_normalized = gpu_version.lstrip('v')
    for entry in catalog_entries:
        if (entry.get('version', '').lstrip('v') == gpu_normalized and
            entry.get('ocp_version') == ocp_version):
            return True
    return False


def fetch_gpu_operator_catalog_entries(
    gpu_versions: list[str],
    ocp_versions: list[str],
    operator_package: str = "gpu-operator-certified"
) -> list[dict]:
    """
    Fetch GPU operator catalog entries from Red Hat Catalog for given versions.

    Args:
        gpu_versions: List of GPU operator versions (e.g., ["25.10.1", "24.6.2"])
        ocp_versions: List of OpenShift versions (e.g., ["4.20", "4.19"])
        operator_package: Package name in the catalog

    Returns:
        List of catalog entry dicts. Use is_available_in_catalog_entries() to check
        specific combinations.

    Raises:
        requests.exceptions.RequestException: If API calls fail
    """
    # Normalize and prepare data
    normalized_gpu_versions = set(v.lstrip('v') for v in gpu_versions)
    channels = set(get_operator_channel(v) for v in gpu_versions)
    ocp_versions_set = set(ocp_versions)

    logger.info(
        f"Fetching {operator_package} catalog entries for versions {list(normalized_gpu_versions)} "
        f"across OCP {', '.join(ocp_versions)}"
    )

    # Build filter and fetch entries
    filter_query = build_catalog_filter(operator_package, normalized_gpu_versions,
                                       channels, ocp_versions_set)
    entries = fetch_catalog_entries(filter_query, normalized_gpu_versions, ocp_versions_set)

    logger.info(f"Fetched {len(entries)} catalog entries")
    return entries


if __name__ == "__main__":
    # CLI for informational checking
    import sys

    if len(sys.argv) < 3:
        print("Usage: python catalog_checker.py <gpu_version> <ocp_version1> [ocp_version2 ...]")
        print("Example: python catalog_checker.py 25.10.1 4.20 4.19 4.18 4.17")
        sys.exit(1)

    gpu_ver = sys.argv[1]
    ocp_vers = sys.argv[2:]

    catalog_entries = fetch_gpu_operator_catalog_entries([gpu_ver], ocp_vers)

    print(f"\nGPU Operator v{gpu_ver} catalog availability:")
    for ocp in sorted(ocp_vers, reverse=True):
        available = is_available_in_catalog_entries(catalog_entries, gpu_ver, ocp)
        status = "✓ Available" if available else "✗ Not available"
        print(f"  OpenShift {ocp}: {status}")



