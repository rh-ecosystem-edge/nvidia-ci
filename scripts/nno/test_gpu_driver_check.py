"""Tests for the GPU precompiled driver check integrated into NNO discovery."""

import json
import os
from pathlib import Path
import runpy
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import signed_images
import gpu_drivers
from test_validate_signed_request import IMAGE, KERNEL

d = SimpleNamespace(**runpy.run_path(str(Path(__file__).with_name('discover-signed-target.py'))))
GLOBALS = d.main.__globals__

A, B, C = ['sha256:' + character * 64 for character in 'abc']
IMAGE_RECORD = dict(signed_images.metadata(IMAGE), manifest_digest=A, config_digest=B, index_digest=C,
                    created_at='2026-10-09T00:00:00+00:00')
RELEASE = dict(version='4.22.15', installer_version='4.22.15', payload='quay.io/release@' + A)

# Sample GPU driver tags that match the NNO kernel.
GPU_TAGS_MATCHING = [
    f'550.90.07-{KERNEL}-rhel9.8-amd64',
    f'580.178.04-{KERNEL}-rhel9.8-amd64',
]
GPU_TAGS_NO_MATCH = [
    '550.90.07-5.14.0-999.99.1.el9_8.x86_64-rhel9.8-amd64',
]


class TestCheckGpuDriver(unittest.TestCase):
    """Test the check_gpu_driver function directly."""

    def test_returns_available_when_tags_match(self):
        """GPU drivers found → available=True with driver versions."""
        def mock_list_tags(repo, auth):
            return GPU_TAGS_MATCHING
        with patch.object(gpu_drivers, 'list_gpu_driver_tags', side_effect=mock_list_tags):
            result = d.check_gpu_driver(KERNEL, '9', 'amd64', '/auth')
        self.assertTrue(result['available'])
        self.assertIn('550.90.07', result['driver_versions'])
        self.assertIn('580.178.04', result['driver_versions'])
        self.assertEqual(result['gpu_repo'], 'nvidia/gpu-driver-rhel9')

    def test_returns_unavailable_when_no_match(self):
        """No matching GPU tags → available=False."""
        with patch.object(gpu_drivers, 'list_gpu_driver_tags', return_value=GPU_TAGS_NO_MATCH):
            result = d.check_gpu_driver(KERNEL, '9', 'amd64', '/auth')
        self.assertFalse(result['available'])
        self.assertEqual(result['driver_versions'], [])

    def test_returns_none_when_skopeo_missing(self):
        """FileNotFoundError (no skopeo) → available=None (skipped)."""
        with patch.object(gpu_drivers, 'list_gpu_driver_tags', side_effect=FileNotFoundError('skopeo')):
            result = d.check_gpu_driver(KERNEL, '9', 'amd64', '/auth')
        self.assertIsNone(result['available'])

    def test_returns_unavailable_on_registry_error(self):
        """Registry/network error → available=False with error."""
        with patch.object(gpu_drivers, 'list_gpu_driver_tags', side_effect=ValueError('registry error')):
            result = d.check_gpu_driver(KERNEL, '9', 'amd64', '/auth')
        self.assertFalse(result['available'])
        self.assertIn('registry error', result.get('error', ''))

    def test_uses_correct_rhel_repo(self):
        """RHEL major determines the registry repository."""
        queried = []
        def capture(repo, auth):
            queried.append(repo)
            return []
        with patch.object(gpu_drivers, 'list_gpu_driver_tags', side_effect=capture):
            d.check_gpu_driver(KERNEL, '10', 'amd64', '/auth')
        self.assertEqual(queried, ['nvidia/gpu-driver-rhel10'])

    def test_uses_same_kernel_as_ofed(self):
        """GPU check uses the exact same kernel string passed to it."""
        seen_kernels = []
        def capture(tags, kernel, arch):
            seen_kernels.append(kernel)
            return []
        with patch.object(gpu_drivers, 'list_gpu_driver_tags', return_value=[]), \
             patch.object(gpu_drivers, 'filter_matching_tags', side_effect=capture):
            d.check_gpu_driver(KERNEL, '9', 'amd64', '/auth')
        self.assertEqual(seen_kernels, [KERNEL])


