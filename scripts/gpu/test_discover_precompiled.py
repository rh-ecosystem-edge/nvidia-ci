"""Unit tests for GPU precompiled driver pre-flight discovery."""

from datetime import datetime, timezone
import json
import os
from pathlib import Path
import runpy
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

# Load the discovery module via runpy (same pattern as NNO test_discovery.py).
d = SimpleNamespace(**runpy.run_path(str(Path(__file__).with_name('discover_precompiled_target.py'))))
GLOBALS = d.main.__globals__

# Import shared modules for patching functions defined there.
# PYTHONPATH must include scripts/common and scripts/gpu (set by the test runner).
import ocp_release
import gpu_drivers

# Test constants.
KERNEL = '5.14.0-427.13.1.el9_4.x86_64'
DRIVER_VERSION = '550.90.07'
TAG = f'{DRIVER_VERSION}-{KERNEL}-rhel9.4-amd64'
A = 'sha256:' + 'a' * 64
B = 'sha256:' + 'b' * 64

STREAM_TAGS = [
    dict(name='4.18.5', phase='Accepted', pullSpec='quay.io/release:4.18.5-x86_64'),
    dict(name='4.18.4', phase='Accepted', pullSpec='quay.io/release:4.18.4-x86_64'),
    dict(name='4.17.9', phase='Accepted', pullSpec='quay.io/release:4.17.9-x86_64'),
    dict(name='4.18.3', phase='Ready', pullSpec='quay.io/release:4.18.3-x86_64'),
]

RELEASE_INFO = dict(
    version='4.18.5',
    payload='quay.io/release@' + A,
    toolkit='quay.io/toolkit@' + B,
    manifest_digest=A,
    index_digest=None,
    os_images={'rhel-coreos': 'quay.io/os@' + A},
)

DTK_INFO = dict(KERNEL_VERSION=KERNEL, RHEL_VERSION='9.4', RT_KERNEL_VERSION='', RHCOS_VERSION='418.94.202410100932-0')

SAMPLE_TAGS = [
    '550.90.07-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-amd64',
    '550.90.07-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-amd64-source',
    '550.54.14-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-amd64',
    '550.90.07-5.14.0-427.22.1.el9_4.x86_64-rhel9.4-amd64',
    '580.178.04-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-amd64',
    '580-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-amd64',
    '550.90.07-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-arm64',
    'latest',
    'invalid-tag',
]

# RHEL 10 test constants.
KERNEL_RHEL10 = '6.12.0-211.55.1.el10_2.x86_64'
DTK_INFO_RHEL10 = dict(KERNEL_VERSION=KERNEL_RHEL10, RHEL_VERSION='10.2', RT_KERNEL_VERSION='', RHCOS_VERSION='')
SAMPLE_TAGS_RHEL10 = [
    '570.86.15-6.12.0-211.55.1.el10_2.x86_64-rhel10.2-amd64',
    '570.86.15-6.12.0-211.55.1.el10_2.x86_64-rhel10.2-amd64-source',
    '570.86.15-6.12.0-100.0.1.el10_1.x86_64-rhel10.1-amd64',
    'latest',
]


