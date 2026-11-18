import argparse
from dataclasses import dataclass
import json
from datetime import datetime, timezone
from pathlib import Path

from common.utils import logger
from gpu_operator_versions.profiles import (
    Profile,
    ProfileManifest,
    current_weekday_name,
    format_weekdays,
    load_profiles,
    partition_profiles,
)
from gpu_operator_versions.openshift import fetch_ocp_version_stream, filter_ocp_versions
from gpu_operator_versions.version_utils import get_latest_versions, get_earliest_versions
from gpu_operator_versions.nvidia_gpu_operator import get_operator_versions, get_sha
from gpu_operator_versions.catalog_checker import (
    fetch_gpu_operator_catalog_entries,
    is_available_in_catalog_entries
)

# Constants

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
                         support_matrix: dict, test_command_template: str,
                         gpu_catalog_entries: list[dict] | None = None) -> set[str]:
    """
    Create test commands from diffs.

    Args:
        gpu_catalog_entries: Optional NVIDIA GPU operator catalog entries for availability checking
        test_command_template: Complete profile-specific /test command template

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
        tests_commands.add(test_command_template.format(
            ocp_version=ocp_version, gpu_version=gpu_version_suffix))

    return tests_commands


def calculate_diffs(old_versions: dict, new_versions: dict) -> dict:
    """Calculate raw differences between two version snapshots."""
    diffs = {}

    for key, value in new_versions.items():
        if isinstance(value, dict):
            logger.info(f'Comparing versions under "{key}"')
            sub_diff = calculate_diffs(old_versions.get(key, {}), value)
            if sub_diff:
                diffs[key] = sub_diff
        else:
            if key not in old_versions or old_versions[key] != value:
                logger.info(f'Key "{key}" has changed: {old_versions.get(key)} > {value}')
                diffs[key] = value

    return diffs


def version2suffix(v: str):
    return v if v == VERSION_MASTER else f'{v.replace(".", "-")}-x'


def filter_new_gpu_versions_by_catalog(
    gpu_diffs: dict,
    ocp_versions: dict,
    support_matrix: dict,
    gpu_catalog_entries: list[dict] | tuple[dict, ...],
) -> tuple[dict, list[dict]]:
    """
    Filter new GPU versions using already-fetched catalog entries.

    Returns:
        Tuple of (filtered_diffs, gpu_catalog_entries) where gpu_catalog_entries are
        NVIDIA GPU operator catalog entries that can be used to check availability
    """
    if not gpu_diffs:
        return gpu_diffs, []

    ocp_releases = list(ocp_versions.keys())
    active_ocp_versions = get_active_ocp_versions(ocp_releases, support_matrix)

    # If there are no active OCP versions, keep all new GPU versions as-is.
    if not active_ocp_versions:
        logger.info('No active OCP versions - skipping catalog availability check')
        return gpu_diffs, []

    # Filter out versions not in any catalog.
    filtered_diffs = {}
    for gpu_version, gpu_full_version in gpu_diffs.items():
        available_in_any = any(
            is_available_in_catalog_entries(gpu_catalog_entries, gpu_full_version, ocp)
            for ocp in active_ocp_versions
        )

        if not available_in_any:
            logger.warning(
                f'GPU operator {gpu_full_version} not available in any active OCP catalog - '
                f'excluding from the profile version file (will retry next run)'
            )
        else:
            filtered_diffs[gpu_version] = gpu_full_version

    return filtered_diffs, list(gpu_catalog_entries)


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


def _version_tuple(version: str) -> tuple[int, ...]:
    return tuple(int(part) for part in version.split('.'))


def filter_gpu_versions(gpu_versions: dict[str, str], minimum_gpu_channel: str) -> dict[str, str]:
    """Filter GPU Operator channels by an optional minimum channel."""
    if not minimum_gpu_channel:
        return dict(gpu_versions)

    minimum = _version_tuple(minimum_gpu_channel)
    return {
        channel: version
        for channel, version in gpu_versions.items()
        if _version_tuple(channel) >= minimum
    }


def discover_versions(request_timeout_sec: int) -> dict:
    """Discover the raw upstream version state exactly once."""
    return {
        VERSION_GPU_MAIN_LATEST: get_sha(request_timeout_sec),
        VERSION_GPU_OPERATOR: get_operator_versions(request_timeout_sec),
        VERSION_OCP: fetch_ocp_version_stream(request_timeout_sec),
    }


@dataclass(frozen=True)
class ProfilePlan:
    """A raw profile calculation and its catalog lookup requirements."""

    profile: Profile
    old_versions: dict
    ocp_versions: dict
    raw_diffs: dict
    catalog_gpu_versions: frozenset[str]
    catalog_ocp_versions: frozenset[str]


@dataclass(frozen=True)
class CatalogLookup:
    """The result of one shared catalog lookup for all active profiles."""

    entries: tuple[dict, ...]
    gpu_versions: tuple[str, ...]
    ocp_versions: tuple[str, ...]


@dataclass(frozen=True)
class ProfileUpdate:
    """A fully calculated profile update, prior to writing any files."""

    profile: Profile
    final_versions: dict
    commands: set[str]
    comments: set[str]
    new_gpu_channels: set[str]


def prepare_profile(snapshot: dict, profile: Profile) -> ProfilePlan:
    """Calculate raw profile differences without performing catalog I/O."""
    ocp_versions = filter_ocp_versions(
        snapshot[VERSION_OCP],
        profile.ignored_versions_regex,
    )
    gpu_versions = filter_gpu_versions(
        snapshot[VERSION_GPU_OPERATOR],
        profile.minimum_gpu_channel,
    )
    new_versions = {
        VERSION_GPU_MAIN_LATEST: snapshot[VERSION_GPU_MAIN_LATEST],
        VERSION_GPU_OPERATOR: gpu_versions,
        VERSION_OCP: ocp_versions,
    }

    version_file = Path(profile.version_file_path)
    old_versions = json.loads(version_file.read_text(encoding='utf-8'))
    raw_diffs = calculate_diffs(old_versions, new_versions)

    active_ocp_versions = get_active_ocp_versions(
        list(ocp_versions.keys()),
        profile.support_matrix,
    )
    needs_catalog = (
        profile.check_catalog_availability
        and bool(raw_diffs.get(VERSION_GPU_OPERATOR, {}))
        and bool(active_ocp_versions)
    )
    catalog_gpu_versions = (
        frozenset(raw_diffs.get(VERSION_GPU_OPERATOR, {}).values())
        if needs_catalog
        else frozenset()
    )
    catalog_ocp_versions = frozenset(active_ocp_versions) if needs_catalog else frozenset()

    return ProfilePlan(
        profile=profile,
        old_versions=old_versions,
        ocp_versions=ocp_versions,
        raw_diffs=raw_diffs,
        catalog_gpu_versions=catalog_gpu_versions,
        catalog_ocp_versions=catalog_ocp_versions,
    )


def fetch_catalog_lookup(plans: list[ProfilePlan]) -> CatalogLookup:
    """Fetch one catalog lookup containing the union of all profile requirements."""
    gpu_versions = set().union(
        *(plan.catalog_gpu_versions for plan in plans)
    ) if plans else set()
    ocp_versions = set().union(
        *(plan.catalog_ocp_versions for plan in plans)
    ) if plans else set()

    if not gpu_versions or not ocp_versions:
        return CatalogLookup(
            entries=(),
            gpu_versions=(),
            ocp_versions=(),
        )

    logger.info(
        f'Checking catalog availability for {len(gpu_versions)} GPU version(s) '
        f'across {len(ocp_versions)} active OCP version(s)'
    )
    entries = fetch_gpu_operator_catalog_entries(
        gpu_versions=sorted(gpu_versions),
        ocp_versions=sorted(ocp_versions),
        operator_package='gpu-operator-certified',
    )
    return CatalogLookup(
        entries=tuple(entries),
        gpu_versions=tuple(sorted(gpu_versions)),
        ocp_versions=tuple(sorted(ocp_versions)),
    )


def finalize_profile(plan: ProfilePlan, catalog_lookup: CatalogLookup) -> ProfileUpdate:
    """Apply a shared catalog lookup to one raw profile plan."""
    profile = plan.profile
    diffs = dict(plan.raw_diffs)
    gpu_catalog_entries: list[dict] = []

    if plan.catalog_gpu_versions and plan.catalog_ocp_versions:
        filtered, gpu_catalog_entries = filter_new_gpu_versions_by_catalog(
            diffs.get(VERSION_GPU_OPERATOR, {}),
            plan.ocp_versions,
            profile.support_matrix,
            catalog_lookup.entries,
        )
        if filtered:
            diffs[VERSION_GPU_OPERATOR] = filtered
        else:
            diffs.pop(VERSION_GPU_OPERATOR, None)

    final_versions = apply_diffs(plan.old_versions, diffs)

    ocp_releases = list(plan.ocp_versions.keys())
    # Use final_versions rather than the raw snapshot so commands are generated
    # only for channels actually tracked in this profile's version file.
    gpu_releases = get_latest_versions(
        list(final_versions[VERSION_GPU_OPERATOR].keys()),
        2,
    )
    # Apply the profile's optional minimum GPU channel filter.
    if profile.minimum_gpu_channel:
        gpu_releases = [
            channel
            for channel in gpu_releases
            if _version_tuple(channel) >= _version_tuple(profile.minimum_gpu_channel)
        ]

    generated_output = create_tests_commands(
        diffs,
        ocp_releases,
        gpu_releases,
        profile.support_matrix,
        gpu_catalog_entries=gpu_catalog_entries,
        test_command_template=profile.test_command_template,
    )
    commands = {
        command for command in generated_output if command.startswith('/test ')
    }
    comments = {
        f'**{profile.name}:** {command.lstrip("# ")}'
        for command in generated_output
        if not command.startswith('/test ')
    }
    new_gpu_channels = (
        set(final_versions[VERSION_GPU_OPERATOR])
        - set(plan.old_versions.get(VERSION_GPU_OPERATOR, {}))
    )
    return ProfileUpdate(
        profile=profile,
        final_versions=final_versions,
        commands=commands,
        comments=comments,
        new_gpu_channels=new_gpu_channels,
    )


def write_profile_update(update: ProfileUpdate) -> None:
    """Write one fully calculated profile version file."""
    version_file = Path(update.profile.version_file_path)
    version_file.write_text(
        json.dumps(update.final_versions, indent=4),
        encoding='utf-8',
    )


def _write_text(path: str | Path, text: str) -> None:
    output = Path(path)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(text, encoding='utf-8')


def _write_lines(path: str | Path, lines: list[str]) -> None:
    _write_text(path, ''.join(f'{line}\n' for line in lines))


def render_test_comments(
    comments: set[str],
    skipped_profiles: list[Profile],
    now: datetime,
    catalog_lookup: CatalogLookup,
) -> list[str]:
    """Render aggregate warnings and non-actionable schedule information."""
    lines = [f'- {comment}' for comment in sorted(comments)]
    if catalog_lookup.gpu_versions and catalog_lookup.ocp_versions:
        lines.append(
            f'- INFO: Checked GPU Operator catalog availability for versions '
            f'{", ".join(catalog_lookup.gpu_versions)} across OCP '
            f'{", ".join(catalog_lookup.ocp_versions)}.'
        )
    lines.extend(
        f'- INFO: **{profile.name}** was not run because '
        f'{current_weekday_name(now)} is outside its configured weekdays '
        f'({format_weekdays(profile.active_weekdays)}).'
        for profile in skipped_profiles
    )
    return lines


def run(manifest: ProfileManifest, now: datetime | None = None) -> list[str]:
    """Discover upstream versions once and dispatch them to active profiles."""
    current_time = now or datetime.now(timezone.utc)
    active_profiles, skipped_profiles = partition_profiles(
        manifest.profiles,
        current_time,
    )
    active_names = [profile.name for profile in active_profiles]
    logger.info(f'Active profiles: {", ".join(active_names) if active_names else "none"}')

    if not active_profiles:
        _write_lines(manifest.tests_to_trigger_file_path, [])
        _write_lines(
            manifest.test_comments_file_path,
            render_test_comments(
                set(),
                skipped_profiles,
                current_time,
                CatalogLookup((), (), ()),
            ),
        )
        _write_lines(manifest.new_gpu_channels_file, [])
        return active_names

    snapshot = discover_versions(manifest.request_timeout_sec)

    plans: list[ProfilePlan] = []
    for profile in active_profiles:
        logger.info(f'Calculating profile: {profile.name}')
        plans.append(prepare_profile(snapshot, profile))

    catalog_lookup = fetch_catalog_lookup(plans)
    updates = [finalize_profile(plan, catalog_lookup) for plan in plans]

    commands: set[str] = set()
    comments: set[str] = set()
    new_gpu_channels: set[str] = set()
    for update in updates:
        logger.info(f'Writing profile: {update.profile.name}')
        write_profile_update(update)
        commands.update(update.commands)
        comments.update(update.comments)
        new_gpu_channels.update(update.new_gpu_channels)

    _write_lines(manifest.tests_to_trigger_file_path, sorted(commands))
    _write_lines(
        manifest.test_comments_file_path,
        render_test_comments(
            comments,
            skipped_profiles,
            current_time,
            catalog_lookup,
        ),
    )
    _write_lines(manifest.new_gpu_channels_file, sorted(new_gpu_channels))
    return active_names


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description='Update configured GPU Operator versions')
    parser.add_argument(
        '--manifest',
        default=str(Path(__file__).with_name('profiles.json')),
        help='Path to the profile manifest',
    )
    return parser.parse_args()


def main():
    args = parse_args()
    manifest = load_profiles(args.manifest)
    run(manifest)


if __name__ == '__main__':
    main()
