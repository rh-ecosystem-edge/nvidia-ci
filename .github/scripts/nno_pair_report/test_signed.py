"""Signed reporting: target correlation, complete evidence and job discovery."""

import copy
import json
import tempfile
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from unittest.mock import patch

from nno_pair_report import collect, render, signed, workflow

KERNEL = "5.14.0-687.35.1.el9_8.x86_64"
VERSION = "26.04-1.1.0.0"
IMAGE = f"registry.stage.redhat.io/nvidia/doca-driver-rhel9:{VERSION}-{KERNEL}-rhel9.8-amd64"
DIGEST = "sha256:" + "a" * 64
IMAGE_ID = IMAGE.rsplit(":", 1)[0] + "@" + DIGEST


def artifacts(job=signed.MANUAL_JOB, build="123"):
    root = (f"logs/{job}/{build}" if job.startswith("periodic-") else
            f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/747/{job}/{build}")
    records = {
        root + "/started.json": {"timestamp": "2026-10-11T09:00:00Z"},
        root + "/finished.json": {"result": "SUCCESS"},
    }
    request = {
        "schema_version": 1,
        "provenance": {"job_name": job, "build_id": build, "pull_number": "747"},
        "pairs": [{"id": "requested-pair", "openshift_version": "4.22.9-multi", "status": "planned",
                   "driver_requested": {"image": IMAGE, "kernel": KERNEL, "architecture": "amd64"}}],
    }
    records[root + "/artifacts/nno-signed/nno-read-signed-target/artifacts/" + signed.REQUEST] = request
    for stage, step in signed.STAGES.items():
        prefix = root + "/artifacts/nno-signed/" + step
        records[prefix + "/finished.json"] = {"result": "SUCCESS"}
        if stage == "gpu":
            continue
        name = "network-operator-e2e-signed-" + stage
        checks = {key: "passed" for key in ("ocp_version", "nno_csv", "precompiled_selection",
                                             "requested_driver_policy", "requested_driver_running")}
        checks.update({"nno_deployment": "passed", "nic_cluster_policy_ready": "passed"} if stage == "deploy" else
                      {"rdma_gpudirect": "passed"})
        records[prefix + "/artifacts/nno-step-result-1.json"] = {
            "schema_version": 1, "job_name": job, "build_id": build, "step": name,
            "attempt": build + "-" + stage, "status": "passed", "recorded_at": "2026-10-11T12:00:00Z",
            "ocp_version": "4.22.9", "nno_csv_version": "26.7.0-1", "checks": checks,
            "configured_ofed_driver": {"version": VERSION, "repository": "registry.stage.redhat.io/nvidia",
                                       "image": "doca-driver-rhel9", "image_id": IMAGE_ID},
            "precompiled_selection": {"outcome": "selected", "version": VERSION, "pull_spec": IMAGE,
                                      "kernel_version": KERNEL, "architecture": "amd64"},
            "metrics": {"bandwidth_gbps": 93.91} if stage == "gpudirect" else {},
        }
        records[prefix + "/artifacts/" + signed.WORKERS] = {
            "step": name, "attempt": build + "-" + stage, "pull_spec": IMAGE,
            "workers": {"worker-a": IMAGE_ID, "worker-b": IMAGE_ID},
        }
    return root, records


def discovery(root, records, *, outcome="selected"):
    identity = collect.source_identity(root + "/")
    data = {"schema_version": 1, "job_name": identity["job_name"], "build_id": identity["build_id"],
            "started_at": "2026-10-11T09:00:00Z", "outcome": outcome,
            "reason": "one_new_compatible_image" if outcome == "selected" else "no_eligible_image",
            "selected": None}
    if outcome == "selected":
        data["selected"] = {"image": IMAGE, "kernel": KERNEL, "architecture": "amd64",
                            "manifest_digest": DIGEST, "config_digest": "sha256:" + "b" * 64,
                            "release": {"installer_version": "4.22.9-multi"}}
    records[root + "/artifacts/nno-signed-auto/nno-discover-signed-target/artifacts/" + signed.DISCOVERY] = data
    return data


def normalize(root, records):
    return collect.normalize_artifacts(list(records.items()), root + "/started.json")


class SignedResultsTests(unittest.TestCase):
    def setUp(self):
        self.root, self.records = artifacts()

    def result(self, stage):
        return next(data for path, data in self.records.items()
                    if "/" + signed.STAGES[stage] + "/" in path and "nno-step-result" in path)

    def pair(self):
        return normalize(self.root, self.records)["pairs"][0]

    def test_different_invocation_ids_make_one_complete_signed_pair(self):
        build = normalize(self.root, self.records)
        self.assertEqual(len(build["pairs"]), 1)
        pair = build["pairs"][0]
        self.assertEqual((pair["mode"], pair["validation"], pair["status"]), ("signed", "signed", "passed"))
        self.assertEqual(pair["metrics"]["bandwidth_gbps"], 93.91)
        self.assertEqual(pair["driver_observed"]["digest"], DIGEST)
        self.assertEqual(pair["driver_requested"]["kernel"], KERNEL)
        self.assertNotEqual(pair["stages"]["deploy"]["attempt"], pair["stages"]["gpudirect"]["attempt"])
        self.assertEqual(build["run"]["source"], "manual")
        self.assertEqual(build["run"]["ocp_version"], "4.22.9")
        self.assertTrue(all(value == "passed" for value in pair["checks"].values()))
        self.assertEqual(len(render.mode_data(collect.merge_builds({}, [build]), "standard")["builds"]), 0)

    def test_request_without_kernel_or_architecture_uses_exact_image_tag(self):
        request = next(data for path, data in self.records.items() if path.endswith(signed.REQUEST))
        request["pairs"][0]["driver_requested"] = {"image": IMAGE}
        self.assertEqual(self.pair()["status"], "passed")

    def test_automatic_selection_preserves_digest_types(self):
        root, records = artifacts(signed.AUTOMATIC_JOB)
        discovery(root, records)
        build = normalize(root, records)
        self.assertEqual(build["pairs"][0]["status"], "passed")
        self.assertEqual(build["run"]["source"], "automatic")
        self.assertEqual(build["pairs"][0]["driver_requested"]["manifest_digest"], DIGEST)

    def test_deploy_only_cannot_pass(self):
        self.records = {path: data for path, data in self.records.items() if "/nno-test-gpudirect/" not in path}
        self.assertEqual(self.pair()["status"], "blocked")

    def test_each_required_stage_check_is_required(self):
        original = copy.deepcopy(self.records)
        for stage, check in [("deploy", "requested_driver_running"), ("gpudirect", "requested_driver_policy"),
                             ("deploy", "nic_cluster_policy_ready"), ("gpudirect", "rdma_gpudirect")]:
            with self.subTest(stage=stage, check=check):
                self.records = copy.deepcopy(original)
                del self.result(stage)["checks"][check]
                self.assertEqual(self.pair()["status"], "blocked")

    def test_failure_in_gpudirect_is_retained(self):
        self.result("gpudirect")["status"] = "failed"
        self.result("gpudirect")["checks"]["rdma_gpudirect"] = "failed"
        self.assertEqual(self.pair()["status"], "failed")

    def test_skipped_gpudirect_is_retained(self):
        self.result("gpudirect")["status"] = "skipped"
        self.result("gpudirect")["checks"]["rdma_gpudirect"] = "skipped"
        self.assertEqual(self.pair()["status"], "skipped")

    def test_missing_check_is_not_hidden_by_a_skipped_check_in_another_stage(self):
        del self.result("deploy")["checks"]["requested_driver_running"]
        self.result("gpudirect")["status"] = "skipped"
        self.result("gpudirect")["checks"]["requested_driver_running"] = "skipped"
        pair = self.pair()
        self.assertEqual(pair["checks"]["requested_driver_running"], "blocked")
        self.assertEqual(pair["status"], "blocked")

    def test_skipped_check_does_not_hide_a_different_blocked_check(self):
        del self.result("deploy")["checks"]["nic_cluster_policy_ready"]
        self.result("gpudirect")["checks"]["rdma_gpudirect"] = "skipped"
        self.result("gpudirect")["status"] = "skipped"
        self.assertEqual(self.pair()["status"], "blocked")

    def test_deployment_tag_failure_is_visible(self):
        self.result("deploy")["checks"]["requested_driver_tag"] = "failed"
        self.result("deploy")["status"] = "failed"
        pair = self.pair()
        self.assertEqual(pair["checks"]["requested_driver_tag"], "failed")
        self.assertEqual(pair["status"], "failed")

    def test_passing_sidecars_do_not_hide_wrapper_or_gpu_failure(self):
        for stage in signed.STAGES:
            with self.subTest(stage=stage):
                root, records = artifacts()
                path = next(path for path in records if path.endswith("/" + signed.STAGES[stage] + "/finished.json"))
                records[path]["result"] = "FAILURE"
                self.assertEqual(normalize(root, records)["pairs"][0]["status"], "failed")

    def test_mismatched_target_cannot_pass(self):
        self.result("gpudirect")["precompiled_selection"]["pull_spec"] = IMAGE + "wrong"
        self.assertEqual(self.pair()["status"], "failed")
        self.root, self.records = artifacts()
        self.result("gpudirect")["ocp_version"] = "4.22.10"
        self.assertEqual(self.pair()["status"], "failed")

    def test_missing_worker_evidence_blocks_pair(self):
        self.records = {path: data for path, data in self.records.items() if not path.endswith(signed.WORKERS)}
        self.assertEqual(self.pair()["status"], "blocked")

    def test_worker_image_mismatch_fails_pair(self):
        evidence = next(data for path, data in self.records.items() if path.endswith(signed.WORKERS))
        evidence["workers"]["worker-b"] = "sha256:" + "c" * 64
        self.assertEqual(self.pair()["status"], "failed")

    def test_worker_evidence_from_another_invocation_is_not_used(self):
        evidence = next(data for path, data in self.records.items() if path.endswith(signed.WORKERS))
        evidence["attempt"] = "different"
        self.assertEqual(self.pair()["status"], "blocked")

    def test_invalid_artifact_structures_are_reported_as_validation_errors(self):
        for value in (None, []):
            with self.subTest(value=value):
                root, records = artifacts()
                path = next(path for path in records if path.endswith(signed.REQUEST))
                records[path] = value
                with self.assertRaises(ValueError):
                    normalize(root, records)

    def test_contradictory_finished_builds_remain_visible_as_blocked(self):
        for contradiction in ("request_schema", "request_copy", "step_path", "discovery_outcome"):
            with self.subTest(contradiction=contradiction):
                root, records = artifacts()
                request_path = next(path for path in records if path.endswith(signed.REQUEST))
                if contradiction == "request_schema":
                    records[request_path]["pairs"] = []
                elif contradiction == "request_copy":
                    records[request_path.replace("nno-read-signed-target", "other-step")] = copy.deepcopy(records[request_path])
                    records[request_path]["pairs"][0]["id"] = "different"
                elif contradiction == "step_path":
                    path = next(path for path in records if "nno-step-result" in path)
                    records[path.replace("nno-install-signed-driver", "other-step")] = records.pop(path)
                else:
                    discovery(root, records)["outcome"] = "invalid"
                build = normalize(root, records)
                self.assertEqual(build["pairs"][0]["status"], "blocked")
                self.assertTrue(build["pairs"][0]["note"])
                self.assertEqual(build["run"]["ocp_version"], "")
                self.assertEqual(build["run"]["status"], "success")
                self.assertEqual(len(render.mode_data(collect.merge_builds({}, [build]), "signed")["builds"]), 1)

    def test_unparseable_driver_version_blocks_the_pair(self):
        request = next(data for path, data in self.records.items() if path.endswith(signed.REQUEST))
        request["pairs"][0]["driver_requested"]["image"] = "registry.stage.redhat.io/nvidia/doca-driver-rhel9:unparseable"
        pair = self.pair()
        self.assertEqual(pair["status"], "blocked")
        self.assertIn("extract driver version", pair["note"])

    def test_discovery_selection_mismatch_fails_pair(self):
        data = discovery(self.root, self.records)
        data["selected"]["image"] = IMAGE + "wrong"
        self.assertEqual(self.pair()["status"], "failed")

    def test_discovery_digest_mismatch_fails_pair(self):
        data = discovery(self.root, self.records)
        data["selected"]["manifest_digest"] = "sha256:" + "c" * 64
        self.assertEqual(self.pair()["status"], "failed")

    def test_nno_version_drift_fails_pair(self):
        self.result("gpudirect")["nno_csv_version"] = "different"
        self.assertEqual(self.pair()["status"], "failed")

    def test_multiple_invocations_are_not_silently_combined(self):
        path = next(path for path in self.records if "nno-step-result" in path)
        self.records[path + ".duplicate.json"] = copy.deepcopy(self.records[path])
        self.records[path + ".duplicate.json"]["attempt"] = "another-invocation"
        self.assertEqual(self.pair()["status"], "blocked")

    def test_identical_duplicate_records_are_deduplicated(self):
        path = next(path for path in self.records if "nno-step-result" in path)
        self.records[path.replace("-1.json", "-copy.json")] = copy.deepcopy(self.records[path])
        self.assertEqual(self.pair()["status"], "passed")

    def test_finished_provenance_mismatch_is_visible_as_blocked(self):
        request = next(data for path, data in self.records.items() if path.endswith(signed.REQUEST))
        request["provenance"]["build_id"] = "999"
        pair = self.pair()
        self.assertEqual(pair["status"], "blocked")
        self.assertIn("provenance", pair["note"])

    def test_unfinished_provenance_mismatch_is_not_saved_as_permanent(self):
        request = next(data for path, data in self.records.items() if path.endswith(signed.REQUEST))
        request["provenance"]["build_id"] = "999"
        self.records.pop(self.root + "/finished.json")
        with self.assertRaisesRegex(ValueError, "provenance"):
            self.pair()

    def test_finished_contradiction_is_not_downloaded_again_outside_refresh_window(self):
        request = next(data for path, data in self.records.items() if path.endswith(signed.REQUEST))
        request["provenance"]["build_id"] = "999"
        blocked = normalize(self.root, self.records)
        self.assertEqual(blocked["pairs"][0]["status"], "blocked")
        history = collect.merge_builds({}, [blocked])
        with patch.object(collect, "datetime", wraps=datetime) as clock, \
             patch.object(collect, "list_presubmit_builds", return_value=[self.root]), \
             patch.object(collect, "list_gcs_report_artifacts") as listing:
            clock.now.return_value = datetime(2026, 10, 20, tzinfo=UTC)
            builds = collect.load_gcs([], [], indexed_presubmit_jobs=[signed.MANUAL_JOB],
                                      baseline=history, lookback_hours=168)
        listing.assert_not_called()
        self.assertEqual(collect.merge_builds(history, builds), history)

    def test_no_work_and_discovery_failure_have_no_validated_pairs(self):
        for result in ("no_work", "failed"):
            with self.subTest(result=result):
                root, records = artifacts(signed.AUTOMATIC_JOB)
                records = {path: data for path, data in records.items() if "/artifacts/" not in path}
                discovery(root, records, outcome=result)
                if result == "failed":
                    records[root + "/finished.json"]["result"] = "FAILURE"
                build = normalize(root, records)
                self.assertEqual(build["pairs"], [])
                history = collect.merge_builds({}, [build])
                data = render.mode_data(history, "signed")
                self.assertEqual(len(data["builds"]), 1)
                self.assertEqual(data["scheduled_stability"][0]["runs"][0]["passed_pairs"], 0)
                self.assertEqual(collect.merge_builds(history, [build]), history)

    def test_early_failure_without_request_is_visible(self):
        records = {path: data for path, data in self.records.items() if "/artifacts/" not in path}
        records[self.root + "/finished.json"]["result"] = "FAILURE"
        build = normalize(self.root, records)
        self.assertEqual(build["pairs"], [])
        self.assertEqual(build["run"]["status"], "failure")
        self.assertIn("No normalized", build["run"]["note"])

    def test_signed_workflows_always_appear_even_before_any_test_artifacts(self):
        for job in (signed.MANUAL_JOB, signed.AUTOMATIC_JOB):
            for result in ("SUCCESS", "FAILURE", "ABORTED"):
                with self.subTest(job=job, result=result):
                    root, records = artifacts(job)
                    records = {path: data for path, data in records.items() if "/artifacts/" not in path}
                    records[root + "/finished.json"]["result"] = result
                    build = normalize(root, records)
                    data = render.mode_data(collect.merge_builds({}, [build]), "signed")
                    self.assertEqual(len(data["builds"]), 1)
                    self.assertEqual(data["builds"][0]["pairs"], [])
                    if job == signed.AUTOMATIC_JOB:
                        run = data["scheduled_stability"][0]["runs"][0]
                        self.assertEqual(run["status"], "blocked" if result == "SUCCESS" else result.lower())

    def test_signed_workflow_pair_requires_successful_overall_job(self):
        for job in (signed.MANUAL_JOB, signed.AUTOMATIC_JOB):
            for result in ("SUCCESS", "FAILURE", "ABORTED"):
                with self.subTest(job=job, result=result):
                    root, records = artifacts(job)
                    records[root + "/finished.json"]["result"] = result
                    build = normalize(root, records)
                    data = render.mode_data(collect.merge_builds({}, [build]), "signed")
                    pair = data["builds"][0]["pairs"][0]
                    self.assertEqual(pair["status"], "passed" if result == "SUCCESS" else "failed")

    def test_cluster_failure_label_preserves_signed_failure_and_counts(self):
        for job in (signed.MANUAL_JOB, signed.AUTOMATIC_JOB):
            with self.subTest(job=job):
                root, records = artifacts(job)
                records = {path: data for path, data in records.items()
                           if not any('/' + stage + '/' in path for stage in signed.STAGES.values())}
                records[root + "/finished.json"]["result"] = "FAILURE"
                failure = root + "/artifacts/nno-signed-auto/dpf-hypervisor-deploy-cluster/finished.json"
                records[failure] = {"result": "FAILURE"}
                build = normalize(root, records)
                self.assertEqual(build["run"]["failed_steps"], [{
                    "name": "dpf-hypervisor-deploy-cluster",
                    "url": "https://prow.ci.openshift.org/view/gs/test-platform-results-public/" + failure,
                }])
                data = render.mode_data(collect.merge_builds({}, [build]), "signed")
                displayed = data["builds"][0]
                self.assertEqual(displayed["run"]["status"], "failure")
                pair = displayed["pairs"][0]
                self.assertEqual(pair["status"], "failed")
                self.assertEqual(pair["result_label"], "Failed before validation")
                self.assertEqual(pair["result_note"], "Cluster deployment failed — precompiled validation not reached")
                self.assertEqual(pair["checks"]["prow_job"], "failed")
                if job == signed.AUTOMATIC_JOB:
                    run = data["scheduled_stability"][0]["runs"][0]
                    self.assertEqual((run["status"], run["passed_pairs"], run["failed_pairs"]), ("failure", 0, 1))

    def test_missing_artifacts_do_not_imply_a_cluster_deployment_failure(self):
        records = {path: data for path, data in self.records.items()
                   if not any('/' + stage + '/' in path for stage in signed.STAGES.values())}
        records[self.root + "/finished.json"]["result"] = "FAILURE"
        build = normalize(self.root, records)
        data = render.mode_data(collect.merge_builds({}, [build]), "signed")
        pair = data["builds"][0]["pairs"][0]
        self.assertEqual(pair["status"], "failed")
        self.assertEqual(pair["result_label"], "Failed — validation not recorded")
        self.assertNotIn("Cluster deployment failed", pair["result_note"])

    def test_deployment_stage_failure_without_observed_nno_is_not_labeled_early(self):
        records = {path: data for path, data in self.records.items()
                   if not any('/' + stage + '/' in path for stage in signed.STAGES.values())}
        records[self.root + "/finished.json"]["result"] = "FAILURE"
        records[self.root + "/artifacts/nno-signed/" + signed.STAGES["deploy"] + "/finished.json"] = {"result": "FAILURE"}
        build = normalize(self.root, records)
        data = render.mode_data(collect.merge_builds({}, [build]), "signed")
        pair = data["builds"][0]["pairs"][0]
        self.assertEqual(pair["nno_observed"], "")
        self.assertEqual(pair["status"], "failed")
        self.assertEqual(pair["checks"]["deploy_step"], "failed")
        self.assertNotIn("result_label", pair)

    def test_local_and_gcs_artifacts_follow_the_same_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            for relative, record in self.records.items():
                path = Path(directory) / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(json.dumps(record))
            local = collect.load_local(Path(directory))
        with patch.object(collect, "list_presubmit_builds", return_value=[self.root]), \
             patch.object(collect, "list_gcs_report_artifacts", return_value=list(self.records)), \
             patch.object(collect, "_fetch_gcs_json", side_effect=self.records.__getitem__):
            remote = collect.load_gcs([], [], indexed_presubmit_jobs=[signed.MANUAL_JOB])
        self.assertEqual(local, remote)
        self.assertEqual(local[0]["pairs"][0]["status"], "passed")

    def test_missing_completion_is_revisited_after_upload(self):
        finished = self.root + "/finished.json"
        self.records.pop(finished)

        def fetch(path):
            if path in self.records:
                return self.records[path]
            response = collect.requests.Response()
            response.status_code = 404
            raise collect.requests.HTTPError(response=response)

        with patch.object(collect, "list_presubmit_builds", return_value=[self.root]), \
             patch.object(collect, "list_gcs_report_artifacts", side_effect=lambda *args, **kwargs: list(self.records)), \
             patch.object(collect, "_fetch_gcs_json", side_effect=fetch):
            first = collect.load_gcs([], [], indexed_presubmit_jobs=[signed.MANUAL_JOB])
            self.assertEqual(first[0]["run"]["status"], "unknown")
            self.records[finished] = {"result": "SUCCESS"}
            second = collect.load_gcs([], [], indexed_presubmit_jobs=[signed.MANUAL_JOB])
        merged = collect.merge_builds(collect.merge_builds({}, first), second)
        self.assertEqual(len(merged["builds"]), 1)
        self.assertEqual(merged["builds"][0]["run"]["status"], "success")

    def test_transport_failure_is_not_treated_as_missing_artifacts(self):
        with patch.object(collect, "list_presubmit_builds", return_value=[self.root]), \
             patch.object(collect, "list_gcs_report_artifacts", return_value=list(self.records)), \
             patch.object(collect, "_fetch_gcs_json", side_effect=collect.requests.ConnectionError("unavailable")), \
             self.assertRaises(collect.requests.ConnectionError):
            collect.load_gcs([], [], indexed_presubmit_jobs=[signed.MANUAL_JOB])


class SignedDiscoveryTests(unittest.TestCase):
    def test_old_signed_history_refreshes_failed_stage_evidence_once(self):
        root, records = artifacts(signed.AUTOMATIC_JOB)
        records = {path: data for path, data in records.items()
                   if not any('/' + stage + '/' in path for stage in signed.STAGES.values())}
        records[root + "/finished.json"]["result"] = "FAILURE"
        failure = root + "/artifacts/nno-signed-auto/dpf-hypervisor-deploy-cluster/finished.json"
        records[failure] = {"result": "FAILURE"}
        old = normalize(root, records)
        del old["run"]["failed_steps"]
        with patch.object(collect, "datetime", wraps=datetime) as clock, \
             patch.object(collect.gcs_utils, "gcsweb_enabled", return_value=False), \
             patch.object(collect, "list_gcs_report_artifacts", return_value=list(records)), \
             patch.object(collect, "_fetch_gcs_json", side_effect=records.__getitem__) as fetch:
            clock.now.return_value = datetime(2026, 10, 25, tzinfo=UTC)
            builds = collect.load_gcs([], [signed.AUTOMATIC_JOB],
                                      baseline=collect.merge_builds({}, [old]), lookback_hours=168)
            self.assertEqual(len(builds), 1)
            self.assertEqual(builds[0]["run"]["failed_steps"][0]["name"], "dpf-hypervisor-deploy-cluster")
            fetch.reset_mock()
            self.assertEqual(collect.load_gcs([], [signed.AUTOMATIC_JOB],
                                              baseline=collect.merge_builds({}, builds), lookback_hours=168), [])
            fetch.assert_not_called()

    def test_seven_day_refresh_keeps_old_history_and_backfills_missing_builds(self):
        now = datetime(2026, 10, 20, tzinfo=UTC)
        roots, records, baseline = [], {}, []
        for build, age in (("1", 192), ("2", 168), ("3", 24), ("4", 240), ("5", 240)):
            root, current = artifacts(build=build)
            current[root + "/started.json"]["timestamp"] = (now - timedelta(hours=age)).isoformat()
            if build == "5":
                del current[root + "/finished.json"]
            roots.append(root)
            records.update(current)
            if build != "4":
                baseline.append(normalize(root, current))

        def listed(prefix, **kwargs):
            return [path for path in records if path.startswith(prefix)]

        def fetch(path):
            if path in records:
                return records[path]
            response = collect.requests.Response()
            response.status_code = 404
            raise collect.requests.HTTPError(response=response)

        history = collect.merge_builds({}, baseline)
        with patch.object(collect, "datetime", wraps=datetime) as clock, \
             patch.object(collect, "list_presubmit_builds", return_value=roots), \
             patch.object(collect, "list_gcs_report_artifacts", side_effect=listed) as listing, \
             patch.object(collect, "_fetch_gcs_json", side_effect=fetch):
            clock.now.return_value = now
            builds = collect.load_gcs([], [], indexed_presubmit_jobs=[signed.MANUAL_JOB],
                                      max_periodic_builds=None, baseline=history, lookback_hours=168)
        self.assertEqual({build["run"]["build_id"] for build in builds}, {"2", "3", "4", "5"})
        self.assertNotIn(roots[0] + "/", [call.args[0] for call in listing.call_args_list])
        merged = collect.merge_builds(history, builds)
        self.assertEqual(len(merged["builds"]), 5)
        self.assertEqual(next(build for build in merged["builds"] if build["run"]["build_id"] == "1"), baseline[0])

    def test_time_window_includes_all_recent_periodics_without_a_build_count_cap(self):
        now = datetime(2026, 10, 20, tzinfo=UTC)
        records, baseline = {}, []
        for number in range(25):
            root, current = artifacts(signed.AUTOMATIC_JOB, str(number))
            current = {path: data for path, data in current.items() if "/artifacts/" not in path}
            current[root + "/started.json"]["timestamp"] = (now - timedelta(hours=192 if number < 2 else 24)).isoformat()
            discovery(root, current, outcome="no_work")
            records.update(current)
            if number != 1:
                baseline.append(normalize(root, current))
        with patch.object(collect, "datetime", wraps=datetime) as clock, \
             patch.object(collect.gcs_utils, "gcsweb_enabled", return_value=False), \
             patch.object(collect, "list_gcs_report_artifacts", return_value=list(records)), \
             patch.object(collect, "_fetch_gcs_json", side_effect=records.__getitem__) as fetch:
            clock.now.return_value = now
            builds = collect.load_gcs([], [signed.AUTOMATIC_JOB], max_periodic_builds=None,
                                      baseline=collect.merge_builds({}, baseline), lookback_hours=168)
        self.assertEqual({build["run"]["build_id"] for build in builds}, {str(i) for i in range(1, 25)})
        self.assertFalse(any("/0/" in call.args[0] for call in fetch.call_args_list))

    def test_job_index_pagination_and_numeric_order(self):
        job = signed.MANUAL_JOB
        prefix = f"pr-logs/directory/{job}/"

        def item(build):
            return {"name": prefix + build + ".txt", "metadata": {
                "x-goog-meta-link": f"gs://{collect.BUCKET}/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/747/{job}/{build}"}}

        with patch.object(collect, "_gcs_json", side_effect=[
            {"items": [item("9"), {"name": prefix + "latest-build.txt"}], "nextPageToken": "next"},
            {"items": [item("10"), item("100")]},
        ]) as listing:
            builds = collect.list_presubmit_builds(job, 2)
        self.assertEqual([path.rsplit("/", 1)[-1] for path in builds], ["100", "10"])
        self.assertEqual(listing.call_args_list[1].args[0]["pageToken"], "next")

    def test_invalid_index_destination_is_rejected(self):
        with patch.object(collect, "_gcs_json", return_value={"items": [{
            "name": f"pr-logs/directory/{signed.MANUAL_JOB}/123.txt",
            "metadata": {"x-goog-meta-link": "https://elsewhere.invalid/job/123"},
        }]}), self.assertRaisesRegex(ValueError, "invalid Prow job index"):
            collect.list_presubmit_builds(signed.MANUAL_JOB, 20)

    def test_unsupported_signed_sources_cannot_fall_back_to_deployment_only(self):
        job = "pull-ci-other-signed-job"
        root, records = artifacts(job)
        with self.assertRaisesRegex(ValueError, "supported production job"):
            normalize(root, records)
        self.assertTrue(signed.is_signed_job(signed.MANUAL_JOB))
        self.assertTrue(signed.is_signed_job(signed.AUTOMATIC_JOB))
        self.assertFalse(signed.is_signed_job(signed.AUTOMATIC_JOB + "-unrelated"))

    def test_periodic_window_includes_twice_daily_week_and_no_work(self):
        job = signed.AUTOMATIC_JOB
        roots, records = [], {}
        for number in range(1, 22):
            root, current = artifacts(job, str(number))
            current = {path: data for path, data in current.items() if "/artifacts/" not in path}
            discovery(root, current, outcome="no_work")
            roots.append(root)
            records.update(current)
        with patch.object(collect.gcs_utils, "gcsweb_enabled", return_value=False), \
             patch.object(collect, "list_gcs_report_artifacts", return_value=list(records)), \
             patch.object(collect, "_fetch_gcs_json", side_effect=records.__getitem__):
            builds = collect.load_gcs([], [job])
        self.assertEqual(len(builds), 20)
        self.assertEqual({build["run"]["build_id"] for build in builds}, {str(i) for i in range(2, 22)})
        self.assertTrue(all(not build["pairs"] for build in builds))

    def test_no_work_has_neutral_stability_outcome(self):
        root, records = artifacts(signed.AUTOMATIC_JOB)
        records = {path: data for path, data in records.items() if "/artifacts/" not in path}
        discovery(root, records, outcome="no_work")
        run = render.scheduled_stability([normalize(root, records)])[0]["runs"][0]
        self.assertEqual(run["status"], "no_work")
        self.assertEqual(run["reason"], "no_eligible_image")

    def test_signed_metadata_listing_is_filtered_by_source_and_name(self):
        root, records = artifacts()
        paths = list(records) + [root + "/artifacts/unrelated.json", "logs/periodic-other/123/started.json"]
        with patch.object(collect.gcs_utils, "gcsweb_enabled", return_value=False), \
             patch.object(collect, "_gcs_json", return_value={"items": [{"name": path} for path in paths]}):
            self.assertEqual(collect.list_gcs_report_artifacts(root + "/", include_metadata=True), sorted(records))

    def test_curated_listing_supports_signed_evidence_without_brace_globs(self):
        root, records = artifacts()
        with patch.object(collect.gcs_utils, "_use_gcsweb", return_value=True), \
             patch.object(collect.gcs_utils, "_gcsweb_list_all_files", return_value=["curated/" + path for path in records]):
            self.assertEqual(collect.list_gcs_report_artifacts(root + "/", include_metadata=True), sorted(records))

    def test_new_periodic_job_without_builds_is_allowed(self):
        with patch.object(collect.gcs_utils, "gcsweb_enabled", return_value=False), \
             patch.object(collect, "_gcs_json", return_value={}):
            self.assertEqual(collect.load_gcs([], [signed.AUTOMATIC_JOB]), [])

    def test_per_source_workflow_summary(self):
        has_data, summary = workflow.summarize_collection(
            {"builds": []}, {"fetched_builds": 2, "sources": {signed.AUTOMATIC_JOB: {
                "builds": 2, "no_work": 1, "discovery_failed": 1}}}, {"builds": []},
        )
        self.assertTrue(has_data)
        self.assertIn("2 builds, 1 no-work, 1 discovery failure", summary)


if __name__ == "__main__":
    unittest.main()
