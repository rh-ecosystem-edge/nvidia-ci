"""Tests for the local and GCS report contract."""

import contextlib
import copy
import io
import json
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from nno_pair_report import collect, render, workflow


def sample_pair(pair_id, nno, driver, status, *, ofed=""):
    checks = {"nno_csv": "passed", "driver_image": "passed",
              "rdma_shared_device": "passed",
              "rdma_gpudirect": "failed" if status == "failed" else "passed"}
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


PR_JOB = "pull-ci-example"
PERIODIC_JOB = "periodic-ci-example"


def sample_manifest_specs():
    """Report history used by the renderer, independent of step-result files."""
    doca = "doca3.5.0-26.07-0.7.7.0-0"
    return [
        (f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/698/{PR_JOB}/10000/artifacts/nno-step-result.json",
         "presubmit", PR_JOB, "10000", "2026-09-23T06:00:00Z", "4.21.44", "success",
         [sample_pair("pr-old-pass", "25.7.0", "25.01-0.6.0.0-0", "passed")]),
        (f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/{PR_JOB}/10001/artifacts/nno-step-result.json",
         "presubmit", PR_JOB, "10001", "2026-09-24T06:00:00Z", "4.22.14", "success",
         [sample_pair("pr-pass", "25.10.0", "25.04-0.6.1.0-0", "passed"),
          sample_pair("pr-fail", "25.10.1", "26.01-0.7.0.0-0", "failed",
                      ofed="OFED-internal-26.01-0.7.0")]),
        (f"logs/{PERIODIC_JOB}/10002/artifacts/nno-step-result.json",
         "periodic", PERIODIC_JOB, "10002", "2026-09-25T06:00:00Z", "4.22.14", "failure",
         [sample_pair("periodic-fail", "25.10.0", "25.04-0.6.1.0-0", "failed"),
          sample_pair("periodic-pass", "26.1.0", doca, "passed")]),
        (f"logs/{PERIODIC_JOB}/10003/artifacts/nno-step-result.json",
         "periodic", PERIODIC_JOB, "10003", "2026-09-26T06:00:00Z", "4.22.14", "aborted", []),
        (f"logs/{PERIODIC_JOB}/10004/artifacts/nno-step-result.json",
         "periodic", PERIODIC_JOB, "10004", "2026-09-27T06:00:00Z", "4.22.14", "success",
         [sample_pair("periodic-new-pass", "26.1.0", "26.04-0.7.1.0-0", "passed")]),
    ]


def manifest_from_spec(spec):
    relative, kind, job, build, started, ocp, status, pairs = spec
    return relative, {
        "schema_version": 1,
        "run": {"kind": kind, "job_name": job, "build_id": build,
                "started_at": started, "ocp_version": ocp, "status": status,
                "planned_modes": ["standard"]},
        "pairs": pairs,
    }


def sample_builds():
    builds = []
    for spec in sample_manifest_specs():
        path, data = manifest_from_spec(spec)
        builds.append(collect.normalize_manifest(data, path))
    return builds


def sample_history():
    return collect.merge_builds({}, sample_builds())


STEP_JOB = "periodic-ci-example"
STEP_BUILD = "20001"
STEP_NNO_VERSION = "26.7.0-1"
STEP_OFED_VERSION = "26.07-0.7.7.0"
STEP_OFED_REPOSITORY = "registry.stage.redhat.io/nvidia"
STEP_OFED_IMAGE = "doca-driver-rhel9"
STEP_OFED_IMAGE_ID = f"{STEP_OFED_REPOSITORY}/{STEP_OFED_IMAGE}@sha256:abc123"


def make_step_result(
    step, *, attempt=None, status="passed", checks=None, metrics=None,
    precompiled_selection=None,
):
    result_checks = {
        "ocp_version": "passed",
        "nno_csv": "passed",
        "configured_ofed_driver": "found",
        "driver_image_id": "found",
    }
    result_checks.update(checks or {})
    return {
        "schema_version": 1,
        "job_name": STEP_JOB,
        "build_id": STEP_BUILD,
        "step": step,
        "attempt": attempt or STEP_BUILD,
        "recorded_at": "2026-10-04T09:00:00Z",
        "status": status,
        "ocp_version": "4.22.15",
        "nno_csv_version": STEP_NNO_VERSION,
        "configured_ofed_driver": {
            "version": STEP_OFED_VERSION,
            "repository": STEP_OFED_REPOSITORY,
            "image": STEP_OFED_IMAGE,
            "image_id": STEP_OFED_IMAGE_ID,
        },
        "checks": result_checks,
        "metrics": metrics or {},
        "skip_reason": None,
        "precompiled_selection": precompiled_selection,
    }


