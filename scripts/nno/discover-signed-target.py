#!/usr/bin/env python3
"""Select one recent, unattempted driver and an installable matching OCP release."""

import argparse
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

from signed_images import command, config_digest, digest, error_message, identity, image_info, metadata, runtime_digests, version_re

REPOSITORIES = ['registry.stage.redhat.io/nvidia/doca-driver-rhel9', 'registry.stage.redhat.io/nvidia/doca-driver-rhel10']
MANUAL_JOB = 'pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-nno-signed'
AUTOMATIC_JOB = 'periodic-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-nno-signed'
BUCKET = 'https://storage.googleapis.com/test-platform-results-public/'
LIST_API = 'https://storage.googleapis.com/storage/v1/b/test-platform-results-public/o'
# Prow's 24h outer timeout + 1h termination grace + 1h upload allowance.
# Keep this above the complete lifetime of every configured signed job.
UNFINISHED_BUILD_MAX_AGE = timedelta(hours=26)


def created_time(value):
    # Python 3.9 needs 3 or 6 fractional digits; OCI/RFC3339 permits nanoseconds.
    value = re.sub(r'\.[0-9]+(?=Z$|[+-][0-9]{2}:[0-9]{2}$)',
                   lambda match: match[0][:7].ljust(7, '0'), value)
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def get_json(url, headers=None, optional=False):
    try:
        with urlopen(Request(url, headers=headers or {}), timeout=60) as response:
            return json.load(response)
    except HTTPError as error:
        if optional and error.code == 404:
            return None
        raise


def objects(prefix, pattern):
    token = ''
    while True:
        params = dict(prefix=prefix, matchGlob=pattern, maxResults=1000, pageToken=token)
        page = get_json(LIST_API + '?' + urlencode(params))
        yield from (item['name'] for item in page.get('items', []))
        token = page.get('nextPageToken', '')
        if not token:
            return


def read(path, optional=False):
    return get_json(BUCKET + quote(path, safe='/'), optional=optional)


def attempted(images, scan_started, jobs):
    """Published selection or worker evidence consumes an automatic attempt."""
    tried, unfinished = set(), False
    current = (os.environ.get('JOB_NAME', 'local'), os.environ.get('BUILD_ID', 'local'))
    for job in jobs:
        prefix = 'pr-logs/pull/rh-ecosystem-edge_nvidia-ci/' if job.startswith('pull-') else 'logs/' + job + '/'
        pattern = prefix + ('*/' + job + '/' if job.startswith('pull-') else '') + '*/started.json'
        for started_path in objects(prefix, pattern):
            build = started_path.rsplit('/', 1)[0]
            build_id = build.rsplit('/', 1)[-1]
            started = read(started_path)['timestamp']
            if type(started) not in (int, float) or started <= 0:
                raise ValueError(f'invalid Prow start timestamp: {started_path}')
            if started < (scan_started - timedelta(days=8)).timestamp() or (job, build_id) == current:
                continue
            finished = read(build + '/finished.json', optional=True)
            if finished is None and scan_started.timestamp() - started <= UNFINISHED_BUILD_MAX_AGE.total_seconds():
                unfinished = True
                continue
            if finished is not None and (
                    not isinstance(finished, dict) or not isinstance(finished.get('result'), str) or not finished['result']):
                raise ValueError(f'invalid Prow completion metadata: {build}')
            for path in objects(build + '/artifacts/', build + '/artifacts/**/nno-*.json'):
                name = path.rsplit('/', 1)[-1]
                if name not in ('nno-discovery-result.json', 'nno-signed-driver-images.json'):
                    continue
                record = read(path)
                if name == 'nno-discovery-result.json':
                    if record['job_name'] != job or str(record['build_id']) != build_id:
                        raise ValueError(f'selection belongs to another build: {path}')
                    if record['outcome'] == 'selected':
                        tried.add(identity(record['selected']))
                    elif record['outcome'] not in ('no_work', 'failed'):
                        raise ValueError(f'invalid discovery outcome: {path}')
                else:
                    source = metadata(record['pull_spec'])
                    workers = record['workers']
                    if len(workers) != 2:
                        raise ValueError(f'incomplete worker evidence: {path}')
                    observed = {digest(value.rsplit('@', 1)[-1]) for value in workers.values()}
                    for image in images:
                        if (image['repository'], image['architecture']) == (source['repository'], source['architecture']):
                            if observed <= runtime_digests(image):
                                tried.add(identity(image))
    return tried, unfinished


def recent_images(architecture, auth_file, scan_started):
    images = {}
    for repository in REPOSITORIES:
        tags = json.loads(command(['skopeo', 'list-tags', '--authfile', str(auth_file), 'docker://' + repository]))['Tags']
        for tag in sorted(tags):
            try:
                image = metadata(repository + ':' + tag)
            except ValueError:
                continue
            if image['architecture'] != architecture:
                continue
            image.update(image_info(image['image'], architecture, auth_file))
            if not isinstance(image['created_at'], str):
                raise ValueError(f'missing image creation timestamp: {image["image"]}')
            created = created_time(image['created_at'])
            if created.tzinfo is None or created > scan_started:
                raise ValueError(f'invalid/future image creation timestamp: {image["image"]}')
            if created < scan_started - timedelta(days=7):
                continue
            image['config_digest'] = config_digest(repository + '@' + image['manifest_digest'], auth_file)
            images.setdefault(identity(image), image)
    return sorted(images.values(), key=lambda image: (created_time(image['created_at']), image['image']))


