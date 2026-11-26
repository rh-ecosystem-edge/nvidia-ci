"""Check precompiled GPU driver image availability for OCP versions.

Reads a JSON state file containing ``diffs.ocp`` (changed OCP versions)
and ``ocp_versions`` (all tracked versions).  For each changed OCP minor
version, queries the Red Hat registry for matching precompiled GPU driver
tags and writes availability results as JSON.

Optionally creates or closes GitHub issues to track missing drivers.
"""

import json
import os
from datetime import datetime, timezone

from common.utils import logger

# PYTHONPATH must include scripts/common and scripts/gpu (set by the workflow step).
from ocp_release import dtk_labels, release_info
from gpu_drivers import (
    alert_driver_found,
    alert_missing_driver,
    deduplicate_by_major,
    filter_matching_tags,
    gpu_driver_repo_for_rhel,
    list_gpu_driver_tags,
    rhel_major_from_dtk,
    rhel_major_from_kernel,
)


def load_config():
    """Load configuration from environment variables."""
    return {
        'state_path': os.environ.get('VERSION_STATE_PATH', 'version_state.json'),
        'output_path': os.environ.get('DRIVER_STATE_PATH', 'driver_state.json'),
        'architecture': os.environ.get('GPU_PREFLIGHT_ARCHITECTURE', 'amd64'),
        'auth_file': os.environ.get(
            'REGISTRY_AUTH_FILE',
            '/var/run/secrets/ci-pull-credentials/.dockerconfigjson'),
        'github_token': os.environ.get('GITHUB_TOKEN') or os.environ.get('GH_TOKEN') or '',
        'github_repo': os.environ.get(
            'GPU_PREFLIGHT_GITHUB_REPO', 'rh-ecosystem-edge/nvidia-ci'),
    }


def check_ocp_version(ocp_minor, ocp_patch, architecture, auth_file):
    """Check driver image availability for a single OCP version.

    Returns a dict with availability info, or an error entry on failure.
    """
    try:
        suffix = {'amd64': 'x86_64', 'arm64': 'aarch64'}.get(architecture, architecture)
        payload = f'quay.io/openshift-release-dev/ocp-release:{ocp_patch}-{suffix}'

        info = release_info(payload, architecture, auth_file)
        dtk = dtk_labels(info['toolkit'], architecture, auth_file)
        kernel_version = dtk['KERNEL_VERSION']
        rhel_version = str(dtk.get('RHEL_VERSION', ''))

        rhel_major = rhel_major_from_dtk(dtk) or rhel_major_from_kernel(kernel_version)
        if not rhel_major:
            return dict(ocp_minor=ocp_minor, ocp_patch=ocp_patch, available=False,
                        error=f'cannot determine RHEL major from kernel {kernel_version}')

        gpu_repo = gpu_driver_repo_for_rhel(rhel_major)
        tags = list_gpu_driver_tags(gpu_repo, auth_file)
        matches = filter_matching_tags(tags, kernel_version, architecture)
        deduplicated = deduplicate_by_major(matches)
        driver_versions = [e['driver_version'] for e in deduplicated]

        return dict(
            ocp_minor=ocp_minor,
            ocp_patch=ocp_patch,
            kernel_version=kernel_version,
            rhel_version=rhel_version,
            gpu_repo=gpu_repo,
            driver_versions=driver_versions,
            available=len(driver_versions) > 0,
        )
    except Exception as error:
        logger.warning(f'Driver check failed for OCP {ocp_minor}: {error}')
        return dict(ocp_minor=ocp_minor, ocp_patch=ocp_patch, available=False,
                    error=str(error))


def check_versions_and_alert(changed_ocp, architecture, auth_file,
                             github_token, github_repo):
    """Check driver availability for each changed OCP version and send alerts.

    Returns a list of result dicts and the timestamp used for alerting.
    """
    results = []
    timestamp = datetime.now(timezone.utc).isoformat()

    for ocp_minor, ocp_patch in changed_ocp.items():
        logger.info(f'Checking driver images for OCP {ocp_minor} ({ocp_patch})')
        result = check_ocp_version(ocp_minor, ocp_patch, architecture, auth_file)
        results.append(result)

        if result['available']:
            logger.info(f'  Found drivers: {result["driver_versions"]}')
            if github_token:
                alert_driver_found(
                    github_token, github_repo, ocp_minor, architecture,
                    result['driver_versions'], timestamp)
        else:
            logger.warning(f'  No matching drivers for OCP {ocp_minor}')
            if github_token and 'gpu_repo' in result:
                alert_missing_driver(
                    github_token, github_repo, ocp_minor, architecture,
                    result.get('kernel_version', ''),
                    result.get('gpu_repo', ''), timestamp)

    return results, timestamp


def write_driver_state(output_path, results, architecture, timestamp):
    """Write driver availability results to JSON."""
    driver_state = {
        'checked_at': timestamp,
        'architecture': architecture,
        'results': results,
    }

    with open(output_path, 'w') as f:
        json.dump(driver_state, f, indent=2)
        f.write('\n')

    available_count = sum(1 for r in results if r['available'])
    logger.info(f'Driver state written to {output_path} '
                f'({available_count}/{len(results)} versions have drivers)')


def main():
    config = load_config()

    with open(config['state_path'], 'r') as f:
        state = json.load(f)

    changed_ocp = state.get('diffs', {}).get('ocp', {})

    if not changed_ocp:
        logger.info('No OCP version changes — skipping driver image check')
        write_driver_state(config['output_path'], [], config['architecture'],
                           datetime.now(timezone.utc).isoformat())
        return

    results, timestamp = check_versions_and_alert(
        changed_ocp, config['architecture'], config['auth_file'],
        config['github_token'], config['github_repo'])

    write_driver_state(config['output_path'], results,
                       config['architecture'], timestamp)


if __name__ == '__main__':
    main()
