"""Generate test commands from version diffs and write updated version tracking.

Reads a version state JSON (already filtered by earlier pipeline steps)
and writes the updated versions file and a file of test commands to trigger.
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

    # Write updated versions.json.
    with open(settings.version_file_path, 'w') as f:
        json.dump(final_versions, f, indent=4)
        f.write('\n')

    ocp_releases = list(ocp_versions.keys())

    tests_commands = create_tests_commands(
        diffs,
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
