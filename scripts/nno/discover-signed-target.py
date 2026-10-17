#!/usr/bin/env python3
"""Select one recent, unattempted driver and an installable matching OCP release."""

import argparse
import base64
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

from signed_images import (command, config_digest, digest, error_message, identity, image_info, metadata,
                           release_info, rpm_info, runtime_digests, strip_installer_suffix, toolkit_info, version_re)

STAGE_REGISTRY = 'registry.stage.redhat.io'
CATALOG_REGISTRY = 'registry.access.redhat.com'  # Pyxis key for these staging repositories.
CATALOG_API = 'https://catalog.stage.redhat.com/api/containers/v1/images'
DRIVER_REPOSITORIES = ('nvidia/doca-driver-rhel9', 'nvidia/doca-driver-rhel10')
CATALOG_PAGE_SIZE = 500
CATALOG_MAX_PAGES = 10
MANUAL_JOB = 'pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-nno-signed'
AUTOMATIC_JOB = 'periodic-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-nno-signed-auto'
BUCKET = 'https://storage.googleapis.com/test-platform-results-public/'
LIST_API = 'https://storage.googleapis.com/storage/v1/b/test-platform-results-public/o'
# https://github.com/openshift/os/blob/master/docs/faq.md (OCP/RHEL compatibility).
# Unknown/new OCP minors remain candidates; this table only excludes known incompatible minors.
RHEL_STREAMS = {
    **dict.fromkeys(['4.11', '4.12'], ('8.6',)),
    **dict.fromkeys(['4.13', '4.14', '4.15'], ('9.2',)),
    **dict.fromkeys(['4.16', '4.17', '4.18'], ('9.4',)),
    **dict.fromkeys(['4.19', '4.20', '4.21'], ('9.6',)),
    '4.22': ('9.8', '10.2'),
}
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


def objects(prefix, pattern, metadata=False):
    token = ''
    while True:
        params = dict(prefix=prefix, matchGlob=pattern, maxResults=1000, pageToken=token,
                      fields='items(name,metadata,timeCreated),nextPageToken' if metadata else 'items(name),nextPageToken')
        page = get_json(LIST_API + '?' + urlencode(params))
        yield from (item if metadata else item['name'] for item in page.get('items', []))
        token = page.get('nextPageToken', '')
        if not token:
            return


def read(path, optional=False):
    return get_json(BUCKET + quote(path, safe='/'), optional=optional)


def builds(job, scan_started):
    if not job.startswith('pull-'):
        prefix = 'logs/' + job + '/'
        for path in objects(prefix, prefix + '*/started.json'):
            yield path.rsplit('/', 1)[0]
        return
    prefix = 'pr-logs/directory/' + job + '/'
    for item in objects(prefix, prefix + '*.txt', metadata=True):
        filename = item['name'].removeprefix(prefix)
        if not re.fullmatch(r'[0-9]+\.txt', filename):
            continue
        created = created_time(item['timeCreated'])
        if created.tzinfo is None:
            raise ValueError('invalid Prow index timestamp')
        # Object creation can be later than job start; read started.json for exact timing.
        if created < scan_started - timedelta(days=8):
            continue
        link = item['metadata']['x-goog-meta-link']
        expected = (r'gs://test-platform-results-public/(pr-logs/pull/rh-ecosystem-edge_nvidia-ci/[0-9]+/'
                    + re.escape(job) + '/' + re.escape(filename[:-4]) + ')')
        match = re.fullmatch(expected, link)
        if not match:
            raise ValueError('invalid Prow index build link')
        yield match[1]


