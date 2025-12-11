import json
from workflows.common.utils import logger

from workflows.gpu_operator_versions.settings import Settings
from workflows.gpu_operator_versions.openshift import fetch_ocp_versions
from workflows.gpu_operator_versions.version_utils import get_latest_versions, get_earliest_versions
from workflows.gpu_operator_versions.nvidia_gpu_operator import get_operator_versions, get_sha
from workflows.gpu_operator_versions.catalog_checker import (
    fetch_gpu_operator_catalog_entries,
    is_available_in_catalog_entries
)

# Constants
test_command_template = "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}"

# Version type constants
VERSION_MASTER = "master"
VERSION_GPU_MAIN_LATEST = "gpu-main-latest"
VERSION_GPU_OPERATOR = "gpu-operator"
VERSION_OCP = "ocp"

# Status constants
STATUS_ACTIVE = "active"
STATUS_MAINTENANCE = "maintenance"

# Configuration keys
CONFIG_STATUS = "status"
CONFIG_PINNED_GPU_OPERATOR = "pinned_gpu_operator"
CONFIG_OPENSHIFT_SUPPORT = "openshift_support"
CONFIG_DEFAULTS = "defaults"
CONFIG_UNLISTED_VERSIONS = "unlisted_versions"


def save_tests_commands(tests_commands: set, file_path: str):
    with open(file_path, "w+") as f:
        for command in sorted(tests_commands):
            f.write(command + "\n")


def get_ocp_support_config(ocp_version: str, support_matrix: dict) -> dict:
    """Get support configuration for a specific OpenShift version."""
    ocp_support = support_matrix.get(CONFIG_OPENSHIFT_SUPPORT, {})
    if ocp_version in ocp_support:
        return ocp_support[ocp_version]
    return support_matrix.get(CONFIG_DEFAULTS, {}).get(CONFIG_UNLISTED_VERSIONS, {
        CONFIG_STATUS: STATUS_ACTIVE
    })


def normalize_pinned_gpu_operator(pinned: any) -> list:
    """Normalize pinned_gpu_operator to a list."""
    if pinned is None:
        return []
    if isinstance(pinned, list):
        return pinned
    if isinstance(pinned, str):
        return [pinned]
    if isinstance(pinned, set):
        return list(pinned)
    return []


def get_active_ocp_versions(ocp_releases: list, support_matrix: dict) -> list:
    """Get list of active (non-maintenance) OpenShift versions."""
    return [
        ocp for ocp in ocp_releases
        if get_ocp_support_config(ocp, support_matrix).get(CONFIG_STATUS) == STATUS_ACTIVE
    ]


def handle_master_bundle_changes(ocp_releases: list, support_matrix: dict) -> set[tuple]:
    """Generate tests for master bundle (gpu-main-latest) changes."""
    tests = set()
    active_ocp_versions = get_active_ocp_versions(ocp_releases, support_matrix)

    # Test with newest active version
    for ocp_version in get_latest_versions(active_ocp_versions, 1):
        tests.add((ocp_version, VERSION_MASTER, None))

    # Test with oldest active version
    for ocp_version in get_earliest_versions(active_ocp_versions, 1):
        tests.add((ocp_version, VERSION_MASTER, None))

    return tests


def handle_ocp_version_changes(diffs: dict, ocp_releases: list, gpu_releases: list,
                               support_matrix: dict) -> set[tuple]:
    """Generate tests for OpenShift version changes (new patches)."""
    tests = set()

    for ocp_version in diffs.get(VERSION_OCP, {}):
        if ocp_version not in ocp_releases:
            logger.warning(
                f'OpenShift version "{ocp_version}" is not in the list of releases. '
                f'Check if there was an update to an old version.'
            )
            continue

        ocp_config = get_ocp_support_config(ocp_version, support_matrix)

        if ocp_config.get(CONFIG_STATUS) == STATUS_MAINTENANCE:
            # Maintenance versions: test only with pinned GPU operators
            pinned_gpus = normalize_pinned_gpu_operator(ocp_config.get(CONFIG_PINNED_GPU_OPERATOR))
            for pinned_gpu in pinned_gpus:
                if pinned_gpu not in gpu_releases:
                    logger.warning(
                        f'Maintenance OCP version "{ocp_version}" has pinned GPU operator "{pinned_gpu}" '
                        f'which is not in the list of supported releases.'
                    )
                    continue

                tests.add((ocp_version, pinned_gpu, None))
        else:
            # Active versions: test with latest 2 GPU operator versions
            for gpu_version in gpu_releases:
                tests.add((ocp_version, gpu_version, None))

    return tests