class TestNnoDiscoveryWithGpuCheck(unittest.TestCase):
    """Test main() with --check-gpu-driver enabled."""

    def _run_discovery(self, gpu_check_result, expected_outcome):
        """Helper to run discovery with mocked OFED selection and GPU check."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            argv = ['discover', '--check-gpu-driver',
                    '--registry-auth-file', 'auth', '--release-auth-file', 'auth',
                    '--output-dir', str(root / 'out'), '--shared-dir', str(root / 'shared')]
            mocks = dict(
                recent_images=lambda *a: [IMAGE_RECORD],
                attempted=lambda *a: (set(), False),
                releases=lambda *a: [RELEASE],
                release_for=lambda *a: RELEASE,
                image_info=lambda *a: IMAGE_RECORD,
                check_gpu_driver=lambda *a: gpu_check_result,
            )
            with patch('sys.argv', argv), patch.dict(GLOBALS, **mocks):
                exit_code = d.main()

            result = json.loads((root / 'shared/nno-discovery-result.json').read_text())
            has_skip = (root / 'shared/skip-work-steps').is_file()
            has_ofed = (root / 'shared/ofed-pullspec').exists()
            has_request = (root / 'shared/nno-doca2-signed-request.json').exists()
            return SimpleNamespace(exit_code=exit_code, result=result,
                                  has_skip=has_skip, has_ofed=has_ofed,
                                  has_request=has_request)

    def test_ofed_and_gpu_both_available(self):
        """Both OFED and GPU drivers available → selected."""
        gpu_ok = dict(gpu_repo='nvidia/gpu-driver-rhel9',
                      driver_versions=['550.90.07', '580.178.04'], available=True)
        run = self._run_discovery(gpu_ok, 'selected')
        self.assertEqual(run.exit_code, 0)
        self.assertEqual(run.result['outcome'], 'selected')
        self.assertTrue(run.result['gpu_driver_available'])
        self.assertIn('550.90.07', run.result['gpu_driver_versions'])
        self.assertFalse(run.has_skip)
        self.assertTrue(run.has_ofed)

    def test_ofed_available_gpu_not(self):
        """OFED available but GPU drivers NOT → no_work, skip marker, cleanup."""
        gpu_missing = dict(gpu_repo='nvidia/gpu-driver-rhel9',
                           driver_versions=[], available=False)
        run = self._run_discovery(gpu_missing, 'no_work')
        self.assertEqual(run.exit_code, 0)
        self.assertEqual(run.result['outcome'], 'no_work')
        self.assertEqual(run.result['reason'], 'no_gpu_precompiled_driver')
        self.assertFalse(run.result['gpu_driver_available'])
        self.assertTrue(run.has_skip)
        # OFED handoff files should be cleaned up.
        self.assertFalse(run.has_ofed)
        self.assertFalse(run.has_request)

    def test_gpu_check_skipped_proceeds_as_selected(self):
        """GPU check returns None (skipped) → still selected."""
        gpu_skipped = dict(gpu_repo='nvidia/gpu-driver-rhel9',
                           driver_versions=[], available=None)
        run = self._run_discovery(gpu_skipped, 'selected')
        self.assertEqual(run.result['outcome'], 'selected')
        self.assertIsNone(run.result['gpu_driver_available'])
        self.assertFalse(run.has_skip)

    def test_gpu_info_added_to_result_json(self):
        """GPU check results appear in the discovery output JSON."""
        gpu_ok = dict(gpu_repo='nvidia/gpu-driver-rhel9',
                      driver_versions=['550.90.07'], available=True)
        run = self._run_discovery(gpu_ok, 'selected')
        self.assertIn('gpu_driver_available', run.result)
        self.assertIn('gpu_driver_versions', run.result)


if __name__ == '__main__':
    unittest.main()
