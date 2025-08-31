import json
import os
import tempfile
import unittest
from unittest import mock, TestCase

from workflows.gpu_operator_dashboard.fetch_ci_data import (
    merge_and_save_results, OCP_FULL_VERSION, GPU_OPERATOR_VERSION)

# Testing final logic of generate_ci_dashboard.py which stores the JSON test data


class TestSaveToJson(TestCase):
    def setUp(self):
        # Create a temporary directory for test files
        self.temp_dir = tempfile.TemporaryDirectory()
        self.output_dir = self.temp_dir.name
        self.test_file = "test_data.json"

    def tearDown(self):
        # Clean up the temporary directory
        self.temp_dir.cleanup()

    def test_save_new_data_to_empty_existing(self):
        """Test saving new data when existing_data is empty."""
        new_data = {
            "4.14": [
                {
                    OCP_FULL_VERSION: "4.14.1",
                    GPU_OPERATOR_VERSION: "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
                    "job_timestamp": "1712345678"
                }
            ]
        }
        existing_data = {}

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        # Read the saved file and verify its contents
        with open(data_file, 'r') as f:
            saved_data = json.load(f)

        # The saved data should have the "tests" wrapper structure
        expected_data = {
            "4.14": {
                "notes": [],
                "tests": [
                    {
                        OCP_FULL_VERSION: "4.14.1",
                        GPU_OPERATOR_VERSION: "23.9.0",
                        "test_status": "SUCCESS",
                        "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
                        "job_timestamp": "1712345678"
                    }
                ]
            }
        }
        self.assertEqual(saved_data, expected_data)

    def test_merge_with_no_duplicates(self):
        """Test merging when no duplicates exist."""
        new_data = {
            "4.14": [
                {
                    OCP_FULL_VERSION: "4.14.1",
                    GPU_OPERATOR_VERSION: "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
                    "job_timestamp": "1712345678"
                }
            ]
        }
        existing_data = {
            "4.14":
                {"tests": [
                    {
                        OCP_FULL_VERSION: "4.14.2",
                        GPU_OPERATOR_VERSION: "24.3.0",
                        "test_status": "SUCCESS",
                        "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/124/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.13-stable-nvidia-gpu-operator-e2e-23-9-x/457",
                        "job_timestamp": "1712345679"
                    }
                ]
                }
        }

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        # Read the saved file and verify its contents
        with open(data_file, 'r') as f:
            saved_data = json.load(f)

        # Both entries should be present
        self.assertEqual(len(saved_data["4.14"]["tests"]), 2)
        self.assertTrue(any(item[GPU_OPERATOR_VERSION]
                        == "23.9.0" for item in saved_data["4.14"]["tests"]))
        self.assertTrue(any(item[GPU_OPERATOR_VERSION]
                        == "24.3.0" for item in saved_data["4.14"]["tests"]))

    def test_exact_duplicates(self):
        """Test handling of exact duplicates - they should not be added."""
        item = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
            "job_timestamp": "1712345678"
        }

        new_data = {"4.14": [item]}
        existing_data = {"4.14": {"tests": [item.copy()]}}

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        with open(data_file, 'r') as f:
            saved_data = json.load(f)

        # Only one entry should be present (no duplicates)
        self.assertEqual(len(saved_data["4.14"]["tests"]), 1)

    def test_different_ocp_keys(self):
        """Test merging data with different OCP keys."""
        new_data = {
            "4.14":[
                    {
                        OCP_FULL_VERSION: "4.14.1",
                        GPU_OPERATOR_VERSION: "23.9.0",
                        "test_status": "SUCCESS",
                        "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
                        "job_timestamp": "1712345678"
                    }
                ]
        }
        existing_data = {
            "4.13":
                {"tests": [{
                    OCP_FULL_VERSION: "4.13.5",
                    GPU_OPERATOR_VERSION: "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/124/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.13-stable-nvidia-gpu-operator-e2e-23-9-x/457",
                    "job_timestamp": "1712345679"
                }
                ]
                }
        }

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        with open(data_file, 'r') as f:
            saved_data = json.load(f)

        # Both OCP keys should be present
        self.assertIn("4.14", saved_data)
        self.assertIn("4.13", saved_data)
        self.assertEqual(len(saved_data["4.14"]["tests"]), 1)
        self.assertEqual(len(saved_data["4.13"]["tests"]), 1)

    def test_partial_duplicates(self):
        """Test handling items from the same build but different status - SUCCESS should be preferred."""
        new_item = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
            "job_timestamp": "1712345678"
        }

        existing_item = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "FAILURE",  # Different test_status
            "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
            "job_timestamp": "1712345678"
        }

        new_data = {"4.14": [new_item]}
        existing_data = {"4.14": {"tests": [existing_item]}}

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        with open(data_file, 'r') as f:
            saved_data = json.load(f)

        # Only one entry should remain (SUCCESS should be preferred over FAILURE)
        self.assertEqual(len(saved_data["4.14"]["tests"]), 1)
        self.assertEqual(saved_data["4.14"]["tests"][0]["test_status"], "SUCCESS")

    def test_json_not_overwritten(self):
        """Test that merging new data does not overwrite existing data fields.
           Each field from the existing data should be preserved and new data should be appended.
        """
        existing_item = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
            "job_timestamp": "1712345678"
        }
        new_item = {
            OCP_FULL_VERSION: "4.14.1",  # Same OCP version key
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "FAILURE",  # New test_status, different from the existing item
            "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/125/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/458",  # New prow_job_url
            "job_timestamp": "1712345680"  # New job_timestamp
        }
        new_data = {"4.14": [new_item]}
        existing_data = {"4.14": {"tests": [existing_item]}}

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        with open(data_file, 'r') as f:
            saved_data = json.load(f)

        # Verify that both the existing item and new item are present and not overwritten
        self.assertEqual(len(saved_data["4.14"]["tests"]), 2)

        # Check that the existing item's fields are preserved
        found_existing = any(
            item["test_status"] == "SUCCESS" and item["prow_job_url"] == "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456" and item["job_timestamp"] == "1712345678"
            for item in saved_data["4.14"]["tests"]
        )
        self.assertTrue(found_existing)

        # Check that the new item's fields are saved
        found_new = any(
            item["test_status"] == "FAILURE" and item["prow_job_url"] == "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/125/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/458" and item["job_timestamp"] == "1712345680"
            for item in saved_data["4.14"]["tests"]
        )
        self.assertTrue(found_new)

    @mock.patch('json.dump')
    def test_empty_new_data(self, mock_json_dump):
        """Test with empty new_data."""
        new_data = {}
        existing_data = {
            "4.14": [
                {
                    OCP_FULL_VERSION: "4.14.1",
                    GPU_OPERATOR_VERSION: "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/123/pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.14-stable-nvidia-gpu-operator-e2e-23-9-x/456",
                    "job_timestamp": "1712345678"
                }
            ]
        }

        data_file = os.path.join(self.output_dir, self.test_file)
        merge_and_save_results(new_data, data_file, existing_data)

        # Verify json.dump was called with the correct arguments
        mock_json_dump.assert_called_once()
        args, _ = mock_json_dump.call_args
        saved_data = args[0]

        # The existing data should remain unchanged
        self.assertEqual(saved_data, existing_data)


if __name__ == '__main__':
    unittest.main()