def attempted(images, scan_started, jobs):
    """Published selection or worker evidence consumes an automatic attempt."""
    tried, unfinished = set(), False
    current = (os.environ.get('JOB_NAME', 'local'), os.environ.get('BUILD_ID', 'local'))
    for job in jobs:
        for build in builds(job, scan_started):
            started_path = build + '/started.json'
            build_id = build.rsplit('/', 1)[-1]
            started = read(started_path)['timestamp']
            if type(started) not in (int, float) or started <= 0:
                raise ValueError(f'invalid Prow start timestamp: {started_path}')
            if started < (scan_started - timedelta(days=8)).timestamp() or (job, build_id) == current:
                continue
            pattern = build + '/**/{finished.json,nno-discovery-result.json,nno-signed-driver-images.json}'
            paths = list(objects(build + '/', pattern))
            if (build + '/finished.json' not in paths
                    and scan_started.timestamp() - started <= UNFINISHED_BUILD_MAX_AGE.total_seconds()):
                unfinished = True
                continue
            worker_evidence_read = False
            for path in sorted(paths):
                if not path.startswith(build + '/artifacts/'):
                    continue
                name = path.rsplit('/', 1)[-1]
                if name not in ('nno-discovery-result.json', 'nno-signed-driver-images.json'):
                    continue
                if name == 'nno-signed-driver-images.json':
                    if worker_evidence_read:
                        continue
                    worker_evidence_read = True
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
    since = scan_started - timedelta(days=7)
    params = dict(
        filter=f'repositories.registry=={CATALOG_REGISTRY};'
               f'repositories.repository=in=({",".join(DRIVER_REPOSITORIES)});'
               f'architecture=={architecture};'
               f'repositories.push_date>={since.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")}',
        sort_by='repositories.push_date[desc]', page_size=CATALOG_PAGE_SIZE,
        include='total,data.image_id,data.architecture,data.repositories.registry,'
                'data.repositories.repository,data.repositories.push_date,data.repositories.tags.name,'
                'data.repositories.manifest_list_digest')
    records = []
    for page in range(CATALOG_MAX_PAGES):
        response = get_json(CATALOG_API + '?' + urlencode(dict(params, page=page)))
        if (not isinstance(response, dict) or type(response.get('total')) is not int
                or response['total'] < 0 or not isinstance(response.get('data'), list)):
            raise ValueError('invalid catalog response')
        records.extend(response['data'])
        if len(records) == response['total']:
            break
        if len(records) > response['total'] or not response['data']:
            raise ValueError('incomplete or inconsistent catalog response')
    else:
        raise ValueError('catalog exceeded maximum pages')

    candidates = {}
    for record in records:
        if record['architecture'] != architecture:
            continue
        for entry in record['repositories']:
            if entry['registry'] != CATALOG_REGISTRY or entry['repository'] not in DRIVER_REPOSITORIES:
                continue
            push_date = entry.get('push_date')
            if not isinstance(push_date, str):
                raise ValueError('missing catalog push timestamp')
            pushed = created_time(push_date)
            if pushed.tzinfo is None or pushed > scan_started:
                raise ValueError('invalid/future catalog push timestamp')
            if pushed < since:
                continue
            for tag in entry.get('tags') or []:
                pull_spec = STAGE_REGISTRY + '/' + entry['repository'] + ':' + tag['name']
                try:
                    image = metadata(pull_spec)
                except ValueError:
                    continue
                if image['architecture'] != architecture:
                    continue
                image.update(manifest_digest=digest(record['image_id']),
                             index_digest=digest(entry['manifest_list_digest']) if entry.get('manifest_list_digest') else None,
                             created_at=push_date)  # Catalog push date, rather than OCI image creation time.
                previous = candidates.get(pull_spec)
                if previous:
                    if pushed > created_time(previous['created_at']):
                        candidates[pull_spec] = image
                        dropped = previous
                    else:
                        dropped = image
                    if previous['manifest_digest'] != image['manifest_digest']:
                        print(f'NNO catalog: dropping stale tag {pull_spec}@{dropped["manifest_digest"]}')
                else:
                    candidates[pull_spec] = image

    images, configs = {}, {}
    for image in candidates.values():
        manifest = image['manifest_digest']
        if manifest not in configs:
            configs[manifest] = config_digest(image['repository'] + '@' + manifest, auth_file)
        image['config_digest'] = configs[manifest]
        # Retain OS aliases until release matching; attempts still share digest identity.
        images.setdefault((*identity(image), image['os_tag']), image)
    return sorted(images.values(), key=lambda image: (created_time(image['created_at']),
                                                     not image['os_tag'].startswith('rhel'), image['image']))


def release_stream_urls(architecture):
    stream = '4-stable' if architecture == 'amd64' else '4-stable-' + architecture
    return [f'https://{architecture}.ocp.releases.ci.openshift.org/api/v1/releasestream/{stream}/tags',
            'https://multi.ocp.releases.ci.openshift.org/api/v1/releasestream/4-stable-multi/tags']


