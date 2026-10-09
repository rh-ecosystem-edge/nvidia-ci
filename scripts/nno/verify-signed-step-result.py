#!/usr/bin/env python3
"""Compare the request with this invocation's existing reports and worker evidence."""

import json
import os
from pathlib import Path
import re
import sys
import subprocess

from signed_images import error_message, image_info, metadata, runtime_digests


def selected_image(shared, request):
    path = shared / "nno-discovery-result.json"
    if not path.exists():
        if os.environ.get("NNO_DISCOVERY_REQUIRED") == "true":
            raise ValueError("required discovery outcome is missing")
        return None
    result = json.loads(path.read_text())
    pair = request["pairs"][0]
    selected = result["selected"]
    if result["outcome"] != "selected" or not selected:
        raise ValueError("discovery did not select an image")
    if (result["job_name"], str(result["build_id"])) != (os.environ.get("JOB_NAME", "local"), os.environ.get("BUILD_ID", "local")):
        raise ValueError("selection belongs to another invocation")
    if pair["openshift_version"] != selected["release"]["installer_version"] or any(
            pair["driver_requested"].get(field) != selected[field] for field in ("image", "kernel", "architecture")):
        raise ValueError("selection does not match the normalized request")
    return selected


def preflight(shared, request):
    selected = selected_image(shared, request)
    if selected:
        actual = image_info(selected["image"], selected["architecture"], os.environ["NNO_REGISTRY_AUTH_FILE"])
        if actual["manifest_digest"] != selected["manifest_digest"]:
            raise ValueError("selected tag no longer identifies the discovered content")


def image_digest(image_id):
    digest = image_id.rsplit("@", 1)[-1]
    if re.fullmatch(r"sha256:[a-f0-9]{64}", digest) is None:
        raise ValueError("result is missing running driver digest evidence")
    return digest


def verify(artifact_dir, step, attempt, request, workers):
    pair = request["pairs"][0]
    driver = pair["driver_requested"]
    tagged = metadata(driver["image"])
    # Installer packaging is absent from ClusterVersion; retain release prereleases.
    requested_version = re.sub(r"-(?:x86_64|aarch64|amd64|arm64|ppc64le|s390x|multi)$", "", pair["openshift_version"])
    reports = [json.loads(path.read_text()) for path in Path(artifact_dir).glob("nno-step-result-*.json")]
    reports = [report for report in reports if report.get("step") == step and report.get("attempt") == attempt]
    if not reports:
        raise ValueError(f"no result for signed step {step}, attempt {attempt}")
    required = ["requested_driver_policy", "requested_driver_running",
                "rdma_gpudirect" if step.endswith("gpudirect") else "nic_cluster_policy_ready"]
    for report in reports:
        if report.get("status") != "passed" or any(report.get("checks", {}).get(check) != "passed" for check in required):
            raise ValueError(f"signed step {step} did not pass all required driver/test checks")
        selection = report.get("precompiled_selection") or {}
        if report.get("ocp_version") != requested_version or selection.get("pull_spec") != driver["image"]:
            raise ValueError("result does not prove the requested exact OCP version and image")
        for requested, observed in [("kernel", "kernel_version"), ("architecture", "architecture")]:
            if selection.get(observed) != tagged[requested] or driver.get(requested, selection[observed]) != selection[observed]:
                raise ValueError(f"result is missing or mismatches requested {requested}")
        image_digest((report.get("configured_ofed_driver") or {}).get("image_id", ""))

    evidence = json.loads((Path(artifact_dir) / "nno-signed-driver-images.json").read_text())
    if evidence.get("step") != step or evidence.get("attempt") != attempt or evidence.get("pull_spec") != driver["image"]:
        raise ValueError("worker image evidence does not belong to this signed invocation")
    observed_workers = evidence.get("workers") or {}
    if len(set(workers)) != 2 or not all(workers) or set(observed_workers) != set(workers):
        raise ValueError("driver evidence must cover both configured RDMA workers")
    for image_id in observed_workers.values():
        digest = image_digest(image_id)
        # This assertion refers to the worker's runtime digest, not an OCI index digest.
        if driver.get("digest", digest) != digest:
            raise ValueError("running driver digest does not match the requested runtime digest")
    worker_digests = {image_digest(value) for value in observed_workers.values()}
    if len(worker_digests) != 1 or any(
            image_digest(report["configured_ofed_driver"]["image_id"]) not in worker_digests for report in reports):
        raise ValueError("reports and both workers must agree on the running driver digest")
    return worker_digests


if __name__ == "__main__":
    try:
        shared = Path(os.environ["SHARED_DIR"])
        request = json.loads((shared / "nno-doca2-signed-request.json").read_text())
        pair = request["pairs"][0]
        if (shared / "dpf-openshift-version").read_text().strip() != pair["openshift_version"] or \
                (shared / "ofed-pullspec").read_text().strip() != pair["driver_requested"]["image"]:
            raise ValueError("shared handoff does not match the normalized request")
        if sys.argv[1:] == ["--preflight"]:
            preflight(shared, request)
            sys.exit(0)
        observed = verify(os.environ["ARTIFACT_DIR"], os.environ["NNO_STEP_NAME"], os.environ["NNO_STEP_ATTEMPT"], request,
                          [os.environ["NVIDIANETWORK_RDMA_CLIENT_HOSTNAME"], os.environ["NVIDIANETWORK_RDMA_SERVER_HOSTNAME"]])
        selected = selected_image(shared, request)
        if selected and not observed <= runtime_digests(selected):
            raise ValueError("worker runtime digest does not match the selected manifest/config/index")
    except (KeyError, IndexError, TypeError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"signed DOCA2 result error: {error_message(error)}", file=sys.stderr)
        sys.exit(1)
    print("Signed DOCA2 step has passing driver evidence on both RDMA workers and test results")
