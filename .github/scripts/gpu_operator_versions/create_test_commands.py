"""Generate test commands from version diffs and write updated version tracking.

Reads a version state JSON and optionally a driver availability JSON to
filter excluded OCP versions.  Writes the updated versions file and a file
of test commands to trigger.
"""

import json
import os

from common.utils import logger
from gpu_operator_versions.settings import Settings
from gpu_operator_versions.update_versions import (
    create_tests_commands,
    save_tests_commands,
)


def load_unavailable_ocp_versions(driver_state_path):
    """Load OCP versions without available drivers from driver state.

    Returns a set of OCP minor versions that lack precompiled driver images,
    or an empty set if no driver state exists.
    """
    if not driver_state_path or not os.path.isfile(driver_state_path):
        return set()

    with open(driver_state_path, 'r') as f:
        driver_state = json.load(f)

    unavailable = {
        r['ocp_minor'] for r in driver_state.get('results', [])
        if not r.get('available', False)
    }

    if unavailable:
        logger.info(f'OCP versions without drivers: {sorted(unavailable)}')

    return unavailable


def filter_diffs_by_availability(diffs, unavailable):
    """Remove unavailable OCP versions from diffs.

    Returns a new diffs dict with unavailable OCP versions excluded.
    """
    if not unavailable or 'ocp' not in diffs:
        return dict(diffs)

    effective = dict(diffs)
    filtered_ocp = {
        minor: patch
        for minor, patch in effective['ocp'].items()
        if minor not in unavailable
    }

    if filtered_ocp:
        effective['ocp'] = filtered_ocp
    else:
        del effective['ocp']

    return effective


def filter_versions_by_availability(final_versions, unavailable):
    """Remove unavailable OCP versions from final_versions.

    Versions without drivers are excluded from version tracking so they
    will be detected as new changes on the next run and retried.
    """
    if not unavailable:
        return final_versions

    filtered = dict(final_versions)
    if 'ocp' in filtered:
        filtered['ocp'] = {
            minor: patch
            for minor, patch in filtered['ocp'].items()
            if minor not in unavailable
        }

    return filtered


def main():
    settings = Settings()
    state_path = os.environ.get('VERSION_STATE_PATH', 'version_state.json')
    driver_state_path = os.environ.get('DRIVER_STATE_PATH', '')

    with open(state_path, 'r') as f:
        state = json.load(f)

    diffs = state.get('diffs', {})
    ocp_versions = state.get('ocp_versions', {})
    final_versions = state.get('final_versions', {})
    gpu_releases = state.get('gpu_releases', [])
    gpu_catalog_entries = state.get('gpu_catalog_entries', [])

    if not diffs:
        logger.info('No version changes — writing empty test commands')
        save_tests_commands(set(), settings.tests_to_trigger_file_path)
        return

    unavailable = load_unavailable_ocp_versions(driver_state_path)

    # Filter unavailable OCP versions from final_versions BEFORE writing
    # versions.json — versions without drivers are not recorded so they
    # will be retried on the next run.
    filtered_versions = filter_versions_by_availability(final_versions, unavailable)

    with open(settings.version_file_path, 'w') as f:
        json.dump(filtered_versions, f, indent=4)
        f.write('\n')

    ocp_releases = list(ocp_versions.keys())
    effective_diffs = filter_diffs_by_availability(diffs, unavailable)

    tests_commands = create_tests_commands(
        effective_diffs,
        ocp_releases,
        gpu_releases,
        settings.support_matrix,
        gpu_catalog_entries,
        test_command_suffix=settings.test_command_suffix,
    )
    save_tests_commands(tests_commands, settings.tests_to_trigger_file_path)
    logger.info(f'Generated {len(tests_commands)} test command(s)')


if __name__ == '__main__':
    main()
