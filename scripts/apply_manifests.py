"""Apply day-2 manifests, optionally batching new worker MachineConfigs."""

import argparse
import json
import os
import shutil
import signal
import subprocess
import sys
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from uuid import uuid4

PAUSE_OWNER = "nvidia-ci/pause-owner"


@dataclass(frozen=True)
class Settings:
    manifest_dir: Path
    wait_for_mcp: bool
    batch_mcp: bool
    pool: str
    start_timeout: str
    rollout_timeout: str


def read_settings():
    return Settings(
        manifest_dir=Path(os.environ.get("MANIFEST_DIR") or "manifests/nno"),
        wait_for_mcp=os.environ.get("WAIT_FOR_WORKER_MCP") == "true",
        batch_mcp=os.environ.get("BATCH_WORKER_MCP") == "true",
        pool="mcp/" + (os.environ.get("WORKER_MCP_NAME") or "worker"),
        start_timeout=os.environ.get("WORKER_MCP_ROLLOUT_START_TIMEOUT") or "2m",
        rollout_timeout=os.environ.get("WORKER_MCP_ROLLOUT_TIMEOUT") or "40m",
    )


def oc(*arguments, manifest=None, check=True, quiet=False):
    return subprocess.run(
        ["oc", *arguments], input=manifest, text=True, check=check,
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL if quiet else None,
    ).stdout


def validate_environment(settings):
    if not os.environ.get("KUBECONFIG"):
        raise RuntimeError("KUBECONFIG is unset or empty")
    for command in ("oc", "envsubst"):
        if not shutil.which(command):
            raise RuntimeError(f"'{command}' binary not found on PATH")
    if not settings.manifest_dir.is_dir():
        raise RuntimeError(f"Manifest directory '{settings.manifest_dir}' does not exist")


def find_manifests(directory):
    files = sorted(path for path in directory.iterdir()
                   if path.is_file() and path.suffix in (".yaml", ".yml"))
    if not files:
        raise RuntimeError(f"No YAML manifests found in '{directory}'")
    return files


def check_required_vars(path):
    required = path.with_suffix(".env-required")
    if required.is_file():
        names = [line.strip() for line in required.read_text().splitlines()]
        missing = [name for name in names
                   if name and not name.startswith("#") and not os.environ.get(name)]
        if missing:
            raise RuntimeError(f"Missing required environment variable(s) for {path}: {', '.join(missing)}")


def render_manifest(path):
    arguments = ["envsubst"]
    if os.environ.get("MANIFEST_VARS"):
        arguments.append(os.environ["MANIFEST_VARS"])
    return subprocess.run(arguments, input=path.read_text(), text=True,
                          stdout=subprocess.PIPE, check=True).stdout


def parse_resources(manifest):
    resource = json.loads(oc("create", "--dry-run=client", "--validate=false",
                             "-f", "-", "-o", "json", manifest=manifest))
    return resource["items"] if resource.get("kind") == "List" else [resource]


def is_machineconfig(resource):
    return (resource.get("apiVersion") == "machineconfiguration.openshift.io/v1"
            and resource.get("kind") == "MachineConfig")


def prepare_batch(files, settings):
    if not (settings.wait_for_mcp and settings.batch_mcp):
        return None
    if any(path.with_suffix(".sh").is_file() for path in files):
        return None
    resources = [resource for path in files
                 for resource in parse_resources(render_manifest(path))]
    # Existing MC names cannot prove updated contents have rendered.
    names = set()
    for resource in filter(is_machineconfig, resources):
        name = resource["metadata"]["name"]
        if name in names or oc("get", f"machineconfig/{name}", "--ignore-not-found", "-o", "name").strip():
            print("Existing or repeated MachineConfig found; using per-file rollout waits.")
            return None
        names.add(name)
    return resources


def get_pool(settings):
    return json.loads(oc("get", settings.pool, "-o", "json"))


def pool_is_settled(pool):
    spec, status = pool["spec"], pool["status"]
    conditions = {item["type"]: item["status"] for item in status["conditions"]}
    return (not spec.get("paused", False)
            and status.get("observedGeneration", -1) >= pool["metadata"]["generation"]
            and spec["configuration"]["name"] == status.get("configuration", {}).get("name")
            and conditions.get("Updated") == "True"
            and all(conditions.get(kind) != "True" for kind in ("Updating", "Degraded")))


def wait_for_pool(settings, condition, timeout, **options):
    print(f"Waiting for {settings.pool}: {condition} (timeout: {timeout})")
    oc("wait", settings.pool, f"--for={condition}", f"--timeout={timeout}", **options)


def check_pool_ready(settings):
    if get_pool(settings)["spec"].get("paused", False):
        raise RuntimeError(f"{settings.pool} is already paused; refusing to change it")
    wait_for_pool(settings, "condition=Updated=True", settings.rollout_timeout)
    pool = get_pool(settings)
    if not pool_is_settled(pool):
        raise RuntimeError(f"{settings.pool} is not settled; refusing to batch changes")
    return pool


