"""Detect current OCP and GPU operator versions and calculate diffs against
tracked versions.  Writes version state as JSON."""

import json
import os
import sys

from common.utils import logger
from gpu_operator_versions.settings import Settings
from gpu_operator_versions.openshift import fetch_ocp_versions
from gpu_operator_versions.nvidia_gpu_operator import get_operator_versions, get_sha
from gpu_operator_versions.update_versions import (
    calculate_diffs,
    apply_diffs,
    get_gpu_releases_for_tests,
    VERSION_GPU_MAIN_LATEST,
    VERSION_GPU_OPERATOR,
    VERSION_OCP,
)
from gpu_operator_versions.version_utils import get_latest_versions


def main():
    settings = Settings()
    output_path = os.environ.get('VERSION_STATE_PATH', 'version_state.json')

    sha = get_sha(settings)
    gpu_versions = get_operator_versions(settings)
    ocp_versions = fetch_ocp_versions(settings)

    # Only the latest GPU channels are eligible for new tests.
    latest_gpu_channels = get_latest_versions(list(gpu_versions), 2)
    gpu_versions = {
        channel: gpu_versions[channel]
        for channel in latest_gpu_channels
    }

    new_versions = {
        VERSION_GPU_MAIN_LATEST: sha,
        VERSION_GPU_OPERATOR: gpu_versions,
        VERSION_OCP: ocp_versions,
    }

    with open(settings.version_file_path, 'r') as f:
        old_versions = json.load(f)

    diffs, gpu_catalog_entries = calculate_diffs(
        old_versions,
        new_versions,
        ocp_versions=ocp_versions,
        support_matrix=settings.support_matrix,
        check_catalog=settings.check_catalog_availability,
    )

    if not diffs:
        logger.info('No version changes detected')
        final_versions = old_versions
    else:
        final_versions = apply_diffs(old_versions, diffs)

    gpu_releases = get_gpu_releases_for_tests(final_versions, latest_gpu_channels)

    state = {
        'ocp_versions': ocp_versions,
        'gpu_versions': gpu_versions,
        'sha': sha,
        'diffs': diffs,
        'gpu_catalog_entries': gpu_catalog_entries,
        'final_versions': final_versions,
        'latest_gpu_channels': latest_gpu_channels,
        'gpu_releases': gpu_releases,
        'old_versions': old_versions,
    }

    with open(output_path, 'w') as f:
        json.dump(state, f, indent=2)
        f.write('\n')

    logger.info(f'Version state written to {output_path} (diffs: {bool(diffs)})')


if __name__ == '__main__':
    main()