class TagFilterTests(unittest.TestCase):
    """Test tag parsing and filtering."""

    def test_gpu_tag_regex_matches_valid_tags(self):
        match = gpu_drivers.GPU_TAG_RE.fullmatch(TAG)
        self.assertIsNotNone(match)
        self.assertEqual(match['driver_version'], DRIVER_VERSION)
        self.assertEqual(match['kernel'], KERNEL)
        self.assertEqual(match['rhel'], 'rhel9.4')
        self.assertEqual(match['arch'], 'amd64')

    def test_gpu_tag_regex_rejects_invalid_tags(self):
        for tag in ['latest', 'invalid-tag', '550.90.07', '550.90.07-source']:
            self.assertIsNone(gpu_drivers.GPU_TAG_RE.fullmatch(tag), f'should not match: {tag}')

    def test_gpu_tag_regex_rejects_source_suffix_in_arch(self):
        # Source tags have a valid structure but are filtered in filter_matching_tags.
        # However the -source suffix is NOT in the arch group — verify regex doesn't match it.
        source = '550.90.07-5.14.0-427.13.1.el9_4.x86_64-rhel9.4-amd64-source'
        self.assertIsNone(gpu_drivers.GPU_TAG_RE.fullmatch(source))

    def test_filter_matching_tags_selects_kernel_and_arch(self):
        matches = gpu_drivers.filter_matching_tags(SAMPLE_TAGS, KERNEL, 'amd64')
        versions = [m['driver_version'] for m in matches]
        # Should match 550.90.07, 550.54.14, 580.178.04, 580 (not source, not arm64, not different kernel)
        self.assertEqual(set(versions), {'550.90.07', '550.54.14', '580.178.04', '580'})

    def test_filter_matching_tags_excludes_different_kernel(self):
        matches = gpu_drivers.filter_matching_tags(SAMPLE_TAGS, '5.14.0-999.99.1.el9_4.x86_64', 'amd64')
        self.assertEqual(matches, [])

    def test_filter_matching_tags_excludes_different_arch(self):
        # Only the arm64 tag matches this kernel
        arm_matches = gpu_drivers.filter_matching_tags(SAMPLE_TAGS, KERNEL, 'arm64')
        self.assertEqual(len(arm_matches), 1)
        self.assertEqual(arm_matches[0]['architecture'], 'arm64')

    def test_deduplicate_by_major_keeps_longest(self):
        entries = [
            dict(driver_version='580.178.04', tag='t1', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
            dict(driver_version='580', tag='t2', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
            dict(driver_version='550.90.07', tag='t3', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
            dict(driver_version='550.54.14', tag='t4', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
        ]
        result = gpu_drivers.deduplicate_by_major(entries)
        versions = [e['driver_version'] for e in result]
        self.assertEqual(versions, ['550.90.07', '580.178.04'])


class RhelVersionTests(unittest.TestCase):
    """Test RHEL major version detection and repository selection."""

    def test_gpu_driver_repo_for_rhel9(self):
        self.assertEqual(gpu_drivers.gpu_driver_repo_for_rhel('9'), 'nvidia/gpu-driver-rhel9')

    def test_gpu_driver_repo_for_rhel10(self):
        self.assertEqual(gpu_drivers.gpu_driver_repo_for_rhel('10'), 'nvidia/gpu-driver-rhel10')

    def test_rhel_major_from_kernel_el9(self):
        self.assertEqual(gpu_drivers.rhel_major_from_kernel('5.14.0-427.13.1.el9_4.x86_64'), '9')

    def test_rhel_major_from_kernel_el10(self):
        self.assertEqual(gpu_drivers.rhel_major_from_kernel('6.12.0-211.55.1.el10_2.x86_64'), '10')

    def test_rhel_major_from_kernel_no_match(self):
        self.assertIsNone(gpu_drivers.rhel_major_from_kernel('5.14.0-427.13.1.x86_64'))

    def test_rhel_major_from_dtk_rhel9(self):
        self.assertEqual(gpu_drivers.rhel_major_from_dtk({'RHEL_VERSION': '9.4'}), '9')

    def test_rhel_major_from_dtk_rhel10(self):
        self.assertEqual(gpu_drivers.rhel_major_from_dtk({'RHEL_VERSION': '10.2'}), '10')

    def test_rhel_major_from_dtk_missing(self):
        self.assertIsNone(gpu_drivers.rhel_major_from_dtk({}))
        self.assertIsNone(gpu_drivers.rhel_major_from_dtk({'RHEL_VERSION': ''}))

    def test_dtk_takes_precedence_over_kernel(self):
        """DTK RHEL_VERSION is the primary signal; kernel string is fallback."""
        # DTK says RHEL 10, kernel says el9 — DTK wins.
        dtk = {'RHEL_VERSION': '10.2', 'KERNEL_VERSION': '5.14.0-427.13.1.el9_4.x86_64'}
        self.assertEqual(gpu_drivers.rhel_major_from_dtk(dtk), '10')


class ReleaseStreamTests(unittest.TestCase):
    """Test OCP release stream querying."""

    def test_latest_accepted_release_finds_newest(self):
        stream = dict(name='4-stable', tags=STREAM_TAGS)
        with patch.dict(GLOBALS, get_json=lambda url, **kw: stream):
            release = d.latest_accepted_release('4.18', 'amd64', ['https://stream.example/tags'])
        self.assertEqual(release['version'], '4.18.5')

    def test_latest_accepted_release_skips_non_accepted(self):
        stream = dict(name='4-stable', tags=STREAM_TAGS)
        with patch.dict(GLOBALS, get_json=lambda url, **kw: stream):
            release = d.latest_accepted_release('4.18', 'amd64', ['https://stream.example/tags'])
        self.assertNotEqual(release['version'], '4.18.3')  # 'Ready' phase should be skipped

    def test_latest_accepted_release_returns_none_for_missing_version(self):
        stream = dict(name='4-stable', tags=STREAM_TAGS)
        with patch.dict(GLOBALS, get_json=lambda url, **kw: stream):
            release = d.latest_accepted_release('4.99', 'amd64', ['https://stream.example/tags'])
        self.assertIsNone(release)

    def test_latest_accepted_release_rewrites_ci_registry(self):
        ci_tag = dict(name='4.18.5', phase='Accepted', pullSpec='registry.ci.openshift.org/ocp/release:4.18.5')
        stream = dict(name='4-stable', tags=[ci_tag])
        with patch.dict(GLOBALS, get_json=lambda url, **kw: stream):
            release = d.latest_accepted_release('4.18', 'amd64', ['https://stream.example/tags'])
        self.assertEqual(release['payload'], 'quay.io/openshift-release-dev/ocp-release:4.18.5-x86_64')

    def test_latest_accepted_release_rewrites_ci_registry_arm64(self):
        ci_tag = dict(name='4.18.5', phase='Accepted', pullSpec='registry.ci.openshift.org/ocp/release:4.18.5')
        stream = dict(name='4-stable-arm64', tags=[ci_tag])
        with patch.dict(GLOBALS, get_json=lambda url, **kw: stream):
            release = d.latest_accepted_release('4.18', 'arm64', ['https://stream.example/tags'])
        self.assertEqual(release['payload'], 'quay.io/openshift-release-dev/ocp-release:4.18.5-aarch64')

    def test_latest_accepted_release_checks_multiple_streams(self):
        stream1 = dict(name='4-stable', tags=[])
        stream2 = dict(name='4-stable-multi', tags=STREAM_TAGS)
        urls = ['https://stream1.example/tags', 'https://stream2.example/tags']
        responses = {urls[0]: stream1, urls[1]: stream2}
        with patch.dict(GLOBALS, get_json=lambda url, **kw: responses[url]):
            release = d.latest_accepted_release('4.18', 'amd64', urls)
        self.assertEqual(release['version'], '4.18.5')


class ReleaseStreamUrlTests(unittest.TestCase):
    """Test release_stream_urls for OCP 4 and 5+ version support."""

    def test_ocp4_amd64_stream(self):
        urls = ocp_release.release_stream_urls('amd64', '4.22')
        self.assertIn('/4-stable/tags', urls[0])
        self.assertIn('/4-stable-multi/tags', urls[1])

    def test_ocp5_amd64_stream(self):
        urls = ocp_release.release_stream_urls('amd64', '5.0')
        self.assertIn('/5-stable/tags', urls[0])
        self.assertIn('/5-stable-multi/tags', urls[1])

    def test_ocp5_arm64_stream(self):
        urls = ocp_release.release_stream_urls('arm64', '5.0')
        self.assertIn('/5-stable-arm64/tags', urls[0])
        self.assertIn('/5-stable-multi/tags', urls[1])

    def test_no_ocp_version_defaults_to_4(self):
        urls = ocp_release.release_stream_urls('amd64')
        self.assertIn('/4-stable/tags', urls[0])
        self.assertIn('/4-stable-multi/tags', urls[1])

    def test_future_major_version(self):
        urls = ocp_release.release_stream_urls('amd64', '6.1')
        self.assertIn('/6-stable/tags', urls[0])
        self.assertIn('/6-stable-multi/tags', urls[1])


class DiscoveryIntegrationTests(unittest.TestCase):
    """Test the full discovery main() flow with mocked external dependencies."""

    def run_discovery(self, ocp_version='4.18', latest_release=None,
                      release_info_mock=None, dtk_result=None,
                      tags=None, extra_env=None):
        """Helper to run main() with controlled mocks.

        Collects all files before the temp directory is cleaned up and returns
        them as dictionaries for assertion.
        """
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out_dir = root / 'out'
            shared_dir = root / 'shared'
            auth_file = root / 'auth.json'
            auth_file.write_text('{}')

            argv = ['discover', '--ocp-version', ocp_version,
                    '--output-dir', str(out_dir), '--shared-dir', str(shared_dir),
                    '--registry-auth-file', str(auth_file),
                    '--release-stream-url', 'https://stream.example/tags']
            env = dict(JOB_NAME='test-job', BUILD_ID='123')
            if extra_env:
                env.update(extra_env)

            mocks = dict(
                latest_accepted_release=lambda *args: latest_release,
                release_info=release_info_mock or (lambda *args: RELEASE_INFO),
                dtk_labels=lambda *args: dtk_result or DTK_INFO,
                list_gpu_driver_tags=lambda repo, auth: tags if tags is not None else SAMPLE_TAGS,
                alert_missing_driver=lambda *args: None,
                alert_driver_found=lambda *args: None,
            )

            with patch('sys.argv', argv), patch.dict(os.environ, env), patch.dict(GLOBALS, **mocks):
                exit_code = d.main()

            # Collect all outputs before temp dir cleanup.
            def read_json(path):
                return json.loads(path.read_text()) if path.exists() else None

            def read_text(path):
                return path.read_text() if path.exists() else None

            shared_files = {p.name for p in shared_dir.iterdir()} if shared_dir.exists() else set()
            out_files = {p.name for p in out_dir.iterdir()} if out_dir.exists() else set()

            return SimpleNamespace(
                exit_code=exit_code,
                result=read_json(shared_dir / 'gpu-precompiled-discovery-result.json'),
                out_result=read_json(out_dir / 'gpu-precompiled-discovery-result.json'),
                config=read_json(shared_dir / 'gpu-precompiled-config.json'),
                kernel_version_text=read_text(shared_dir / 'gpu-precompiled-kernel-version'),
                driver_versions_text=read_text(shared_dir / 'gpu-precompiled-driver-versions'),
                shared_files=shared_files,
                out_files=out_files,
            )

    def test_successful_discovery_writes_config_and_result(self):
        release = dict(version='4.18.5', payload='quay.io/release:4.18.5-x86_64')
        run = self.run_discovery(latest_release=release)
        self.assertEqual(run.exit_code, 0)
        self.assertEqual(run.result['outcome'], 'selected')
        self.assertIn('550.90.07', run.result['driver_versions'])
        self.assertIn('580.178.04', run.result['driver_versions'])
        self.assertEqual(run.result['kernel_version'], KERNEL)
        self.assertEqual(run.result['release_version'], '4.18.5')

        # Check config file.
        self.assertIsNotNone(run.config)
        self.assertEqual(run.config['kernel_version'], KERNEL)
        self.assertEqual(run.config['ocp_version'], '4.18.5')
        self.assertIn('550.90.07', run.config['driver_versions'])

        # Check env-var-style files.
        self.assertEqual(run.kernel_version_text.strip(), KERNEL)
        self.assertIn('550.90.07', run.driver_versions_text.strip())

        # No skip marker.
        self.assertNotIn('skip-work-steps', run.shared_files)

    def test_no_accepted_release_writes_skip_marker(self):
        run = self.run_discovery(latest_release=None)
        self.assertEqual(run.exit_code, 0)
        self.assertEqual(run.result['outcome'], 'no_work')
        self.assertEqual(run.result['reason'], 'no_accepted_release')
        self.assertIn('skip-work-steps', run.shared_files)
        self.assertNotIn('gpu-precompiled-config.json', run.shared_files)

    def test_no_matching_tags_writes_skip_marker(self):
        release = dict(version='4.18.5', payload='quay.io/release:4.18.5-x86_64')
        run = self.run_discovery(latest_release=release, tags=[])
        self.assertEqual(run.exit_code, 0)
        self.assertEqual(run.result['outcome'], 'no_work')
        self.assertEqual(run.result['reason'], 'no_matching_driver_tags')
        self.assertIn('skip-work-steps', run.shared_files)

    def test_failed_discovery_returns_exit_code_1(self):
        release = dict(version='4.18.5', payload='quay.io/release:4.18.5-x86_64')

        def raise_error(*args):
            raise ValueError('release unavailable')

        run = self.run_discovery(
            latest_release=release,
            release_info_mock=raise_error,
        )
        self.assertEqual(run.exit_code, 1)
        self.assertEqual(run.result['outcome'], 'failed')
        self.assertIn('release unavailable', run.result['error'])
        self.assertNotIn('skip-work-steps', run.shared_files)

    def test_stale_output_directory_is_rejected(self):
        """Discovery requires fresh output and shared directories."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out_dir = root / 'out'
            shared_dir = root / 'shared'
            out_dir.mkdir()
            shared_dir.mkdir()
            (shared_dir / 'gpu-precompiled-discovery-result.json').write_text('{}')
            auth_file = root / 'auth.json'
            auth_file.write_text('{}')
            argv = ['discover', '--ocp-version', '4.18',
                    '--output-dir', str(out_dir), '--shared-dir', str(shared_dir),
                    '--registry-auth-file', str(auth_file),
                    '--release-stream-url', 'https://stream.example/tags']
            env = {'JOB_NAME': 'test', 'BUILD_ID': '1'}
            with patch('sys.argv', argv), patch.dict(os.environ, env):
                exit_code = d.main()
            self.assertEqual(exit_code, 1)
            result = json.loads((shared_dir / 'gpu-precompiled-discovery-result.json').read_text())
            self.assertEqual(result['outcome'], 'failed')
            self.assertNotIn('skip-work-steps', {p.name for p in shared_dir.iterdir()})

    def test_result_written_to_both_directories(self):
        run = self.run_discovery(latest_release=None)
        self.assertIsNotNone(run.result)
        self.assertIsNotNone(run.out_result)
        self.assertEqual(run.result, run.out_result)

    def test_ci_environment_variables(self):
        """Verify CI env vars are picked up in the result."""
        release = dict(version='4.18.5', payload='quay.io/release:4.18.5-x86_64')
        run = self.run_discovery(
            latest_release=release,
            extra_env={'JOB_NAME': 'periodic-gpu-test', 'BUILD_ID': '42'},
        )
        self.assertEqual(run.result['job_name'], 'periodic-gpu-test')
        self.assertEqual(run.result['build_id'], '42')

    def test_error_message_redacts_credentials(self):
        """Verify error messages don't leak auth tokens."""
        run = self.run_discovery(latest_release=None)
        # The error_message function is shared — test it directly.
        msg = d.error_message(ValueError('Bearer my-secret-token failed'))
        self.assertNotIn('my-secret-token', msg)
        self.assertIn('[redacted authorization]', msg)

    def test_deduplicate_preserves_sorted_order(self):
        """When multiple versions share a major prefix, keep longest and sort ascending."""
        entries = [
            dict(driver_version='595.91.07', tag='t1', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
            dict(driver_version='595', tag='t2', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
            dict(driver_version='550.90.07', tag='t3', kernel=KERNEL, rhel='rhel9.4', architecture='amd64'),
        ]
        result = gpu_drivers.deduplicate_by_major(entries)
        self.assertEqual([e['driver_version'] for e in result], ['550.90.07', '595.91.07'])

    def test_ocp_version_required(self):
        """Missing OCP version should cause an error."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            argv = ['discover', '--output-dir', str(root / 'out'), '--shared-dir', str(root / 'shared')]
            with patch('sys.argv', argv), self.assertRaises(SystemExit) as ctx:
                d.main()
            self.assertNotEqual(ctx.exception.code, 0)

    def test_rhel10_selects_gpu_driver_rhel10_repo(self):
        """RHEL 10 kernel/DTK should query nvidia/gpu-driver-rhel10."""
        release = dict(version='4.22.5', payload='quay.io/release:4.22.5-x86_64')
        queried_repos = []
        def capture_tags(repo, auth):
            queried_repos.append(repo)
            return SAMPLE_TAGS_RHEL10
        run = self.run_discovery(
            latest_release=release,
            dtk_result=DTK_INFO_RHEL10,
            tags=None,  # Unused when list_gpu_driver_tags is mocked below.
        )
        # Re-run with the repo capture mock.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out_dir = root / 'out'
            shared_dir = root / 'shared'
            auth_file = root / 'auth.json'
            auth_file.write_text('{}')
            argv = ['discover', '--ocp-version', '4.22',
                    '--output-dir', str(out_dir), '--shared-dir', str(shared_dir),
                    '--registry-auth-file', str(auth_file),
                    '--release-stream-url', 'https://stream.example/tags']
            mocks = dict(
                latest_accepted_release=lambda *args: release,
                release_info=lambda *args: RELEASE_INFO,
                dtk_labels=lambda *args: DTK_INFO_RHEL10,
                list_gpu_driver_tags=capture_tags,
                alert_missing_driver=lambda *args: None,
                alert_driver_found=lambda *args: None,
            )
            with patch('sys.argv', argv), patch.dict(os.environ, {'JOB_NAME': 'test', 'BUILD_ID': '1'}), \
                    patch.dict(GLOBALS, **mocks):
                exit_code = d.main()
            self.assertEqual(exit_code, 0)
            self.assertEqual(queried_repos, ['nvidia/gpu-driver-rhel10'])
            result = json.loads((shared_dir / 'gpu-precompiled-discovery-result.json').read_text())
            self.assertEqual(result['outcome'], 'selected')
            self.assertIn('570.86.15', result['driver_versions'])
            config = json.loads((shared_dir / 'gpu-precompiled-config.json').read_text())
            self.assertEqual(config['driver_repository'], 'registry.redhat.io/nvidia/gpu-driver-rhel10')

    def test_rhel9_selects_gpu_driver_rhel9_repo(self):
        """RHEL 9 kernel/DTK should query nvidia/gpu-driver-rhel9."""
        release = dict(version='4.18.5', payload='quay.io/release:4.18.5-x86_64')
        queried_repos = []
        def capture_tags(repo, auth):
            queried_repos.append(repo)
            return SAMPLE_TAGS
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out_dir = root / 'out'
            shared_dir = root / 'shared'
            auth_file = root / 'auth.json'
            auth_file.write_text('{}')
            argv = ['discover', '--ocp-version', '4.18',
                    '--output-dir', str(out_dir), '--shared-dir', str(shared_dir),
                    '--registry-auth-file', str(auth_file),
                    '--release-stream-url', 'https://stream.example/tags']
            mocks = dict(
                latest_accepted_release=lambda *args: release,
                release_info=lambda *args: RELEASE_INFO,
                dtk_labels=lambda *args: DTK_INFO,
                list_gpu_driver_tags=capture_tags,
                alert_missing_driver=lambda *args: None,
                alert_driver_found=lambda *args: None,
            )
            with patch('sys.argv', argv), patch.dict(os.environ, {'JOB_NAME': 'test', 'BUILD_ID': '1'}), \
                    patch.dict(GLOBALS, **mocks):
                exit_code = d.main()
            self.assertEqual(exit_code, 0)
            self.assertEqual(queried_repos, ['nvidia/gpu-driver-rhel9'])
            config = json.loads((shared_dir / 'gpu-precompiled-config.json').read_text())
            self.assertEqual(config['driver_repository'], 'registry.redhat.io/nvidia/gpu-driver-rhel9')

    def test_kernel_fallback_when_dtk_rhel_version_missing(self):
        """When DTK has no RHEL_VERSION, fall back to kernel .el<N> parsing."""
        release = dict(version='4.18.5', payload='quay.io/release:4.18.5-x86_64')
        dtk_no_rhel = dict(KERNEL_VERSION=KERNEL, RT_KERNEL_VERSION='')
        queried_repos = []
        def capture_tags(repo, auth):
            queried_repos.append(repo)
            return SAMPLE_TAGS
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out_dir = root / 'out'
            shared_dir = root / 'shared'
            auth_file = root / 'auth.json'
            auth_file.write_text('{}')
            argv = ['discover', '--ocp-version', '4.18',
                    '--output-dir', str(out_dir), '--shared-dir', str(shared_dir),
                    '--registry-auth-file', str(auth_file),
                    '--release-stream-url', 'https://stream.example/tags']
            mocks = dict(
                latest_accepted_release=lambda *args: release,
                release_info=lambda *args: RELEASE_INFO,
                dtk_labels=lambda *args: dtk_no_rhel,
                list_gpu_driver_tags=capture_tags,
                alert_missing_driver=lambda *args: None,
                alert_driver_found=lambda *args: None,
            )
            with patch('sys.argv', argv), patch.dict(os.environ, {'JOB_NAME': 'test', 'BUILD_ID': '1'}), \
                    patch.dict(GLOBALS, **mocks):
                exit_code = d.main()
            self.assertEqual(exit_code, 0)
            # Kernel has .el9, so should select rhel9 repo.
            self.assertEqual(queried_repos, ['nvidia/gpu-driver-rhel9'])


class ListGPUDriverTagsTests(unittest.TestCase):
    """Test skopeo tag listing."""

    def test_list_gpu_driver_tags_parses_skopeo_output(self):
        skopeo_output = json.dumps({'Repository': 'registry.redhat.io/nvidia/gpu-driver-rhel9',
                                    'Tags': ['tag1', 'tag2', 'tag3']})
        with patch.object(gpu_drivers, 'command', return_value=skopeo_output):
            tags = gpu_drivers.list_gpu_driver_tags('nvidia/gpu-driver-rhel9', '/path/to/auth')
        self.assertEqual(tags, ['tag1', 'tag2', 'tag3'])

    def test_list_gpu_driver_tags_builds_correct_command(self):
        with patch.object(gpu_drivers, 'command',
                          return_value=json.dumps({'Tags': []})) as mock_cmd:
            gpu_drivers.list_gpu_driver_tags('nvidia/gpu-driver-rhel9', '/path/to/auth')
        mock_cmd.assert_called_once_with([
            'skopeo', 'list-tags',
            'docker://registry.redhat.io/nvidia/gpu-driver-rhel9',
            '--authfile', '/path/to/auth',
        ])

    def test_list_gpu_driver_tags_builds_correct_command_rhel10(self):
        with patch.object(gpu_drivers, 'command',
                          return_value=json.dumps({'Tags': []})) as mock_cmd:
            gpu_drivers.list_gpu_driver_tags('nvidia/gpu-driver-rhel10', '/path/to/auth')
        mock_cmd.assert_called_once_with([
            'skopeo', 'list-tags',
            'docker://registry.redhat.io/nvidia/gpu-driver-rhel10',
            '--authfile', '/path/to/auth',
        ])

    def test_list_gpu_driver_tags_handles_empty_tags(self):
        with patch.object(gpu_drivers, 'command',
                          return_value=json.dumps({'Tags': []})):
            tags = gpu_drivers.list_gpu_driver_tags('nvidia/gpu-driver-rhel9', '/path/to/auth')
        self.assertEqual(tags, [])

    def test_list_gpu_driver_tags_handles_missing_tags_key(self):
        with patch.object(gpu_drivers, 'command',
                          return_value=json.dumps({'Repository': 'x'})):
            tags = gpu_drivers.list_gpu_driver_tags('nvidia/gpu-driver-rhel9', '/path/to/auth')
        self.assertEqual(tags, [])


class DtkLabelsTests(unittest.TestCase):
    """Test skopeo-based DTK label extraction."""

    def test_dtk_labels_extracts_kernel_and_rhel(self):
        """skopeo inspect returns Labels with KERNEL_VERSION and RHEL_VERSION."""
        skopeo_output = json.dumps({
            'Labels': {'KERNEL_VERSION': KERNEL, 'RHEL_VERSION': '9.4', 'other': 'value'},
            'Digest': A,
        })
        with patch.object(ocp_release, 'command', return_value=skopeo_output):
            info = ocp_release.dtk_labels('quay.io/toolkit@' + A, 'amd64', '/auth.json')
        self.assertEqual(info['KERNEL_VERSION'], KERNEL)
        self.assertEqual(info['RHEL_VERSION'], '9.4')

    def test_dtk_labels_uses_hyphenated_label_names(self):
        """Some DTK images use hyphenated label names (kernel-version, rhel-version)."""
        skopeo_output = json.dumps({
            'Labels': {'kernel-version': KERNEL, 'rhel-version': '9.4'},
        })
        with patch.object(ocp_release, 'command', return_value=skopeo_output):
            info = ocp_release.dtk_labels('quay.io/toolkit@' + A, 'amd64', '/auth.json')
        self.assertEqual(info['KERNEL_VERSION'], KERNEL)
        self.assertEqual(info['RHEL_VERSION'], '9.4')

    def test_dtk_labels_raises_when_no_kernel(self):
        """Missing kernel label should raise ValueError."""
        skopeo_output = json.dumps({'Labels': {'RHEL_VERSION': '9.4'}})
        with patch.object(ocp_release, 'command', return_value=skopeo_output):
            with self.assertRaises(ValueError):
                ocp_release.dtk_labels('quay.io/toolkit@' + A, 'amd64', '/auth.json')

    def test_dtk_labels_builds_correct_skopeo_command(self):
        """Verify the skopeo inspect command uses --authfile."""
        with patch.object(ocp_release, 'command',
                          return_value=json.dumps({'Labels': {'KERNEL_VERSION': KERNEL}})) as mock_cmd:
            ocp_release.dtk_labels('quay.io/toolkit@' + A, 'amd64', '/path/auth.json')
        mock_cmd.assert_called_once_with([
            'skopeo', 'inspect', '--authfile', '/path/auth.json',
            'docker://quay.io/toolkit@' + A,
        ])

    def test_dtk_labels_handles_empty_labels(self):
        """Labels dict is present but empty → error."""
        skopeo_output = json.dumps({'Labels': {}})
        with patch.object(ocp_release, 'command', return_value=skopeo_output):
            with self.assertRaises(ValueError):
                ocp_release.dtk_labels('quay.io/toolkit@' + A, 'amd64', '/auth.json')

    def test_dtk_labels_handles_null_labels(self):
        """Labels is null → error."""
        skopeo_output = json.dumps({'Labels': None})
        with patch.object(ocp_release, 'command', return_value=skopeo_output):
            with self.assertRaises(ValueError):
                ocp_release.dtk_labels('quay.io/toolkit@' + A, 'amd64', '/auth.json')


class GitHubAlertTests(unittest.TestCase):
    """Test GitHub issue alerting with mocked API calls."""

    def test_alert_missing_driver_skipped_without_token(self):
        """No token → silent skip, no API calls."""
        with patch.object(gpu_drivers, '_gh_api', side_effect=lambda *a: self.fail('should not call API')):
            gpu_drivers.alert_missing_driver('', 'owner/repo', '4.18', 'amd64', KERNEL, 'nvidia/gpu-driver-rhel9', 'now')

    def test_alert_driver_found_skipped_without_token(self):
        """No token → silent skip."""
        with patch.object(gpu_drivers, '_gh_api', side_effect=lambda *a: self.fail('should not call API')):
            gpu_drivers.alert_driver_found('', 'owner/repo', '4.18', 'amd64', ['550.90.07'], 'now')

    def test_alert_missing_driver_creates_new_issue(self):
        """When no existing issue, creates one with correct title and label."""
        api_calls = []
        def mock_api(method, url, token, body=None):
            api_calls.append((method, url, body))
            if method == 'GET':
                return {'items': []}
            return {'number': 42}
        with patch.object(gpu_drivers, '_gh_api', side_effect=mock_api):
            gpu_drivers.alert_missing_driver('tok', 'owner/repo', '4.18', 'amd64', KERNEL, 'nvidia/gpu-driver-rhel9', 'now')
        self.assertEqual(len(api_calls), 2)
        self.assertEqual(api_calls[0][0], 'GET')  # search
        self.assertEqual(api_calls[1][0], 'POST')  # create
        self.assertIn('owner/repo', api_calls[1][1])
        self.assertEqual(api_calls[1][2]['labels'], ['missing-precompiled-driver'])
        self.assertIn('OCP 4.18', api_calls[1][2]['title'])
        self.assertIn('amd64', api_calls[1][2]['title'])

    def test_alert_missing_driver_comments_on_existing_issue(self):
        """When issue already exists, adds a comment instead of creating."""
        existing = {'number': 7, 'comments_url': 'https://api.github.com/repos/o/r/issues/7/comments'}
        api_calls = []
        def mock_api(method, url, token, body=None):
            api_calls.append((method, url, body))
            if method == 'GET':
                return {'items': [existing]}
            return {}
        with patch.object(gpu_drivers, '_gh_api', side_effect=mock_api):
            gpu_drivers.alert_missing_driver('tok', 'owner/repo', '4.18', 'amd64', KERNEL, 'nvidia/gpu-driver-rhel9', 'now')
        self.assertEqual(len(api_calls), 2)
        self.assertEqual(api_calls[1][0], 'POST')
        self.assertEqual(api_calls[1][1], existing['comments_url'])
        self.assertIn('Still no matching', api_calls[1][2]['body'])

    def test_alert_driver_found_closes_existing_issue(self):
        """When drivers found, close the open issue with a comment."""
        existing = {
            'number': 7,
            'url': 'https://api.github.com/repos/o/r/issues/7',
            'comments_url': 'https://api.github.com/repos/o/r/issues/7/comments',
        }
        api_calls = []
        def mock_api(method, url, token, body=None):
            api_calls.append((method, url, body))
            if method == 'GET':
                return {'items': [existing]}
            return {}
        with patch.object(gpu_drivers, '_gh_api', side_effect=mock_api):
            gpu_drivers.alert_driver_found('tok', 'owner/repo', '4.18', 'amd64', ['550.90.07'], 'now')
        self.assertEqual(len(api_calls), 3)
        self.assertEqual(api_calls[1][0], 'POST')  # comment
        self.assertIn('Resolved', api_calls[1][2]['body'])
        self.assertEqual(api_calls[2][0], 'PATCH')  # close
        self.assertEqual(api_calls[2][2], {'state': 'closed'})

    def test_alert_driver_found_noop_when_no_open_issue(self):
        """When no open issue exists, driver-found does nothing."""
        api_calls = []
        def mock_api(method, url, token, body=None):
            api_calls.append((method, url))
            return {'items': []}
        with patch.object(gpu_drivers, '_gh_api', side_effect=mock_api):
            gpu_drivers.alert_driver_found('tok', 'owner/repo', '4.18', 'amd64', ['550.90.07'], 'now')
        self.assertEqual(len(api_calls), 1)  # search only

    def test_issue_title_format(self):
        self.assertEqual(gpu_drivers._issue_title('4.18', 'amd64'), 'Missing precompiled GPU driver: OCP 4.18 (amd64)')
        self.assertEqual(gpu_drivers._issue_title('4.22', 'arm64'), 'Missing precompiled GPU driver: OCP 4.22 (arm64)')


if __name__ == '__main__':
    unittest.main()