def handle_gpu_operator_changes(diffs: dict, ocp_releases: list, gpu_releases: list,
                                support_matrix: dict, gpu_catalog_entries: list[dict] | None = None) -> set[tuple]:
    """
    Generate tests for GPU operator version changes.

    Args:
        gpu_catalog_entries: Optional list of NVIDIA GPU operator catalog entries
                            to check availability. If provided and version is not
                            available, a warning comment will be included.

    Returns:
        Set of 3-tuples: (ocp_version, gpu_version, comment)
        - comment is None for normal tests
        - comment is a warning string if not available in catalog
    """
    tests = set()
    active_ocp_versions = get_active_ocp_versions(ocp_releases, support_matrix)

    for gpu_version in diffs.get(VERSION_GPU_OPERATOR, {}):
        if gpu_version not in gpu_releases:
            logger.warning(
                f'GPU operator version "{gpu_version}" is not in the list of releases: {list(gpu_releases)}. '
                f'Check if there was an update to an old version.'
            )
            continue

        for ocp_version in active_ocp_versions:
            comment = None

            # Check catalog availability if entries provided
            if gpu_catalog_entries:
                # Use full patch version for catalog check
                gpu_full_version = diffs[VERSION_GPU_OPERATOR][gpu_version]
                available = is_available_in_catalog_entries(gpu_catalog_entries, gpu_full_version, ocp_version)
                if not available:
                    comment = f"# WARNING: GPU {gpu_full_version} not in {ocp_version} catalog"
                    logger.warning(f"GPU {gpu_full_version} not available in OCP {ocp_version} catalog")

            tests.add((ocp_version, gpu_version, comment))

    return tests


def create_tests_matrix(diffs: dict, ocp_releases: list, gpu_releases: list,
                       support_matrix: dict, gpu_catalog_entries: list[dict] | None = None) -> set[tuple]:
    """
    Create test matrix based on version changes and support matrix.

    Rules:
    1. GPU main-latest changed: Test with newest/oldest active OCP versions
    2. OCP version changed: Active versions test with latest 2 GPU operators,
                           maintenance versions test only with pinned GPU operators
    3. GPU operator changed: Test only with active OCP versions (maintenance is frozen)
                            Include warning comment if not available in catalog

    Args:
        gpu_catalog_entries: Optional NVIDIA GPU operator catalog entries for availability checking

    Returns:
        Set of 3-tuples: (ocp_version, gpu_version, comment)
        - comment is None for normal tests
        - comment is a string (e.g., warning) when additional context is needed
    """
    tests = set()

    if VERSION_GPU_MAIN_LATEST in diffs:
        tests.update(handle_master_bundle_changes(ocp_releases, support_matrix))

    if VERSION_OCP in diffs:
        tests.update(handle_ocp_version_changes(diffs, ocp_releases, gpu_releases, support_matrix))

    if VERSION_GPU_OPERATOR in diffs:
        tests.update(handle_gpu_operator_changes(diffs, ocp_releases, gpu_releases,
                                                 support_matrix, gpu_catalog_entries))

    return tests


def create_tests_commands(diffs: dict, ocp_releases: list, gpu_releases: list,
                         support_matrix: dict, gpu_catalog_entries: list[dict] | None = None) -> set[str]:
    """
    Create test commands from diffs.

    Args:
        gpu_catalog_entries: Optional NVIDIA GPU operator catalog entries for availability checking

    Returns:
        Set of test command strings and comment strings (e.g., warnings)
    """
    tests_commands = set()
    tests = create_tests_matrix(diffs, ocp_releases, gpu_releases, support_matrix, gpu_catalog_entries)

    for ocp_version, gpu_version, comment in tests:
        # Add comment if present (e.g., warning about catalog availability)
        if comment:
            tests_commands.add(comment)

        # Generate test command
        gpu_version_suffix = version2suffix(gpu_version)
        tests_commands.add(test_command_template.format(ocp_version=ocp_version, gpu_version=gpu_version_suffix))

    return tests_commands


def calculate_diffs(old_versions: dict, new_versions: dict, ocp_versions: dict | None = None,
                    support_matrix: dict | None = None, check_catalog: bool = False) -> tuple[dict, list[dict]]:
    """
    Calculate differences between old and new versions.

    Returns:
        Tuple of (diffs, gpu_catalog_entries) where gpu_catalog_entries are NVIDIA GPU
        operator catalog entries that can be used for warnings
    """
    diffs = {}
    gpu_catalog_entries = []

    for key, value in new_versions.items():
        if isinstance(value, dict):
            logger.info(f'Comparing versions under "{key}"')
            sub_diff, _ = calculate_diffs(old_versions.get(key, {}), value)
            if sub_diff:
                diffs[key] = sub_diff
        else:
            if key not in old_versions or old_versions[key] != value:
                logger.info(f'Key "{key}" has changed: {old_versions.get(key)} > {value}')
                diffs[key] = value

    # Filter GPU operator diffs by catalog availability
    if check_catalog and VERSION_GPU_OPERATOR in diffs and ocp_versions and support_matrix:
        gpu_diffs = diffs[VERSION_GPU_OPERATOR]
        if gpu_diffs:
            filtered, gpu_catalog_entries = filter_new_gpu_versions_by_catalog(
                gpu_diffs,
                ocp_versions,
                support_matrix
            )
            if filtered:
                diffs[VERSION_GPU_OPERATOR] = filtered
            else:
                del diffs[VERSION_GPU_OPERATOR]

    return diffs, gpu_catalog_entries


