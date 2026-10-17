"""Exercise selection boundaries; external registry, Prow and release services are mocked."""

from datetime import datetime, timedelta, timezone
import json
import io
import os
from pathlib import Path
import runpy
import tempfile
import base64
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlparse

import signed_images
from test_validate_signed_request import IMAGE, KERNEL, REQUEST
from test_verify_signed_step_result import signed_result

d = SimpleNamespace(**runpy.run_path(str(Path(__file__).with_name('discover-signed-target.py'))))
GLOBALS = d.main.__globals__
NOW = datetime(2026, 10, 9, tzinfo=timezone.utc)
A, B, C = ['sha256:' + character * 64 for character in 'abc']
IMAGE_RECORD = dict(signed_images.metadata(IMAGE), manifest_digest=A, config_digest=B, index_digest=C, created_at=NOW.isoformat())
RELEASE = dict(version='4.22.15', installer_version='4.22.15', payload='quay.io/release@' + A)


class DiscoveryTests(unittest.TestCase):
    def test_remote_catalog_preserves_metadata_and_removes_private_key(self):
        with tempfile.TemporaryDirectory() as directory:
            profile = Path(directory)
            (profile / 'private-key').write_bytes(base64.b64encode(b'existing-key'))
            (profile / 'remote-host').write_text('lab.example')
            url = 'https://api.openshift.com/api/assisted-install/v2/openshift-versions?only_latest=false&version=4.22'
            catalog = {'4.22.15': {'cpu_architectures': ['x86_64', 'arm64']}}
            keys = []
            def ssh(arguments, **kwargs):
                key = Path(arguments[arguments.index('-i') + 1])
                keys.append(key)
                self.assertEqual(key.read_bytes(), b'existing-key\n')
                self.assertEqual(key.stat().st_mode & 0o777, 0o600)
                self.assertEqual(arguments[-2], 'root@lab.example')
                self.assertEqual(d.shlex.split(arguments[-1]), ['python3', '-', url])
                self.assertEqual(kwargs['input'], Path(__file__).with_name('assisted-versions.py').read_bytes())
                self.assertTrue(kwargs['capture_output'])
                return SimpleNamespace(returncode=0, stdout=json.dumps(catalog).encode())
            with patch.object(d.subprocess, 'run', side_effect=ssh):
                self.assertEqual(d.installer_versions(profile, url), catalog)
            self.assertFalse(keys[0].exists())
            for returncode, data in [(1, b'private-token'), (0, b'private-token'), (0, b'[]')]:
                with patch.object(d.subprocess, 'run', return_value=SimpleNamespace(returncode=returncode, stdout=data)):
                    with self.assertRaises(ValueError) as failure:
                        d.installer_versions(profile, url)
                    self.assertNotIn('private-token', str(failure.exception))

    def test_remote_helper_queries_full_catalog_without_outputting_token(self):
        token = 'private-token'
        url = 'https://api.openshift.com/api/assisted-install/v2/openshift-versions?only_latest=false'
        catalog = {'4.22.15': {'cpu_architectures': ['x86_64', 'arm64'], 'display_name': 'OpenShift 4.22.15'}}
        def client(**kwargs):
            self.assertTrue(kwargs['quiet'])
            print('client initialization: ' + token)
            return SimpleNamespace(token=token)
        def request_version(request, timeout):
            self.assertEqual(request.full_url, url)
            self.assertEqual(request.get_header('Authorization'), 'Bearer ' + token)
            self.assertEqual(timeout, 60)
            return io.StringIO(json.dumps(catalog))
        output = io.StringIO()
        helper = Path(__file__).with_name('assisted-versions.py')
        with patch.dict('sys.modules', ailib=SimpleNamespace(AssistedClient=client)), patch(
                'sys.argv', ['assisted-versions', url]), patch('sys.stdout', output), patch(
                'urllib.request.urlopen', side_effect=request_version):
            runpy.run_path(str(helper), run_name='__main__')
        self.assertEqual(json.loads(output.getvalue()), catalog)
        self.assertNotIn(token, output.getvalue())

    def test_catalog_uses_remote_json_or_explicit_local_token(self):
        supported = {'4.22.15': {'cpu_architectures': ['x86_64']}}
        stream = {'name': '4-stable', 'tags': [dict(name='4.22.15', phase='Accepted', pullSpec='payload')]}
        args = SimpleNamespace(installer_token_file=None, cluster_profile_dir=Path('profile'),
                               installer_versions_url='assisted?only_latest=false', architecture='amd64',
                               release_stream_url=['controller'])
        remote = unittest.mock.Mock(return_value=supported)
        def get_json(url, *args):
            self.assertEqual(url, 'controller')
            return stream
        with patch.dict(GLOBALS, installer_versions=remote, get_json=get_json):
            self.assertEqual(d.releases(args)[0]['installer_version'], '4.22.15')
        remote.assert_called_once_with(args.cluster_profile_dir, args.installer_versions_url)
        with tempfile.TemporaryDirectory() as directory:
            args.installer_token_file = Path(directory) / 'token'
            args.installer_token_file.write_text('local-token')
            get_json = unittest.mock.Mock(side_effect=[supported, stream])
            remote.reset_mock()
            with patch.dict(GLOBALS, installer_versions=remote, get_json=get_json):
                self.assertEqual(d.releases(args)[0]['installer_version'], '4.22.15')
            get_json.assert_any_call(args.installer_versions_url, {'Authorization': 'Bearer local-token'})
            remote.assert_not_called()

    def test_ci_environment_supplies_auth_history_and_directories(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            environment = dict(ARTIFACT_DIR=str(root / 'out'), SHARED_DIR=str(root / 'shared'),
                               REGISTRY_AUTH_FILE=str(root / 'ci-auth'), NNO_SIGNED_HISTORY_JOBS='manual automatic')
            def images(architecture, auth, _started):
                self.assertEqual(architecture, 'amd64')
                self.assertEqual(auth, root / 'ci-auth')
                return []
            def history(_images, _started, jobs):
                self.assertEqual(jobs, ['manual', 'automatic'])
                return set(), False
            with patch.dict(os.environ, environment), patch('sys.argv', ['discover']), patch.dict(
                    GLOBALS, recent_images=images, attempted=history, installer_versions=unittest.mock.Mock()) as patched:
                self.assertEqual(d.main(), 0)
                patched['installer_versions'].assert_not_called()
            self.assertEqual(json.loads((root / 'shared/nno-discovery-result.json').read_text())['outcome'], 'no_work')
            self.assertTrue((root / 'shared/skip-work-steps').is_file())

    def test_remote_catalog_failure_fails_discovery_without_handoffs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            argv = ['discover', '--cluster-profile-dir', str(root / 'profile'),
                    '--output-dir', str(root / 'out'), '--shared-dir', str(root / 'shared')]
            with patch('sys.argv', argv), patch.dict(GLOBALS, recent_images=lambda *args: [IMAGE_RECORD],
                    attempted=lambda *args: (set(), False),
                    installer_versions=unittest.mock.Mock(side_effect=ValueError('catalog unavailable'))):
                self.assertEqual(d.main(), 1)
            result = json.loads((root / 'shared/nno-discovery-result.json').read_text())
            self.assertEqual(result['outcome'], 'failed')
            self.assertFalse((root / 'shared/dpf-openshift-version').exists())
            self.assertFalse((root / 'shared/skip-work-steps').exists())

    @staticmethod
    def catalog_record(manifest=A, age=0, tags=None, **entry_fields):
        # Minimal /images response fields described by the verified staging query.
        entry = dict(registry=d.CATALOG_REGISTRY, repository='nvidia/doca-driver-rhel9',
                     push_date=(NOW - timedelta(days=age)).isoformat(),
                     tags=[dict(name=tag) for tag in (tags if tags is not None else [IMAGE.rsplit(':', 1)[1]])],
                     manifest_list_digest=C)
        entry.update(entry_fields)
        return dict(image_id=manifest, architecture='amd64', repositories=[entry])

    def catalog_images(self, records, **patches):
        get_json = unittest.mock.Mock(return_value=dict(total=len(records), data=records))
        configs = unittest.mock.Mock(return_value=B)
        mocks = dict(get_json=get_json, config_digest=configs,
                     command=unittest.mock.Mock(side_effect=AssertionError('unexpected CLI call')),
                     image_info=unittest.mock.Mock(side_effect=AssertionError('unexpected image inspection')))
        mocks.update(patches)
        with patch.dict(GLOBALS, mocks):
            images = d.recent_images('amd64', 'auth', NOW)
        return images, mocks['get_json'], mocks['config_digest']

    def test_catalog_one_request_and_query(self):
        images, get_json, configs = self.catalog_images([self.catalog_record()])
        self.assertEqual(images, [IMAGE_RECORD])
        get_json.assert_called_once()
        url = urlparse(get_json.call_args[0][0])
        self.assertEqual((url.scheme, url.netloc, url.path),
                         ('https', 'catalog.stage.redhat.com', '/api/containers/v1/images'))
        params = parse_qs(url.query)
        self.assertEqual(params['filter'], [
            'repositories.registry==registry.access.redhat.com;'
            'repositories.repository=in=(nvidia/doca-driver-rhel9,nvidia/doca-driver-rhel10);'
            'architecture==amd64;repositories.push_date>=2026-10-02T00:00:00Z'])
        self.assertEqual(params['sort_by'], ['repositories.push_date[desc]'])
        self.assertEqual(params['page_size'], ['500'])
        self.assertEqual(params['page'], ['0'])
        self.assertEqual(set(params['include'][0].split(',')), {
            'total', 'data.image_id', 'data.architecture', 'data.repositories.registry',
            'data.repositories.repository', 'data.repositories.push_date', 'data.repositories.tags.name',
            'data.repositories.manifest_list_digest'})
        configs.assert_called_once_with(IMAGE_RECORD['repository'] + '@' + A, 'auth')

    def test_catalog_aliases_share_config_lookup_and_identity(self):
        tag = IMAGE.rsplit(':', 1)[1]
        record = self.catalog_record(tags=['1792015679', '26.04-1.1.0.0-1792015679',
                                          tag.replace('rhel9.8', 'rhcos4.22'), tag, '1792015679-source'])
        images, _get_json, configs = self.catalog_images([record, record])
        self.assertEqual([image['os_tag'] for image in images], ['rhel9.8', 'rhcos4.22'])
        self.assertEqual({signed_images.identity(image) for image in images}, {signed_images.identity(IMAGE_RECORD)})
        self.assertTrue(all(image['manifest_digest'] == A and image['index_digest'] == C for image in images))
        configs.assert_called_once_with(IMAGE_RECORD['repository'] + '@' + A, 'auth')
        record['repositories'][0].pop('manifest_list_digest')
        images, _, _ = self.catalog_images([record])
        self.assertTrue(all(image['index_digest'] is None for image in images))

    def test_catalog_stale_tags_discarded_before_registry_lookup(self):
        old, new = self.catalog_record(age=1), self.catalog_record(manifest=C)
        def config(image, auth):
            self.assertEqual(image, IMAGE_RECORD['repository'] + '@' + C)
            self.assertEqual(auth, 'auth')
            return B
        for records in ([old, new], [new, old]):
            with self.subTest(records=records), patch('sys.stdout', new_callable=io.StringIO) as output:
                images, _, configs = self.catalog_images(records, config_digest=unittest.mock.Mock(side_effect=config))
                self.assertEqual(len(images), 1)
                self.assertEqual(images[0]['manifest_digest'], C)
                configs.assert_called_once()
                self.assertIn('dropping stale tag', output.getvalue())
                self.assertIn(A, output.getvalue())

    def test_catalog_different_tags_keep_distinct_rebuilds_oldest_first(self):
        tag = IMAGE.rsplit(':', 1)[1]
        new = self.catalog_record(manifest=C, tags=['rebuilt-' + tag])
        old = self.catalog_record(age=1)
        images, _, configs = self.catalog_images([new, old])
        self.assertEqual([image['manifest_digest'] for image in images], [A, C])
        self.assertEqual(configs.call_count, 2)

    def test_catalog_push_date_window_and_timestamps(self):
        for age, count in [(0, 1), (7, 1), (7.001, 0)]:
            with self.subTest(age=age):
                images, _, configs = self.catalog_images([self.catalog_record(age=age)])
                self.assertEqual(len(images), count)
                self.assertEqual(configs.call_count, count)
        for date in ['bad', '2026-10-09T00:00:00', (NOW + timedelta(seconds=1)).isoformat(), None]:
            with self.subTest(date=date), self.assertRaises(ValueError):
                self.catalog_images([self.catalog_record(push_date=date)])
        record = self.catalog_record()
        record['repositories'][0].pop('push_date')
        with self.assertRaises(ValueError):
            self.catalog_images([record])
        for date in ['2026-10-08T12:34:56.123456789Z', '2026-10-08T12:34:56.123456789+00:00']:
            images, _, _ = self.catalog_images([self.catalog_record(push_date=date)])
            self.assertEqual(images[0]['created_at'], date)

    def test_catalog_foreign_entries_and_architectures_ignored(self):
        for fields in [dict(registry='registry.redhat.io'), dict(repository='nvidia/other')]:
            with self.subTest(fields=fields):
                record = self.catalog_record(push_date=None, **fields)
                self.assertEqual(self.catalog_images([record])[0], [])
        record = self.catalog_record()
        foreign = self.catalog_record(registry='registry.redhat.io')['repositories'][0]
        record['repositories'].append(foreign)
        self.assertEqual(len(self.catalog_images([record])[0]), 1)
        record['architecture'] = 'arm64'
        self.assertEqual(self.catalog_images([record])[0], [])
        record = self.catalog_record(tags=[IMAGE.rsplit(':', 1)[1].replace('-amd64', '-arm64')])
        self.assertEqual(self.catalog_images([record])[0], [])

    def test_catalog_empty_result(self):
        images, get_json, configs = self.catalog_images([])
        self.assertEqual(images, [])
        get_json.assert_called_once()
        configs.assert_not_called()

    def test_catalog_pagination_and_limit(self):
        record = self.catalog_record()
        pages = unittest.mock.Mock(side_effect=[dict(total=3, data=[record, record]), dict(total=3, data=[record])])
        self.assertEqual(len(self.catalog_images([], get_json=pages)[0]), 1)
        self.assertEqual([parse_qs(urlparse(call[0][0]).query)['page'] for call in pages.call_args_list], [['0'], ['1']])
        pages = unittest.mock.Mock(return_value=dict(total=3, data=[record]))
        with patch.dict(GLOBALS, CATALOG_MAX_PAGES=2), self.assertRaisesRegex(ValueError, 'maximum pages'):
            self.catalog_images([], get_json=pages)
        self.assertEqual(pages.call_count, 2)

    def test_catalog_errors_fail_discovery_without_handoffs(self):
        bad_digest = self.catalog_record(manifest='bad')
        incomplete = [dict(total=2, data=[self.catalog_record()]), dict(total=2, data=[])]
        failures = [HTTPError(d.CATALOG_API, 403, 'Forbidden', {}, io.BytesIO()), URLError('catalog unavailable'),
                    {}, dict(data=[]), dict(total=0), dict(total='0', data=[]),
                    dict(total=True, data=[]), dict(total=-1, data=[]), dict(total=0, data={}),
                    dict(total=1, data=[bad_digest]), incomplete]
        for failure in failures:
            with self.subTest(failure=failure):
                def failing_catalog():
                    if isinstance(failure, Exception):
                        return unittest.mock.Mock(side_effect=failure)
                    if isinstance(failure, list):
                        return unittest.mock.Mock(side_effect=failure)
                    return unittest.mock.Mock(return_value=failure)
                configs = unittest.mock.Mock(side_effect=AssertionError('registry called before catalog validated'))
                with self.assertRaises((OSError, ValueError)):
                    self.catalog_images([], get_json=failing_catalog(), config_digest=configs)
                with tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    argv = ['discover', '--output-dir', str(root / 'out'), '--shared-dir', str(root / 'shared')]
                    clock = unittest.mock.Mock(wraps=datetime)
                    clock.now.return_value = NOW
                    with patch('sys.argv', argv), patch.dict(
                            GLOBALS, get_json=failing_catalog(), config_digest=configs, datetime=clock):
                        self.assertEqual(d.main(), 1)
                    result = json.loads((root / 'shared/nno-discovery-result.json').read_text())
                    self.assertEqual(result['outcome'], 'failed')
                    for name in ['skip-work-steps', 'dpf-openshift-version', 'ofed-pullspec',
                                 'nno-doca2-signed-request.json', 'nno-selected-request.json']:
                        for folder in ['shared', 'out']:
                            self.assertFalse((root / folder / name).exists())
                configs.assert_not_called()
        failures[0].close()

    def test_oci_fractional_seconds_on_python39(self):
        for fraction, micros in [('1', 100000), ('123', 123000), ('1234', 123400),
                                 ('123456', 123456), ('123456789', 123456)]:
            for offset in ['Z', '+00:00', '+03:00']:
                parsed = d.created_time('2026-10-08T12:34:56.' + fraction + offset)
                self.assertEqual(parsed.microsecond, micros)
                self.assertIsNotNone(parsed.tzinfo)

    def history(self, record=None, finished=True, age=0, job=d.MANUAL_JOB, build_id='1'):
        build = ('pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/' if job.startswith('pull-') else 'logs/') + job + '/' + build_id
        filename = 'nno-discovery-result.json' if record and 'outcome' in record else 'nno-signed-driver-images.json'
        artifact = build + '/artifacts/test/' + filename
        def objects(prefix, pattern):
            return ([build + '/finished.json'] if finished else []) + ([artifact] if record else [])
        def read(path, optional=False):
            if path.endswith('started.json'):
                return {'timestamp': (NOW - timedelta(days=age)).timestamp()}
            if path.endswith('finished.json'):
                raise AssertionError('finished.json must only be checked in the listing')
            return record
        with patch.dict(GLOBALS, builds=lambda *args: [build], objects=objects, read=read):
            return d.attempted([IMAGE_RECORD], NOW, [job])

    @staticmethod
    def index_item(build_id='1', age=0):
        return dict(name='pr-logs/directory/' + d.MANUAL_JOB + '/' + build_id + '.txt',
                    timeCreated=(NOW - timedelta(days=age)).isoformat(),
                    metadata={'x-goog-meta-link': 'gs://test-platform-results-public/pr-logs/pull/'
                              'rh-ecosystem-edge_nvidia-ci/747/' + d.MANUAL_JOB + '/' + build_id})

    def test_presubmit_build_index_uses_listing_metadata_without_pointer_reads(self):
        item = self.index_item()
        get_json = unittest.mock.Mock(return_value=dict(items=[
            item, self.index_item(build_id='latest-build'), self.index_item(build_id='2', age=8.001)]))
        with patch.dict(GLOBALS, get_json=get_json):
            paths = list(d.builds(d.MANUAL_JOB, NOW))
        self.assertEqual(paths, [item['metadata']['x-goog-meta-link'].removeprefix('gs://test-platform-results-public/')])
        get_json.assert_called_once()
        params = parse_qs(urlparse(get_json.call_args[0][0]).query)
        prefix = 'pr-logs/directory/' + d.MANUAL_JOB + '/'
        self.assertEqual(params['prefix'], [prefix])
        self.assertEqual(params['matchGlob'], [prefix + '*.txt'])
        self.assertEqual(params['fields'], ['items(name,metadata,timeCreated),nextPageToken'])

    def test_build_index_pagination_and_periodic_path(self):
        get_json = unittest.mock.Mock(side_effect=[dict(items=[self.index_item()], nextPageToken='next'),
                                                   dict(items=[self.index_item(build_id='2')])])
        with patch.dict(GLOBALS, get_json=get_json):
            self.assertEqual(len(list(d.builds(d.MANUAL_JOB, NOW))), 2)
        self.assertEqual(parse_qs(urlparse(get_json.call_args_list[1][0][0]).query)['pageToken'], ['next'])
        prefix = 'logs/' + d.AUTOMATIC_JOB + '/'
        objects = unittest.mock.Mock(return_value=[prefix + '1/started.json'])
        with patch.dict(GLOBALS, objects=objects):
            self.assertEqual(list(d.builds(d.AUTOMATIC_JOB, NOW)), [prefix + '1'])
        objects.assert_called_once_with(prefix, prefix + '*/started.json')

    def test_build_index_errors_are_not_silently_ignored(self):
        for link in ['gs://other-bucket/build', 'https://example.com/build',
                     self.index_item()['metadata']['x-goog-meta-link'] + '2']:
            item = self.index_item()
            item['metadata']['x-goog-meta-link'] = link
            with self.subTest(link=link), patch.dict(GLOBALS, get_json=lambda *args: dict(items=[item])):
                with self.assertRaises(ValueError):
                    list(d.builds(d.MANUAL_JOB, NOW))
        for field in ['metadata', 'timeCreated']:
            item = self.index_item()
            item.pop(field)
            with self.subTest(field=field), patch.dict(GLOBALS, get_json=lambda *args: dict(items=[item])):
                with self.assertRaises(KeyError):
                    list(d.builds(d.MANUAL_JOB, NOW))

    def test_index_creation_time_does_not_replace_actual_start_time(self):
        item = self.index_item()  # Pointer refreshed recently for a build that started earlier.
        def get_json(url):
            prefix = parse_qs(urlparse(url).query)['prefix'][0]
            return dict(items=[item] if prefix.startswith('pr-logs/directory/') else [])
        for hours, expected in [(25, True), (27, False), (8 * 24 + 1, False)]:
            def read(path, **kwargs):
                return {'timestamp': (NOW - timedelta(hours=hours)).timestamp()} if path.endswith('started.json') else None
            with self.subTest(hours=hours), patch.dict(GLOBALS, get_json=get_json, read=read):
                self.assertEqual(d.attempted([IMAGE_RECORD], NOW, [d.MANUAL_JOB]), (set(), expected))

    def test_attempts_from_selection_and_all_runtime_digest_forms(self):
        selection = dict(outcome='selected', selected=IMAGE_RECORD, job_name=d.MANUAL_JOB, build_id='1')
        self.assertEqual(self.history(selection)[0], {signed_images.identity(IMAGE_RECORD)})
        self.assertEqual(self.history(selection, finished='ERROR')[0], {signed_images.identity(IMAGE_RECORD)})
        for value in [A, B, C, IMAGE.rsplit(':', 1)[0] + '@' + B]:
            evidence = dict(pull_spec=IMAGE, workers=dict(client=value, server=value))
            self.assertEqual(self.history(evidence)[0], {signed_images.identity(IMAGE_RECORD)})
            evidence['pull_spec'] = IMAGE.replace('rhel9', 'rhel10') if 'rhel9' in IMAGE else IMAGE.replace('rhel10', 'rhel9')
            self.assertEqual(self.history(evidence)[0], set())
        self.assertEqual(self.history(selection, age=8.001)[0], set())
        self.assertEqual(self.history(selection, age=7.5)[0], {signed_images.identity(IMAGE_RECORD)})

    def test_unfinished_other_run_defers_but_current_run_does_not(self):
        self.assertTrue(self.history(finished=False)[1])
        with patch.dict(os.environ, JOB_NAME=d.AUTOMATIC_JOB, BUILD_ID='1'):
            self.assertFalse(self.history(finished=False, job=d.AUTOMATIC_JOB)[1])
            self.assertTrue(self.history(finished=False, job=d.AUTOMATIC_JOB, build_id='2')[1])

    def test_expired_builds_keep_attempts_and_mid_scan_starts_defer(self):
        selection = dict(outcome='selected', selected=IMAGE_RECORD, job_name=d.MANUAL_JOB, build_id='1')
        for hours, blocks in [(8, True), (25, True), (26, True), (26.001, False), (48, False), (-0.1, True)]:
            tried, unfinished = self.history(selection, finished=False, age=hours / 24)
            self.assertEqual(unfinished, blocks)
            self.assertEqual(tried, set() if blocks else {signed_images.identity(IMAGE_RECORD)})
        self.assertEqual(self.history(finished=False, age=2), (set(), False))

    def test_history_combines_completion_and_evidence_and_reads_workers_once(self):
        build = 'pr-logs/pull/rh-ecosystem-edge_nvidia-ci/747/' + d.MANUAL_JOB + '/1'
        install = build + '/artifacts/nno-signed/nno-install-signed-driver/artifacts/nno-signed-driver-images.json'
        gpudirect = install.replace('nno-install-signed-driver', 'nno-test-gpudirect')
        evidence = dict(pull_spec=IMAGE, workers=dict(client=B, server=B))
        objects = unittest.mock.Mock(return_value=[gpudirect, build + '/finished.json', install])
        def read(path, **kwargs):
            if path == build + '/started.json':
                return dict(timestamp=NOW.timestamp())
            self.assertEqual(path, install)
            return evidence
        reads = unittest.mock.Mock(side_effect=read)
        with patch.dict(GLOBALS, builds=lambda *args: [build], objects=objects, read=reads):
            self.assertEqual(d.attempted([IMAGE_RECORD], NOW, [d.MANUAL_JOB]),
                             ({signed_images.identity(IMAGE_RECORD)}, False))
        objects.assert_called_once_with(
            build + '/', build + '/**/{finished.json,nno-discovery-result.json,nno-signed-driver-images.json}')
        self.assertEqual([call[0][0] for call in reads.call_args_list], [build + '/started.json', install])
        # A run with only GPUDirect evidence is also a recorded attempt.
        objects.return_value = [gpudirect, build + '/finished.json']
        with patch.dict(GLOBALS, builds=lambda *args: [build], objects=objects,
                        read=lambda path: dict(timestamp=NOW.timestamp()) if path.endswith('started.json') else evidence):
            self.assertEqual(d.attempted([IMAGE_RECORD], NOW, [d.MANUAL_JOB])[0], {signed_images.identity(IMAGE_RECORD)})

    def test_history_waits_for_all_listing_pages_before_checking_completion(self):
        build = 'pr-logs/pull/rh-ecosystem-edge_nvidia-ci/747/' + d.MANUAL_JOB + '/1'
        evidence_path = build + '/artifacts/test/nno-signed-driver-images.json'
        get_json = unittest.mock.Mock(side_effect=[dict(items=[dict(name=evidence_path)], nextPageToken='next'),
                                                   dict(items=[dict(name=build + '/finished.json')])])
        def read(path):
            if path.endswith('started.json'):
                return dict(timestamp=NOW.timestamp())
            self.assertEqual(path, evidence_path)
            return dict(pull_spec=IMAGE, workers=dict(client=B, server=B))
        with patch.dict(GLOBALS, builds=lambda *args: [build], get_json=get_json, read=read):
            self.assertEqual(d.attempted([IMAGE_RECORD], NOW, [d.MANUAL_JOB]),
                             ({signed_images.identity(IMAGE_RECORD)}, False))
        self.assertEqual(get_json.call_count, 2)

    def test_history_reads_are_complete_and_errors_fail(self):
        pages = [{'items': [{'name': 'first'}], 'nextPageToken': 'next'}, {'items': [{'name': 'last'}]}]
        with patch.dict(GLOBALS, get_json=unittest.mock.Mock(side_effect=pages)):
            self.assertEqual(list(d.objects('prefix/', '**')), ['first', 'last'])
        for code in [404, 403, 500]:
            error = HTTPError('https://history', code, 'failed', {}, io.BytesIO())
            self.addCleanup(error.close)
            with patch.dict(d.get_json.__globals__, urlopen=unittest.mock.Mock(side_effect=error)):
                if code == 404:
                    self.assertIsNone(d.get_json('https://history', optional=True))
                else:
                    with self.assertRaises(HTTPError):
                        d.get_json('https://history', optional=True)
        with patch.dict(GLOBALS, builds=lambda *args: ['build'], read=lambda *args, **kwargs: {'timestamp': 'bad'}):
            with self.assertRaises(ValueError):
                d.attempted([IMAGE_RECORD], NOW, [d.MANUAL_JOB])

    def test_installable_accepted_release_and_exact_default_dtk(self):
        args = SimpleNamespace(installer_token_file=None, installer_versions_url='assisted', architecture='amd64',
                               release_stream_url=['controller'], release_auth_file='auth')
        supported = {'4.22.0-x86_64': {'cpu_architectures': ['x86_64']}}
        tags = [dict(name='4.22.0', phase='Accepted', pullSpec='payload'), dict(name='4.22.1', phase='Accepted', pullSpec='other')]
        with patch.dict(GLOBALS, get_json=lambda url, *args: supported if url == 'assisted' else {'name': '4-stable', 'tags': tags}):
            available = d.releases(args)
        self.assertEqual([r['installer_version'] for r in available], ['4.22.0-x86_64'])
        def release_info(payload, *args):
            return dict(version='4.21.0' if payload == 'other-minor' else '4.22.0', payload=payload,
                        toolkit='toolkit@' + A, manifest_digest=A, index_digest=C,
                        os_images={'rhel-coreos': 'quay.io/os@' + B})
        with patch.dict(GLOBALS, release_info=release_info,
                        rpm_info=lambda *args: dict(kernels={KERNEL}, rhel_version='9.8'),
                        toolkit_info=lambda *args: dict(KERNEL_VERSION=KERNEL, RHEL_VERSION='9.8')):
            cache = {}
            selected = d.release_for(IMAGE_RECORD, available, args, cache)
            self.assertEqual(selected['installer_version'], available[0]['installer_version'])
            self.assertEqual(selected['manifest_digest'], A)
            self.assertIsNone(d.release_for(dict(IMAGE_RECORD, kernel='wrong'), available, args, cache))
            self.assertIsNone(d.release_for(dict(IMAGE_RECORD, os_tag='rhel9.9'), available, args, cache))
            self.assertIsNone(d.release_for(dict(IMAGE_RECORD, rhel_major='10'), available, args, cache))
            self.assertIsNone(d.release_for(dict(IMAGE_RECORD, architecture='arm64'), available, args, cache))
            other_minor = [dict(available[0], version='4.21.0', installer_version='4.21.0', payload='other-minor')]
            self.assertIsNone(d.release_for(IMAGE_RECORD, other_minor, args, cache))
            legacy = dict(IMAGE_RECORD, os_tag='rhcos4.22')
            self.assertEqual(d.release_for(legacy, available, args, cache), selected)
            self.assertIsNone(d.release_for(legacy, other_minor, args, cache))

    def test_architecture_streams_and_assisted_names(self):
        for architecture, assisted in [('amd64', 'x86_64'), ('arm64', 'arm64'),
                                       ('ppc64le', 'ppc64le'), ('s390x', 's390x')]:
            urls = d.release_stream_urls(architecture)
            stream = '4-stable' if architecture == 'amd64' else '4-stable-' + architecture
            self.assertIn('/' + stream + '/tags', urls[0])
            self.assertIn('/4-stable-multi/tags', urls[1])
            args = SimpleNamespace(installer_token_file=None, installer_versions_url='assisted',
                                   architecture=architecture, release_stream_url=urls)
            records = {'assisted': {'4.22.0': {'cpu_architectures': [assisted]},
                                    '4.22.0-multi': {'cpu_architectures': [assisted]}},
                       urls[0]: {'name': stream, 'tags': [dict(name='4.22.0', phase='Accepted', pullSpec='native')]},
                       urls[1]: {'name': '4-stable-multi', 'tags': [dict(name='4.22.0', phase='Accepted', pullSpec='multi')]}}
            with self.subTest(architecture=architecture), patch.dict(GLOBALS, get_json=lambda url, *args: records[url]):
                self.assertEqual([r['payload'] for r in d.releases(args)], ['native', 'multi'])

    def test_installer_packaging_and_preference_do_not_depend_on_api_order(self):
        args = SimpleNamespace(installer_token_file=None, installer_versions_url='assisted', architecture='amd64',
                               release_stream_url=['native', 'multi'])
        ids = ['4.22.0', '4.22.0-x86_64', '4.22.0-multi']
        records = {name: {'name': '4-stable' + ('-multi' if name == 'multi' else ''),
                         'tags': [dict(name='4.22.0', phase='Accepted', pullSpec=name)]} for name in ['native', 'multi']}
        for identifiers in [ids, list(reversed(ids)), [ids[-1]]]:
            records['assisted'] = {identifier: {'cpu_architectures': ['x86_64']} for identifier in identifiers}
            with patch.dict(GLOBALS, get_json=lambda url, *args: records[url]):
                available = d.releases(args)
            expected = ids if len(identifiers) > 1 else [ids[-1]]
            self.assertEqual([r['installer_version'] for r in available], expected)
            self.assertEqual([r['payload'] for r in available], ['native', 'native', 'multi'] if len(expected) > 1 else ['multi'])
        records['multi']['tags'][0]['phase'] = 'Rejected'
        with patch.dict(GLOBALS, get_json=lambda url, *args: records[url]):
            self.assertEqual(d.releases(args), [])

    def test_ci_mirrored_stable_tags_use_published_installer_images(self):
        for architecture, assisted, suffix in [('amd64', 'x86_64', 'x86_64'), ('arm64', 'arm64', 'aarch64')]:
            args = SimpleNamespace(installer_token_file=None, installer_versions_url='assisted',
                                   architecture=architecture, release_stream_url=['native', 'multi'])
            records = {'assisted': {identifier: {'cpu_architectures': [assisted]} for identifier in ['4.22.0', '4.22.0-multi']}}
            for name in ['native', 'multi']:
                records[name] = {'name': '4-stable' + ('-multi' if name == 'multi' else ''),
                                 'tags': [dict(name='4.22.0', phase='Accepted', pullSpec='registry.ci.openshift.org/ocp/release:4.22.0')]}
            with patch.dict(GLOBALS, get_json=lambda url, *args: records[url]):
                available = d.releases(args)
            self.assertEqual([r['payload'] for r in available],
                             ['quay.io/openshift-release-dev/ocp-release:4.22.0-' + value for value in [suffix, 'multi']])

    def test_release_adapter_pins_platform_payload_and_requires_default_toolkit(self):
        data = dict(config=dict(architecture='amd64', os='linux'), digest=A, listDigest=C,
                    metadata=dict(version='4.22.0'),
                    references=dict(spec=dict(tags=[dict(name='driver-toolkit', **{'from': {'name': 'quay.io/toolkit@' + B}})])))
        with patch.object(signed_images, 'command', return_value=json.dumps(data)):
            for reference in ['quay.io/release:4.22.0-multi', 'quay.io/release@' + C, 'registry:5000/release:4.22.0']:
                info = signed_images.release_info(reference, 'amd64', 'auth')
                repository = 'registry:5000/release' if reference.startswith('registry:') else 'quay.io/release'
                self.assertEqual(info['payload'], repository + '@' + A)
                self.assertEqual(info['index_digest'], C)
                self.assertEqual(info['toolkit'], 'quay.io/toolkit@' + B)
        data['references']['spec']['tags'] = []
        with patch.object(signed_images, 'command', return_value=json.dumps(data)), self.assertRaisesRegex(ValueError, 'default driver-toolkit'):
            signed_images.release_info('quay.io/release:4.22.0', 'amd64', 'auth')

    def test_sequential_lookup_stops_at_newest_match_and_uses_matching_payload(self):
        args = SimpleNamespace(architecture='amd64', release_auth_file='auth')
        available = [dict(RELEASE, payload='native'), dict(RELEASE, installer_version='4.22.15-multi', payload='multi')]
        def info(payload, *args):
            return dict(version=RELEASE['version'], payload=payload + '@' + (A if payload == 'native' else B),
                        toolkit=payload, os_images={'rhel-coreos': 'quay.io/os@' + (A if payload == 'native' else B)})
        for native_kernel, expected in [(KERNEL, 'native'), ('different', 'multi')]:
            release_info = unittest.mock.Mock(side_effect=info)
            with patch.dict(GLOBALS, release_info=release_info,
                            rpm_info=lambda *args: dict(kernels={KERNEL}, rhel_version='9.8'),
                            toolkit_info=lambda toolkit, *args: dict(
                                KERNEL_VERSION=native_kernel if toolkit == 'native' else KERNEL, RHEL_VERSION='9.8')):
                selected = d.release_for(IMAGE_RECORD, available, args, {})
            self.assertEqual(selected['payload'], expected + '@' + (A if expected == 'native' else B))
            self.assertEqual([call.args[0] for call in release_info.call_args_list],
                             ['native'] if expected == 'native' else ['native', 'multi'])

    def test_toolkit_auth_is_private_and_removed_after_streaming(self):
        with tempfile.TemporaryDirectory() as directory:
            auth = Path(directory) / 'auth.json'
            auth.write_text('{}')
            paths = []
            def command(arguments, cwd=None, env=None):
                root = Path(env['DOCKER_CONFIG'])
                paths.append(root)
                self.assertEqual((root / 'config.json').resolve(), auth.resolve())
                self.assertEqual(root.stat().st_mode & 0o777, 0o700)
                self.assertEqual(Path(env['REGCTL_CONFIG']).read_text(), '{}')
                self.assertEqual(arguments, ['regctl', 'image', 'get-file', '--platform', 'linux/amd64',
                                              'quay.io/toolkit@' + A, '/etc/driver-toolkit-release.json'])
                return json.dumps(dict(KERNEL_VERSION=KERNEL, RHEL_VERSION='9.8'))
            with patch.object(signed_images, 'command', side_effect=command):
                self.assertEqual(signed_images.toolkit_info('quay.io/toolkit@' + A, 'amd64', auth)['KERNEL_VERSION'], KERNEL)
            self.assertTrue(auth.exists())
            self.assertFalse(paths[0].exists())

    def test_unmatched_kernel_reads_shared_os_once_and_never_reads_dtk(self):
        args = SimpleNamespace(architecture='amd64', release_auth_file='auth')
        releases = [dict(RELEASE, version='4.22.' + str(i), payload='payload-' + str(i)) for i in [2, 1]]
        release_info = unittest.mock.Mock(side_effect=lambda payload, *args: dict(
            version='4.22.' + payload[-1], payload=payload, toolkit='quay.io/toolkit@' + A,
            os_images={'rhel-coreos': 'quay.io/os@' + B}))
        rpm_info = unittest.mock.Mock(return_value=dict(kernels={KERNEL}, rhel_version='9.8'))
        toolkit_info = unittest.mock.Mock(return_value=dict(KERNEL_VERSION=KERNEL, RHEL_VERSION='9.8'))
        with patch.dict(GLOBALS, release_info=release_info, rpm_info=rpm_info, toolkit_info=toolkit_info):
            cache = {}
            self.assertIsNone(d.release_for(dict(IMAGE_RECORD, kernel='unmatched'), releases, args, cache))
            self.assertEqual(release_info.call_count, 2)
            self.assertEqual(rpm_info.call_count, 1)
            toolkit_info.assert_not_called()
            self.assertEqual(d.release_for(IMAGE_RECORD, releases, args, cache)['version'], '4.22.2')
            self.assertEqual(toolkit_info.call_count, 1)
        with patch.dict(GLOBALS, release_info=lambda *args: dict(version='4.22.3')), self.assertRaisesRegex(ValueError, 'accepted tag'):
            d.release_for(IMAGE_RECORD, releases, args, {})
        with patch.dict(GLOBALS, release_info=release_info,
                        rpm_info=rpm_info,
                        toolkit_info=unittest.mock.Mock(side_effect=OSError('DTK unavailable'))), self.assertRaises(OSError):
            d.release_for(IMAGE_RECORD, releases, args, {})

    def test_os_compatibility_excludes_known_minors_but_keeps_future_minors(self):
        args = SimpleNamespace(architecture='amd64', release_auth_file='auth')
        releases = [dict(RELEASE, version=version, payload=version) for version in ['4.24.0', '4.22.15', '4.21.0', '4.18.0']]
        info = unittest.mock.Mock(side_effect=lambda payload, *args: dict(
            version=payload, payload=payload, toolkit='quay.io/toolkit@' + A,
            os_images={'rhel-coreos': 'quay.io/os@' + B}))
        rpm = unittest.mock.Mock(return_value=dict(kernels={'other'}, rhel_version='9.8'))
        with patch.dict(GLOBALS, release_info=info, rpm_info=rpm, toolkit_info=unittest.mock.Mock()):
            self.assertIsNone(d.release_for(IMAGE_RECORD, releases, args, {}))
        self.assertEqual([call.args[0] for call in info.call_args_list], ['4.24.0', '4.22.15'])

    def test_complete_rpm_adapter_explicit_stream_architecture_and_validation(self):
        packages = {'kernel-core': '5.14.0-687.48.1.el9_8', 'redhat-release': '9.8-1.0.el9'}
        with patch.object(signed_images, 'command', return_value=json.dumps(packages)) as command:
            info = signed_images.rpm_info('quay.io/release@' + A, 'rhel-coreos', 'amd64', 'auth')
            self.assertEqual(info, dict(kernels={KERNEL}, rhel_version='9.8'))
            arguments = command.call_args.args[0]
            self.assertIn('--rpmdb-image=rhel-coreos', arguments)
            self.assertIn('--filter-by-os=linux/amd64', arguments)
            self.assertIn('--registry-config=auth', arguments)
            cache = next(value.split('=', 1)[1] for value in arguments if value.startswith('--rpmdb-cache='))
            self.assertFalse(Path(cache).exists())
        packages['kernel-64k-core'] = packages['kernel-core']
        with patch.object(signed_images, 'command', return_value=json.dumps(packages)):
            self.assertIn('5.14.0-687.48.1.el9_8.aarch64_64k',
                          signed_images.rpm_info('payload', 'rhel-coreos', 'arm64', 'auth')['kernels'])
        for packages in [{'redhat-release': '9.8-1.0.el9'}, {'redhat-release': 'bad', 'kernel-core': 'value'},
                         {'redhat-release': '9.8-1.0.el9', 'kernel-core': 'invalid'}]:
            with patch.object(signed_images, 'command', return_value=json.dumps(packages)), self.assertRaises(ValueError):
                signed_images.rpm_info('payload', 'rhel-coreos', 'amd64', 'auth')

    def test_rhel10_uses_its_explicit_os_image_and_matching_payload_for_rpm(self):
        args = SimpleNamespace(architecture='amd64', release_auth_file='auth')
        kernel = '6.12.0-211.55.1.el10_2.x86_64'
        image = dict(IMAGE_RECORD, os_tag='rhel10.2', rhel_major='10', kernel=kernel)
        info = dict(version=RELEASE['version'], payload='pinned@' + C, toolkit='quay.io/toolkit@' + A,
                    os_images={'rhel-coreos': 'quay.io/os@' + A, 'rhel-coreos-10': 'quay.io/os@' + B})
        rpm = unittest.mock.Mock(return_value=dict(kernels={kernel}, rhel_version='10.2'))
        with patch.dict(GLOBALS, release_info=lambda *args: info, rpm_info=rpm,
                        toolkit_info=lambda *args: dict(KERNEL_VERSION=kernel, RHEL_VERSION='10.2')):
            self.assertEqual(d.release_for(image, [RELEASE], args, {})['payload'], info['payload'])
        rpm.assert_called_once_with(info['payload'], 'rhel-coreos-10', 'amd64', 'auth')

    def test_legacy_release_keeps_dtk_fallback_and_metadata_errors_fail(self):
        args = SimpleNamespace(architecture='amd64', release_auth_file='auth')
        kernel = '5.14.0-427.68.1.el9_4.x86_64'
        image = dict(IMAGE_RECORD, os_tag='rhel9.4', kernel=kernel)
        release = dict(RELEASE, version='4.18.14', installer_version='4.18.14')
        info = dict(version=release['version'], payload=release['payload'], toolkit='quay.io/toolkit@' + A)
        rpm = unittest.mock.Mock(side_effect=OSError('RPM metadata unavailable'))
        with patch.dict(GLOBALS, release_info=lambda *args: info, rpm_info=rpm,
                        toolkit_info=lambda *args: dict(KERNEL_VERSION=kernel, RHEL_VERSION='9.4')):
            self.assertEqual(d.release_for(image, [release], args, {})['version'], release['version'])
        rpm.assert_not_called()
        info.update(version=RELEASE['version'], os_images={'rhel-coreos': 'quay.io/os@' + B})
        with patch.dict(GLOBALS, release_info=lambda *args: info, rpm_info=rpm), self.assertRaises(OSError):
            d.release_for(IMAGE_RECORD, [RELEASE], args, {})

    def test_one_request_real_phase1_handoff_and_no_work(self):
        for unfinished, tried, expected in [(False, set(), 'selected'), (True, set(), 'no_work'),
                                           (False, {signed_images.identity(IMAGE_RECORD)}, 'no_work')]:
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                argv = ['discover', '--registry-auth-file', 'auth', '--release-auth-file', 'auth',
                        '--output-dir', str(root / 'out'), '--shared-dir', str(root / 'shared')]
                with patch('sys.argv', argv), patch.dict(GLOBALS, recent_images=lambda *a: [IMAGE_RECORD],
                        attempted=lambda *a: (tried, unfinished), releases=lambda *a: [RELEASE],
                        release_for=lambda *a: RELEASE, image_info=lambda *a: IMAGE_RECORD):
                    self.assertEqual(d.main(), 0)
                result = json.loads((root / 'shared/nno-discovery-result.json').read_text())
                self.assertEqual(result['outcome'], expected)
                self.assertEqual((root / 'shared/skip-work-steps').is_file(), expected == 'no_work')
                self.assertEqual((root / 'shared/ofed-pullspec').exists(), expected == 'selected')
                if expected == 'selected':
                    request = json.loads((root / 'shared/nno-doca2-signed-request.json').read_text())
                    self.assertEqual(len(request['pairs']), 1)
                    self.assertEqual(request['pairs'][0]['driver_requested']['image'], IMAGE)

    def test_selection_handoff_and_invocation_checks_need_no_auth(self):
        with tempfile.TemporaryDirectory() as directory:
            shared = Path(directory)
            with patch.dict(os.environ, {}, clear=True):
                self.assertIsNone(signed_result.selected_image(shared, REQUEST))
            record = dict(job_name='local', build_id='local', outcome='selected', selected=dict(IMAGE_RECORD, release=RELEASE))
            d.write_json(shared / 'nno-discovery-result.json', record)
            with patch.dict(os.environ, {}, clear=True):
                self.assertEqual(signed_result.selected_image(shared, REQUEST), record['selected'])
                record['selected']['image'] = 'another-image'
                d.write_json(shared / 'nno-discovery-result.json', record)
                with self.assertRaisesRegex(ValueError, 'normalized request'):
                    signed_result.selected_image(shared, REQUEST)
            record['selected']['image'] = IMAGE
            d.write_json(shared / 'nno-discovery-result.json', record)
            with patch.dict(os.environ, JOB_NAME='other'):
                with self.assertRaisesRegex(ValueError, 'invocation'):
                    signed_result.selected_image(shared, REQUEST)

    def test_history_failure_and_mid_scan_tag_change_publish_no_target(self):
        for failure in [OSError('history unavailable'), None]:
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                argv = ['discover', '--registry-auth-file', 'auth', '--release-auth-file', 'auth',
                        '--output-dir', str(root / 'out'), '--shared-dir', str(root / 'shared')]
                history = unittest.mock.Mock(side_effect=failure) if failure else lambda *a: (set(), False)
                with patch('sys.argv', argv), patch.dict(GLOBALS, recent_images=lambda *a: [IMAGE_RECORD],
                        attempted=history, releases=lambda *a: [RELEASE], release_for=lambda *a: RELEASE,
                        image_info=lambda *a: {'manifest_digest': B}):
                    self.assertEqual(d.main(), 1)
                result = json.loads((root / 'shared/nno-discovery-result.json').read_text())
                self.assertEqual(result['outcome'], 'failed')
                self.assertIsNone(result['selected'])
                self.assertFalse((root / 'shared/skip-work-steps').exists())
                self.assertFalse((root / 'shared/ofed-pullspec').exists())
                self.assertFalse((root / 'out/nno-selected-request.json').exists())
