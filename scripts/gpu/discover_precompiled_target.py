#!/usr/bin/env python3
"""Check GPU precompiled driver image availability for a target OCP version.

Queries the OCP release API for the latest accepted release, extracts the
kernel version from the driver-toolkit image via skopeo inspect, then checks
registry.redhat.io for matching precompiled GPU driver tags.

If matching driver images are found, writes configuration to SHARED_DIR for
downstream test steps.  If no matching images exist, writes a skip-work-steps
marker so subsequent steps exit gracefully.
"""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import sys
from urllib.request import Request, urlopen

from ocp_release import (
    dtk_labels,
    error_message,
    release_info,
    release_stream_urls,
    strip_installer_suffix,
)
from gpu_drivers import (
    PRODUCTION_REGISTRY,
    alert_driver_found,
    alert_missing_driver,
    deduplicate_by_major,
    filter_matching_tags,
    gpu_driver_repo_for_rhel,
    list_gpu_driver_tags,
    rhel_major_from_dtk,
    rhel_major_from_kernel,
)


def get_json(url, headers=None):
    """Fetch JSON from a URL with a 60-second timeout."""
    with urlopen(Request(url, headers=headers or {}), timeout=60) as response:
        return json.load(response)


def latest_accepted_release(ocp_version, architecture, release_stream_url):
    """Return the newest accepted OCP release matching the target minor version."""
    for url in release_stream_url:
        stream = get_json(url)
        for tag in stream['tags']:
            if tag['phase'] != 'Accepted':
                continue
            version = strip_installer_suffix(tag['name'])
            minor = '.'.join(version.split('.')[:2])
            if minor != ocp_version:
                continue
            payload = tag['pullSpec']
            if payload.startswith('registry.ci.openshift.org/'):
                suffix = {'amd64': 'x86_64', 'arm64': 'aarch64'}.get(architecture, architecture)
                payload = f'quay.io/openshift-release-dev/ocp-release:{version}-{suffix}'
            return dict(version=version, payload=payload)
    return None


