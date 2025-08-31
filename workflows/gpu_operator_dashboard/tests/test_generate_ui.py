from unittest import TestCase
from datetime import datetime, timezone

from workflows.gpu_operator_dashboard.generate_ci_dashboard import (
    build_bundle_info, build_catalog_table_rows, has_valid_semantic_versions)
from workflows.gpu_operator_dashboard.fetch_ci_data import (
    OCP_FULL_VERSION, GPU_OPERATOR_VERSION)


class TestBuildBundleInfo(TestCase):
    def test_empty_bundle_results(self):
        """Test build_bundle_info with empty bundle_results."""
        bundle_html = build_bundle_info([])
        self.assertEqual(bundle_html, "")

    def test_sorting_by_timestamp(self):
        """Test that bundles are sorted by timestamp (newest first)."""
        bundle_results = [
            {
                "test_status": "SUCCESS",
                "job_timestamp": 1712000000,  # Oldest
                "prow_job_url": "https://example.com/job1"
            },
            {
                "test_status": "FAILURE",
                "job_timestamp": 1712100000,  # Middle
                "prow_job_url": "https://example.com/job2"
            },
            {
                "test_status": "SUCCESS",
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
                "test_status": "SUCCESS",
                "job_timestamp": 1712200000,
                "prow_job_url": "https://example.com/job1"
            },
            {
                "test_status": "FAILURE",
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
                "test_status": "SUCCESS",
                "job_timestamp": 1712200000,
                "prow_job_url": "https://example.com/job1"
            },
            {
                "test_status": "FAILURE",
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
                "test_status": "FAILURE",
                "job_timestamp": 1712100000,
                "prow_job_url": "https://example.com/job2"
            },
            {
                "test_status": "SUCCESS",
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
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/1",
                "job_timestamp": 100
            },
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "24.6.2",
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/1-dup",
                "job_timestamp": 200  # Latest for this GPU
            },
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "24.9.2",
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/2",
                "job_timestamp": 150
            },
            {
                OCP_FULL_VERSION: "4.14.48",
                GPU_OPERATOR_VERSION: "25.0.0",
                "test_status": "FAILURE",  # Not SUCCESS, should be excluded
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
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/job1-success",
                "job_timestamp": "1712345678"
            },
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": "FAILURE",
                "prow_job_url": "https://example.com/job1-failure",
                "job_timestamp": "1712345679"
            },
            # Combination 2: GPU version 23.8.0 has only failures - should show as failed
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.8.0",
                "test_status": "FAILURE",
                "prow_job_url": "https://example.com/job2-failure1",
                "job_timestamp": "1712345680"
            },
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "23.8.0",
                "test_status": "FAILURE",
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
                "test_status": "FAILURE",
                "prow_job_url": "https://example.com/old-failure",
                "job_timestamp": "1712345600"
            },
            # Newer success - this should be the one shown
            {
                OCP_FULL_VERSION: "4.13.5",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/new-success",
                "job_timestamp": "1712345700"
            },
            # Even newer failure - should be ignored since we have a success
            {
                OCP_FULL_VERSION: "4.13.5",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": "FAILURE",
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
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertTrue(has_valid_semantic_versions(valid_result))

    def test_gpu_version_with_suffix(self):
        """Test that GPU versions with suffix (like bundle) are handled correctly."""
        result_with_suffix = {
            OCP_FULL_VERSION: "4.13.5",
            GPU_OPERATOR_VERSION: "23.9.0(bundle)",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertTrue(has_valid_semantic_versions(result_with_suffix))

    def test_invalid_ocp_version(self):
        """Test that invalid OCP versions are rejected."""
        invalid_ocp_result = {
            OCP_FULL_VERSION: "4.14.invalid",
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(invalid_ocp_result))

    def test_invalid_gpu_version(self):
        """Test that invalid GPU operator versions are rejected."""
        invalid_gpu_result = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "invalid.version",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(invalid_gpu_result))

    def test_missing_versions(self):
        """Test that missing version fields are rejected."""
        # Missing OCP version
        missing_ocp = {
            GPU_OPERATOR_VERSION: "23.9.0",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }
        self.assertFalse(has_valid_semantic_versions(missing_ocp))

        # Missing GPU version
        missing_gpu = {
            OCP_FULL_VERSION: "4.14.1",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }
        self.assertFalse(has_valid_semantic_versions(missing_gpu))

    def test_empty_versions(self):
        """Test that empty version fields are rejected."""
        empty_versions = {
            OCP_FULL_VERSION: "",
            GPU_OPERATOR_VERSION: "",
            "test_status": "SUCCESS",
            "prow_job_url": "https://example.com/job1",
            "job_timestamp": "1712345678"
        }

        self.assertFalse(has_valid_semantic_versions(empty_versions))

    def test_master_version_rejected(self):
        """Test that 'master' GPU version is rejected for regular results."""
        master_version = {
            OCP_FULL_VERSION: "4.14.1",
            GPU_OPERATOR_VERSION: "master",
            "test_status": "SUCCESS",
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
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/valid",
                "job_timestamp": "1712345678"
            },
            # Invalid OCP version - should be filtered out before reaching build_catalog_table_rows
            {
                OCP_FULL_VERSION: "invalid.ocp",
                GPU_OPERATOR_VERSION: "23.9.0",
                "test_status": "SUCCESS",
                "prow_job_url": "https://example.com/invalid-ocp",
                "job_timestamp": "1712345679"
            },
            # Invalid GPU version - should be filtered out before reaching build_catalog_table_rows
            {
                OCP_FULL_VERSION: "4.14.1",
                GPU_OPERATOR_VERSION: "invalid.gpu",
                "test_status": "SUCCESS",
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


if __name__ == '__main__':
    from unittest import main
    main()
