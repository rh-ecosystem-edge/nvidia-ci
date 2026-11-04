#!/usr/bin/env python3
"""Require this invocation's selected driver and passing deploy/RDMA evidence."""

import json
import os
from pathlib import Path
import sys

from signed_images import nno_channel, nno_version_matches


def verify(directory, image, attempt, workers):
    """Reject skipped suites, stale reports and evidence for another image/worker."""
    directory = Path(directory)
    step = "network-operator-e2e-precompiled"
    reports = [json.loads(path.read_text()) for path in directory.glob("nno-step-result-*.json")]
    for name, required in [
        (step, ["requested_driver_policy", "requested_driver_running", "nic_cluster_policy_ready"]),
        (step + "-rdma", ["rdma_shared_device"]),
    ]:
        matching = [report for report in reports if report.get("step") == name and report.get("attempt") == attempt]
        if not matching:
            raise ValueError(f"missing result for {name}, attempt {attempt}")
        for report in matching:
            if report.get("status") != "passed" or any(report.get("checks", {}).get(key) != "passed" for key in required):
                raise ValueError(f"{name} did not pass all required checks")
            if (report.get("precompiled_selection") or {}).get("pull_spec") != image:
                raise ValueError(f"{name} did not use the prepared image")
            if not nno_version_matches(nno_channel(image), report.get("nno_csv_version")):
                raise ValueError(f"{name} did not retain the selected NNO release family")
    evidence = json.loads((directory / "nno-signed-driver-images.json").read_text())
    if (evidence.get("step"), evidence.get("attempt"), evidence.get("pull_spec")) != (step, attempt, image):
        raise ValueError("driver evidence does not belong to this image and invocation")
    observed = evidence.get("workers") or {}
    if len(set(workers)) != 2 or not all(workers) or set(observed) != set(workers) or not all(observed.values()):
        raise ValueError("driver evidence must cover both configured RDMA workers")


if __name__ == "__main__":
    try:
        verify(os.environ["ARTIFACT_DIR"], os.environ["NVIDIANETWORK_OFED_DRIVER_PULLSPEC"],
               os.environ["NNO_STEP_ATTEMPT"], [os.environ["NVIDIANETWORK_RDMA_CLIENT_HOSTNAME"],
                                             os.environ["NVIDIANETWORK_RDMA_SERVER_HOSTNAME"]])
    except (KeyError, OSError, TypeError, ValueError) as error:
        print(f"precompiled phase result error: {error}", file=sys.stderr)
        sys.exit(1)
    print("Prepared signed driver has passing deploy and RDMA evidence on both workers")