def version2suffix(v: str):
    return v if v == VERSION_MASTER else f'{v.replace(".", "-")}-x'


def filter_new_gpu_versions_by_catalog(
    gpu_diffs: dict,
    ocp_versions: dict,
    support_matrix: dict
) -> tuple[dict, list[dict]]:
    """
    Filter out new GPU versions that aren't in any active OCP catalog.

    Returns:
        Tuple of (filtered_diffs, gpu_catalog_entries) where gpu_catalog_entries are
        NVIDIA GPU operator catalog entries that can be used to check availability
    """
    if not gpu_diffs:
        return gpu_diffs, []

    ocp_releases = list(ocp_versions.keys())
    active_ocp_versions = get_active_ocp_versions(ocp_releases, support_matrix)

    # If there are no active OCP versions, keep all new GPU versions as-is
    if not active_ocp_versions:
        logger.info('No active OCP versions - skipping catalog availability check')
        return gpu_diffs, []

    # Fetch GPU operator catalog entries using FULL patch versions
    full_patch_versions = [gpu_diffs[v] for v in gpu_diffs.keys()]
    logger.info(f'Checking catalog availability for {len(gpu_diffs)} new GPU version(s)')
    gpu_catalog_entries = fetch_gpu_operator_catalog_entries(
        gpu_versions=full_patch_versions,
        ocp_versions=active_ocp_versions,
        operator_package="gpu-operator-certified"
    )

    # Filter out versions not in any catalog
    filtered_diffs = {}
    for gpu_version, gpu_full_version in gpu_diffs.items():
        available_in_any = any(
            is_available_in_catalog_entries(gpu_catalog_entries, gpu_full_version, ocp)
            for ocp in active_ocp_versions
        )

        if not available_in_any:
            logger.warning(
                f'GPU operator {gpu_full_version} not available in any active OCP catalog - '
                f'excluding from versions.json (will retry next run)'
            )
        else:
            filtered_diffs[gpu_version] = gpu_full_version

    return filtered_diffs, gpu_catalog_entries


def apply_diffs(old_versions: dict, diffs: dict) -> dict:
    """Apply diffs to old versions to create updated versions."""
    updated = dict(old_versions)
    for key, value in diffs.items():
        if isinstance(value, dict) and key in updated and isinstance(updated[key], dict):
            # Recursively apply nested diffs
            updated[key] = apply_diffs(updated[key], value)
        else:
            updated[key] = value
    return updated


def main():
    settings = Settings()
    sha = get_sha(settings)
    gpu_versions = get_operator_versions(settings)
    ocp_versions = fetch_ocp_versions(settings)

    new_versions = {
        VERSION_GPU_MAIN_LATEST: sha,
        VERSION_GPU_OPERATOR: gpu_versions,
        VERSION_OCP: ocp_versions
    }

    with open(settings.version_file_path, "r+") as json_f:
        old_versions = json.load(json_f)

        # Calculate diffs with catalog filtering
        diffs, gpu_catalog_entries = calculate_diffs(
            old_versions,
            new_versions,
            ocp_versions=ocp_versions,
            support_matrix=settings.support_matrix,
            check_catalog=settings.check_catalog_availability
        )

        # Apply filtered diffs to get final versions
        final_versions = apply_diffs(old_versions, diffs)

        json_f.seek(0)
        json.dump(final_versions, json_f, indent=4)
        json_f.truncate()

    # Use the already-filtered diffs for test generation
    ocp_releases = list(ocp_versions.keys())
    # Use final_versions (after catalog filtering) instead of raw gpu_versions
    # to ensure tests are only generated for versions actually tracked in versions.json
    gpu_releases = get_latest_versions(list(final_versions[VERSION_GPU_OPERATOR].keys()), 2)

    tests_commands = create_tests_commands(
        diffs,
        ocp_releases,
        gpu_releases,
        settings.support_matrix,
        gpu_catalog_entries  # Pass GPU operator catalog entries for warning generation
    )
    save_tests_commands(tests_commands, settings.tests_to_trigger_file_path)

if __name__ == '__main__':
    main()