def write_json(path, value):
    """Write a JSON file, creating parent directories as needed."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ocp-version', default=os.environ.get('GPU_PREFLIGHT_OCP_VERSION'),
                        help='Target OCP minor version (e.g. 4.18). Env: GPU_PREFLIGHT_OCP_VERSION')
    parser.add_argument('--architecture', choices=['amd64', 'arm64', 'ppc64le', 's390x'],
                        default=os.environ.get('GPU_PREFLIGHT_ARCHITECTURE', 'amd64'))
    parser.add_argument('--registry-auth-file', type=Path, default=os.environ.get(
        'REGISTRY_AUTH_FILE', '/var/run/secrets/ci-pull-credentials/.dockerconfigjson'))
    parser.add_argument('--release-stream-url', action='append',
                        help='OCP release stream API URL (repeatable; derived from architecture if omitted)')
    parser.add_argument('--output-dir', type=Path, default=os.environ.get('ARTIFACT_DIR'))
    parser.add_argument('--shared-dir', type=Path, default=os.environ.get('SHARED_DIR'))
    parser.add_argument('--github-repo', default=os.environ.get(
        'GPU_PREFLIGHT_GITHUB_REPO', 'rh-ecosystem-edge/nvidia-ci'),
        help='GitHub owner/repo for issue alerting. Env: GPU_PREFLIGHT_GITHUB_REPO')
    args = parser.parse_args()
    args.github_token = os.environ.get('GITHUB_TOKEN') or os.environ.get('GH_TOKEN') or ''

    if not args.ocp_version:
        parser.error('--ocp-version or GPU_PREFLIGHT_OCP_VERSION is required')
    if not args.output_dir or not args.shared_dir:
        parser.error('Artifact and shared directories are required')

    args.release_stream_url = args.release_stream_url or release_stream_urls(args.architecture, args.ocp_version)
    started = datetime.now(timezone.utc)

    result = dict(
        schema_version=1,
        job_name=os.environ.get('JOB_NAME', 'local'),
        build_id=os.environ.get('BUILD_ID', 'local'),
        started_at=started.isoformat(),
        ocp_version=args.ocp_version,
        architecture=args.architecture,
        outcome='failed',
        driver_versions=None,
        kernel_version=None,
        release_version=None,
    )

    gpu_repo = ''

    try:
        handoff_names = ['gpu-precompiled-discovery-result.json', 'gpu-precompiled-config.json', 'skip-work-steps']
        if any((directory / name).exists() for directory in (args.output_dir, args.shared_dir) for name in handoff_names):
            raise ValueError('discovery requires fresh output and shared directories')

        print(f'GPU preflight: looking up OCP {args.ocp_version} release for {args.architecture}')
        release = latest_accepted_release(args.ocp_version, args.architecture, args.release_stream_url)
        if release is None:
            result.update(outcome='no_work', reason='no_accepted_release')
            print(f'GPU preflight: no accepted OCP {args.ocp_version} release found')
        else:
            print(f'GPU preflight: found OCP {release["version"]} payload')
            result['release_version'] = release['version']

            info = release_info(release['payload'], args.architecture, args.registry_auth_file)
            dtk = dtk_labels(info['toolkit'], args.architecture, args.registry_auth_file)
            kernel_version = dtk['KERNEL_VERSION']
            rhel_version = str(dtk.get('RHEL_VERSION', ''))
            result['kernel_version'] = kernel_version
            print(f'GPU preflight: DTK kernel={kernel_version} RHEL={rhel_version}')

            rhel_major = rhel_major_from_dtk(dtk) or rhel_major_from_kernel(kernel_version)
            if not rhel_major:
                raise ValueError(f'cannot determine RHEL major version from DTK or kernel {kernel_version}')

            gpu_repo = gpu_driver_repo_for_rhel(rhel_major)

            print(f'GPU preflight: listing tags from {PRODUCTION_REGISTRY}/{gpu_repo}')
            tags = list_gpu_driver_tags(gpu_repo, args.registry_auth_file)
            print(f'GPU preflight: found {len(tags)} total tags')

            matches = filter_matching_tags(tags, kernel_version, args.architecture)
            deduplicated = deduplicate_by_major(matches)

            if not deduplicated:
                result.update(outcome='no_work', reason='no_matching_driver_tags')
                print(f'GPU preflight: no driver tags match kernel {kernel_version}')
            else:
                driver_versions = [entry['driver_version'] for entry in deduplicated]
                result.update(
                    outcome='selected',
                    reason='matching_drivers_found',
                    driver_versions=driver_versions,
                )
                print(f'GPU preflight: found driver versions {driver_versions} for kernel {kernel_version}')

                config = dict(
                    kernel_version=kernel_version,
                    rhel_version=rhel_version,
                    ocp_version=release['version'],
                    architecture=args.architecture,
                    driver_versions=driver_versions,
                    driver_repository=PRODUCTION_REGISTRY + '/' + gpu_repo,
                )
                write_json(args.shared_dir / 'gpu-precompiled-config.json', config)

                (args.shared_dir / 'gpu-precompiled-kernel-version').write_text(kernel_version + '\n')
                (args.shared_dir / 'gpu-precompiled-driver-versions').write_text(
                    '\n'.join(driver_versions) + '\n')

    except (OSError, ValueError, KeyError, TypeError, IndexError) as error:
        result.update(outcome='failed', driver_versions=None, error=error_message(error))
        print('GPU preflight failed: ' + result.get('error', str(error)), file=sys.stderr)

    result['finished_at'] = datetime.now(timezone.utc).isoformat()

    for directory in (args.output_dir, args.shared_dir):
        write_json(directory / 'gpu-precompiled-discovery-result.json', result)

    if result['outcome'] == 'no_work':
        (args.shared_dir / 'skip-work-steps').touch()

    finished = result.get('finished_at', started.isoformat())
    if result['outcome'] == 'no_work' and result.get('reason') == 'no_matching_driver_tags':
        alert_missing_driver(args.github_token, args.github_repo,
                             args.ocp_version, args.architecture,
                             result.get('kernel_version', ''), gpu_repo, finished)
    elif result['outcome'] == 'selected' and result.get('driver_versions'):
        alert_driver_found(args.github_token, args.github_repo,
                           args.ocp_version, args.architecture,
                           result['driver_versions'], finished)

    print(f'GPU preflight outcome={result["outcome"]}')
    return int(result['outcome'] == 'failed')


if __name__ == '__main__':
    sys.exit(main())
