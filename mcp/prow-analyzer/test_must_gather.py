"""
Unit tests for must-gather directory discovery.

Tests that the find_must_gather_dirs function correctly discovers
must-gather directories at any depth in the artifacts tree.
"""

import unittest
from unittest.mock import patch
from must_gather.tools import find_must_gather_dirs
from config import RepositoryInfo


class TestMustGatherDiscovery(unittest.TestCase):
    """Tests for must-gather directory discovery functionality."""

    def setUp(self):
        """Set up common test fixtures."""
        self.config = {
            "gcs_bucket": "test-platform-results",
            "path_template": "pr-logs/pull/{org}_{repo}/{pr_number}",
            "gcsweb_base_url": "https://gcsweb.ci.openshift.org"
        }
        self.repo_info = RepositoryInfo(
            org="rh-ecosystem-edge",
            repo="nvidia-ci"
        )

    def test_deep_nested_must_gather(self):
        """
        Test that we can find must-gather directories deeply nested in the artifacts tree.

        This simulates the structure:
        artifacts/
          nvidia-gpu-operator-e2e-master/
            gpu-operator-e2e/
              artifacts/
                gpu-operator-tests-must-gather/
                  gpu-must-gather/
                    <actual files>
        """
        pr_number = "387"
        job_name = "pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-master"
        build_id = "1998217237932019712"

        # Mock all objects that would be returned by list_all_objects
        # This simulates what GCS would return for all files under artifacts/
        mock_objects = [
            {"name": "nvidia-gpu-operator-e2e-master/gpu-operator-e2e/artifacts/gpu-operator-tests-must-gather/gpu-must-gather/version",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/387/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-master/1998217237932019712/artifacts/nvidia-gpu-operator-e2e-master/gpu-operator-e2e/artifacts/gpu-operator-tests-must-gather/gpu-must-gather/version",
             "size": 10, "updated": "2024-01-01T00:00:00Z"},
            {"name": "nvidia-gpu-operator-e2e-master/gpu-operator-e2e/artifacts/gpu-operator-tests-must-gather/gpu-must-gather/timestamp",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/387/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-master/1998217237932019712/artifacts/nvidia-gpu-operator-e2e-master/gpu-operator-e2e/artifacts/gpu-operator-tests-must-gather/gpu-must-gather/timestamp",
             "size": 20, "updated": "2024-01-01T00:00:00Z"},
            {"name": "nvidia-gpu-operator-e2e-master/gpu-operator-e2e/artifacts/gpu-operator-tests-must-gather/gpu-must-gather/namespaces/test/events.yaml",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/387/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-master/1998217237932019712/artifacts/nvidia-gpu-operator-e2e-master/gpu-operator-e2e/artifacts/gpu-operator-tests-must-gather/gpu-must-gather/namespaces/test/events.yaml",
             "size": 1000, "updated": "2024-01-01T00:00:00Z"},
            {"name": "some-other-step/logs.txt",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/387/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-master/1998217237932019712/artifacts/some-other-step/logs.txt",
             "size": 500, "updated": "2024-01-01T00:00:00Z"},
        ]

        def mock_list_all_objects(_bucket, _prefix):
            return mock_objects

        with patch('must_gather.tools.gcs_client.list_all_objects', side_effect=mock_list_all_objects):
            results = find_must_gather_dirs(self.config, self.repo_info, pr_number, job_name, build_id)

        # Verify we found the must-gather directories
        self.assertGreater(len(results), 0, "Should have found at least one must-gather directory")

        # Check that we found both must-gather directories
        found_paths = [r['path'] for r in results]

        # Should find gpu-operator-tests-must-gather
        self.assertTrue(
            any('gpu-operator-tests-must-gather' in path for path in found_paths),
            f"Should find gpu-operator-tests-must-gather, found: {found_paths}"
        )

        # Should find gpu-must-gather
        self.assertTrue(
            any('gpu-must-gather' in path for path in found_paths),
            f"Should find gpu-must-gather, found: {found_paths}"
        )

    def test_must_gather_with_underscore(self):
        """Test that we can find must_gather directories (with underscore)."""
        mock_objects = [
            {"name": "test-results/some_must_gather/data/info.txt",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/test-results/some_must_gather/data/info.txt",
             "size": 100, "updated": "2024-01-01T00:00:00Z"},
            {"name": "test-results/some_must_gather/config.yaml",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/test-results/some_must_gather/config.yaml",
             "size": 200, "updated": "2024-01-01T00:00:00Z"},
        ]

        def mock_list_all_objects(_bucket, _prefix):
            return mock_objects

        with patch('must_gather.tools.gcs_client.list_all_objects', side_effect=mock_list_all_objects):
            results = find_must_gather_dirs(self.config, self.repo_info, "1", "test-job", "123")

        self.assertGreater(len(results), 0, "Should have found must_gather directory")
        self.assertTrue(
            any('some_must_gather' in r['path'] for r in results),
            f"Should find some_must_gather, found: {[r['path'] for r in results]}"
        )

    def test_must_gather_archives(self):
        """Test that we can find must-gather archive files."""
        mock_objects = [
            {"name": "results/must-gather.tar.gz",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/results/must-gather.tar.gz",
             "size": 1024000, "updated": "2024-01-01T00:00:00Z"},
            {"name": "results/gpu_must_gather.tar",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/results/gpu_must_gather.tar",
             "size": 2048000, "updated": "2024-01-01T00:00:00Z"},
            {"name": "results/regular-file.log",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/results/regular-file.log",
             "size": 100, "updated": "2024-01-01T00:00:00Z"},
        ]

        def mock_list_all_objects(_bucket, _prefix):
            return mock_objects

        with patch('must_gather.tools.gcs_client.list_all_objects', side_effect=mock_list_all_objects):
            results = find_must_gather_dirs(self.config, self.repo_info, "1", "test-job", "123")

        # Should find both archive files
        archives = [r for r in results if r['type'] == 'archive']
        self.assertEqual(len(archives), 2, f"Should find 2 archives, found {len(archives)}")

        archive_names = [a['filename'] for a in archives]
        self.assertIn('must-gather.tar.gz', archive_names)
        self.assertIn('gpu_must_gather.tar', archive_names)

        # Should have download URLs
        for archive in archives:
            self.assertIn('download_url', archive)
            self.assertTrue(archive['download_url'].startswith('https://'))

    def test_must_gather_filename_not_directory(self):
        """
        Test that files with 'must-gather' in their name are not treated as directories.
        Only actual directory components should be detected.
        """
        mock_objects = [
            # File with must-gather in name (should NOT be treated as directory)
            {"name": "step/my-must-gather-report.json",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/step/my-must-gather-report.json",
             "size": 500, "updated": "2024-01-01T00:00:00Z"},
            # Another file with must-gather in name
            {"name": "logs/must-gather-output.log",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/logs/must-gather-output.log",
             "size": 1000, "updated": "2024-01-01T00:00:00Z"},
            # Real must-gather directory
            {"name": "real-must-gather/data/info.txt",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/real-must-gather/data/info.txt",
             "size": 200, "updated": "2024-01-01T00:00:00Z"},
        ]

        def mock_list_all_objects(_bucket, _prefix):
            return mock_objects

        with patch('must_gather.tools.gcs_client.list_all_objects', side_effect=mock_list_all_objects):
            results = find_must_gather_dirs(self.config, self.repo_info, "1", "test-job", "123")

        # Should only find the real directory, not the files
        extracted_dirs = [r for r in results if r['type'] == 'extracted']
        self.assertEqual(
            len(extracted_dirs), 1,
            f"Should find exactly 1 directory, found {len(extracted_dirs)}: {[r['path'] for r in extracted_dirs]}"
        )

        # Verify it's the correct directory
        self.assertEqual(
            extracted_dirs[0]['path'], 'real-must-gather',
            f"Should find 'real-must-gather', found: {extracted_dirs[0]['path']}"
        )

        # Verify the files with must-gather in their names were NOT added as directories
        all_paths = [r['path'] for r in results]
        self.assertNotIn(
            'step/my-must-gather-report.json', all_paths,
            "Files with must-gather in name should not be treated as directories"
        )
        self.assertNotIn(
            'logs/must-gather-output.log', all_paths,
            "Files with must-gather in name should not be treated as directories"
        )

    def test_archive_in_must_gather_dir_not_matched(self):
        """
        Test that archives inside a must-gather directory are not matched as must-gather archives
        unless the filename itself contains must-gather.
        """
        mock_objects = [
            # Archive inside must-gather dir, but without must-gather in filename
            # Should NOT be detected as must-gather archive
            {"name": "test-must-gather/backup.tar.gz",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/test-must-gather/backup.tar.gz",
             "size": 5000, "updated": "2024-01-01T00:00:00Z"},
            {"name": "test-must-gather/data.tar",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/test-must-gather/data.tar",
             "size": 3000, "updated": "2024-01-01T00:00:00Z"},
            # Archive WITH must-gather in filename - should be detected
            {"name": "test-must-gather/must-gather-results.tar.gz",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/test-must-gather/must-gather-results.tar.gz",
             "size": 8000, "updated": "2024-01-01T00:00:00Z"},
            # Regular file in must-gather dir
            {"name": "test-must-gather/info.txt",
             "full_path": "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/1/test-job/123/artifacts/test-must-gather/info.txt",
             "size": 100, "updated": "2024-01-01T00:00:00Z"},
        ]

        def mock_list_all_objects(_bucket, _prefix):
            return mock_objects

        with patch('must_gather.tools.gcs_client.list_all_objects', side_effect=mock_list_all_objects):
            results = find_must_gather_dirs(self.config, self.repo_info, "1", "test-job", "123")

        # Should find the directory
        extracted_dirs = [r for r in results if r['type'] == 'extracted']
        self.assertEqual(len(extracted_dirs), 1, f"Should find 1 directory, found {len(extracted_dirs)}")
        self.assertEqual(extracted_dirs[0]['path'], 'test-must-gather')

        # Should only find the archive with must-gather in its filename
        archives = [r for r in results if r['type'] == 'archive']
        self.assertEqual(
            len(archives), 1,
            f"Should find exactly 1 archive, found {len(archives)}: {[a['filename'] for a in archives]}"
        )

        # Verify it's the correct archive
        self.assertEqual(
            archives[0]['filename'], 'must-gather-results.tar.gz',
            f"Should only find must-gather-results.tar.gz, found: {archives[0]['filename']}"
        )

        # Verify backup.tar.gz and data.tar were NOT matched
        archive_names = [a['filename'] for a in archives]
        self.assertNotIn(
            'backup.tar.gz', archive_names,
            "Archives without must-gather in filename should not be matched"
        )
        self.assertNotIn(
            'data.tar', archive_names,
            "Archives without must-gather in filename should not be matched"
        )


if __name__ == "__main__":
    unittest.main()
