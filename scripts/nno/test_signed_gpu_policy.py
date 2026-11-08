"""Check the signed GPU policy override without running a Ginkgo suite."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from test_validate_signed_request import REQUEST


REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "scripts/test-runner.sh"
LEGACY_PATCH = [{"op": "add", "path": "/spec/driver/rdma", "value": {"enabled": True}}]
DMA_BUF_PATCH = [
    {"op": "add", "path": "/spec/driver/enabled", "value": True},
    {"op": "add", "path": "/spec/driver/rdma", "value": {"enabled": False}},
    {"op": "add", "path": "/spec/driver/kernelModuleType", "value": "open"},
]


class SignedGPUPolicyTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.shared = self.root / "shared"
        self.shared.mkdir()
        self.handoff = self.shared / "nno-doca2-signed-request.json"
        self.handoff.write_text(json.dumps(REQUEST))
        self.capture = self.root / "capture.json"
        binary = self.root / "bin"
        binary.mkdir()
        (binary / "ginkgo").write_text(
            f"#!{sys.executable}\n"
            "import json, os, sys\n"
            "from pathlib import Path\n"
            "Path(os.environ['CAPTURE']).write_text(json.dumps(dict(os.environ)))\n"
            "sys.exit(int(os.environ.get('MOCK_GINKGO_EXIT', '0')))\n")
        (binary / "ginkgo").chmod(0o755)
        self.env = {
            "PATH": str(binary) + os.pathsep + os.environ["PATH"],
            "HOME": os.environ["HOME"],
            "SHARED_DIR": str(self.shared),
            "CAPTURE": str(self.capture),
            "TEST_FEATURES": "nvidiagpu",
            "NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH": json.dumps(LEGACY_PATCH),
            "NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH": "595.91.07",
        }

    def invoke(self):
        return subprocess.run(["bash", str(SCRIPT)], cwd=REPO, env=self.env,
                              capture_output=True, text=True, check=False)

    def test_signed_gpu_overrides_legacy_patch_before_ginkgo(self):
        original = LEGACY_PATCH + [
            {"op": "add", "path": "/spec/driver/kernelModuleType", "value": "auto"},
            {"op": "add", "path": "/spec/toolkit/enabled", "value": True},
        ]
        self.env["NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"] = json.dumps(original)
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        patch = json.loads(json.loads(self.capture.read_text())["NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"])
        self.assertEqual(patch, original + DMA_BUF_PATCH)
        self.assertEqual(json.loads(self.capture.read_text())["NVIDIAGPU_USE_PRECOMPILED_DRIVER"], "true")
        self.assertIn("DMA-BUF", result.stdout)

    def test_signed_gpu_without_existing_patch(self):
        for value in [None, ""]:
            with self.subTest(value=value):
                self.env.pop("NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH", None)
                if value is not None:
                    self.env["NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"] = value
                result = self.invoke()
                self.assertEqual(result.returncode, 0, result.stderr)
                patch = json.loads(json.loads(self.capture.read_text())["NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"])
                self.assertEqual(patch, DMA_BUF_PATCH)

    def test_regular_gpu_and_signed_network_keep_existing_patch(self):
        """Leave policy and precompiled mode unchanged outside the signed GPU gate."""
        for case in ["no-shared-dir", "no-handoff", "empty-handoff", "network", "all"]:
            with self.subTest(case=case):
                self.env["SHARED_DIR"] = str(self.shared)
                self.env["TEST_FEATURES"] = "nvidiagpu"
                self.handoff.write_text(json.dumps(REQUEST))
                if case == "no-shared-dir":
                    self.env.pop("SHARED_DIR")
                elif case == "no-handoff":
                    self.handoff.unlink()
                elif case == "empty-handoff":
                    self.handoff.write_text("")
                elif case == "network":
                    self.env["TEST_FEATURES"] = "nvidianetwork"
                else:
                    self.env["TEST_FEATURES"] = "all"
                result = self.invoke()
                self.assertEqual(result.returncode, 0, result.stderr)
                captured = json.loads(self.capture.read_text())
                self.assertEqual(captured["NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"], json.dumps(LEGACY_PATCH))
                self.assertNotIn("DMA-BUF", result.stdout)
                self.assertNotIn("NVIDIAGPU_USE_PRECOMPILED_DRIVER", captured)

    def test_signed_gpu_requires_one_full_release(self):
        """Reject explicit floating, list, and malformed releases before deployment."""
        for value in [" ", "595", "all", "595.91.07,580.178.04", "595.91", "595.91.07-source"]:
            with self.subTest(value=value):
                self.env["NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"] = value
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("one full GPU release", result.stderr)
                self.assertFalse(self.capture.exists())

    def test_signed_gpu_defaults_release_before_release_wiring(self):
        """Keep signed jobs usable before the release step exposes the input."""
        for value in [None, ""]:
            with self.subTest(value=value):
                self.env.pop("NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH", None)
                if value is not None:
                    self.env["NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"] = value
                result = self.invoke()
                self.assertEqual(result.returncode, 0, result.stderr)
                captured = json.loads(self.capture.read_text())
                self.assertEqual(captured["NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"], "595.91.07")
                self.assertEqual(captured["NVIDIAGPU_USE_PRECOMPILED_DRIVER"], "true")
                self.assertIn("595.91.07", result.stdout)

    def test_signed_gpu_preserves_explicit_full_release(self):
        """Keep a caller-selected release instead of substituting the default."""
        self.env["NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"] = "580.178.04"
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        captured = json.loads(self.capture.read_text())
        self.assertEqual(captured["NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"], "580.178.04")
        self.assertIn("580.178.04", result.stdout)

    def test_unsigned_selection_modes_remain_available(self):
        """Preserve all existing selections and avoid defaulting unsigned runs."""
        self.handoff.unlink()
        for value in [None, "", "all", "595", "580.178.04,595.91.07"]:
            with self.subTest(value=value):
                self.env.pop("NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH", None)
                if value is not None:
                    self.env["NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"] = value
                result = self.invoke()
                self.assertEqual(result.returncode, 0, result.stderr)
                captured = json.loads(self.capture.read_text())
                self.assertEqual(captured.get("NVIDIAGPU_PRECOMPILED_DRIVER_BRANCH"), value)
                self.assertNotIn("NVIDIAGPU_USE_PRECOMPILED_DRIVER", captured)

    def test_invalid_signed_patch_fails_before_ginkgo(self):
        for value in ["not-json", "{}", "null"]:
            with self.subTest(value=value):
                self.env["NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"] = value
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("signed NNO GPU policy error", result.stderr)
                self.assertFalse(self.capture.exists())

    def test_ginkgo_failure_is_preserved(self):
        self.env["MOCK_GINKGO_EXIT"] = "7"
        self.assertEqual(self.invoke().returncode, 7)


if __name__ == "__main__":
    unittest.main()
