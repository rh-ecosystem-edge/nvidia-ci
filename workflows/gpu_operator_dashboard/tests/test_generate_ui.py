from unittest import TestCase
from unittest.mock import patch
from datetime import datetime, timezone

from workflows.gpu_operator_dashboard.generate_ci_dashboard import (
    build_bundle_info, build_catalog_table_rows, has_valid_semantic_versions, generate_test_matrix)
from workflows.gpu_operator_dashboard.fetch_ci_data import (
    OCP_FULL_VERSION, GPU_OPERATOR_VERSION, STATUS_ABORTED, STATUS_SUCCESS, STATUS_FAILURE)


class TestBuildBundleInfo(TestCase):
    def test_empty_bundle_results(self):
        """Test build_bundle_info with empty bundle_results."""
        bundle_html = build_bundle_info([])
        self.assertEqual(bundle_html, "")

    def test_sorting_by_timestamp(self):
        """Test that bundles are sorted by timestamp (newest first)."""
        bundle_results = [
            {
                "test_status": STATUS_SUCCESS,
                "job_timestamp": 1712000000,  # Oldest
                "prow_job_url": "https://example.com/job1"
            },
            {
                "test_status": STATUS_FAILURE,
                "job_timestamp": 1712100000,  # Middle
                "prow_job_url": "https://example.com/job2"
            },
            {
                "test_status": STATUS_SUCCESS,
                "job_timestamp": 1712200000,  # Newest
                "prow_job_url": "https://example.com/job3"
            }
        ]

        bundle_html = build_bundle_info(bundle_results)

        # The newest timestamp should be used for the "Last Bundle Job Date"
        newest_date = datetime.fromtimestamp(
            1712200000, timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        self.assertIn(
            f"Last Bundle Job Date:</strong> {newest_date}", bundle_html)

        # The order in the HTML should be newest to oldest
        # Check that the links appear in the correct order
        pos1 = bundle_html.find("https://example.com/job3")  # Newest
        pos2 = bundle_html.find("https://example.com/job2")  # Middle
        pos3 = bundle_html.find("https://example.com/job1")  # Oldest

        self.assertGreater(pos1, 0)  # Should be found
        self.assertGreater(pos2, pos1)  # Middle comes after newest
        self.assertGreater(pos3, pos2)  # Oldest comes after middle

    def test_html_structure(self):
        """Test the overall HTML structure and CSS classes."""
        bundle_results = [
            {
                "test_status": STATUS_SUCCESS,
                "job_timestamp": 1712200000,
                "prow_job_url": "https://example.com/job1"
            },
            {
                "test_status": STATUS_FAILURE,
                "job_timestamp": 1712100000,
                "prow_job_url": "https://example.com/job2"
            }
        ]

        bundle_html = build_bundle_info(bundle_results)

        # Check for required HTML elements and CSS classes
        self.assertIn('<div class="history-bar-inner history-bar-outer"', bundle_html)
        self.assertIn(
            '<div class=\'history-square history-success\'', bundle_html)
        self.assertIn(
            '<div class=\'history-square history-failure\'', bundle_html)
        self.assertIn(
            'onclick=\'window.open("https://example.com/job1", "_blank")\'', bundle_html)
        self.assertIn(
            'onclick=\'window.open("https://example.com/job2", "_blank")\'', bundle_html)

    def test_status_classes(self):
        """Test that different statuses get different CSS classes."""
        bundle_results = [
            {
                "test_status": STATUS_SUCCESS,
                "job_timestamp": 1712200000,
                "prow_job_url": "https://example.com/job1"
            },
            {
                "test_status": STATUS_FAILURE,
                "job_timestamp": 1712100000,
                "prow_job_url": "https://example.com/job2"
            },
            {
                "test_status": "UNKNOWN",  # Unknown status
                "job_timestamp": 1712000000,
                "prow_job_url": "https://example.com/job3"
            }
        ]

        bundle_html = build_bundle_info(bundle_results)

        # Verify CSS classes
        self.assertIn('history-square history-success', bundle_html)
        self.assertIn('history-square history-failure', bundle_html)
        # Unknown status uses aborted class
        self.assertIn('history-square history-aborted', bundle_html)

    def test_timestamp_formatting(self):
        """Test that timestamps are correctly formatted using the newest (leftmost) bundle."""
        bundle_results = [
            {
                "test_status": STATUS_FAILURE,
                "job_timestamp": 1712100000,
                "prow_job_url": "https://example.com/job2"
            },
            {
                "test_status": STATUS_SUCCESS,
                "job_timestamp": 1712200000,
                "prow_job_url": "https://example.com/job1"
            }
        ]
        # The newest (leftmost) bundle has job_timestamp 1712200000.
        expected_date = datetime.fromtimestamp(
            1712200000, timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        bundle_html = build_bundle_info(bundle_results)

        # Verify that the "Last Bundle Job Date" is derived from the newest bundle.
        self.assertIn(
            f'Last Bundle Job Date:</strong> {expected_date}', bundle_html)
        # Also, check that the tooltip span for the newest bundle is correctly formatted.
        self.assertIn('history-square-tooltip', bundle_html)
        self.assertIn(
            f"Status: SUCCESS | Timestamp: {expected_date}", bundle_html)

    def test_catalog_regular_table_integrity(self):
        """Test that the catalog-regular table:
           - Contains only entries with test_status SUCCESS
           - Has no duplication of (OCP, GPU) combinations,
             retaining only the entry with the latest job_timestamp.
        """
        # Create a list of regular results with potential duplicates.
        regular_results = [
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "24.6.2",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/1",
                "job_timestamp": 100
            },
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "24.6.2",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/1-dup",
                "job_timestamp": 200  # Latest for this GPU
            },
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "24.9.2",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/2",
                "job_timestamp": 150
            },
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "25.0.0",
                "test_status": STATUS_FAILURE,  # Not SUCCESS, should be excluded
                "prow_job_url": "https://example.com/3-fail",
                "job_timestamp": 300
            },
        ]
        # Mimic the filtering done in the UI generation:
        filtered_regular = [r for r in regular_results if r.get(
            "test_status") == "SUCCESS"]
        html = build_catalog_table_rows(filtered_regular)

        # Ensure that each GPU version appears only once in the HTML.
        self.assertEqual(html.count("24.6.2"), 1)
        self.assertEqual(html.count("24.9.2"), 1)
        # Verify that the deduplication logic selected the entry with the latest job_timestamp.
        self.assertIn('href="https://example.com/1-dup"', html)
        # Ensure that the entry with test_status FAILURE (gpu "25.0.0") does not appear.
        self.assertNotIn("25.0.0", html)

    def test_failed_only_combinations_displayed(self):
        """Test that combinations with only failed results are displayed as failed."""
        regular_results = [
            # Combination 1: GPU version 23.9.0 has both success and failure - should show as success
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/job1-success",
                "job_timestamp": "1712345678"
            },
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_FAILURE,
                "prow_job_url": "https://example.com/job1-failure",
                "job_timestamp": "1712345679"
            },
            # Combination 2: GPU version 23.8.0 has only failures - should show as failed
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.8.0",
                "test_status": STATUS_FAILURE,
                "prow_job_url": "https://example.com/job2-failure1",
                "job_timestamp": "1712345680"
            },
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.8.0",
                "test_status": STATUS_FAILURE,
                "prow_job_url": "https://example.com/job2-failure2",
                "job_timestamp": "1712345681"
            }
        ]

        html = build_catalog_table_rows(regular_results)

        # Should contain both GPU versions
        self.assertIn("23.9.0", html)
        self.assertIn("23.8.0", html)

        # 23.9.0 should be marked as successful (has success-link class)
        self.assertIn('class="success-link">23.9.0</a>', html)

        # 23.8.0 should be marked as failed (has failed-link class and (Failed) suffix)
        self.assertIn('class="failed-link">23.8.0 (Failed)</a>', html)

        # Should link to the correct URLs (latest success for 23.9.0, latest failure for 23.8.0)
        self.assertIn('href="https://example.com/job1-success"', html)
        self.assertIn('href="https://example.com/job2-failure2"', html)

    def test_mixed_success_failure_priority(self):
        """Test that if any test succeeds for a combination, it's marked as successful."""
        regular_results = [
            # Older failure
            {
                OCP_FULL_VERSION: "4.13.5",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_FAILURE,
                "prow_job_url": "https://example.com/old-failure",
                "job_timestamp": "1712345600"
            },
            # Newer success - this should be the one shown
            {
                OCP_FULL_VERSION: "4.13.5",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/new-success",
                "job_timestamp": "1712345700"
            },
            # Even newer failure - should be ignored since we have a success
            {
                OCP_FULL_VERSION: "4.13.5",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_FAILURE,
                "prow_job_url": "https://example.com/newer-failure",
                "job_timestamp": "1712345800"
            }
        ]

        html = build_catalog_table_rows(regular_results)

        # Should show as successful and link to the success URL
        self.assertIn('class="success-link">23.9.0</a>', html)
        self.assertIn('href="https://example.com/new-success"', html)

        # Should not contain the failed styling or failure URLs
        self.assertNotIn('23.9.0 (Failed)', html)
        self.assertNotIn('class="failed-link"', html)


