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
    old_versions = state.get('old_versions', {})

    if not diffs:
        logger.info('No version changes — writing empty test commands')
        save_tests_commands(set(), settings.tests_to_trigger_file_path)
        return

    # Write updated versions.json.
    with open(settings.version_file_path, 'w') as f:
        json.dump(final_versions, f, indent=4)
        f.write('\n')

    ocp_releases = list(ocp_versions.keys())

    # When driver_state.json exists, filter out OCP versions without drivers.
    effective_diffs = dict(diffs)
    if driver_state_path and os.path.isfile(driver_state_path):
        with open(driver_state_path, 'r') as f:
            driver_state = json.load(f)

        unavailable = {
            r['ocp_minor'] for r in driver_state.get('results', [])
            if not r.get('available', False)
        }

        if unavailable and 'ocp' in effective_diffs:
            filtered_ocp = {
                minor: patch
                for minor, patch in effective_diffs['ocp'].items()
                if minor not in unavailable
            }
            if filtered_ocp:
                effective_diffs['ocp'] = filtered_ocp
            else:
                del effective_diffs['ocp']

            if unavailable:
                logger.info(f'Filtered OCP versions without drivers: {sorted(unavailable)}')

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
