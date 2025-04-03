import json
import os
import tempfile
import unittest
from unittest import mock, TestCase

from generate_ci_dashboard import merge_and_save_results


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
                    "ocp_full_version": "4.14.1",
                    "gpu_operator_version": "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://example.com/job1",
                    "job_timestamp": "1712345678"
                }
            ]
        }
        existing_data = {}

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        # Read the saved file and verify its contents
        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        self.assertEqual(saved_data, new_data)

    def test_merge_with_no_duplicates(self):
        """Test merging when no duplicates exist."""
        new_data = {
            "4.14": [
                {
                    "ocp_full_version": "4.14.1",
                    "gpu_operator_version": "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://example.com/job1",
                    "job_timestamp": "1712345678"
                }
            ]
        }
        existing_data = {
            "4.14": [
                {
                    "ocp_full_version": "4.14.2",
                    "gpu_operator_version": "24.3.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://example.com/job2",
                    "job_timestamp": "1712345679"
                }
            ]
        }

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        # Read the saved file and verify its contents
        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Both entries should be present
        self.assertEqual(len(saved_data["4.14"]), 2)
        self.assertTrue(any(item["gpu_operator_version"] == "23.9.0" for item in saved_data["4.14"]))
        self.assertTrue(any(item["gpu_operator_version"] == "24.3.0" for item in saved_data["4.14"]))

    def test_exact_duplicates(self):
        """Test handling of exact duplicates - they should not be added."""
        item = {
            "ocp_full_version": "4.14.1",
            "gpu_operator_version": "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        new_data = {"4.14": [item]}
        existing_data = {"4.14": [item.copy()]}

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Only one entry should be present (no duplicates)
        self.assertEqual(len(saved_data["4.14"]), 1)

    def test_different_ocp_keys(self):
        """Test merging data with different OCP keys."""
        new_data = {
            "4.14": [
                {
                    "ocp_full_version": "4.14.1",
                    "gpu_operator_version": "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://example.com/job1",
                    "job_timestamp": "1712345678"
                }
            ]
        }
        existing_data = {
            "4.13": [
                {
                    "ocp_full_version": "4.13.5",
                    "gpu_operator_version": "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://example.com/job2",
                    "job_timestamp": "1712345679"
                }
            ]
        }

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Both OCP keys should be present
        self.assertIn("4.14", saved_data)
        self.assertIn("4.13", saved_data)
        self.assertEqual(len(saved_data["4.14"]), 1)
        self.assertEqual(len(saved_data["4.13"]), 1)

    def test_partial_duplicates(self):
        """Test handling items that match in some fields but not all."""
        new_item = {
            "ocp_full_version": "4.14.1",
            "gpu_operator_version": "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        existing_item = {
            "ocp_full_version": "4.14.1",
            "gpu_operator_version": "23.9.0",
            "test_status": "FAILURE",  # Different test_status
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        new_data = {"4.14": [new_item]}
        existing_data = {"4.14": [existing_item]}

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Both entries should be present as they differ in test_status
        self.assertEqual(len(saved_data["4.14"]), 2)
        self.assertTrue(any(item["test_status"] == "SUCCESS" for item in saved_data["4.14"]))
        self.assertTrue(any(item["test_status"] == "FAILURE" for item in saved_data["4.14"]))

    def test_json_not_overwritten(self):
        """Test that merging new data does not overwrite existing data fields.
           Each field from the existing data should be preserved and new data should be appended.
        """
        existing_item = {
            "ocp_full_version": "4.14.1",
            "gpu_operator_version": "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }
        new_item = {
            "ocp_full_version": "4.14.1",  # Same OCP version key
            "gpu_operator_version": "23.9.0",
            "test_status": "FAILURE",  # New test_status, different from the existing item
            "prow_job_url": "https://example.com/job1-new",  # New prow_job_url
            "job_timestamp": "1712345680"  # New job_timestamp
        }
        new_data = {"4.14": [new_item]}
        existing_data = {"4.14": [existing_item]}

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Verify that both the existing item and new item are present and not overwritten
        self.assertEqual(len(saved_data["4.14"]), 2)

        # Check that the existing item's fields are preserved
        found_existing = any(
            item["test_status"] == "SUCCESS" and item["prow_job_url"] == "https://example.com/job1" and item["job_timestamp"] == "1712345678"
            for item in saved_data["4.14"]
        )
        self.assertTrue(found_existing)

        # Check that the new item's fields are saved
        found_new = any(
            item["test_status"] == "FAILURE" and item["prow_job_url"] == "https://example.com/job1-new" and item["job_timestamp"] == "1712345680"
            for item in saved_data["4.14"]
        )
        self.assertTrue(found_new)

    @mock.patch('json.dump')
    def test_empty_new_data(self, mock_json_dump):
        """Test with empty new_data."""
        new_data = {}
        existing_data = {
            "4.14": [
                {
                    "ocp_full_version": "4.14.1",
                    "gpu_operator_version": "23.9.0",
                    "test_status": "SUCCESS",
                    "prow_job_url": "https://example.com/job1",
                    "job_timestamp": "1712345678"
                }
            ]
        }

        merge_and_save_results(new_data, self.output_dir, self.test_file, existing_data)

        # Verify json.dump was called with the correct arguments
        mock_json_dump.assert_called_once()
        args, _ = mock_json_dump.call_args
        saved_data = args[0]

        # The existing data should remain unchanged
        self.assertEqual(saved_data, existing_data)


if __name__ == '__main__':
    unittest.main()