def installer_versions(profile, url):
    """Read catalog JSON through the server's aicli account, without its token."""
    with tempfile.TemporaryDirectory(prefix='nno-discovery-') as directory:
        key = Path(directory) / 'id_rsa'
        encoded = b''.join((profile / 'private-key').read_bytes().split())
        key.write_bytes(base64.b64decode(encoded, validate=True).rstrip() + b'\n')
        key.chmod(0o600)
        host = (profile / 'remote-host').read_text().strip()
        result = subprocess.run(
            ['ssh', '-i', str(key), '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=30',
             '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null',
             '-o', 'LogLevel=ERROR', 'root@' + host, shlex.join(['python3', '-', url])],
            input=Path(__file__).with_name('assisted-versions.py').read_bytes(),
            capture_output=True, timeout=120, check=False)
        if result.returncode:
            raise ValueError('Unable to read the lab Assisted version catalog')
        try:
            versions = json.loads(result.stdout)
        except ValueError as error:
            raise ValueError('Lab Assisted version catalog is not valid JSON') from error
        if not isinstance(versions, dict):
            raise ValueError('Lab Assisted version catalog must be a JSON object')
        return versions


def release_order(release):
    version = release['version']
    installer = release['installer_version']
    numeric = tuple(map(int, version.split('-', 1)[0].split('.')))
    natural_version = re.sub(r'[0-9]+', lambda match: match[0].zfill(10), version)
    # Newest version first, stable before prerelease, native before multi;
    # prefer the unsuffixed Assisted identifier within the same payload.
    return numeric, '-' not in version, natural_version, not installer.endswith('-multi'), installer == version, installer


def releases(args):
    if args.installer_token_file:
        token = args.installer_token_file.read_text().strip()
        supported = get_json(args.installer_versions_url, {'Authorization': 'Bearer ' + token})
    elif getattr(args, 'cluster_profile_dir', None):
        supported = installer_versions(args.cluster_profile_dir, args.installer_versions_url)
    else:
        supported = get_json(args.installer_versions_url)
    exact = {identifier: data for identifier, data in supported.items() if version_re.fullmatch(identifier)}
    if not exact:
        raise ValueError('Assisted must expose exact installer versions; minor-only identifiers cannot select a payload')
    architecture = {'amd64': 'x86_64'}.get(args.architecture, args.architecture)
    payloads = {}
    for url in args.release_stream_url:
        stream = get_json(url)
        multi = stream['name'].endswith('-multi')
        for tag in stream['tags']:
            if tag['phase'] != 'Accepted':
                continue
            key = strip_installer_suffix(tag['name']), multi
            payload = tag['pullSpec']
            # Older stable tags may refer to a protected CI mirror, not the installer image.
            if payload.startswith('registry.ci.openshift.org/'):
                suffix = 'multi' if multi else {'amd64': 'x86_64', 'arm64': 'aarch64'}.get(args.architecture, args.architecture)
                payload = f'quay.io/openshift-release-dev/ocp-release:{key[0]}-{suffix}'
            if key in payloads and payloads[key] != payload:
                raise ValueError(f'conflicting accepted release payloads: {key[0]}')
            payloads[key] = payload
    result = []
    for identifier, data in exact.items():
        version = strip_installer_suffix(identifier)
        payload = payloads.get((version, identifier.endswith('-multi')))
        if payload and architecture in data['cpu_architectures']:
            result.append(dict(version=version, installer_version=identifier, payload=payload))
    return sorted(result, key=release_order, reverse=True)


