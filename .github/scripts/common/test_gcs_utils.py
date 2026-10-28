"""Exercise GCS retries over real HTTP connections without external services."""

import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from unittest.mock import patch

import requests

from common import gcs_utils


class GCSRetryTests(unittest.TestCase):
    def setUp(self):
        self.responses = []
        self.paths = []
        test = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                test.paths.append(self.path)
                status, headers, body = test.responses.pop(0)
                self.send_response(status)
                for key, value in headers.items():
                    self.send_header(key, value)
                self.end_headers()
                self.wfile.write(body.encode())

            def log_message(self, *args):
                pass

        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_server)
        self.url = f"http://127.0.0.1:{self.server.server_port}"
        for name, value in (("GCS_API_BASE_URL", self.url),
                            ("_GCSWEB_API_URL", self.url),
                            ("_PROW_TOKEN", ""), ("_CURATED_PREFIX", "curated/")):
            patcher = patch.object(gcs_utils, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        sleeper = patch("urllib3.util.retry.time.sleep")
        self.sleep = sleeper.start()
        self.addCleanup(sleeper.stop)
        gcs_utils._files_cache.clear()
        self.addCleanup(gcs_utils._files_cache.clear)

    def stop_server(self):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()

    def test_artifact_download_recovers_from_rate_limit_on_both_backends(self):
        for token in ("", "test-token"):
            with self.subTest(gcsweb=bool(token)), patch.object(gcs_utils, "_PROW_TOKEN", token):
                self.paths.clear()
                self.responses = [(429, {"Retry-After": "7"}, "busy"), (200, {}, '{"result":"SUCCESS"}')]
                self.assertEqual(gcs_utils.fetch_gcs_file_content("logs/job/1/finished.json"),
                                 '{"result":"SUCCESS"}')
                self.assertEqual(len(self.paths), 2)
                self.assertEqual(self.paths[0], self.paths[1])
                self.sleep.assert_called_with(7)

    def test_json_listing_recovers_from_transient_server_errors(self):
        for status in (408, 500, 502, 503, 504):
            with self.subTest(status=status):
                self.paths.clear()
                self.responses = [(status, {}, "busy"), (200, {}, '{"items":[]}')]
                self.assertEqual(gcs_utils.http_get_json(self.url, params={"prefix": "logs/job/"}),
                                 {"items": []})
                self.assertEqual(len(self.paths), 2)
                self.assertEqual(self.paths[0], self.paths[1])

    def test_gcsweb_directory_listings_retry(self):
        prefix = "curated/logs/job/"
        body = f'<a href="/gcs/{gcs_utils.GCS_BUCKET}/{prefix}finished.json">file</a>'
        for recursive in (False, True):
            with self.subTest(recursive=recursive):
                self.paths.clear()
                self.responses = [(429, {}, "busy"), (200, {}, body)]
                if recursive:
                    self.assertEqual(gcs_utils._gcsweb_list_all_files(prefix), [prefix + "finished.json"])
                else:
                    self.assertEqual(gcs_utils.list_gcsweb_directory("logs/job/"), ([], ["finished.json"]))
                self.assertEqual(len(self.paths), 2)

    def test_persistent_rate_limit_raises_http_error_after_bounded_retries(self):
        self.responses = [(429, {}, "busy")] * 6
        with self.assertRaises(requests.HTTPError) as raised:
            gcs_utils.fetch_gcs_file_content("logs/job/1/finished.json")
        self.assertEqual(raised.exception.response.status_code, 429)
        self.assertEqual(len(self.paths), 6)
        self.assertEqual(self.responses, [])
        delays = [call.args[0] for call in self.sleep.call_args_list]
        self.assertEqual(len(delays), 4)
        for delay, minimum in zip(delays, (2, 4, 8, 16)):
            self.assertGreaterEqual(delay, minimum)
            self.assertLessEqual(delay, minimum + 0.5)

    def test_permanent_http_errors_are_not_retried(self):
        for status in (403, 404):
            with self.subTest(status=status):
                self.paths.clear()
                self.responses = [(status, {}, "unavailable")]
                with self.assertRaises(requests.HTTPError) as raised:
                    gcs_utils.fetch_gcs_file_content("logs/job/1/finished.json")
                self.assertEqual(raised.exception.response.status_code, status)
                self.assertEqual(len(self.paths), 1)
        self.sleep.assert_not_called()

    def test_curated_missing_file_keeps_its_distinct_error(self):
        self.responses = [(200, {"Content-Type": "text/html"}, "<title>GCS browser:</title>")]
        with patch.object(gcs_utils, "_PROW_TOKEN", "test-token"):
            with self.assertRaises(gcs_utils.GCSFileNotFoundError):
                gcs_utils.fetch_gcs_file_content("logs/job/1/finished.json")
        self.assertEqual(len(self.paths), 1)
        self.sleep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
