"""Tests for the local and GCS report contract."""

import copy
import json
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from nno_pair_report import collect, render


def sample_pair(pair_id, nno, driver, status, *, ofed=""):
    checks = {"nno_csv": "passed", "driver_image": "passed",
              "gpudirect_rdma_write": "failed" if status == "failed" else "passed"}
    return {
        "id": pair_id, "mode": "standard", "status": status,
        "nno_requested": nno, "nno_observed": nno,
        "driver_requested": {"version": driver, "image": f"example.invalid/ofed:{driver}"},
        "driver_observed": {"version": driver, "image": f"example.invalid/ofed:{driver}",
                            "ofed_version": ofed},
        "checks": checks,
        "metrics": {"bandwidth_gbps": 8.4 if status == "failed" else 73.1,
                    "min_bandwidth_gbps": 10},
    }


def write_sample_manifests(root):
    """Create small synthetic Prow artifacts in a temporary test directory."""
    pr_job = "pull-ci-example"
    periodic_job = "periodic-ci-example"
    doca = "doca3.5.0-26.07-0.7.7.0-0"
    specs = [
        (f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/698/{pr_job}/10000/artifacts/nno-pairs.json",
         "presubmit", pr_job, "10000", "2026-09-23T06:00:00Z", "4.21.44", "success",
         [sample_pair("pr-old-pass", "25.7.0", "25.01-0.6.0.0-0", "passed")]),
        (f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/{pr_job}/10001/artifacts/nno-pairs.json",
         "presubmit", pr_job, "10001", "2026-09-24T06:00:00Z", "4.22.14", "success",
         [sample_pair("pr-pass", "25.10.0", "25.04-0.6.1.0-0", "passed"),
          sample_pair("pr-fail", "25.10.1", "26.01-0.7.0.0-0", "failed",
                      ofed="OFED-internal-26.01-0.7.0")]),
        (f"logs/{periodic_job}/10002/artifacts/nno-pairs.json",
         "periodic", periodic_job, "10002", "2026-09-25T06:00:00Z", "4.22.14", "failure",
         [sample_pair("periodic-fail", "25.10.0", "25.04-0.6.1.0-0", "failed"),
          sample_pair("periodic-pass", "26.1.0", doca, "passed")]),
        (f"logs/{periodic_job}/10003/artifacts/nno-pairs.json",
         "periodic", periodic_job, "10003", "2026-09-26T06:00:00Z", "4.22.14", "aborted", []),
        (f"logs/{periodic_job}/10004/artifacts/nno-pairs.json",
         "periodic", periodic_job, "10004", "2026-09-27T06:00:00Z", "4.22.14", "success",
         [sample_pair("periodic-new-pass", "26.1.0", "26.04-0.7.1.0-0", "passed")]),
    ]
    for relative, kind, job, build, started, ocp, status, pairs in specs:
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "schema_version": 1,
            "run": {"kind": kind, "job_name": job, "build_id": build,
                    "started_at": started, "ocp_version": ocp, "status": status,
                    "planned_modes": ["standard"]},
            "pairs": pairs,
        }))