def patch_pool(settings, operations):
    oc("patch", settings.pool, "--type=json", "-p", json.dumps(operations))


def pause_pool(settings, pool, owner):
    print(f"Pausing {settings.pool}")
    annotations = dict(pool["metadata"].get("annotations") or {})
    annotations[PAUSE_OWNER] = owner
    patch_pool(settings, [
        {"op": "test", "path": "/metadata/resourceVersion", "value": pool["metadata"]["resourceVersion"]},
        {"op": "add", "path": "/metadata/annotations", "value": annotations},
        {"op": "add", "path": "/spec/paused", "value": True},
    ])


def resume_pool(settings, owner, required):
    pool = get_pool(settings)
    if (pool["metadata"].get("annotations") or {}).get(PAUSE_OWNER) != owner:
        if required:
            raise RuntimeError("Pause ownership changed; refusing to resume another caller's pause")
        return
    print(f"Resuming {settings.pool}")
    patch_pool(settings, [
        {"op": "test", "path": "/metadata/annotations/nvidia-ci~1pause-owner", "value": owner},
        {"op": "add", "path": "/spec/paused", "value": False},
        {"op": "remove", "path": "/metadata/annotations/nvidia-ci~1pause-owner"},
    ])


def terminate(signum, _frame):
    raise SystemExit(128 + signum)


@contextmanager
def defer_termination():
    pending = []
    previous = {number: signal.signal(number, lambda signum, frame: pending.append(signum))
                for number in (signal.SIGINT, signal.SIGTERM)}
    try:
        yield
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)
        if pending:
            raise SystemExit(128 + pending[0])


@contextmanager
def paused_pool(settings):
    pool = check_pool_ready(settings)
    owner, paused = uuid4().hex, False
    try:
        # Defer signals until cleanup can identify the outcome of the patch.
        with defer_termination():
            pause_pool(settings, pool, owner)
            paused = True
        yield
    finally:
        with defer_termination():
            try:
                # Read back ownership even when the pause client reported failure.
                resume_pool(settings, owner, required=paused)
            except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
                raise RuntimeError(f"Could not resume {settings.pool}; check the pool before retrying: {error}") from error


def apply_resources(resources):
    for resource in resources:
        command = "create" if is_machineconfig(resource) else "apply"
        options = ["--save-config"] if command == "create" else []
        print(oc(command, *options, "-f", "-", manifest=json.dumps(resource)), end="")


def wait_for_render(settings, names):
    for name in names:
        source = f'{{.spec.configuration.source[?(@.name=="{name}")].name}}={name}'
        wait_for_pool(settings, "jsonpath=" + source, settings.start_timeout)
    return get_pool(settings)["spec"]["configuration"]["name"]


def wait_for_rollout(settings, target=None):
    wait_for_pool(settings, "condition=Updating=True", settings.start_timeout, check=False, quiet=True)
    wait_for_pool(settings, "condition=Updated=True", settings.rollout_timeout)
    if target:
        wait_for_pool(settings, f"jsonpath={{.status.configuration.name}}={target}", settings.rollout_timeout)
        if not pool_is_settled(get_pool(settings)):
            raise RuntimeError(f"{settings.pool} has not converged to its current configuration")


def apply_batch(resources, settings):
    names = [resource["metadata"]["name"] for resource in resources if is_machineconfig(resource)]
    if not names:
        apply_resources(resources)
        return
    with paused_pool(settings):
        apply_resources(resources)
        target = wait_for_render(settings, names)
    wait_for_rollout(settings, target)


def apply_sequential(files, settings):
    for path in files:
        print(f"Applying manifest: {path}")
        print(oc("apply", "-f", "-", manifest=render_manifest(path)), end="")
        if settings.wait_for_mcp:
            wait_for_rollout(settings)
        hook = path.with_suffix(".sh")
        if hook.is_file():
            print(f"Running post-apply hook: {hook}")
            subprocess.run(["bash", str(hook)], check=True)


def delete_manifests(files):
    for path in reversed(files):
        print(f"Deleting manifest: {path}")
        print(oc("delete", "--ignore-not-found=true", "-f", "-", manifest=render_manifest(path)), end="")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("apply", "delete"), nargs="?", default="apply")
    action = parser.parse_args().action
    os.chdir(Path(__file__).resolve().parents[1])
    for number in (signal.SIGINT, signal.SIGTERM):
        signal.signal(number, terminate)
    settings = read_settings()
    validate_environment(settings)
    files = find_manifests(settings.manifest_dir)
    if action == "delete":
        delete_manifests(files)
        return
    for path in files:
        check_required_vars(path)
    resources = prepare_batch(files, settings)
    if resources is None:
        apply_sequential(files, settings)
    else:
        apply_batch(resources, settings)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)
