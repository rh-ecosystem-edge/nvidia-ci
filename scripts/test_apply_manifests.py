"""Check NNO batching and pause recovery with a mocked oc; no cluster required."""

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
MOCK_OC = r'''
import json, os, re, signal, sys
from pathlib import Path

args = sys.argv[1:]
state_file = Path(os.environ["MOCK_STATE"])
state = json.loads(state_file.read_text())
paused = state["paused"]
mode = os.environ["MOCK_MODE"]
event = {"args": args, "paused": paused}
if args[0] in ("apply", "create", "delete"):
    manifest = sys.stdin.read()
    if manifest.lstrip().startswith("{"):
        objects = [json.loads(manifest)]
    else:
        objects = [{"apiVersion": re.search(r"^apiVersion: (.+)$", doc, re.M)[1],
                    "kind": re.search(r"^kind: (.+)$", doc, re.M)[1],
                    "metadata": {"name": re.search(r"^ +name: (.+)$", doc, re.M)[1]}}
                   for doc in re.split(r"^---\s*$", manifest, flags=re.M) if doc.strip()]
    event.update(name=objects[0]["metadata"]["name"], kind=objects[0]["kind"])
with open(os.environ["MOCK_EVENTS"], "a") as log:
    log.write(json.dumps(event) + "\n")

if args[0] == "get":
    if args[1].startswith("machineconfig/"):
        if mode == "existing":
            print(args[1])
        sys.exit(0)
    rendered = state.get("applied") and set(state["applied"]) <= set(state.get("sources", []))
    target = "rendered-new" if rendered else "rendered-old"
    pool = {"metadata": {"generation": 1, "resourceVersion": "1",
                         "annotations": state.get("annotations", {})},
            "spec": {"paused": paused, "configuration": {"name": target}},
            "status": {"observedGeneration": 1,
                       "configuration": {"name": "rendered-old" if paused else target},
                       "conditions": [{"type": "Updated", "status": "True"}]}}
    if mode == "stale" and target == "rendered-new" and not paused:
        pool["status"]["observedGeneration"] = 0
    print(json.dumps(pool))
elif args[0] == "patch":
    patch = json.loads(args[args.index("-p") + 1])
    paused = next(op["value"] for op in patch if op["path"] == "/spec/paused")
    if paused:
        state["annotations"] = next(op["value"] for op in patch if op["path"] == "/metadata/annotations")
        if mode == "pause-conflict":
            state["annotations"]["nvidia-ci/pause-owner"] = "another-owner"
    else:
        assert patch[0]["value"] == state["annotations"]["nvidia-ci/pause-owner"]
        state["annotations"].pop("nvidia-ci/pause-owner")
    state["paused"] = paused
    state_file.write_text(json.dumps(state))
    if paused and mode in ("pause-error", "pause-conflict"):
        sys.exit(7)
    if paused and mode == "term-pause":
        os.kill(os.getppid(), signal.SIGTERM)
elif args[0] in ("apply", "create"):
    if "--dry-run=client" in args:
        print(json.dumps(objects[0] if len(objects) == 1 else {"kind": "List", "items": objects}))
        sys.exit(0)
    if mode == "apply-fail" or (mode == "create-race" and args[0] == "create"):
        sys.exit(7)
    state.setdefault("applied", []).extend(obj["metadata"]["name"] for obj in objects
                                          if obj["kind"] == "MachineConfig")
    state_file.write_text(json.dumps(state))
    if mode == "term-apply":
        os.kill(os.getppid(), signal.SIGTERM)
        sys.exit(143)
    action = "configured" if mode == "existing" else "created"
    print(f"{event['kind']}/{event['name']} {action}")
elif args[0] == "delete":
    print(f"{event['kind']}/{event['name']} deleted")
elif args[0] == "wait":
    if any(".spec.configuration.source" in arg for arg in args):
        assert paused
        name = re.search(r'@\.name=="([^"]+)"', " ".join(args))[1]
        assert name in state["applied"]
        state.setdefault("sources", []).append(name)
        state_file.write_text(json.dumps(state))
    if mode == "render-timeout" and state.get("sources"):
        sys.exit(1)
    if any(".status.configuration.name" in arg for arg in args):
        assert not paused and set(state["sources"]) == set(state["applied"])
        assert "--for=jsonpath={.status.configuration.name}=rendered-new" in args
else:
    sys.exit(f"Unexpected oc command: {args}")
'''