def make_standard_rdma_result(*, attempt=None, status="passed"):
    return make_step_result(
        "network-operator-e2e-shared-device-rdma",
        attempt=attempt,
        status=status,
        checks={"rdma_shared_device": status},
        metrics={"standard_bandwidth_gbps": 91.2, "standard_message_rate_mpps": 0.18},
    )


def write_step_result(root, result, filename):
    relative = Path(
        Path("logs") / STEP_JOB / STEP_BUILD / "artifacts" / result["step"] /
        "artifacts" / filename
    )
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(result))
    return relative.as_posix(), path


def gcs_step_result(path):
    identity = collect.source_identity(path)
    return {
        "schema_version": 1,
        "job_name": identity["job_name"],
        "build_id": identity["build_id"],
        "step": "network-operator-e2e-shared-device",
        "attempt": identity["build_id"],
        "recorded_at": "2026-09-24T00:00:00Z",
        "status": "passed",
        "ocp_version": "4.22.14",
        "checks": {"nno_deployment": "passed"},
        "metrics": {},
    }


class CollectTests(unittest.TestCase):
    def setUp(self):
        gcsweb = patch.object(collect.gcs_utils, "_use_gcsweb", return_value=False)
        gcsweb.start()
        self.addCleanup(gcsweb.stop)

    def test_pr_and_periodic_paths(self):
        pr = collect.source_identity(
            "pr-logs/pull/openshift_release/85277/rehearse-85277-pull-ci-example/123/artifacts/nno-step-result.json")
        periodic = collect.source_identity("logs/periodic-ci-example/456/artifacts/nno-step-result.json")
        self.assertEqual((pr["kind"], pr["pr"], pr["build_id"]), ("presubmit", "85277", "123"))
        self.assertEqual((periodic["kind"], periodic["job_name"], periodic["build_id"]),
                         ("periodic", "periodic-ci-example", "456"))

    def test_report_link_uses_public_prow_bucket(self):
        path, manifest = manifest_from_spec(sample_manifest_specs()[2])
        manifest["run"]["prow_url"] = (
            "https://prow.ci.openshift.org/view/gs/test-platform-results/"
            "logs/periodic-ci-example/10002")
        normalized = collect.normalize_manifest(manifest, path)
        self.assertEqual(
            normalized["run"]["prow_url"],
            "https://prow.ci.openshift.org/view/gs/test-platform-results-public/"
            "logs/periodic-ci-example/10002")

    def test_public_bucket_uses_shared_artifact_client(self):
        with patch.object(collect.gcs_utils, "http_get_json", return_value={"items": []}) as fetch:
            self.assertEqual(collect._gcs_json({"prefix": "logs/periodic-ci-example/"}), {"items": []})
        fetch.assert_called_once_with(
            "https://storage.googleapis.com/storage/v1/b/test-platform-results-public/o",
            params={"prefix": "logs/periodic-ci-example/"})

    def test_missing_metadata_uses_error_type_or_http_status(self):
        response = collect.requests.Response()
        response.status_code = 200
        missing_curated = collect.gcs_utils.GCSFileNotFoundError("message may change", response=response)
        self.assertTrue(collect._is_not_found(missing_curated))
        response.status_code = 404
        self.assertTrue(collect._is_not_found(collect.requests.HTTPError(response=response)))
        response.status_code = 500
        self.assertFalse(collect._is_not_found(collect.requests.HTTPError(response=response)))

    def test_listing_matches_only_step_results(self):
        prefix = "logs/periodic-ci-example/10002/"
        step = f"{prefix}artifacts/nno-step-result-deploy.json"
        paths = [
            f"curated/{prefix}artifacts/nno-pairs.json",
            f"curated/{step}",
            f"curated/{prefix}artifacts/ocp.version",
        ]
        with patch.object(collect.gcs_utils, "_use_gcsweb", return_value=True):
            with patch.object(collect.gcs_utils, "_gcsweb_list_all_files", return_value=paths):
                self.assertEqual(collect.list_gcs_report_artifacts(prefix), [step])

    def test_history_has_multiple_pairs_per_build(self):
        builds = sample_builds()
        self.assertEqual(len(builds), 5)
        self.assertEqual(sum(len(build["pairs"]) for build in builds), 6)
        self.assertEqual({build["run"]["kind"] for build in builds}, {"presubmit", "periodic"})
        self.assertEqual({build["run"]["ocp_version"] for build in builds}, {"4.21.44", "4.22.14"})
        self.assertEqual(len([build for build in builds if not build["pairs"]]), 1)
        self.assertIn("doca3.5.0-26.07-0.7.7.0-0", {pair["driver_requested"]["version"] for build in builds for pair in build["pairs"]})
        observed = [pair["driver_observed"]["ofed_version"] for build in builds for pair in build["pairs"] if pair["driver_observed"].get("ofed_version")]
        self.assertIn("OFED-internal-26.01-0.7.0", observed)

    def test_explicit_doca_version_is_preserved(self):
        path, data = manifest_from_spec(sample_manifest_specs()[0])
        data["pairs"][0]["driver_requested"]["doca_version"] = "3.5.0"
        normalized = collect.normalize_manifest(data, path)
        self.assertEqual(normalized["pairs"][0]["driver_requested"]["doca_version"], "3.5.0")

    def test_legacy_precompiled_id_routes_to_signed_report(self):
        path, data = manifest_from_spec(sample_manifest_specs()[0])
        pair = data["pairs"][0]
        pair["id"] = "doca2-precompiled-legacy"
        pair["checks"] = {
            "nno_csv": "passed", "precompiled_selection": "passed",
            "nno_deployment": "passed", "nic_cluster_policy_ready": "passed",
        }
        normalized = collect.normalize_manifest(data, path)
        self.assertEqual(normalized["pairs"][0]["mode"], "signed")
        self.assertEqual(normalized["pairs"][0]["validation"], "precompiled")
        history = collect.merge_builds({}, [normalized])
        self.assertEqual(render.mode_data(history, "standard")["builds"][0]["pairs"], [])
        self.assertEqual(len(render.mode_data(history, "signed")["builds"][0]["pairs"]), 1)

    def test_merge_is_idempotent_and_keeps_build_history(self):
        builds = sample_builds()
        first = collect.merge_builds({}, builds)
        second = collect.merge_builds(first, builds)
        self.assertEqual(first, second)
        self.assertEqual(len(second["builds"]), 5)

    def test_collection_summary_excludes_baseline_builds(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            empty_input = root / "empty-input"
            empty_input.mkdir()
            baseline = root / "baseline.json"
            baseline.write_text(json.dumps(collect.merge_builds({}, sample_builds()[:1])))
            output = root / "history.json"
            summary = root / "collection.json"
            with patch.object(sys, "argv", [
                "collect", "--input-dir", str(empty_input), "--baseline", str(baseline),
                "--output", str(output), "--summary-output", str(summary),
            ]):
                collect.main()

            self.assertEqual(len(json.loads(output.read_text())["builds"]), 1)
            self.assertEqual(json.loads(summary.read_text()), {"fetched_builds": 0})

    def test_rejects_duplicate_results_for_one_build(self):
        build = sample_builds()[0]
        with self.assertRaisesRegex(ValueError, "multiple results found for one Prow build"):
            collect.merge_builds({}, [build, build])

    def test_path_and_run_identity_must_match(self):
        path, data = manifest_from_spec(sample_manifest_specs()[0])
        data["run"]["build_id"] = "wrong"
        with self.assertRaisesRegex(ValueError, "disagrees"):
            collect.normalize_manifest(data, path)

    def test_signed_pass_uses_recorded_test_checks(self):
        path, data = manifest_from_spec(sample_manifest_specs()[0])
        pair = copy.deepcopy(data["pairs"][0])
        pair["mode"] = "signed"
        data["pairs"] = [pair]
        normalized = collect.normalize_manifest(data, path)
        self.assertEqual(normalized["pairs"][0]["mode"], "signed")
        pair["checks"]["rdma_shared_device"] = "failed"
        with self.assertRaisesRegex(ValueError, "requires passed checks"):
            collect.normalize_manifest(data, path)

    def test_dynamic_pass_requires_gpudirect_rdma(self):
        path, data = manifest_from_spec(sample_manifest_specs()[0])
        del data["pairs"][0]["checks"]["rdma_gpudirect"]
        with self.assertRaisesRegex(ValueError, "requires GPUDirect RDMA"):
            collect.normalize_manifest(data, path)

    def test_gcs_lists_pr_and_periodic_prefixes(self):
        pr_path = "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/pull-ci-example/10001/artifacts/nno-step-result.json"
        periodic_path = "logs/periodic-ci-example/10002/artifacts/nno-step-result.json"

        def page(params):
            prefix = params["prefix"]
            paths = [path for path in (pr_path, periodic_path) if path.startswith(prefix)]
            return {"items": [{"name": path} for path in paths]}

        with patch.object(collect, "_gcs_json", side_effect=page):
            with patch.object(collect, "_fetch_gcs_json", side_effect=gcs_step_result):
                builds = collect.load_gcs(["700"], ["periodic-ci-example"])
        self.assertEqual({build["run"]["kind"] for build in builds}, {"presubmit", "periodic"})

    def test_gcs_filters_other_pr_jobs_but_keeps_rehearsals(self):
        job = "pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-deploy-cluster"
        listed_prefixes = []
        paths = [
            f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/{job}/10001/artifacts/nno-step-result.json",
            f"pr-logs/pull/openshift_release/700/rehearse-700-{job}/10002/artifacts/nno-step-result.json",
            "pr-logs/pull/rh-ecosystem-edge_nvidia-ci/700/pull-ci-unrelated/10003/artifacts/nno-step-result.json",
        ]

        def page(params):
            listed_prefixes.append(params["prefix"])
            return {"items": [{"name": path} for path in paths if path.startswith(params["prefix"])]}

        with patch.object(collect, "_gcs_json", side_effect=page):
            with patch.object(collect, "_fetch_gcs_json", side_effect=gcs_step_result):
                builds = collect.load_gcs(["700"], [], presubmit_job=job)
        self.assertEqual({build["run"]["build_id"] for build in builds}, {"10001", "10002"})
        self.assertEqual(len(listed_prefixes), 4)
        self.assertTrue(all(prefix.endswith((f"/{job}/", f"/rehearse-700-{job}/"))
                            for prefix in listed_prefixes))

    def test_prow_finished_result_overrides_build_status(self):
        path = "logs/periodic-ci-example/10002/artifacts/nno-step-result.json"

        def listing(params):
            return {"items": [{"name": path}]} if params["prefix"] == "logs/periodic-ci-example/" else {}

        def fetch(item):
            if item.endswith("/finished.json"):
                return {"result": "FAILURE"}
            if item.endswith("/started.json"):
                return {"timestamp": "2026-09-25T06:00:00Z"}
            return gcs_step_result(item)

        with patch.object(collect, "_gcs_json", side_effect=listing):
            with patch.object(collect, "_fetch_gcs_json", side_effect=fetch):
                builds = collect.load_gcs([], ["periodic-ci-example"])
        self.assertEqual(builds[0]["run"]["status"], "failure")

    def test_gcs_skips_a_malformed_build_and_keeps_the_rest(self):
        good = "logs/periodic-ci-example/10002/artifacts/nno-step-result.json"
        failed = "logs/periodic-ci-example/10003/artifacts/nno-step-result-failed.json"
        passed = "logs/periodic-ci-example/10003/artifacts/nno-step-result-passed.json"

        def listing(params):
            names = (good, failed, passed)
            return {"items": [{"name": path} for path in names if path.startswith(params["prefix"])]}

        def fetch(item):
            result = gcs_step_result(item)
            if item == failed:
                result["status"] = "failed"
            return result

        stderr = io.StringIO()
        with patch.object(collect, "_gcs_json", side_effect=listing):
            with patch.object(collect, "_fetch_gcs_json", side_effect=fetch):
                with contextlib.redirect_stderr(stderr):
                    builds = collect.load_gcs([], ["periodic-ci-example"])

        self.assertEqual([build["run"]["build_id"] for build in builds], ["10002"])
        self.assertIn("logs/periodic-ci-example/10003", stderr.getvalue())
        self.assertIn("conflicting step results", stderr.getvalue())


class StepResultTests(unittest.TestCase):
    def setUp(self):
        gcsweb = patch.object(collect.gcs_utils, "_use_gcsweb", return_value=False)
        gcsweb.start()
        self.addCleanup(gcsweb.stop)

    def collect_local(self, records):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        for result, filename in records:
            write_step_result(root, result, filename)
        return collect.load_local(root)

    def test_step_timestamps_are_ordered_by_instant(self):
        deployment = make_step_result("network-operator-e2e-shared-device")
        deployment["recorded_at"] = "2026-10-04T09:00:00+02:00"
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect", checks={"rdma_gpudirect": "passed"},
        )
        gpudirect["recorded_at"] = "2026-10-04T08:30:00Z"
        build = collect.normalize_step_results([
            ("local/deployment.json", deployment),
            ("local/gpudirect.json", gpudirect),
        ], "local/deployment.json")
        self.assertEqual(build["run"]["started_at"], "2026-10-04T07:00:00Z")
        self.assertEqual(build["pairs"][0]["completed_at"], "2026-10-04T08:30:00Z")

    def test_local_step_sidecars_combine_into_a_pair_with_evidence(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_gpudirect": "passed"},
            metrics={"bandwidth_gbps": 93.91, "message_rate_mpps": 0.179124},
        )

        builds = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect.json"),
        ])

        self.assertEqual(len(builds), 1)
        build = builds[0]
        self.assertEqual(build["build_path"], f"logs/{STEP_JOB}/{STEP_BUILD}")
        self.assertEqual(build["run"]["ocp_version"], "4.22.15")
        self.assertEqual(len(build["pairs"]), 1)
        pair = build["pairs"][0]
        self.assertEqual(pair["status"], "passed")
        self.assertEqual(pair["nno_observed"], STEP_NNO_VERSION)
        self.assertEqual(pair["driver_requested"]["version"], STEP_OFED_VERSION)
        self.assertEqual(pair["driver_observed"]["digest"], "sha256:abc123")
        self.assertEqual(pair["metrics"], {
            "bandwidth_gbps": 93.91,
            "message_rate_mpps": 0.179124,
        })
        self.assertNotIn("rdma_shared_device", pair["checks"])
        self.assertTrue(all(value == "passed" for value in pair["checks"].values()))
        self.assertTrue(any(
            "/artifacts/network-operator-e2e-gpudirect/artifacts/" in url
            for url in pair["artifacts"].values()
        ))

    def test_precompiled_pair_is_reported_as_signed_without_rdma_check(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_gpudirect": "passed"},
        )
        precompiled = make_step_result(
            "network-operator-e2e-precompiled",
            checks={
                "nno_deployment": "passed",
                "nic_cluster_policy_ready": "passed",
                "precompiled_selection": "passed",
            },
            precompiled_selection={
                "outcome": "selected",
                "kernel_version": "5.14.0-test",
                "architecture": "x86_64",
                "version": STEP_OFED_VERSION,
                "pull_spec": f"{STEP_OFED_REPOSITORY}/{STEP_OFED_IMAGE}:kernel-test",
                "error": None,
            },
        )

        builds = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect.json"),
            (precompiled, "nno-step-result-precompiled.json"),
        ])

        pairs = builds[0]["pairs"]
        self.assertEqual(len(pairs), 2)
        standard, precompiled_pair = pairs
        self.assertIn("standard", standard["id"])
        self.assertEqual(standard["status"], "passed")
        self.assertIn("precompiled", precompiled_pair["id"])
        self.assertEqual(precompiled_pair["status"], "passed")
        self.assertEqual(precompiled_pair["mode"], "signed")
        self.assertEqual(precompiled_pair["validation"], "precompiled")
        self.assertNotIn("rdma_shared_device", precompiled_pair["checks"])
        self.assertNotIn("rdma_gpudirect", precompiled_pair["checks"])
        self.assertEqual(builds[0]["run"]["planned_modes"], ["signed", "standard"])
        self.assertEqual(
            precompiled_pair["driver_requested"]["image"],
            f"{STEP_OFED_REPOSITORY}/{STEP_OFED_IMAGE}:kernel-test",
        )
        history = collect.merge_builds({}, builds)
        self.assertEqual(len(render.mode_data(history, "standard")["builds"][0]["pairs"]), 1)
        signed = render.mode_data(history, "signed")["builds"]
        self.assertEqual(len(signed), 1)
        self.assertEqual(signed[0]["pairs"][0]["id"], precompiled_pair["id"])

    def test_missing_deployment_step_is_blocked(self):
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_shared_device": "passed"},
        )

        pair = self.collect_local([(gpudirect, "nno-step-result-gpudirect.json")])[0]["pairs"][0]

        self.assertEqual(pair["status"], "blocked")
        self.assertEqual(pair["checks"]["nno_deployment"], "blocked")
        self.assertEqual(pair["checks"]["nic_cluster_policy_ready"], "blocked")
        self.assertEqual(pair["checks"]["rdma_gpudirect"], "passed")

    def test_non_gpudirect_rdma_does_not_satisfy_dynamic_pair(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        standard_rdma = make_standard_rdma_result()
        pair = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (standard_rdma, "nno-step-result-standard-rdma.json"),
        ])[0]["pairs"][0]
        self.assertEqual(pair["status"], "blocked")
        self.assertEqual(pair["checks"]["nno_deployment"], "passed")
        self.assertEqual(pair["checks"]["nic_cluster_policy_ready"], "passed")
        self.assertEqual(pair["checks"]["rdma_gpudirect"], "blocked")

    def test_deployment_cannot_claim_gpudirect_result(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={
                "nno_deployment": "passed",
                "nic_cluster_policy_ready": "passed",
                "rdma_gpudirect": "passed",
            },
        )
        pair = self.collect_local([(deployment, "nno-step-result-deployment.json")])[0]["pairs"][0]
        self.assertEqual(pair["status"], "blocked")
        self.assertEqual(pair["checks"]["rdma_gpudirect"], "blocked")

    def test_failed_gpudirect_makes_dynamic_pair_fail(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            status="failed",
            checks={"rdma_gpudirect": "failed"},
        )
        pair = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect.json"),
        ])[0]["pairs"][0]
        self.assertEqual(pair["status"], "failed")
        self.assertEqual(pair["checks"]["rdma_gpudirect"], "failed")

    def test_blocked_deployment_with_passed_checks_does_not_pass(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            status="blocked",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_gpudirect": "passed"},
        )
        pair = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect.json"),
        ])[0]["pairs"][0]
        self.assertEqual(pair["status"], "blocked")
        self.assertEqual(pair["checks"]["nno_deployment"], "passed")
        self.assertEqual(pair["checks"]["rdma_gpudirect"], "passed")

    def test_different_driver_digest_cannot_be_combined_as_one_pair(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_gpudirect": "passed"},
        )
        gpudirect["configured_ofed_driver"]["image_id"] = (
            f"{STEP_OFED_REPOSITORY}/{STEP_OFED_IMAGE}@sha256:different"
        )
        pair = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect.json"),
        ])[0]["pairs"][0]
        self.assertEqual(pair["status"], "failed")
        self.assertIn("digest differed", pair["note"])

    def test_precompiled_no_match_is_skipped(self):
        precompiled = make_step_result(
            "network-operator-e2e-precompiled",
            status="skipped",
            checks={"precompiled_selection": "not_found"},
            precompiled_selection={
                "outcome": "no_match",
                "kernel_version": "5.14.0-test",
                "architecture": "x86_64",
                "version": None,
                "pull_spec": None,
                "error": None,
            },
        )

        pair = self.collect_local([(precompiled, "nno-step-result-precompiled.json")])[0]["pairs"][0]

        self.assertEqual(pair["status"], "skipped")
        self.assertEqual(pair["mode"], "signed")
        self.assertEqual(pair["checks"]["precompiled_selection"], "skipped")
        self.assertIn("no matching precompiled OFED image", pair["note"])

    def test_conflicting_results_for_same_attempt_fail_closed(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        passed = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_shared_device": "passed"},
        )
        failed = make_step_result(
            "network-operator-e2e-gpudirect",
            status="failed",
            checks={"rdma_shared_device": "failed"},
        )

        with self.assertRaisesRegex(ValueError, "conflicting step results"):
            self.collect_local([
                (deployment, "nno-step-result-deployment.json"),
                (failed, "nno-step-result-gpudirect-failed.json"),
                (passed, "nno-step-result-gpudirect-passed.json"),
            ])

    def test_identical_duplicate_step_result_is_deduplicated(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_shared_device": "passed"},
            metrics={"bandwidth_gbps": 93.91},
        )
        pair = self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect-a.json"),
            (copy.deepcopy(gpudirect), "nno-step-result-gpudirect-b.json"),
        ])[0]["pairs"][0]

        self.assertEqual(pair["status"], "passed")
        self.assertEqual(pair["checks"]["rdma_gpudirect"], "passed")
        self.assertEqual(pair["metrics"]["bandwidth_gbps"], 93.91)
        self.assertEqual(len(pair["artifacts"]), 2)

    def test_distinct_attempt_ids_are_collected_separately(self):
        records = []
        for attempt in ("attempt-1", "attempt-2"):
            deployment = make_step_result(
                "network-operator-e2e-shared-device",
                attempt=attempt,
                checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
            )
            gpudirect = make_step_result(
                "network-operator-e2e-gpudirect",
                attempt=attempt,
                checks={"rdma_gpudirect": "passed"},
            )
            records.extend((
                (deployment, f"nno-step-result-{attempt}-deployment.json"),
                (gpudirect, f"nno-step-result-{attempt}-gpudirect.json"),
            ))

        pairs = self.collect_local(records)[0]["pairs"]

        self.assertEqual(len(pairs), 2)
        self.assertEqual({pair["id"] for pair in pairs}, {
            "doca2-standard-attempt-1", "doca2-standard-attempt-2",
        })
        self.assertTrue(all(pair["status"] == "passed" for pair in pairs))

    def test_gcs_collection_loads_step_sidecars(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_gpudirect": "passed"},
            metrics={"bandwidth_gbps": 93.91},
        )
        with tempfile.TemporaryDirectory() as directory:
            records = {}
            for result, filename in (
                (deployment, "nno-step-result-deployment.json"),
                (gpudirect, "nno-step-result-gpudirect.json"),
            ):
                path, _ = write_step_result(Path(directory), result, filename)
                records[path] = result

            def fetch(path):
                if path in records:
                    return records[path]
                if path.endswith("/started.json"):
                    return {"timestamp": "2026-10-04T08:00:00Z"}
                if path.endswith("/finished.json"):
                    return {"result": "SUCCESS"}
                raise AssertionError(f"unexpected Prow artifact fetch: {path}")

            artifact_paths = list(records)
            with patch.object(
                collect, "_gcs_json",
                return_value={"items": [{"name": path} for path in artifact_paths]},
            ):
                with patch.object(collect, "_fetch_gcs_json", side_effect=fetch):
                    builds = collect.load_gcs([], [STEP_JOB])

        self.assertEqual(len(builds), 1)
        self.assertEqual(builds[0]["run"]["status"], "success")
        self.assertEqual(len(builds[0]["pairs"]), 1)
        self.assertEqual(builds[0]["pairs"][0]["status"], "passed")
        self.assertEqual(builds[0]["pairs"][0]["metrics"]["bandwidth_gbps"], 93.91)

    def test_step_results_render_into_report_pages(self):
        deployment = make_step_result(
            "network-operator-e2e-shared-device",
            checks={"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"},
        )
        gpudirect = make_step_result(
            "network-operator-e2e-gpudirect",
            checks={"rdma_gpudirect": "passed"},
        )
        history = collect.merge_builds({}, self.collect_local([
            (deployment, "nno-step-result-deployment.json"),
            (gpudirect, "nno-step-result-gpudirect.json"),
        ]))
        with tempfile.TemporaryDirectory() as directory:
            paths = render.write_reports(history, Path(directory), "Step results")
            self.assertEqual(len(paths), 6)
            page = (Path(directory) / "nno_doca2_matrix.html").read_text()
            self.assertIn(STEP_NNO_VERSION, page)
            self.assertIn(STEP_OFED_VERSION, page)
            self.assertIn("nno_doca2_explorer.html", page)


