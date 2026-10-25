"""Require this invocation's selected driver on both workers and passing tests."""

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from test_validate_signed_request import IMAGE, KERNEL, REQUEST

spec = importlib.util.spec_from_file_location("signed_result", Path(__file__).with_name("verify-signed-step-result.py"))
signed_result = importlib.util.module_from_spec(spec)
spec.loader.exec_module(signed_result)
STEP = "network-operator-e2e-signed-gpudirect"
WORKERS = ["rdma-client", "rdma-server"]
DIGEST = "sha256:" + "a" * 64
# Runtime ID recorded by both workers in failed build 2113306987105095680.
BARE_CONFIG_ID = "e65fc701cc1488be5ad4b0e8c11a65af604b1cc016a8b92406641529072c23e1"
SELECTED = dict(REQUEST["pairs"][0]["driver_requested"], manifest_digest=DIGEST,
                config_digest="sha256:" + BARE_CONFIG_ID, index_digest="sha256:" + "c" * 64,
                release={"installer_version": "4.22.15"})
REPORT = {
    "step": STEP, "attempt": "this-invocation", "status": "passed", "ocp_version": "4.22.15",
    "nno_csv_version": "26.7.0-1",
    "checks": dict.fromkeys(["requested_driver_policy", "requested_driver_running", "rdma_gpudirect"], "passed"),
    "precompiled_selection": {"pull_spec": IMAGE, "kernel_version": KERNEL, "architecture": "amd64"},
    "configured_ofed_driver": {"image_id": IMAGE.split(":")[0] + "@" + DIGEST},
}
EVIDENCE = {"step": STEP, "attempt": "this-invocation", "pull_spec": IMAGE,
            "workers": dict.fromkeys(WORKERS, REPORT["configured_ofed_driver"]["image_id"])}