@unittest.skipUnless(shutil.which("envsubst"), "requires envsubst")
class ApplyManifestsTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        binary = root / "bin"
        binary.mkdir()
        oc = binary / "oc"
        oc.write_text(f"#!{sys.executable}\n" + MOCK_OC)
        oc.chmod(0o755)
        self.state = root / "paused.json"
        self.log = root / "events.jsonl"
        self.env = {
            "PATH": str(binary) + os.pathsep + os.environ["PATH"],
            "KUBECONFIG": str(root / "unused-kubeconfig"),
            "DOCA1_UDEV_NETWORK_RULES_BASE64": "dGVzdA==",
            "MOCK_STATE": str(self.state), "MOCK_EVENTS": str(self.log),
        }

    def invoke(self, mode="", paused=False, target="apply-manifests"):
        self.state.write_text(json.dumps({"paused": paused}))
        self.log.write_text("")
        return subprocess.run(["make", "--no-print-directory", target],
                              cwd=REPO, env={**self.env, "MOCK_MODE": mode},
                              capture_output=True, text=True, timeout=15, check=False)

    def events(self, command=None):
        return [event for line in self.log.read_text().splitlines()
                if (event := json.loads(line)) and (command is None or event["args"][0] == command)]

    def applied(self):
        return [event for event in self.events() if event["args"][0] in ("apply", "create")
                and "--dry-run=client" not in event["args"]]

    def test_nno_defaults_batch_all_manifests_before_one_rollout(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        expected = []
        for manifest in sorted((REPO / "manifests/nno").glob("*.yaml")):
            expected.append(re.search(r"^ +name: (.+)$", manifest.read_text(), re.MULTILINE)[1])
        applies = self.applied()
        self.assertEqual([event["name"] for event in applies], expected)
        self.assertTrue(all(event["paused"] for event in applies))
        starts = [event for event in self.events("wait")
                  if "--for=condition=Updating=True" in event["args"]]
        self.assertEqual(len(starts), 1)
        self.assertFalse(starts[0]["paused"])
        self.assertEqual(len(self.events("patch")), 2)
        self.assertFalse(json.loads(self.state.read_text())["paused"])

    def test_failures_and_termination_restore_the_pool(self):
        for mode in ("apply-fail", "create-race", "term-apply", "term-pause", "pause-error", "render-timeout", "stale"):
            with self.subTest(mode=mode):
                result = self.invoke(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(self.events("patch")), 2)
                self.assertFalse(json.loads(self.state.read_text())["paused"])

    def test_administrator_paused_pool_is_untouched(self):
        for mode, paused, patches in (("", True, 0), ("pause-conflict", False, 1)):
            with self.subTest(mode=mode):
                result = self.invoke(mode, paused=paused)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(self.events("patch")), patches)
                self.assertFalse(self.applied())
                self.assertTrue(json.loads(self.state.read_text())["paused"])

    def test_existing_machineconfigs_keep_sequential_rollout_waits(self):
        result = self.invoke("existing")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.events("patch"))
        self.assertFalse(any(event["paused"] for event in self.applied()))
        self.assertEqual(sum("--for=condition=Updating=True" in event["args"]
                             for event in self.events("wait")), len(self.applied()))

    def test_fresh_batch_checks_rendered_target_before_resuming(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        events = [json.loads(line) for line in self.log.read_text().splitlines()]
        sources = [i for i, event in enumerate(events)
                   if any(".spec.configuration.source" in arg for arg in event["args"])]
        self.assertEqual(len(sources), 2)
        resume = events.index(self.events("patch")[-1])
        capture = max(i for i, event in enumerate(events[:resume]) if event["args"][0] == "get")
        self.assertTrue(max(sources) < capture < resume)
        names = {event["name"] for event in self.applied() if event["kind"] == "MachineConfig"}
        self.assertEqual({re.search(r'@\.name=="([^"]+)"', " ".join(events[i]["args"]))[1]
                          for i in sources}, names)
        self.assertTrue(any("--for=jsonpath={.status.configuration.name}=rendered-new" in event["args"]
                            for event in self.events("wait")))

    def test_make_passes_settings_as_environment_values(self):
        marker = self.state.parent / "shell-expanded"
        self.env["BATCH_WORKER_MCP"] = f'true; touch "{marker}"; #'
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(marker.exists())

    def test_added_multidocument_manifest_joins_the_same_rollout(self):
        directory = self.state.parent / "manifests"
        shutil.copytree(REPO / "manifests/nno", directory)
        (directory / "30-extra.yaml").write_text(
            "apiVersion: machineconfiguration.openshift.io/v1\nkind: MachineConfig\n"
            "metadata:\n  name: worker-extra\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: extra\n")
        self.env.update(MANIFEST_DIR=str(directory), BATCH_WORKER_MCP="true")
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(len(self.applied()), 6)
        self.assertEqual(len(self.events("patch")), 2)
        state = json.loads(self.state.read_text())
        self.assertIn("worker-extra", state["sources"])
        self.assertEqual(set(state["sources"]), set(state["applied"]))

    def test_required_variables_are_checked_before_cluster_changes(self):
        for name in ("KUBECONFIG", "DOCA1_UDEV_NETWORK_RULES_BASE64"):
            with self.subTest(variable=name):
                value = self.env.pop(name)
                result = self.invoke()
                self.env[name] = value
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(name, result.stderr)
                self.assertFalse(self.events())

    def test_non_machineconfig_batch_skips_waits_unless_a_hook_requires_them(self):
        directory = self.state.parent / "manifests"
        directory.mkdir()
        (directory / "00-config.yaml").write_text(
            "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n")
        later = directory / "10-later.yaml"
        later.write_text("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: before-hook\n")
        marker = directory / "hook-ran"
        self.env.update(MANIFEST_DIR=str(directory), BATCH_WORKER_MCP="true",
                        HOOK_MARKER=str(marker), NEXT_MANIFEST=str(later))
        for hook in (False, True):
            with self.subTest(hook=hook):
                if hook:
                    (directory / "00-config.sh").write_text(
                        'touch "$HOOK_MARKER"\nprintf "apiVersion: v1\\nkind: ConfigMap\\n'
                        'metadata:\\n  name: after-hook\\n" > "$NEXT_MANIFEST"\n')
                result = self.invoke()
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertFalse(self.events("patch"))
                self.assertEqual(bool(self.events("wait")), hook)
                self.assertEqual(marker.exists(), hook)
                self.assertEqual(self.applied()[-1]["name"], "after-hook" if hook else "before-hook")

    def test_delete_keeps_reverse_order_without_pool_changes(self):
        result = self.invoke(target="delete-manifests")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        expected = [re.search(r"^ +name: (.+)$", path.read_text(), re.MULTILINE)[1]
                    for path in sorted((REPO / "manifests/nno").glob("*.yaml"), reverse=True)]
        self.assertEqual([event["name"] for event in self.events("delete")], expected)
        self.assertFalse(self.events("wait") or self.events("patch"))


if __name__ == "__main__":
    unittest.main()