class RenderTests(unittest.TestCase):
    def setUp(self):
        self.history = sample_history()

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
        signed["validation"] = "signed"
        build["pairs"].append(signed)
        build["run"]["planned_modes"].append("signed")
        signed_data = render.mode_data(history, "signed")
        standard_data = render.mode_data(history, "standard")
        self.assertEqual(sum(len(item["pairs"]) for item in signed_data["builds"]), 1)
        self.assertEqual(sum(len(item["pairs"]) for item in standard_data["builds"]), 6)

    def test_old_history_without_gpudirect_result_is_not_green(self):
        history = copy.deepcopy(self.history)
        build = next(build for build in history["builds"] if any(
            pair["status"] == "passed" and pair["mode"] == "standard" for pair in build["pairs"]
        ))
        pair = next(pair for pair in build["pairs"] if pair["status"] == "passed")
        del pair["checks"]["rdma_gpudirect"]

        displayed_build = next(item for item in render.mode_data(history, "standard")["builds"]
                               if item["build_path"] == build["build_path"])
        displayed = next(item for item in displayed_build["pairs"] if item["id"] == pair["id"])

        self.assertEqual(displayed["status"], "blocked")
        self.assertEqual(displayed["checks"]["rdma_gpudirect"], "blocked")

    def test_old_signed_history_does_not_need_a_signature_check(self):
        history = copy.deepcopy(self.history)
        build = next(build for build in history["builds"] if any(
            pair["status"] == "passed" and pair["mode"] == "standard" for pair in build["pairs"]
        ))
        signed = copy.deepcopy(next(pair for pair in build["pairs"] if pair["status"] == "passed"))
        signed["id"] = "signed-test-pass"
        signed["mode"] = "signed"
        signed["validation"] = "signed"
        build["pairs"].append(signed)

        displayed_build = next(item for item in render.mode_data(history, "signed")["builds"]
                               if item["build_path"] == build["build_path"])
        displayed = next(item for item in displayed_build["pairs"] if item["id"] == signed["id"])

        self.assertEqual(displayed["status"], "passed")
        self.assertNotIn("signature_verification", displayed["checks"])

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