def release_for(image, available, args, cache):
    rhcos = re.fullmatch(r'rhcos([0-9]+\.[0-9]+)', image['os_tag'])
    rhel = re.fullmatch(r'rhel([0-9]+\.[0-9]+)', image['os_tag'])
    if image['architecture'] != args.architecture or not (rhcos or rhel):
        return None
    kernel_os = re.search(r'\.el([0-9]+)_([0-9]+)\.', image['kernel'])
    target_rhel = rhel[1] if rhel else kernel_os[1] + '.' + kernel_os[2]
    if target_rhel.split('.')[0] != image['rhel_major']:
        return None
    candidates = [release for release in available
                  if (not rhcos or '.'.join(release['version'].split('.')[:2]) == rhcos[1])
                  and target_rhel in RHEL_STREAMS.get('.'.join(release['version'].split('.')[:2]), (target_rhel,))]
    print(f'NNO release lookup: kernel={image["kernel"]} RHEL={target_rhel} candidates={len(candidates)}')
    for release in candidates:
        payload = release['payload']
        if payload not in cache:
            cache[payload] = release_info(payload, args.architecture, args.release_auth_file)
        info = cache[payload]
        if info['version'] != release['version']:
            raise ValueError('release payload version disagrees with its accepted tag')
        if tuple(map(int, release['version'].split('.')[:2])) >= (4, 19):
            component = 'rhel-coreos-' + image['rhel_major']
            if component not in info['os_images']:
                component = 'rhel-coreos'
            os_image = info['os_images'][component]
            key = ('rpm', digest(os_image.rsplit('@', 1)[-1]), args.architecture)
            if key not in cache:
                cache[key] = rpm_info(info['payload'], component, args.architecture, args.release_auth_file)
            rpm = cache[key]
            if rpm['rhel_version'] != target_rhel or image['kernel'] not in rpm['kernels']:
                continue
        # Keep the existing default-worker compatibility rule and pre-4.19 fallback.
        key = (info['toolkit'], args.architecture)
        if key not in cache:
            cache[key] = toolkit_info(*key, args.release_auth_file)
        dtk = cache[key]
        rhel_version = str(dtk['RHEL_VERSION'])
        if (not rhel or rhel_version == rhel[1]) and dtk['KERNEL_VERSION'] == image['kernel'] and (
                rhel_version.split('.')[0] == image['rhel_major']):
            return dict(release, **info)
    return None


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--architecture', choices=['amd64', 'arm64', 'ppc64le', 's390x'],
                        default=os.environ.get('NNO_DISCOVERY_ARCHITECTURE', 'amd64'))
    parser.add_argument('--registry-auth-file', type=Path, default=os.environ.get(
        'REGISTRY_AUTH_FILE', '/var/run/secrets/ci-pull-credentials/.dockerconfigjson'))
    parser.add_argument('--release-auth-file', type=Path, help='Defaults to the existing CI registry auth file')
    parser.add_argument('--installer-token-file', type=Path)
    parser.add_argument('--cluster-profile-dir', type=Path, default=os.environ.get('CLUSTER_PROFILE_DIR'))
    parser.add_argument('--installer-versions-url',
                        default='https://api.openshift.com/api/assisted-install/v2/openshift-versions?only_latest=false')
    parser.add_argument('--release-stream-url', action='append')
    parser.add_argument('--signed-job', action='append', help='All automatic and manual signed job names (repeatable)')
    parser.add_argument('--output-dir', type=Path, default=os.environ.get('ARTIFACT_DIR'))
    parser.add_argument('--shared-dir', type=Path, default=os.environ.get('SHARED_DIR'))
    args = parser.parse_args()
    if not args.output_dir or not args.shared_dir:
        parser.error('Artifact and shared directories are required')
    args.release_auth_file = args.release_auth_file or args.registry_auth_file
    args.signed_job = args.signed_job or os.environ.get('NNO_SIGNED_HISTORY_JOBS', '').split() or [MANUAL_JOB, AUTOMATIC_JOB]
    args.release_stream_url = args.release_stream_url or release_stream_urls(args.architecture)
    started = datetime.now(timezone.utc)
    result = dict(schema_version=1, job_name=os.environ.get('JOB_NAME', 'local'), build_id=os.environ.get('BUILD_ID', 'local'),
                  started_at=started.isoformat(), outcome='failed', selected=None)
    try:
        names = ['nno-discovery-result.json', 'nno-selected-request.json', 'nno-doca2-signed-request.json',
                 'dpf-openshift-version', 'ofed-pullspec', 'skip-work-steps']
        if any((directory / name).exists() for directory in (args.output_dir, args.shared_dir) for name in names):
            raise ValueError('discovery requires fresh output and signed handoff directories')
        images = recent_images(args.architecture, args.registry_auth_file, started)
        tried, unfinished = attempted(images, started, args.signed_job)
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
    if result['outcome'] == 'no_work':
        (args.shared_dir / 'skip-work-steps').touch()
    print('NNO discovery outcome=' + result['outcome'])
    return int(result['outcome'] == 'failed')


if __name__ == '__main__':
    sys.exit(main())
