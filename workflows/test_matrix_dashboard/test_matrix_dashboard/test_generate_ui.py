import unittest
from datetime import datetime, timezone
from unittest import mock

from workflows.test_matrix_dashboard.generate_test_matrix_ui import build_bundle_info, build_table_rows


class TestBuildBundleInfo(unittest.TestCase):
    def test_empty_bundle_results(self):
        """Test build_bundle_info with empty bundle_results."""
        bundle_html = build_bundle_info([])
        self.assertEqual(bundle_html, "")

    def test_sorting_by_timestamp(self):
        """Test that bundles are sorted by timestamp (newest first)."""
        bundle_results = [
            {
                "status": "SUCCESS",
                "timestamp": 1712000000,  # Oldest
                "link": "https://example.com/job1"
            },
            {
                "status": "FAILURE",
                "timestamp": 1712100000,  # Middle
                "link": "https://example.com/job2"
            },
            {
                "status": "SUCCESS",
                "timestamp": 1712200000,  # Newest
                "link": "https://example.com/job3"
            }
        ]

        bundle_html = build_bundle_info(bundle_results)

        # The newest timestamp should be used for the "Last Bundle Job Date"
        newest_date = datetime.fromtimestamp(1712200000, timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        self.assertIn(f"Last Bundle Job Date:</strong> {newest_date}", bundle_html)

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
                "status": "SUCCESS",
                "timestamp": 1712200000,
                "link": "https://example.com/job1"
            },
            {
                "status": "FAILURE",
                "timestamp": 1712100000,
                "link": "https://example.com/job2"
            }
        ]

        bundle_html = build_bundle_info(bundle_results)

        # Check for required HTML elements and CSS classes
        self.assertIn('<div class="history-bar"', bundle_html)
        self.assertIn('<div class=\'history-square history-success\'', bundle_html)
        self.assertIn('<div class=\'history-square history-failure\'', bundle_html)
        self.assertIn('onclick=\'window.open("https://example.com/job1", "_blank")\'', bundle_html)
        self.assertIn('onclick=\'window.open("https://example.com/job2", "_blank")\'', bundle_html)

    def test_status_classes(self):
        """Test that different statuses get different CSS classes."""
        bundle_results = [
            {
                "status": "SUCCESS",
                "timestamp": 1712200000,
                "link": "https://example.com/job1"
            },
            {
                "status": "FAILURE",
                "timestamp": 1712100000,
                "link": "https://example.com/job2"
            },
            {
                "status": "UNKNOWN",  # Unknown status
                "timestamp": 1712000000,
                "link": "https://example.com/job3"
            }
        ]

        bundle_html = build_bundle_info(bundle_results)

        # Verify CSS classes
        self.assertIn('history-square history-success', bundle_html)
        self.assertIn('history-square history-failure', bundle_html)
        self.assertIn('history-square history-aborted', bundle_html)  # Unknown status uses aborted class

    def test_timestamp_formatting(self):
        """Test that timestamps are correctly formatted using the newest (leftmost) bundle."""
        bundle_results = [
            {
                "status": "FAILURE",
                "timestamp": 1712100000,
                "link": "https://example.com/job2"
            },
            {
                "status": "SUCCESS",
                "timestamp": 1712200000,
                "link": "https://example.com/job1"
            }
        ]
        # The newest (leftmost) bundle has timestamp 1712200000.
        expected_date = datetime.fromtimestamp(1712200000, timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        bundle_html = build_bundle_info(bundle_results)

        # Verify that the "Last Bundle Job Date" is derived from the newest bundle.
        self.assertIn(f'Last Bundle Job Date:</strong> {expected_date}', bundle_html)
        # Also, check that the title attribute for the newest bundle is correctly formatted.
        self.assertIn(f"title='Status: SUCCESS | Timestamp: {expected_date}'", bundle_html)

    def test_catalog_regular_table_integrity(self):
        """Verify that the catalog-regular table is correctly filtered and deduplicated.
        
        This test ensures that the generated HTML contains only entries with a "SUCCESS" status and that duplicate entries for the same (OCP, GPU) pair are consolidated by retaining only the entry with the latest timestamp. Entries with statuses other than "SUCCESS" are omitted.
        """
        # Create a list of regular results with potential duplicates.
        regular_results = [
            {
                "ocp": "4.14.48",
                "gpu": "24.6.2",
                "status": "SUCCESS",
                "link": "https://example.com/1",
                "timestamp": 100
            },
            {
                "ocp": "4.14.48",
                "gpu": "24.6.2",
                "status": "SUCCESS",
                "link": "https://example.com/1-dup",
                "timestamp": 200  # Latest for this GPU
            },
            {
                "ocp": "4.14.48",
                "gpu": "24.9.2",
                "status": "SUCCESS",
                "link": "https://example.com/2",
                "timestamp": 150
            },
            {
                "ocp": "4.14.48",
                "gpu": "25.0.0",
                "status": "FAILURE",  # Not SUCCESS, should be excluded
                "link": "https://example.com/3-fail",
                "timestamp": 300
            },
        ]
        # Mimic the filtering done in the UI generation:
        filtered_regular = [r for r in regular_results if r.get("status") == "SUCCESS"]
        html = build_table_rows(filtered_regular)

        # Ensure that each GPU version appears only once in the HTML.
        self.assertEqual(html.count("24.6.2"), 1)
        self.assertEqual(html.count("24.9.2"), 1)
        # Verify that the deduplication logic selected the entry with the latest timestamp.
        self.assertIn('href="https://example.com/1-dup"', html)
        # Ensure that the entry with status FAILURE (gpu "25.0.0") does not appear.
        self.assertNotIn("25.0.0", html)


if __name__ == '__main__':
    unittest.main()