class ResultTests(unittest.TestCase):
    def invoke(self, report=REPORT, evidence=EVIDENCE, request=REQUEST, selected=None):
        with tempfile.TemporaryDirectory() as directory:
            if report is not None:
                (Path(directory) / "nno-step-result-test.json").write_text(json.dumps(report))
            if evidence is not None:
                (Path(directory) / "nno-signed-driver-images.json").write_text(json.dumps(evidence))
            return signed_result.verify(directory, STEP, "this-invocation", request, WORKERS, selected)

    def runtime_evidence(self, image_id):
        report, evidence = copy.deepcopy(REPORT), copy.deepcopy(EVIDENCE)
        report["configured_ofed_driver"]["image_id"] = image_id
        evidence["workers"] = dict.fromkeys(WORKERS, image_id)
        return report, evidence

    def test_verified_bare_config_id_preserves_raw_evidence(self):
        report, evidence = self.runtime_evidence(BARE_CONFIG_ID)
        self.assertEqual(self.invoke(report, evidence, selected=SELECTED), {SELECTED["config_digest"]})
        self.assertEqual(report["configured_ofed_driver"]["image_id"], BARE_CONFIG_ID)
        self.assertEqual(set(evidence["workers"].values()), {BARE_CONFIG_ID})
        # Qualified config evidence and verified bare config evidence have the same bytes.
        evidence["workers"][WORKERS[1]] = "docker-pullable://registry/example@" + SELECTED["config_digest"]
        self.invoke(report, evidence, selected=SELECTED)

    def test_bare_id_requires_exact_trustworthy_config_metadata(self):
        report, evidence = self.runtime_evidence(BARE_CONFIG_ID)
        for selected in [None, {}, {**SELECTED, "config_digest": None},
                         {**SELECTED, "config_digest": ""}, {**SELECTED, "config_digest": BARE_CONFIG_ID},
                         {**SELECTED, "config_digest": "sha256:short"},
                         {**SELECTED, "config_digest": "sha256:" + "b" * 64}]:
            with self.subTest(selected=selected), self.assertRaises(ValueError):
                self.invoke(report, evidence, selected=selected)
        with self.assertRaisesRegex(ValueError, "manual requests"):
            self.invoke(report, evidence)

    def test_bare_manifest_and_index_ids_are_not_config_evidence(self):
        for field in ["manifest_digest", "index_digest"]:
            report, evidence = self.runtime_evidence(SELECTED[field].removeprefix("sha256:"))
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "config digest"):
                self.invoke(report, evidence, selected=SELECTED)
            report, evidence = self.runtime_evidence(SELECTED[field])
            self.invoke(report, evidence, selected=SELECTED)

    def test_malformed_bare_ids_fail_in_reports_and_workers(self):
        for image_id in ["", "a" * 63, "a" * 65, "g" * 64, BARE_CONFIG_ID.upper(),
                         " " + BARE_CONFIG_ID, BARE_CONFIG_ID + "\n", "sha512:" + BARE_CONFIG_ID,
                         "registry/example@" + BARE_CONFIG_ID, "docker://" + BARE_CONFIG_ID, None, 123]:
            for location in ["report", "worker"]:
                report, evidence = self.runtime_evidence(BARE_CONFIG_ID)
                if location == "report":
                    report["configured_ofed_driver"]["image_id"] = image_id
                else:
                    evidence["workers"][WORKERS[1]] = image_id
                with self.subTest(image_id=image_id, location=location), self.assertRaises(ValueError):
                    self.invoke(report, evidence, selected=SELECTED)

    def test_verified_config_does_not_relax_agreement_or_explicit_digest(self):
        report, evidence = self.runtime_evidence(BARE_CONFIG_ID)
        for worker_ids in [{WORKERS[0]: BARE_CONFIG_ID, WORKERS[1]: DIGEST}, dict.fromkeys(WORKERS, DIGEST)]:
            with self.subTest(workers=worker_ids), self.assertRaisesRegex(ValueError, "agree"):
                self.invoke(report, {**evidence, "workers": worker_ids}, selected=SELECTED)
        for requested in [SELECTED["config_digest"], SELECTED["manifest_digest"], SELECTED["index_digest"]]:
            request = copy.deepcopy(REQUEST)
            request["pairs"][0]["driver_requested"]["digest"] = requested
            if requested == SELECTED["config_digest"]:
                self.invoke(report, evidence, request, SELECTED)
            else:
                with self.assertRaisesRegex(ValueError, "requested runtime digest"):
                    self.invoke(report, evidence, request, SELECTED)

    def test_qualified_digest_still_requires_selected_image_membership(self):
        report, evidence = self.runtime_evidence("sha256:" + "d" * 64)
        with self.assertRaisesRegex(ValueError, "selected manifest/config/index"):
            self.invoke(report, evidence, selected=SELECTED)

    def test_selection_must_match_job_build_and_exact_request(self):
        selection = dict(outcome="selected", job_name="signed-job", build_id="123", selected=SELECTED)
        with tempfile.TemporaryDirectory() as directory, \
                patch.dict(signed_result.os.environ, {"JOB_NAME": "signed-job", "BUILD_ID": "123"}):
            path = Path(directory) / "nno-discovery-result.json"
            path.write_text(json.dumps(selection))
            self.assertEqual(signed_result.selected_image(Path(directory), REQUEST), SELECTED)
            for key, value in [("job_name", "other-job"), ("build_id", "122"), ("outcome", "failed")]:
                path.write_text(json.dumps({**selection, key: value}))
                with self.subTest(key=key), self.assertRaises(ValueError):
                    signed_result.selected_image(Path(directory), REQUEST)
            for key, value in [("image", IMAGE + "-other"), ("kernel", "other-kernel"),
                               ("architecture", "arm64"), ("release", {"installer_version": "4.22.1"})]:
                path.write_text(json.dumps({**selection, "selected": {**SELECTED, key: value}}))
                with self.subTest(key=key), self.assertRaises(ValueError):
                    signed_result.selected_image(Path(directory), REQUEST)

    def test_selected_driver_on_both_workers_and_gpudirect_pass(self):
        self.invoke()
        for image_id in [DIGEST, "docker-pullable://" + REPORT["configured_ofed_driver"]["image_id"]]:
            evidence = copy.deepcopy(EVIDENCE)
            evidence["workers"] = dict.fromkeys(WORKERS, image_id)
            self.invoke(evidence=evidence)

    def test_absent_skipped_failed_or_stale_reports_fail(self):
        with self.assertRaises(ValueError):
            self.invoke(report=None)
        for key, value in [("status", "skipped"), ("status", "failed"), ("attempt", "previous"),
                           ("step", "regular-step"), ("ocp_version", "4.22.1")]:
            report = copy.deepcopy(REPORT)
            report[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.invoke(report=report)
        for check in REPORT["checks"]:
            report = copy.deepcopy(REPORT)
            report["checks"][check] = "not_run"
            with self.subTest(check=check), self.assertRaises(ValueError):
                self.invoke(report=report)

    def test_nno_csv_must_match_the_image_release_in_each_signed_stage(self):
        for version, passes in [("26.7.0-1", True), ("26.7.1", True), ("26.7.1+build.2", True),
                                ("26.4.1-1", False), ("26.70.0", False), ("26.7", False),
                                ("", False), (None, False)]:
            report = copy.deepcopy(REPORT)
            report["nno_csv_version"] = version
            with self.subTest(version=version):
                if passes:
                    self.invoke(report=report)
                else:
                    with self.assertRaisesRegex(ValueError, "NNO CSV"):
                        self.invoke(report=report)

    def test_installer_suffixes_preserve_exact_release_version(self):
        for requested, observed, passes in [
            ("4.22.0-x86_64", "4.22.0", True),
            ("4.22.0-multi", "4.22.0", True),
            ("4.22.0-aarch64", "4.22.0", True),
            ("4.22.0-ec.1", "4.22.0-ec.1", True),
            ("4.22.0-ec.1-multi", "4.22.0-ec.1", True),
            ("4.22.1-x86_64", "4.22.0", False),
            ("4.22.0-ec.1-multi", "4.22.0", False),
            ("4.22.0-ec.1-multi", "4.22.0-ec.2", False),
        ]:
            request = copy.deepcopy(REQUEST)
            request["pairs"][0]["openshift_version"] = requested
            report = copy.deepcopy(REPORT)
            report["ocp_version"] = observed
            with self.subTest(requested=requested, observed=observed):
                if passes:
                    self.invoke(report=report, request=request)
                else:
                    with self.assertRaises(ValueError):
                        self.invoke(report=report, request=request)

    def test_wrong_image_kernel_architecture_or_missing_digest_fails(self):
        for key, value in [("pull_spec", "another-image"), ("kernel_version", "another-kernel"),
                           ("architecture", "arm64"), ("kernel_version", "")]:
            report = copy.deepcopy(REPORT)
            report["precompiled_selection"][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.invoke(report=report)
        for image_id in ["", IMAGE, "sha256:short", "sha256:" + "z" * 64]:
            evidence = copy.deepcopy(EVIDENCE)
            evidence["workers"][WORKERS[1]] = image_id
            with self.subTest(image_id=image_id), self.assertRaises(ValueError):
                self.invoke(evidence=evidence)

    def test_worker_evidence_must_match_invocation_and_both_rdma_workers(self):
        with self.assertRaises(OSError):
            self.invoke(evidence=None)
        for key, value in [("step", "regular-step"), ("attempt", "previous"), ("pull_spec", "another-image"),
                           ("workers", {WORKERS[0]: DIGEST}), ("workers", {WORKERS[0]: DIGEST, "unrelated-worker": DIGEST})]:
            evidence = copy.deepcopy(EVIDENCE)
            evidence[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.invoke(evidence=evidence)

    def test_optional_digest_compares_both_runtime_images(self):
        request = copy.deepcopy(REQUEST)
        request["pairs"][0]["driver_requested"]["digest"] = DIGEST
        self.invoke(request=request)
        evidence = copy.deepcopy(EVIDENCE)
        evidence["workers"][WORKERS[1]] = "sha256:" + "b" * 64
        with self.assertRaises(ValueError):
            self.invoke(evidence=evidence, request=request)

    def test_reports_and_both_workers_must_agree_without_optional_assertion(self):
        evidence = copy.deepcopy(EVIDENCE)
        evidence["workers"][WORKERS[1]] = "sha256:" + "b" * 64
        with self.assertRaisesRegex(ValueError, "agree"):
            self.invoke(evidence=evidence)
        evidence["workers"] = dict.fromkeys(WORKERS, "sha256:" + "b" * 64)
        with self.assertRaisesRegex(ValueError, "agree"):
            self.invoke(evidence=evidence)


if __name__ == "__main__":
    unittest.main()
