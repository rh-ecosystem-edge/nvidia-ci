"""Require this invocation's selected driver on both workers and passing tests."""

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

from test_validate_signed_request import IMAGE, KERNEL, REQUEST

spec = importlib.util.spec_from_file_location("signed_result", Path(__file__).with_name("verify-signed-step-result.py"))
signed_result = importlib.util.module_from_spec(spec)
spec.loader.exec_module(signed_result)
STEP = "network-operator-e2e-signed-gpudirect"
WORKERS = ["rdma-client", "rdma-server"]
DIGEST = "sha256:" + "a" * 64
REPORT = {
    "step": STEP, "attempt": "this-invocation", "status": "passed", "ocp_version": "4.22.0",
    "checks": dict.fromkeys(["requested_driver_policy", "requested_driver_running", "rdma_gpudirect"], "passed"),
    "precompiled_selection": {"pull_spec": IMAGE, "kernel_version": KERNEL, "architecture": "amd64"},
    "configured_ofed_driver": {"image_id": IMAGE.split(":")[0] + "@" + DIGEST},
}
EVIDENCE = {"step": STEP, "attempt": "this-invocation", "pull_spec": IMAGE,
            "workers": dict.fromkeys(WORKERS, REPORT["configured_ofed_driver"]["image_id"])}


class ResultTests(unittest.TestCase):
    def invoke(self, report=REPORT, evidence=EVIDENCE, request=REQUEST):
        with tempfile.TemporaryDirectory() as directory:
            if report is not None:
                (Path(directory) / "nno-step-result-test.json").write_text(json.dumps(report))
            if evidence is not None:
                (Path(directory) / "nno-signed-driver-images.json").write_text(json.dumps(evidence))
            signed_result.verify(directory, STEP, "this-invocation", request, WORKERS)

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


if __name__ == "__main__":
    unittest.main()
