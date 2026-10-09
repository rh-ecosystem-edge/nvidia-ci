"""Exercise selection boundaries; external registry, Prow and release services are mocked."""

from datetime import datetime, timedelta, timezone
import json
import io
import os
from pathlib import Path
import runpy
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

import signed_images
from test_validate_signed_request import IMAGE, KERNEL, REQUEST
from test_verify_signed_step_result import signed_result

d = SimpleNamespace(**runpy.run_path(str(Path(__file__).with_name('discover-signed-target.py'))))
GLOBALS = d.main.__globals__
NOW = datetime(2026, 10, 9, tzinfo=timezone.utc)
A, B, C = ['sha256:' + character * 64 for character in 'abc']
IMAGE_RECORD = dict(signed_images.metadata(IMAGE), manifest_digest=A, config_digest=B, index_digest=C, created_at=NOW.isoformat())
RELEASE = dict(version='4.22.0', installer_version='4.22.0', payload='quay.io/release@' + A)


class DiscoveryTests(unittest.TestCase):
    def test_registry_window_aliases_and_rebuilds(self):
        tag = IMAGE.rsplit(':', 1)[1]
        alias = tag.replace('24.10', '25.01') if '24.10' in tag else 'alias-' + tag
        with patch.dict(GLOBALS, command=lambda args: json.dumps({'Tags': [tag, alias, 'latest']})), \
                patch.dict(GLOBALS, config_digest=lambda *args: B):
            for age, count in [(0, 2), (7, 2), (7.001, 0)]:
                def info(image, *args):
                    return dict(manifest_digest=A, created_at=(NOW - timedelta(days=age)).isoformat(), index_digest=C)
                with patch.dict(GLOBALS, image_info=info):
                    self.assertEqual(len(d.recent_images('amd64', 'auth', NOW)), count)
            def rebuilt(image, *args):
                return dict(manifest_digest=A if image.endswith(tag) and not image.endswith(alias) else B, created_at=NOW.isoformat())
            with patch.dict(GLOBALS, image_info=rebuilt):
                self.assertEqual(len(d.recent_images('amd64', 'auth', NOW)), 4)
            with patch.dict(GLOBALS, image_info=lambda *args: dict(
                    manifest_digest=A, created_at='2026-10-08T12:34:56.123456789Z')):
                self.assertEqual(len(d.recent_images('amd64', 'auth', NOW)), 2)
            for date in ['bad', '2026-10-09T00:00:00', (NOW + timedelta(seconds=1)).isoformat(), None]:
                with patch.dict(GLOBALS, image_info=lambda *args: dict(manifest_digest=A, created_at=date)):
                    with self.assertRaises((ValueError, TypeError, AttributeError)):
                        d.recent_images('amd64', 'auth', NOW)

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
            return [build + '/started.json'] if pattern.endswith('started.json') else ([artifact] if record else [])
        def read(path, optional=False):
            if path.endswith('started.json'):
                return {'timestamp': (NOW - timedelta(days=age)).timestamp()}
            if path.endswith('finished.json'):
                return {'result': finished if isinstance(finished, str) else 'FAILURE'} if finished else None
            return record
        with patch.dict(GLOBALS, objects=objects, read=read):
            return d.attempted([IMAGE_RECORD], NOW, [job])

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
        with patch.dict(GLOBALS, objects=lambda *args: ['build/started.json'], read=lambda *args, **kwargs: {'timestamp': 'bad'}):
            with self.assertRaises(ValueError):
                d.attempted([IMAGE_RECORD], NOW, [d.MANUAL_JOB])

    def test_installable_accepted_release_and_exact_default_dtk(self):
        args = SimpleNamespace(installer_token_file=None, installer_versions_url='assisted', architecture='amd64',
                               release_stream_url=['controller'], release_auth_file='auth')
        supported = {'4.22.0-x86_64': {'cpu_architectures': ['x86_64']}}
        tags = [dict(name='4.22.0', phase='Accepted', pullSpec='payload'), dict(name='4.22.1', phase='Accepted', pullSpec='other')]
        with patch.dict(GLOBALS, get_json=lambda url, *args: supported if url == 'assisted' else {'tags': tags}):
            available = d.releases(args)
        self.assertEqual([r['installer_version'] for r in available], ['4.22.0-x86_64'])
        def command(arguments, cwd=None):
            if cwd:
                (Path(cwd) / 'driver-toolkit-release.json').write_text(json.dumps(dict(KERNEL_VERSION=KERNEL, RHEL_VERSION=IMAGE_RECORD['rhel_major'])))
            else:
                self.assertIn('--image-for=driver-toolkit', arguments)
            return 'toolkit@' + A
        with patch.dict(GLOBALS, command=command):
            cache = {}
            self.assertEqual(d.release_for(IMAGE_RECORD, available, args, cache), available[0])
            self.assertIsNone(d.release_for(dict(IMAGE_RECORD, kernel='wrong'), available, args, cache))

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
                self.assertEqual((root / 'shared/ofed-pullspec').exists(), expected == 'selected')
                if expected == 'selected':
                    request = json.loads((root / 'shared/nno-doca2-signed-request.json').read_text())
                    self.assertEqual(len(request['pairs']), 1)
                    self.assertEqual(request['pairs'][0]['driver_requested']['image'], IMAGE)

    def test_preflight_content_and_invocation_checks_manual_needs_no_auth(self):
        with tempfile.TemporaryDirectory() as directory:
            shared = Path(directory)
            with patch.dict(os.environ, {}, clear=True):
                signed_result.preflight(shared, REQUEST)
            record = dict(job_name='local', build_id='local', outcome='selected', selected=dict(IMAGE_RECORD, release=RELEASE))
            d.write_json(shared / 'nno-discovery-result.json', record)
            with patch.dict(os.environ, NNO_REGISTRY_AUTH_FILE='auth'), patch.object(signed_result, 'image_info', return_value=IMAGE_RECORD):
                signed_result.preflight(shared, REQUEST)
            with patch.dict(os.environ, NNO_REGISTRY_AUTH_FILE='auth'), patch.object(signed_result, 'image_info', return_value={'manifest_digest': B}):
                with self.assertRaisesRegex(ValueError, 'content'):
                    signed_result.preflight(shared, REQUEST)
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
                self.assertFalse((root / 'shared/ofed-pullspec').exists())
                self.assertFalse((root / 'out/nno-selected-request.json').exists())
