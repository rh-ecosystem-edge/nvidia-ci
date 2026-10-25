#!/usr/bin/env python3
"""Compare the request with this invocation's existing reports and worker evidence."""

import json
import os
from pathlib import Path
import re
import sys

from signed_images import digest, error_message, metadata, nno_channel, nno_version_matches, runtime_digests, strip_installer_suffix


def selected_image(shared, request):
    path = shared / "nno-discovery-result.json"
    if not path.exists():
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


def image_digest(image_id, selected=None):
    if not isinstance(image_id, str):
        raise ValueError("result is missing running driver digest evidence")
    if re.fullmatch(r"[a-f0-9]{64}", image_id):
        # A bare runtime ID is config evidence only after checking trusted discovery
        # metadata. Never interpret manifest/index bytes as a bare config ID.
        config = (selected or {}).get("config_digest")
        if not config:
            raise ValueError("bare runtime imageID requires trusted selected-image SHA-256 config metadata; "
                             "use the discovery handoff (manual requests without it cannot verify bare IDs)")
        if digest(config).removeprefix("sha256:") != image_id:
            raise ValueError("bare runtime imageID does not match the selected image config digest")
        return config
    qualified = image_id.rsplit("@", 1)[-1]
    if re.fullmatch(r"sha256:[a-f0-9]{64}", qualified) is None:
        raise ValueError("result is missing running driver digest evidence")
    return qualified


def verify(artifact_dir, step, attempt, request, workers, selected=None):
    pair = request["pairs"][0]
    driver = pair["driver_requested"]
    channel = nno_channel(driver["image"])
    tagged = metadata(driver["image"])
    # Installer packaging is absent from ClusterVersion; retain release prereleases.
    requested_version = strip_installer_suffix(pair["openshift_version"])
    reports = [json.loads(path.read_text()) for path in Path(artifact_dir).glob("nno-step-result-*.json")]
    reports = [report for report in reports if report.get("step") == step and report.get("attempt") == attempt]
    if not reports:
        raise ValueError(f"no result for signed step {step}, attempt {attempt}")
    required = ["requested_driver_policy", "requested_driver_running",
                "rdma_gpudirect" if step.endswith("gpudirect") else "nic_cluster_policy_ready"]
    for report in reports:
        if report.get("status") != "passed" or any(report.get("checks", {}).get(check) != "passed" for check in required):
            raise ValueError(f"signed step {step} did not pass all required driver/test checks")
        if not nno_version_matches(channel, report.get("nno_csv_version")):
            raise ValueError(f"installed NNO CSV {report.get('nno_csv_version')!r} does not match requested channel {channel}")
        selection = report.get("precompiled_selection") or {}
        if report.get("ocp_version") != requested_version or selection.get("pull_spec") != driver["image"]:
            raise ValueError("result does not prove the requested exact OCP version and image")
        for requested, observed in [("kernel", "kernel_version"), ("architecture", "architecture")]:
            if selection.get(observed) != tagged[requested] or driver.get(requested, selection[observed]) != selection[observed]:
                raise ValueError(f"result is missing or mismatches requested {requested}")
        image_digest((report.get("configured_ofed_driver") or {}).get("image_id", ""), selected)

    evidence = json.loads((Path(artifact_dir) / "nno-signed-driver-images.json").read_text())
    if evidence.get("step") != step or evidence.get("attempt") != attempt or evidence.get("pull_spec") != driver["image"]:
        raise ValueError("worker image evidence does not belong to this signed invocation")
    observed_workers = evidence.get("workers") or {}
    if len(set(workers)) != 2 or not all(workers) or set(observed_workers) != set(workers):
        raise ValueError("driver evidence must cover both configured RDMA workers")
    for image_id in observed_workers.values():
        digest = image_digest(image_id, selected)
        # This assertion refers to the worker's runtime digest, not an OCI index digest.
        if driver.get("digest", digest) != digest:
            raise ValueError("running driver digest does not match the requested runtime digest")
    worker_digests = {image_digest(value, selected) for value in observed_workers.values()}
    if len(worker_digests) != 1 or any(
            image_digest(report["configured_ofed_driver"]["image_id"], selected) not in worker_digests for report in reports):
        raise ValueError("reports and both workers must agree on the running driver digest")
    if selected and not worker_digests <= runtime_digests(selected):
        raise ValueError("worker runtime digest does not match the selected manifest/config/index")
    return worker_digests


if __name__ == "__main__":
    try:
        shared = Path(os.environ["SHARED_DIR"])
        request = json.loads((shared / "nno-doca2-signed-request.json").read_text())
        pair = request["pairs"][0]
        channel = nno_channel(pair["driver_requested"]["image"])
        if (shared / "dpf-openshift-version").read_text().strip() != pair["openshift_version"] or \
                (shared / "ofed-pullspec").read_text().strip() != pair["driver_requested"]["image"]:
            raise ValueError("shared handoff does not match the normalized request")
        selected = selected_image(shared, request)
        if sys.argv[1:] == ["--preflight"]:
            print(channel)
            sys.exit(0)
        if os.environ.get("NVIDIANETWORK_SUBSCRIPTION_CHANNEL") != channel:
            raise ValueError("NNO subscription channel does not match the staging driver release")
        verify(os.environ["ARTIFACT_DIR"], os.environ["NNO_STEP_NAME"], os.environ["NNO_STEP_ATTEMPT"], request,
               [os.environ["NVIDIANETWORK_RDMA_CLIENT_HOSTNAME"], os.environ["NVIDIANETWORK_RDMA_SERVER_HOSTNAME"]], selected)
    except (KeyError, IndexError, TypeError, OSError, ValueError) as error:
        print(f"signed DOCA2 result error: {error_message(error)}", file=sys.stderr)
        sys.exit(1)
    print("Signed DOCA2 step has passing driver evidence on both RDMA workers and test results")
