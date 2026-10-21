"""Cluster-free tests for the signed request contract and shared-file handoff."""

import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from signed_images import nno_channel

SCRIPT = Path(__file__).with_name("validate-signed-request.py")
KERNEL = "5.14.0-687.48.1.el9_8.x86_64"
IMAGE = f"registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-{KERNEL}-rhel9.8-amd64"
REQUEST = {
    "schema_version": 1,
    "pairs": [{
        "id": "ocp-4.22.15-doca-26.07-amd64",
        "status": "planned",
        "openshift_version": "4.22.15",
        "driver_requested": {"image": IMAGE, "kernel": KERNEL, "architecture": "amd64"},
    }],
}


class RequestTests(unittest.TestCase):
    def invoke(self, root, request=REQUEST, raw=None, shared=True, artifacts=True):
        matrix = root / "request.json"
        if request is not None:
            matrix.write_text(raw if raw is not None else json.dumps(request))
        env = dict(os.environ, JOB_NAME="manual-signed", BUILD_ID="123", PULL_NUMBER="42",
                   PULL_PULL_SHA="abcdef", PULL_REFS="main:abc,42:def")
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--matrix", str(matrix),
             "--shared-dir", str(root / "shared") if shared else "",
             "--artifact-dir", str(root / "artifacts") if artifacts else ""],
            env=env, capture_output=True, text=True, check=False,
        )

    def test_normalized_request_and_handoff(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = copy.deepcopy(REQUEST)
            request["pairs"].append({"id": "old", "status": "passed"})
            request["pairs"][0]["openshift_version"] = " 4.22.15 "
            result = self.invoke(root, request)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "shared/dpf-openshift-version").read_text(), "4.22.15\n")
            self.assertEqual((root / "shared/ofed-pullspec").read_text(), IMAGE + "\n")
            normalized = (root / "shared/nno-doca2-signed-request.json").read_text()
            self.assertEqual(normalized, (root / "artifacts/nno-doca2-signed-request.json").read_text())
            self.assertEqual(json.loads(normalized)["pairs"], REQUEST["pairs"])
            self.assertEqual(json.loads(normalized)["provenance"], {
                "job_name": "manual-signed", "build_id": "123", "pull_number": "42",
                "pull_sha": "abcdef", "pull_refs": "main:abc,42:def",
            })

    def test_suffix_and_digest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = copy.deepcopy(REQUEST)
            request["pairs"][0]["openshift_version"] = "4.22.0-ec.1"
            request["pairs"][0]["driver_requested"]["digest"] = "sha256:" + "a" * 64
            result = self.invoke(root, request)
            self.assertEqual(result.returncode, 0, result.stderr)
            normalized = json.loads((root / "shared/nno-doca2-signed-request.json").read_text())
            self.assertEqual(normalized["pairs"][0]["driver_requested"]["digest"], "sha256:" + "a" * 64)

    def test_legacy_rhcos_tag(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = copy.deepcopy(REQUEST)
            request["pairs"][0]["openshift_version"] = "4.16.0"
            request["pairs"][0]["driver_requested"]["image"] = IMAGE.replace("rhel9.8", "rhcos4.16")
            result = self.invoke(root, request)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_driver_release_selects_the_nno_channel_without_an_input_field(self):
        for version, channel in [("26.04-1.1.0.0", "v26.4"), ("26.01-1.1.0.0", "v26.1"),
                                 ("25.10-0.7.0.0", "v25.10"), ("26.07-0.7.7.0", "v26.7"),
                                 ("doca3.4.0-26.04-0.8.5.0-1", "v26.4"),
                                 ("26.04-OFED.26.04.1.1.0.0", "v26.4")]:
            with self.subTest(version=version), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                request = copy.deepcopy(REQUEST)
                image = IMAGE.replace("26.07-0.7.7.0", version)
                request["pairs"][0]["driver_requested"]["image"] = image
                result = self.invoke(root, request)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(nno_channel(image), channel)
                self.assertIn("nno_channel=" + channel, result.stdout)

    def test_invalid_inputs_write_nothing(self):
        cases = {"missing": None, "not-object": [], "no-schema": {"pairs": REQUEST["pairs"]}}
        for name, key, value in [
            ("schema-bool", "schema_version", True), ("schema-version", "schema_version", 2),
            ("no-pairs", "pairs", None), ("zero-planned", "pairs", []),
            ("two-planned", "pairs", REQUEST["pairs"] * 2),
        ]:
            request = copy.deepcopy(REQUEST)
            request[key] = value
            cases[name] = request
        for name, key, value in [
            ("bad-id", "id", "../bad"), ("missing-id", "id", None),
            ("minor-version", "openshift_version", "4.22"),
            ("version-type", "openshift_version", 4.22),
            ("bad-version", "openshift_version", "4.22.0; touch /tmp/no"),
            ("missing-driver", "driver_requested", None),
        ]:
            request = copy.deepcopy(REQUEST)
            request["pairs"][0][key] = value
            cases[name] = request
        for name, key, value in [
            ("missing-image", "image", None), ("incomplete-image", "image", "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07"),
            ("production-image", "image", IMAGE.replace("registry.stage.", "registry.")),
            ("digest-only", "image", IMAGE.split(":")[0] + "@sha256:" + "a" * 64),
            ("bad-os-tag", "image", IMAGE.replace("rhel9.8", "bad/os")),
            ("unknown-release", "image", IMAGE.replace("26.07-0.7.7.0", "unknown-1.1.0.0")),
            ("not-quarterly", "image", IMAGE.replace("26.07-0.7.7.0", "26.08-1.1.0.0")),
            ("kernel-mismatch", "kernel", "wrong"), ("kernel-type", "kernel", 123),
            ("arch-mismatch", "architecture", "arm64"), ("arch-invalid", "architecture", "x86_64"),
            ("digest-invalid", "digest", "sha256:short"),
        ]:
            request = copy.deepcopy(REQUEST)
            request["pairs"][0]["driver_requested"][key] = value
            cases[name] = request
        for name, request in cases.items():
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                result = self.invoke(root, request)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("signed DOCA2 request error", result.stderr)
                self.assertFalse((root / "shared").exists())
                self.assertFalse((root / "artifacts").exists())

    def test_malformed_json_and_missing_output_directories(self):
        for options in [{"raw": "{"}, {"shared": False}, {"artifacts": False}]:
            with self.subTest(options=options), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                result = self.invoke(root, **options)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((root / "shared").exists())
                self.assertFalse((root / "artifacts").exists())


class LatestChannelTests(unittest.TestCase):
    def test_latest_ga_uses_configured_catalog(self):
        import signed_images
        from unittest.mock import patch
        manifest = {'items': [
            {'status': {'catalogSource': catalog, 'channels': [
                {'name': channel, 'currentCSV': 'nno.'+version, 'currentCSVDesc': {'version': version}}
            ]}}
            for catalog, channel, version in [('certified-operators', 'v26.7', '26.7.0'),
                                               ('custom-nno', 'v26.04', '26.4.0')]
        ]}
        with patch.dict(os.environ, {'NVIDIANETWORK_CATALOGSOURCE': 'custom-nno'}, clear=True), \
                patch.object(signed_images, 'command', return_value=json.dumps(manifest)):
            self.assertEqual(signed_images.resolve_nno_channel(), 'v26.04')

    def test_latest_ga_and_job_handoff(self):
        import signed_images
        from unittest.mock import patch
        def channel(name, version, **extra):
            return dict(name=name, currentCSV='nno.'+version, currentCSVDesc={'version': version}, **extra)
        channels = [channel('v26.04', '26.4.0-1'), channel('v26.7', '26.7.0-beta.2'),
                    channel('v26.10', '26.10.0', deprecation={}), channel('v26.1', '26.1.0')]
        self.assertEqual(signed_images.latest_ga(channels), 'v26.04')
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'SHARED_DIR': directory}, clear=True):
            manifest = {'items': [{'status': {'catalogSource': 'certified-operators', 'channels': channels}}]}
            with patch.object(signed_images, 'command', return_value=json.dumps(manifest)) as query:
                self.assertEqual(signed_images.resolve_nno_channel(), 'v26.04')
                channels.append(channel('v26.7', '26.7.0-1'))
                self.assertEqual(signed_images.resolve_nno_channel(), 'v26.04')
                self.assertEqual(query.call_count, 1)
            self.assertEqual(signed_images.latest_ga(channels), 'v26.7')
        for unavailable in [[], [channel('v26.7', '26.7.0-rc.1')], [channel('v26.7', '26.4.0')]]:
            with self.assertRaises(ValueError):
                signed_images.latest_ga(unavailable)


if __name__ == "__main__":
    unittest.main()