class TestSemanticVersionValidation(TestCase):
    """Test cases for semantic version validation."""

    def test_valid_semantic_versions(self):
        """Test that valid semantic versions are accepted."""
        valid_result = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertTrue(has_valid_semantic_versions(valid_result))

    def test_gpu_version_with_suffix(self):
        """Test that GPU versions with suffix (like bundle) are handled correctly."""
        result_with_suffix = {
            OCP_FULL_VERSION: "4.13.5",
            GPU_OPERATOR_VERSION: "23.9.0(bundle)",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertTrue(has_valid_semantic_versions(result_with_suffix))

    def test_invalid_ocp_version(self):
        """Test that invalid OCP versions are rejected."""
        invalid_ocp_result = {
            OCP_FULL_VERSION: "4.14.invalid",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(invalid_ocp_result))

    def test_invalid_gpu_version(self):
        """Test that invalid GPU operator versions are rejected."""
        invalid_gpu_result = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "invalid.version",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(invalid_gpu_result))

    def test_missing_versions(self):
        """Test that missing version fields are rejected."""
        # Missing OCP version
        missing_ocp = {
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }
        self.assertFalse(has_valid_semantic_versions(missing_ocp))

        # Missing GPU version
        missing_gpu = {
            OCP_FULL_VERSION: "4.14.1",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }
        self.assertFalse(has_valid_semantic_versions(missing_gpu))

    def test_empty_versions(self):
        """Test that empty version fields are rejected."""
        empty_versions = {
            OCP_FULL_VERSION: "",
            GPU_OPERATOR_VERSION: "",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(empty_versions))

    def test_master_version_rejected(self):
        """Test that 'master' GPU version is rejected for regular results."""
        master_version = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "master",
            "test_status": STATUS_SUCCESS,
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(master_version))

    def test_integration_with_table_rows(self):
        """Test that invalid semantic versions are filtered out in the full processing flow."""
        # This test demonstrates the integration between semantic version validation
        # and the table generation
        regular_results = [
            # Valid entry - should appear
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/valid",
                "job_timestamp": "1712345678"
            },
            # Invalid OCP version - should be filtered out before reaching build_catalog_table_rows
            {
                OCP_FULL_VERSION: "invalid.ocp",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/invalid-ocp",
                "job_timestamp": "1712345679"
            },
            # Invalid GPU version - should be filtered out before reaching build_catalog_table_rows
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "invalid.gpu",
                "test_status": STATUS_SUCCESS,
                "prow_job_url": "https://example.com/invalid-gpu",
                "job_timestamp": "1712345680"
            }
        ]

        # Filter results using the same logic as the main code
        filtered_results = [r for r in regular_results if has_valid_semantic_versions(r)]

        # Only the valid entry should remain
        self.assertEqual(len(filtered_results), 1)
        self.assertEqual(filtered_results[0]["prow_job_url"], "https://example.com/valid")

        # Generate HTML table with filtered results
        html = build_catalog_table_rows(filtered_results)

        # Should contain the valid entry
        self.assertIn("23.9.0", html)
        self.assertIn("4.14.1", html)
        self.assertIn("https://example.com/valid", html)

        # Should not contain the invalid entries
        self.assertNotIn("invalid-ocp", html)
        self.assertNotIn("invalid-gpu", html)


