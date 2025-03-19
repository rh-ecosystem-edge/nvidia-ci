#!/usr/bin/env python

import unittest
from unittest.mock import patch, MagicMock
from requests.exceptions import RequestException

# Import the module to test
import sys
import os
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from workflows.openshift import fetch_ocp_versions, RELEASE_URL_API


class TestOpenShift(unittest.TestCase):
    """Test cases for workflows/openshift.py functions."""

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_basic(self, mock_get, mock_settings):
        """Test basic functionality of fetch_ocp_versions."""
        # Mock settings
        mock_settings.ignored_versions = "x^"  # Regex that matches nothing
        mock_settings.request_timeout_sec = 30

        # Mock API response
        mock_response = MagicMock()
        mock_response.json.return_value = {'4-stable': ['4.10.1', '4.10.2', '4.11.0', '4.12.3']}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        # Call the function
        result = fetch_ocp_versions()

        # Verify the results
        expected = {
            '4.10': '4.10.2',  # Highest patch for 4.10
            '4.11': '4.11.0',  # Only patch for 4.11
            '4.12': '4.12.3',  # Only patch for 4.12
        }
        self.assertEqual(result, expected)

        # Verify the correct URL was called with timeout
        mock_get.assert_called_once_with(RELEASE_URL_API, timeout=30)
        mock_response.raise_for_status.assert_called_once()

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_ignored(self, mock_get, mock_settings):
        """Test that ignored versions are correctly filtered out."""
        # Mock settings with a regex to ignore 4.10 and 4.12
        mock_settings.ignored_versions = "4.10|4.12"
        mock_settings.request_timeout_sec = 30

        # Mock API response
        mock_response = MagicMock()
        mock_response.json.return_value = {'4-stable': ['4.10.1', '4.10.2', '4.11.0', '4.12.3']}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        # Call the function
        result = fetch_ocp_versions()

        # Verify only the non-ignored version is included
        expected = {
            '4.11': '4.11.0',  # Only 4.11 should remain
        }
        self.assertEqual(result, expected)

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_highest_patch(self, mock_get, mock_settings):
        """Test that highest patch version is selected for each minor version."""
        # Mock settings
        mock_settings.ignored_versions = "x^"  # Regex that matches nothing
        mock_settings.request_timeout_sec = 30

        # Mock API response with multiple patch versions for the same minor version
        mock_response = MagicMock()
        mock_response.json.return_value = {'4-stable': [
            '4.10.0', '4.10.1', '4.10.2', '4.10.1-rc.3',
            '4.11.5', '4.11.3', '4.11.8', '4.11.4'
        ]}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        # Call the function
        result = fetch_ocp_versions()

        # Verify the highest patch version is selected for each minor version
        expected = {
            '4.10': '4.10.2',     # Highest patch for 4.10
            '4.11': '4.11.8',     # Highest patch for 4.11
        }
        self.assertEqual(result, expected)

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_empty_response(self, mock_get, mock_settings):
        """Test behavior when API returns an empty list of versions."""
        # Mock settings
        mock_settings.ignored_versions = "x^"
        mock_settings.request_timeout_sec = 30

        # Mock empty API response
        mock_response = MagicMock()
        mock_response.json.return_value = {'4-stable': []}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        # Call the function
        result = fetch_ocp_versions()

        # Verify the result is an empty dictionary
        self.assertEqual(result, {})

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_api_error(self, mock_get, mock_settings):
        """Test error handling when API request fails."""
        # Mock settings
        mock_settings.ignored_versions = "x^"
        mock_settings.request_timeout_sec = 30

        # Mock API error
        mock_response = MagicMock()
        mock_response.raise_for_status.side_effect = RequestException("API error")
        mock_get.return_value = mock_response

        # Verify the exception is raised
        with self.assertRaises(RequestException):
            fetch_ocp_versions()

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_invalid_response(self, mock_get, mock_settings):
        """Test behavior when API returns an invalid response structure."""
        # Mock settings
        mock_settings.ignored_versions = "x^"
        mock_settings.request_timeout_sec = 30

        # Mock invalid API response (missing 4-stable key)
        mock_response = MagicMock()
        mock_response.json.return_value = {'some-other-key': []}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        # Verify the exception is raised
        with self.assertRaises(KeyError):
            fetch_ocp_versions()

    @patch('workflows.openshift.settings')
    @patch('workflows.openshift.requests.get')
    def test_fetch_ocp_versions_invalid_semver(self, mock_get, mock_settings):
        """Test behavior when API returns invalid semver format."""
        # Mock settings
        mock_settings.ignored_versions = "x^"
        mock_settings.request_timeout_sec = 30

        # Mock API response with invalid semver
        mock_response = MagicMock()
        mock_response.json.return_value = {'4-stable': ['4.10.1', 'not-a-semver', '4.11.0']}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response

        # Call the function - it should skip the invalid version
        with self.assertRaises(ValueError):
            fetch_ocp_versions()


if __name__ == '__main__':
    unittest.main()