class SampleArtifacts(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls._temporary_artifacts = tempfile.TemporaryDirectory()
        cls.fixtures = Path(cls._temporary_artifacts.name)
        write_sample_manifests(cls.fixtures)

    @classmethod
    def tearDownClass(cls):
        cls._temporary_artifacts.cleanup()
        super().tearDownClass()


class CollectTests(SampleArtifacts):
    def test_pr_and_periodic_paths(self):
        pr = collect.source_identity("pr-logs/pull/openshift_release/85277/rehearse-85277-pull-ci-example/123/artifacts/nno-pairs.json")
        periodic = collect.source_identity("logs/periodic-ci-example/456/artifacts/nno-pairs.json")
        self.assertEqual((pr["kind"], pr["pr"], pr["build_id"]), ("presubmit", "85277", "123"))
        self.assertEqual((periodic["kind"], periodic["job_name"], periodic["build_id"]), ("periodic", "periodic-ci-example", "456"))

    def test_local_sample_has_multiple_pairs_per_build(self):
        builds = collect.load_local(self.fixtures)
        self.assertEqual(len(builds), 5)
        self.assertEqual(sum(len(build["pairs"]) for build in builds), 6)
        self.assertEqual({build["run"]["kind"] for build in builds}, {"presubmit", "periodic"})
        self.assertEqual({build["run"]["ocp_version"] for build in builds}, {"4.21.44", "4.22.14"})
        self.assertEqual(len([build for build in builds if not build["pairs"]]), 1)
        self.assertIn("doca3.5.0-26.07-0.7.7.0-0", {pair["driver_requested"]["version"] for build in builds for pair in build["pairs"]})
        observed = [pair["driver_observed"]["ofed_version"] for build in builds for pair in build["pairs"] if pair["driver_observed"].get("ofed_version")]
        self.assertIn("OFED-internal-26.01-0.7.0", observed)

    def test_explicit_doca_version_is_preserved(self):
        source = next(self.fixtures.glob("pr-logs/**/nno-pairs.json"))
        data = json.loads(source.read_text())
        data["pairs"][0]["driver_requested"]["doca_version"] = "3.5.0"
        normalized = collect.normalize_manifest(data, source.relative_to(self.fixtures).as_posix())
        self.assertEqual(normalized["pairs"][0]["driver_requested"]["doca_version"], "3.5.0")

    def test_merge_is_idempotent_and_keeps_build_history(self):
        builds = collect.load_local(self.fixtures)
        first = collect.merge_builds({}, builds)
        second = collect.merge_builds(first, builds)
        self.assertEqual(first, second)
        self.assertEqual(len(second["builds"]), 5)

    def test_rejects_multiple_manifests_for_one_build(self):
        build = collect.load_local(self.fixtures)[0]
        with self.assertRaisesRegex(ValueError, "multiple pair manifests"):
            collect.merge_builds({}, [build, build])

    def test_path_and_manifest_identity_must_match(self):
        source = next(self.fixtures.rglob("nno-pairs.json"))
        data = json.loads(source.read_text())
        data["run"]["build_id"] = "wrong"
        with self.assertRaisesRegex(ValueError, "disagrees"):
            collect.normalize_manifest(data, source.relative_to(self.fixtures).as_posix())

    def test_signed_pass_requires_signature_check(self):
        source = next(self.fixtures.glob("pr-logs/**/nno-pairs.json"))
        data = json.loads(source.read_text())
        pair = copy.deepcopy(data["pairs"][0])
        pair["mode"] = "signed"
        data["pairs"] = [pair]
        with self.assertRaisesRegex(ValueError, "signature verification"):
            collect.normalize_manifest(data, source.relative_to(self.fixtures).as_posix())
        pair["checks"]["signature_verification"] = "passed"
        normalized = collect.normalize_manifest(data, source.relative_to(self.fixtures).as_posix())
        self.assertEqual(normalized["pairs"][0]["mode"], "signed")

    def test_gcs_lists_pr_and_periodic_prefixes(self):
        pr_path = "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/pull-ci-example/10001/artifacts/nno-pairs.json"
        periodic_path = "logs/periodic-ci-example/10002/artifacts/nno-pairs.json"
        def page(params):
            prefix = params["prefix"]
            paths = [path for path in (pr_path, periodic_path) if path.startswith(prefix)]
            return {"items": [{"name": path} for path in paths]}
        with patch.object(collect, "_gcs_json", side_effect=page):
            with patch.object(collect, "_gcs_manifest") as fetch:
                pr_data = {"schema_version": 1, "run": {"kind": "presubmit", "job_name": "pull-ci-example", "build_id": "10001", "started_at": "2026-09-24T00:00:00Z"}, "pairs": []}
                periodic_data = {"schema_version": 1, "run": {"kind": "periodic", "job_name": "periodic-ci-example", "build_id": "10002", "started_at": "2026-09-25T00:00:00Z"}, "pairs": []}
                fetch.side_effect = lambda path: pr_data if path == pr_path else periodic_data
                builds = collect.load_gcs(["700"], ["periodic-ci-example"])
        self.assertEqual({build["run"]["kind"] for build in builds}, {"presubmit", "periodic"})

    def test_gcs_filters_other_pr_jobs_but_keeps_rehearsals(self):
        job = "pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-deploy-cluster"
        paths = [
            f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/{job}/10001/artifacts/nno-pairs.json",
            f"pr-logs/pull/openshift_release/700/rehearse-700-{job}/10002/artifacts/nno-pairs.json",
            "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/pull-ci-unrelated/10003/artifacts/nno-pairs.json",
        ]
        def page(params):
            return {"items": [{"name": path} for path in paths if path.startswith(params["prefix"])]}
        def manifest(path):
            identity = collect.source_identity(path)
            return {"schema_version": 1,
                    "run": {"kind": "presubmit", "job_name": identity["job_name"],
                            "build_id": identity["build_id"], "started_at": "2026-09-24T00:00:00Z"},
                    "pairs": []}
        with patch.object(collect, "_gcs_json", side_effect=page):
            with patch.object(collect, "_gcs_manifest", side_effect=manifest):
                builds = collect.load_gcs(["700"], [], presubmit_job=job)
        self.assertEqual({build["run"]["build_id"] for build in builds}, {"10001", "10002"})

    def test_prow_finished_result_overrides_manifest_build_status(self):
        path = "logs/periodic-ci-example/10002/artifacts/nno-pairs.json"
        def listing(params):
            return {"items": [{"name": path}]} if params["prefix"] == "logs/periodic-ci-example/" else {}
        def fetch(item):
            if item.endswith("/finished.json"):
                return {"result": "FAILURE"}
            return {"schema_version": 1,
                    "run": {"kind": "periodic", "job_name": "periodic-ci-example",
                            "build_id": "10002", "started_at": "2026-09-25T00:00:00Z",
                            "status": "unknown", "planned_modes": ["standard"]},
                    "pairs": []}
        with patch.object(collect, "_gcs_json", side_effect=listing):
            with patch.object(collect, "_gcs_manifest", side_effect=fetch):
                builds = collect.load_gcs([], ["periodic-ci-example"])
        self.assertEqual(builds[0]["run"]["status"], "failure")


class RenderTests(SampleArtifacts):
    def setUp(self):
        self.history = collect.merge_builds({}, collect.load_local(self.fixtures))

    def test_standard_includes_both_sources_and_signed_is_empty(self):
        standard = render.mode_data(self.history, "standard")
        signed = render.mode_data(self.history, "signed")
        self.assertEqual(len(standard["builds"]), 5)
        self.assertEqual(len(signed["builds"]), 0)
        self.assertEqual(sum(pair["status"] == "passed" for build in standard["builds"] for pair in build["pairs"]), 4)
        self.assertEqual(signed["scheduled_stability"], [])

    def test_stability_tracks_builds_separately_from_pairs(self):
        groups = render.mode_data(self.history, "standard")["scheduled_stability"]
        self.assertEqual(len(groups), 1)
        self.assertEqual(groups[0]["ocp_version"], "4.22.14")
        runs = groups[0]["runs"]
        self.assertEqual([(run["build_id"], run["status"]) for run in runs],
                         [("10004", "success"), ("10003", "aborted"), ("10002", "failure")])
        self.assertEqual((runs[2]["passed_pairs"], runs[2]["failed_pairs"]), (1, 1))
        self.assertEqual((runs[1]["passed_pairs"], runs[1]["failed_pairs"]), (0, 0))

    def test_signed_pair_appears_only_in_signed_report(self):
        history = copy.deepcopy(self.history)
        build = history["builds"][0]
        signed = copy.deepcopy(next(pair for pair in build["pairs"] if pair["status"] == "passed"))
        signed["id"] = "signed-attempt-1"
        signed["mode"] = "signed"
        signed["checks"]["signature_verification"] = "passed"
        build["pairs"].append(signed)
        build["run"]["planned_modes"].append("signed")
        signed_data = render.mode_data(history, "signed")
        standard_data = render.mode_data(history, "standard")
        self.assertEqual(sum(len(item["pairs"]) for item in signed_data["builds"]), 1)
        self.assertEqual(sum(len(item["pairs"]) for item in standard_data["builds"]), 6)

    @unittest.skipUnless(shutil.which("node"), "Node.js is needed to exercise the report script")
    def test_display_result_prefers_newest_pass(self):
        for template_name in ("report_template.html", "gpu_style_template.html"):
            with self.subTest(template=template_name):
                template = Path(render.__file__).with_name(template_name).read_text()
                function = re.search(r"function displayRun\(runs\) \{[^\n]+\}", template)
                self.assertIsNotNone(function)
                script = function.group() + """
const runs = [
  {pair:{status:'failed'}, id:'newest failure'},
  {pair:{status:'passed'}, id:'newest pass'},
  {pair:{status:'passed'}, id:'older pass'}
];
if (displayRun(runs).id !== 'newest pass') process.exit(1);
if (displayRun([runs[0], {pair:{status:'failed'}, id:'older failure'}]).id !== 'newest failure') process.exit(2);
"""
                subprocess.run(["node", "-e", script], check=True, capture_output=True, text=True)

    def test_output_files_and_safe_embedding(self):
        malicious = copy.deepcopy(self.history)
        malicious["builds"][0]["run"]["environment"] = "</script><script>alert(1)</script>"
        with tempfile.TemporaryDirectory() as directory:
            paths = render.write_reports(malicious, Path(directory), "Local fixture")
            self.assertEqual(len(paths), 6)
            matrix_html = (Path(directory) / "nno_doca2_matrix.html").read_text()
            signed_matrix_html = (Path(directory) / "nno_doca2_signed_matrix.html").read_text()
            explorer_html = (Path(directory) / "nno_doca2_explorer.html").read_text()
            signed_explorer_html = (Path(directory) / "nno_doca2_signed_explorer.html").read_text()
            for page in (matrix_html, explorer_html):
                self.assertIn("\\u003c/script\\u003e", page)
                self.assertNotIn("</script><script>alert", page)
                self.assertIn("OFED-internal-26.01-0.7.0", page)
            self.assertIn("Test Matrix: NNO and DOCA/OFED", matrix_html)
            self.assertIn("Open compatibility explorer", matrix_html)
            self.assertIn('href="nno_doca2_explorer.html"', matrix_html)
            self.assertIn("stability-mark", matrix_html)
            self.assertIn("A combination passes when any recorded execution passed", explorer_html)
            self.assertIn("Compatibility explorer", explorer_html)
            self.assertIn('href="nno_doca2_matrix.html"', explorer_html)
            self.assertNotIn("ocp-filter", explorer_html)
            self.assertIn("pair-row", explorer_html)
            self.assertNotIn("matrix-scroll", explorer_html)
            self.assertIn("Installed OFED:", explorer_html)
            self.assertNotIn("Prow builds</dt>", explorer_html)
            self.assertNotIn("DOCA/OFED releases", explorer_html)
            self.assertIn("No signed DOCA/OFED pair artifacts", signed_matrix_html)
            self.assertIn("No signed DOCA/OFED pair artifacts", signed_explorer_html)
            def embedded_data(page):
                match = re.search(r"const report = (\{[^\n]+\});", page)
                self.assertIsNotNone(match)
                return json.loads(match.group(1))
            self.assertEqual(embedded_data(matrix_html), embedded_data(explorer_html))
            self.assertEqual(embedded_data(signed_matrix_html), embedded_data(signed_explorer_html))


if __name__ == "__main__":
    unittest.main()