class WorkflowOutputTests(unittest.TestCase):
    def test_summary_counts_new_builds(self):
        has_data, text = workflow.summarize_collection(
            {"builds": [{"build_path": "logs/job/2"}, {"build_path": "logs/job/1"}]},
            {"fetched_builds": 1},
            {"builds": [{"build_path": "logs/job/1"}]},
        )
        self.assertTrue(has_data)
        self.assertEqual(
            text,
            "NNO report: 1 builds fetched from Prow, 1 new builds, 2 builds in history.\n",
        )

    def test_summary_without_fetched_builds(self):
        has_data, text = workflow.summarize_collection(
            {"builds": [{"build_path": "logs/job/1"}]},
            {"fetched_builds": 0},
            {"builds": []},
        )
        self.assertFalse(has_data)
        self.assertIn("nothing was published", text)

    def test_missing_baseline_history_counts_every_build_as_new(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            history = root / "history.json"
            collection = root / "collection.json"
            output = root / "github-output.txt"
            summary = root / "summary.md"
            history.write_text(json.dumps({"builds": [{"build_path": "logs/job/1"}]}))
            collection.write_text(json.dumps({"fetched_builds": 1}))
            with patch.object(sys, "argv", [
                "workflow", "summarize",
                "--history", str(history),
                "--collection", str(collection),
                "--baseline", str(root / "missing.json"),
                "--github-output", str(output),
                "--step-summary", str(summary),
            ]):
                workflow.main()
            self.assertEqual(output.read_text(), "has_data=true\n")
            self.assertIn("1 new builds", summary.read_text())

    def test_diff_is_unchanged_when_files_match(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output_dir = root / "output"
            baseline = root / "baseline"
            output_dir.mkdir()
            baseline.mkdir()
            (output_dir / "nno_doca2_matrix.html").write_bytes(b"same")
            (baseline / "nno_doca2_matrix.html").write_bytes(b"same")
            self.assertFalse(workflow.has_new_or_modified_files(output_dir, baseline))

    def test_diff_detects_a_changed_or_new_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output_dir = root / "output"
            baseline = root / "baseline"
            output_dir.mkdir()
            baseline.mkdir()
            (output_dir / "nno_doca2_matrix.html").write_bytes(b"new")
            (baseline / "nno_doca2_matrix.html").write_bytes(b"old")
            (output_dir / "nno_doca2_history.json").write_bytes(b"{}")
            github_output = root / "github-output.txt"
            with patch.object(sys, "argv", [
                "workflow", "diff",
                "--output-dir", str(output_dir),
                "--baseline-dir", str(baseline),
                "--github-output", str(github_output),
            ]):
                workflow.main()
            self.assertEqual(github_output.read_text(), "changed=true\n")


if __name__ == "__main__":
    unittest.main()
