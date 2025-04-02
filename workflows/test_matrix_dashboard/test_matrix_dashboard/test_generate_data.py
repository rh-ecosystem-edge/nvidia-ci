import json
import os
import tempfile
import unittest
from typing import Dict, List, Any
from unittest import mock

from workflows.test_matrix_dashboard.generate_test_matrix_data import save_to_json

# Testing final logic of generate_test_matrix_data.py which stores the JSON test data
class TestSaveToJson(unittest.TestCase):
    def setUp(self):
        # Create a temporary directory for test files
        """
        Initialize temporary test environment.
        
        Creates a temporary directory for storing test files and sets the output
        directory and default test file name for JSON data tests.
        """
        self.temp_dir = tempfile.TemporaryDirectory()
        self.output_dir = self.temp_dir.name
        self.test_file = "test_data.json"

    def tearDown(self):
        # Clean up the temporary directory
        """
        Clean up temporary directory after test execution.
        
        This method is automatically called after each test to release resources
        by removing the temporary directory created during test setup.
        """
        self.temp_dir.cleanup()

    def test_save_new_data_to_empty_existing(self):
        """Test saving new data when existing_data is empty."""
        new_data = {
            "4.14": [
                {
                    "ocp": "4.14.1",
                    "gpu": "23.9.0",
                    "status": "SUCCESS",
                    "link": "https://example.com/job1",
                    "timestamp": "1712345678"
                }
            ]
        }
        existing_data = {}

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

        # Read the saved file and verify its contents
        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        self.assertEqual(saved_data, new_data)

    def test_merge_with_no_duplicates(self):
        """
        Verifies that merging new and existing JSON data without duplicates retains all entries.
        
        This test merges a set of new data into pre-existing data for the same version key and confirms 
        that the final saved JSON file includes both distinct entries. It validates that the merge operation 
        adds the new unique entry without removing any of the existing ones.
        """
        new_data = {
            "4.14": [
                {
                    "ocp": "4.14.1",
                    "gpu": "23.9.0",
                    "status": "SUCCESS",
                    "link": "https://example.com/job1",
                    "timestamp": "1712345678"
                }
            ]
        }
        existing_data = {
            "4.14": [
                {
                    "ocp": "4.14.2",
                    "gpu": "24.3.0",
                    "status": "SUCCESS",
                    "link": "https://example.com/job2",
                    "timestamp": "1712345679"
                }
            ]
        }

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

        # Read the saved file and verify its contents
        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Both entries should be present
        self.assertEqual(len(saved_data["4.14"]), 2)
        self.assertTrue(any(item["gpu"] == "23.9.0" for item in saved_data["4.14"]))
        self.assertTrue(any(item["gpu"] == "24.3.0" for item in saved_data["4.14"]))

    def test_exact_duplicates(self):
        """Test handling of exact duplicates - they should not be added."""
        item = {
            "ocp": "4.14.1",
            "gpu": "23.9.0",
            "status": "SUCCESS",
            "link": "https://example.com/job1",
            "timestamp": "1712345678"
        }

        new_data = {"4.14": [item]}
        existing_data = {"4.14": [item.copy()]}

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Only one entry should be present (no duplicates)
        self.assertEqual(len(saved_data["4.14"]), 1)

    def test_different_ocp_keys(self):
        """Test merging data with different OCP keys."""
        new_data = {
            "4.14": [
                {
                    "ocp": "4.14.1",
                    "gpu": "23.9.0",
                    "status": "SUCCESS",
                    "link": "https://example.com/job1",
                    "timestamp": "1712345678"
                }
            ]
        }
        existing_data = {
            "4.13": [
                {
                    "ocp": "4.13.5",
                    "gpu": "23.9.0",
                    "status": "SUCCESS",
                    "link": "https://example.com/job2",
                    "timestamp": "1712345679"
                }
            ]
        }

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

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
            "ocp": "4.14.1",
            "gpu": "23.9.0",
            "status": "SUCCESS",
            "link": "https://example.com/job1",
            "timestamp": "1712345678"
        }

        existing_item = {
            "ocp": "4.14.1",
            "gpu": "23.9.0",
            "status": "FAILURE",  # Different status
            "link": "https://example.com/job1",
            "timestamp": "1712345678"
        }

        new_data = {"4.14": [new_item]}
        existing_data = {"4.14": [existing_item]}

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Both entries should be present as they differ in status
        self.assertEqual(len(saved_data["4.14"]), 2)
        self.assertTrue(any(item["status"] == "SUCCESS" for item in saved_data["4.14"]))
        self.assertTrue(any(item["status"] == "FAILURE" for item in saved_data["4.14"]))

    def test_json_not_overwritten(self):
        """Test that merging new data does not overwrite existing data fields.
           Each field from the existing data should be preserved and new data should be appended.
        """
        existing_item = {
            "ocp": "4.14.1",
            "gpu": "23.9.0",
            "status": "SUCCESS",
            "link": "https://example.com/job1",
            "timestamp": "1712345678"
        }
        new_item = {
            "ocp": "4.14.1",  # Same OCP version key
            "gpu": "23.9.0",
            "status": "FAILURE",  # New status, different from the existing item
            "link": "https://example.com/job1-new",  # New link
            "timestamp": "1712345680"  # New timestamp
        }
        new_data = {"4.14": [new_item]}
        existing_data = {"4.14": [existing_item]}

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

        with open(os.path.join(self.output_dir, self.test_file), 'r') as f:
            saved_data = json.load(f)

        # Verify that both the existing item and new item are present and not overwritten
        self.assertEqual(len(saved_data["4.14"]), 2)

        # Check that the existing item's fields are preserved
        found_existing = any(
            item["status"] == "SUCCESS" and item["link"] == "https://example.com/job1" and item["timestamp"] == "1712345678"
            for item in saved_data["4.14"]
        )
        self.assertTrue(found_existing)

        # Check that the new item's fields are saved
        found_new = any(
            item["status"] == "FAILURE" and item["link"] == "https://example.com/job1-new" and item["timestamp"] == "1712345680"
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
                    "ocp": "4.14.1",
                    "gpu": "23.9.0",
                    "status": "SUCCESS",
                    "link": "https://example.com/job1",
                    "timestamp": "1712345678"
                }
            ]
        }

        save_to_json(new_data, self.output_dir, self.test_file, existing_data)

        # Verify json.dump was called with the correct arguments
        mock_json_dump.assert_called_once()
        args, _ = mock_json_dump.call_args
        saved_data = args[0]

        # The existing data should remain unchanged
        self.assertEqual(saved_data, existing_data)


if __name__ == '__main__':
    unittest.main()