def releases(args):
    headers = {'Authorization': 'Bearer ' + args.installer_token_file.read_text().strip()} if args.installer_token_file else {}
    supported = get_json(args.installer_versions_url, headers)
    exact = {identifier: data for identifier, data in supported.items() if version_re.fullmatch(identifier)}
    if not exact:
        raise ValueError('Assisted must expose exact installer versions; minor-only identifiers cannot select a payload')
    architecture = {'amd64': 'x86_64', 'arm64': 'aarch64'}.get(args.architecture, args.architecture)
    result = []
    for url in args.release_stream_url:
        for tag in get_json(url)['tags']:
            if tag['phase'] != 'Accepted':
                continue
            for identifier, data in exact.items():
                version = re.sub(r'-(?:x86_64|aarch64|amd64|arm64|ppc64le|s390x|multi)$', '', identifier)
                if tag['name'] == version and architecture in data['cpu_architectures']:
                    result.append(dict(version=version, installer_version=identifier, payload=tag['pullSpec']))
    return sorted(result, key=lambda release: (tuple(map(int, release['version'].split('-', 1)[0].split('.'))),
                                               '-' not in release['version'],
                                               re.sub(r'[0-9]+', lambda match: match[0].zfill(10), release['version'])), reverse=True)


def release_for(image, available, args, cache):
    for release in available:
        if '.'.join(release['version'].split('.')[:2]) != image['rhcos']:
            continue
        payload = release['payload']
        flags = ['--filter-by-os=linux/' + args.architecture, '--registry-config=' + str(args.release_auth_file)]
        if payload not in cache:
            toolkit = command(['oc', 'adm', 'release', 'info', payload, '--image-for=driver-toolkit', *flags]).strip()
            with tempfile.TemporaryDirectory(prefix='nno-dtk-') as directory:
                command(['oc', 'image', 'extract', toolkit, '--file=/etc/driver-toolkit-release.json', '--confirm', *flags], cwd=directory)
                cache[payload] = json.loads((Path(directory) / 'driver-toolkit-release.json').read_text())
        dtk = cache[payload]
        if dtk['KERNEL_VERSION'] == image['kernel'] and str(dtk['RHEL_VERSION']).split('.')[0] == image['rhel_major']:
            return release
    return None


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--architecture', choices=['amd64', 'arm64', 'ppc64le', 's390x'], default='amd64')
    parser.add_argument('--registry-auth-file', required=True, type=Path)
    parser.add_argument('--release-auth-file', required=True, type=Path)
    parser.add_argument('--installer-token-file', type=Path)
    parser.add_argument('--installer-versions-url',
                        default='https://api.openshift.com/api/assisted-install/v2/openshift-versions?only_latest=false')
    parser.add_argument('--release-stream-url', action='append')
    parser.add_argument('--signed-job', action='append', help='All automatic and manual signed job names (repeatable)')
    parser.add_argument('--output-dir', required=True, type=Path)
    parser.add_argument('--shared-dir', required=True, type=Path)
    args = parser.parse_args()
    args.release_stream_url = args.release_stream_url or [
        f'https://{args.architecture}.ocp.releases.ci.openshift.org/api/v1/releasestream/4-stable/tags']
    started = datetime.now(timezone.utc)
    result = dict(schema_version=1, job_name=os.environ.get('JOB_NAME', 'local'), build_id=os.environ.get('BUILD_ID', 'local'),
                  started_at=started.isoformat(), outcome='failed', selected=None)
    try:
        names = ['nno-discovery-result.json', 'nno-selected-request.json', 'nno-doca2-signed-request.json',
                 'dpf-openshift-version', 'ofed-pullspec']
        if any((directory / name).exists() for directory in (args.output_dir, args.shared_dir) for name in names):
            raise ValueError('discovery requires fresh output and signed handoff directories')
        images = recent_images(args.architecture, args.registry_auth_file, started)
        tried, unfinished = attempted(images, started, args.signed_job or [MANUAL_JOB, AUTOMATIC_JOB])
        result.update(outcome='no_work', reason='unfinished_signed_build' if unfinished else 'no_eligible_image')
        images = [image for image in images if identity(image) not in tried]
        available, cache = releases(args) if images and not unfinished else [], {}
        for image in ([] if unfinished else images):
            release = release_for(image, available, args, cache)
            if release is None:
                continue
            selected = dict(image, release=release)
            if image_info(image['image'], args.architecture, args.registry_auth_file)['manifest_digest'] != image['manifest_digest']:
                raise ValueError('selected tag changed during discovery')
            pair_id = 'discovery-' + image['manifest_digest'][7:] + '-' + release['installer_version']
            request = dict(schema_version=1, pairs=[dict(id=pair_id,
                           status='planned', openshift_version=release['installer_version'],
                           driver_requested={field: image[field] for field in ('image', 'kernel', 'architecture')})])
            request_path = args.output_dir / 'nno-selected-request.json'
            write_json(request_path, request)
            command([sys.executable, str(Path(__file__).with_name('validate-signed-request.py')), '--matrix', str(request_path),
                     '--shared-dir', str(args.shared_dir), '--artifact-dir', str(args.output_dir)])
            result.update(outcome='selected', reason='one_new_compatible_image', selected=selected)
            break
    except (OSError, ValueError, KeyError, TypeError, IndexError, subprocess.SubprocessError) as error:
        result.update(outcome='failed', selected=None, error=error_message(error))
        (args.output_dir / 'nno-selected-request.json').unlink(missing_ok=True)
        print('NNO discovery failed: ' + result['error'], file=sys.stderr)
    result['finished_at'] = datetime.now(timezone.utc).isoformat()
    for directory in (args.output_dir, args.shared_dir):
        write_json(directory / 'nno-discovery-result.json', result)
    print('NNO discovery outcome=' + result['outcome'])
    return int(result['outcome'] == 'failed')


if __name__ == '__main__':
    sys.exit(main())
