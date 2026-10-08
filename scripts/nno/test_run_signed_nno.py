"""Check only the signed NNO invocation and handoff; make is mocked."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from test_validate_signed_request import IMAGE, REQUEST, SCRIPT as VALIDATOR

SCRIPT = Path(__file__).with_name("run-signed-nno.sh")
REPO = SCRIPT.parents[2]
MOCK_MAKE = """import json, os, sys
from pathlib import Path
Path(os.environ['CAPTURE']).write_text(json.dumps({'args': sys.argv[1:], 'env': dict(os.environ)}))
if os.environ.get('MOCK_NO_REPORT'):
    sys.exit(0)
step, attempt = os.environ['NNO_STEP_NAME'], os.environ['NNO_STEP_ATTEMPT']
pair = json.loads((Path(os.environ['SHARED_DIR']) / 'nno-doca2-signed-request.json').read_text())['pairs'][0]
check = 'rdma_gpudirect' if step.endswith('gpudirect') else 'nic_cluster_policy_ready'
result = {'step': step, 'attempt': attempt, 'status': 'passed', 'ocp_version': pair['openshift_version'],
          'checks': dict.fromkeys(['requested_driver_policy', 'requested_driver_running', check], 'passed'),
          'precompiled_selection': {'pull_spec': os.environ['NVIDIANETWORK_OFED_DRIVER_PULLSPEC'],
              'kernel_version': pair['driver_requested']['kernel'], 'architecture': 'amd64'},
          'configured_ofed_driver': {'image_id': 'sha256:' + 'a' * 64}}
artifacts = Path(os.environ['ARTIFACT_DIR'])
artifacts.mkdir(exist_ok=True)
(artifacts / 'nno-step-result-mock.json').write_text(json.dumps(result))
evidence = {'step': step, 'attempt': attempt, 'pull_spec': os.environ['NVIDIANETWORK_OFED_DRIVER_PULLSPEC'],
            'workers': {os.environ['NVIDIANETWORK_RDMA_CLIENT_HOSTNAME']: result['configured_ofed_driver']['image_id'],
                        os.environ['NVIDIANETWORK_RDMA_SERVER_HOSTNAME']: result['configured_ofed_driver']['image_id']}}
(artifacts / 'nno-signed-driver-images.json').write_text(json.dumps(evidence))
"""


class SignedInvocationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.profile = self.root / "profile"
        self.profile.mkdir()
        (self.profile / "nno-e2e-env").write_text(
            'NVIDIANETWORK_OFED_DRIVER_VERSION=profile-version\n'
            'NVIDIANETWORK_OFED_REPOSITORY=profile-repository\n'
            'NVIDIANETWORK_OFED_DRIVER_PULLSPEC=profile-image\n'
            'NVIDIANETWORK_USE_PRECOMPILED_OFED=false\n'
            'NVIDIANETWORK_RDMA_CLIENT_HOSTNAME=rdma-client\n'
            'NVIDIANETWORK_RDMA_SERVER_HOSTNAME=rdma-server\n')
        self.capture = self.root / "capture.json"
        binary = self.root / "bin"
        binary.mkdir()
        (binary / "make").write_text(f'#!{sys.executable}\n' + MOCK_MAKE)
        (binary / "make").chmod(0o755)
        self.env = {"PATH": str(binary) + os.pathsep + os.environ["PATH"], "HOME": os.environ["HOME"],
                    "CLUSTER_PROFILE_DIR": str(self.profile), "SHARED_DIR": str(self.root / "shared"),
                    "ARTIFACT_DIR": str(self.root / "artifacts"), "CAPTURE": str(self.capture)}
        matrix = self.root / "request.json"
        matrix.write_text(json.dumps(REQUEST))
        subprocess.run([sys.executable, str(VALIDATOR), "--matrix", str(matrix),
                        "--shared-dir", self.env["SHARED_DIR"], "--artifact-dir", self.env["ARTIFACT_DIR"]],
                       check=True, capture_output=True)

    def invoke(self, stage, script=SCRIPT):
        return subprocess.run(["bash", str(script), stage], cwd=REPO, env=self.env,
                              capture_output=True, text=True, check=False)

    def test_signed_stages_override_profile_and_keep_existing_test_labels(self):
        for stage, label, gpudirect in [("deploy", "deploy", "false"), ("gpudirect", "rdma-shared-dev", "true")]:
            result = self.invoke(stage)
            self.assertEqual(result.returncode, 0, result.stderr)
            captured = json.loads(self.capture.read_text())
            env = captured["env"]
            self.assertEqual(captured["args"], ["run-tests"])
            self.assertEqual(env["NVIDIANETWORK_OFED_DRIVER_PULLSPEC"], IMAGE)
            self.assertNotIn("NVIDIANETWORK_OFED_DRIVER_VERSION", env)
            self.assertNotIn("NVIDIANETWORK_OFED_REPOSITORY", env)
            self.assertEqual(env["NVIDIANETWORK_USE_PRECOMPILED_OFED"], "true")
            self.assertEqual(env["TEST_LABELS"], label)
            self.assertEqual(env["NVIDIANETWORK_RDMA_GPUDIRECT"], gpudirect)

    def test_missing_handoff_profile_or_bad_stage_fails_before_make(self):
        for name in ["dpf-openshift-version", "ofed-pullspec", "nno-doca2-signed-request.json"]:
            path = self.root / "shared" / name
            content = path.read_bytes()
            for invalid in [None, b""] + ([b"\n"] if name != "nno-doca2-signed-request.json" else []):
                if invalid is None:
                    path.unlink()
                else:
                    path.write_bytes(invalid)
                result = self.invoke("deploy")
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.capture.exists())
            path.write_bytes(content)
        for stage in ["day2", "install-driver signed", "regular", "unknown"]:
            self.assertNotEqual(self.invoke(stage).returncode, 0)
            self.assertFalse(self.capture.exists())
        (self.profile / "nno-e2e-env").unlink()
        self.assertNotEqual(self.invoke("deploy").returncode, 0)
        self.assertFalse(self.capture.exists())

    def test_successful_make_without_test_evidence_fails(self):
        self.env["MOCK_NO_REPORT"] = "true"
        for stage in ["deploy", "gpudirect"]:
            result = self.invoke(stage)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("no result for signed step", result.stderr)

    @unittest.skipUnless(os.environ.get("DOCA2_RELEASE_DIR"), "set DOCA2_RELEASE_DIR to check release callers")
    def test_release_signed_callers(self):
        registry = Path(os.environ["DOCA2_RELEASE_DIR"]) / "ci-operator/step-registry/nno/doca2"
        for directory in ["install-signed-driver", "test-gpudirect"]:
            script = registry / directory / f"nno-doca2-{directory}-commands.sh"
            result = subprocess.run(["bash", str(script)], cwd=REPO, env=self.env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