class TestGenerateTestMatrix(TestCase):
    """Test cases for the main generate_test_matrix function with separated data structure."""

    @patch('workflows.gpu_operator_dashboard.generate_ci_dashboard.load_template')
    def test_generate_test_matrix_with_separated_structure(self, mock_load_template):
        """Test that generate_test_matrix works with the new separated bundle_tests and release_tests structure."""
        # Mock templates
        mock_load_template.side_effect = [
            '<html><head><title>Test Matrix</title></head><body>',  # header.html
            '<div id="ocp-{ocp_key}"><h2>{ocp_key}</h2>{notes}{table_rows}{bundle_info}</div>',  # main_table.html
            '<div class="footer">Last updated: {LAST_UPDATED}</div></body></html>'  # footer.html
        ]

        # Test data with new separated structure
        ocp_data = {
            '4.14': {
                'notes': ['Test note for 4.14'],
                'bundle_tests': [
                    {
                        OCP_FULL_VERSION: '4.14.1',
                        GPU_OPERATOR_VERSION: 'master',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/bundle-job1',
                        'job_timestamp': '1712345678'
                    },
                    {
                        OCP_FULL_VERSION: '4.14.1',
                        GPU_OPERATOR_VERSION: 'master',
                        'test_status': STATUS_FAILURE,
                        'prow_job_url': 'https://example.com/bundle-job2',
                        'job_timestamp': '1712345680'
                    }
                ],
                'release_tests': [
                    {
                        OCP_FULL_VERSION: '4.14.1',
                        GPU_OPERATOR_VERSION: '23.9.0',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/release-job1',
                        'job_timestamp': '1712345679'
                    },
                    {
                        OCP_FULL_VERSION: '4.14.2',
                        GPU_OPERATOR_VERSION: '24.3.0',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/release-job2',
                        'job_timestamp': '1712345681'
                    }
                ]
            },
            '4.13': {
                'notes': [],
                'bundle_tests': [],
                'release_tests': [
                    {
                        OCP_FULL_VERSION: '4.13.5',
                        GPU_OPERATOR_VERSION: '23.6.0',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/release-job3',
                        'job_timestamp': '1712345682'
                    }
                ]
            }
        }

        # Generate HTML
        html_result = generate_test_matrix(ocp_data)

        # Verify that the function ran without errors and generated HTML
        self.assertIsInstance(html_result, str)
        self.assertGreater(len(html_result), 100)  # Should generate substantial HTML

        # Verify basic HTML structure
        self.assertIn('<html>', html_result)
        self.assertIn('</html>', html_result)
        self.assertIn('<title>Test Matrix</title>', html_result)

        # Verify OCP versions are processed (sorted in reverse order)
        self.assertIn('4.14', html_result)
        self.assertIn('4.13', html_result)

        # Verify release test results appear in the table
        self.assertIn('23.9.0', html_result)
        self.assertIn('24.3.0', html_result)
        self.assertIn('23.6.0', html_result)

        # Verify bundle test results appear in bundle info section
        self.assertIn('https://example.com/bundle-job1', html_result)
        self.assertIn('https://example.com/bundle-job2', html_result)

        # Verify release test URLs appear
        self.assertIn('https://example.com/release-job1', html_result)
        self.assertIn('https://example.com/release-job2', html_result)
        self.assertIn('https://example.com/release-job3', html_result)

        # Verify notes appear
        self.assertIn('Test note for 4.14', html_result)

        # Verify last updated timestamp is added
        self.assertIn('Last updated:', html_result)

    @patch('workflows.gpu_operator_dashboard.generate_ci_dashboard.load_template')
    def test_generate_test_matrix_with_empty_sections(self, mock_load_template):
        """Test generate_test_matrix with empty bundle_tests and release_tests sections."""
        # Mock templates
        mock_load_template.side_effect = [
            '<html><body>',  # header.html
            '<div id="ocp-{ocp_key}">{table_rows}{bundle_info}</div>',  # main_table.html
            '</body></html>'  # footer.html
        ]

        # Test data with empty sections
        ocp_data = {
            '4.15': {
                'notes': [],
                'bundle_tests': [],  # Empty
                'release_tests': []  # Empty
            }
        }

        # Should not raise an error
        html_result = generate_test_matrix(ocp_data)

        # Should generate basic HTML structure even with no data
        self.assertIsInstance(html_result, str)
        self.assertIn('<html>', html_result)
        self.assertIn('4.15', html_result)

    @patch('workflows.gpu_operator_dashboard.generate_ci_dashboard.load_template')
    def test_generate_test_matrix_filters_invalid_versions(self, mock_load_template):
        """Test that generate_test_matrix properly filters out invalid semantic versions from release tests."""
        # Mock templates
        mock_load_template.side_effect = [
            '<html><body>',  # header.html
            '<div>{table_rows}</div>',  # main_table.html
            '</body></html>'  # footer.html
        ]

        # Test data with invalid versions that should be filtered out
        ocp_data = {
            '4.14': {
                'notes': [],
                'bundle_tests': [],
                'release_tests': [
                    # Valid entry - should appear
                    {
                        OCP_FULL_VERSION: '4.14.1',
                        GPU_OPERATOR_VERSION: '23.9.0',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/valid-job',
                        'job_timestamp': '1712345678'
                    },
                    # Invalid OCP version - should be filtered out
                    {
                        OCP_FULL_VERSION: 'invalid.ocp',
                        GPU_OPERATOR_VERSION: '23.9.0',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/invalid-ocp-job',
                        'job_timestamp': '1712345679'
                    },
                    # Invalid GPU version - should be filtered out
                    {
                        OCP_FULL_VERSION: '4.14.1',
                        GPU_OPERATOR_VERSION: 'invalid.gpu',
                        'test_status': STATUS_SUCCESS,
                        'prow_job_url': 'https://example.com/invalid-gpu-job',
                        'job_timestamp': '1712345680'
                    },
                    # ABORTED status - should be filtered out
                    {
                        OCP_FULL_VERSION: '4.14.1',
                        GPU_OPERATOR_VERSION: '23.8.0',
                        'test_status': STATUS_ABORTED,
                        'prow_job_url': 'https://example.com/aborted-job',
                        'job_timestamp': '1712345681'
                    }
                ]
            }
        }

        html_result = generate_test_matrix(ocp_data)

        # Should contain the valid entry
        self.assertIn('23.9.0', html_result)
        self.assertIn('https://example.com/valid-job', html_result)

        # Should not contain the invalid entries
        self.assertNotIn('invalid-ocp-job', html_result)
        self.assertNotIn('invalid-gpu-job', html_result)
        self.assertNotIn('aborted-job', html_result)
        self.assertNotIn('23.8.0', html_result)


if __name__ == '__main__':
    from unittest import main
    main()
