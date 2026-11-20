"""Combine the signed request, Prow step outcomes and worker image evidence."""

from __future__ import annotations

import re
from datetime import UTC, datetime
from pathlib import PurePosixPath
from typing import Any

MANUAL_JOB = "pull-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-nno-signed"
AUTOMATIC_JOB = "periodic-ci-rh-ecosystem-edge-nvidia-ci-main-doca2-nno-signed-auto"
REQUEST = "nno-doca2-signed-request.json"
DISCOVERY = "nno-discovery-result.json"
WORKERS = "nno-signed-driver-images.json"
STAGES = {
    "deploy": "nno-install-signed-driver",
    "gpu": "nno-install-gpu-operator-rdma",
    "gpudirect": "nno-test-gpudirect",
}
DIGEST = re.compile(r"sha256:[a-f0-9]{64}")
DRIVER_TAG = re.compile(
    r"(?P<version>.+)-(?P<kernel>\d+\.\d+\.\d+-[A-Za-z0-9._+]+\."
    r"(?:x86_64|aarch64(?:_64k)?|ppc64le|s390x))-"
    r"(?P<os>[A-Za-z0-9][A-Za-z0-9._-]*)-(?P<architecture>amd64|arm64|ppc64le|s390x)"
)


def is_signed_job(job: str) -> bool:
    """The report supports these two production sources."""
    return job in {MANUAL_JOB, AUTOMATIC_JOB}


def timestamp(value: Any) -> str:
    if isinstance(value, (int, float)):
        return datetime.fromtimestamp(value, UTC).isoformat().replace("+00:00", "Z")
    return str(value or "")


def outcome(record: dict[str, Any] | None) -> str:
    if record is None:
        return "blocked"
    result = str(record.get("result", "")).lower()
    return "passed" if result == "success" else "failed" if result in {"failure", "error", "aborted"} else "blocked"


def failed_steps(records: list[tuple[str, dict[str, Any]]], root: str) -> list[dict[str, str]]:
    """Retain the failing Prow stage even when no driver tests were reached."""
    return [
        {"name": PurePosixPath(path).parent.name,
         "url": f"https://prow.ci.openshift.org/view/gs/test-platform-results-public/{path}"}
        for path, data in records
        if path.startswith(root + "/artifacts/") and PurePosixPath(path).name == "finished.json"
        and outcome(data) == "failed"
    ]


def blocked_manifest(records: list[tuple[str, dict[str, Any]]], identity: dict[str, str], reason: str) -> dict[str, Any]:
    """Keep contradictory completed builds visible without trusting their target."""
    root = identity["build_path"]
    by_path = dict(records)
    return {
        "schema_version": 1,
        "run": {
            **{key: identity[key] for key in ("kind", "job_name", "build_id")},
            "started_at": timestamp(by_path.get(root + "/started.json", {}).get("timestamp")),
            "status": str(by_path.get(root + "/finished.json", {}).get("result", "unknown")).lower(),
            "source": "manual" if identity["kind"] == "presubmit" else "automatic",
            "planned_modes": ["signed"],
            "failed_steps": failed_steps(records, root),
        },
        "pairs": [{
            "id": "doca2-signed-invalid-evidence", "mode": "signed", "validation": "signed",
            "status": "blocked", "checks": {"artifact_consistency": "blocked"}, "note": reason,
            "artifacts": {path.removeprefix(root + "/artifacts/"):
                          f"https://prow.ci.openshift.org/view/gs/test-platform-results-public/{path}"
                          for path, _ in records if path.startswith(root + "/artifacts/")},
        }],
    }


def normalize(records: list[tuple[str, dict[str, Any]]], identity: dict[str, str]) -> dict[str, Any]:
    """One build has one requested target, with independent signed stage attempts.

    Missing evidence blocks a pass. Conflicting evidence fails the pair, and
    wrapper outcomes include verification performed after Ginkgo has exited.
    """
    root = identity["build_path"]
    if any(not isinstance(data, dict) for _, data in records):
        raise ValueError("signed artifacts must be JSON objects")
    by_path = dict(records)
    notes: list[str] = []

    def named(name: str) -> tuple[str, dict[str, Any]] | None:
        matches = [(path, data) for path, data in records if path.rsplit("/", 1)[-1] == name]
        if any(data != matches[0][1] for _, data in matches[1:]):
            raise ValueError(f"conflicting {name} artifacts")
        return matches[0] if matches else None

    request_record = named(REQUEST)
    discovery_record = named(DISCOVERY)
    request = request_record[1] if request_record else None
    discovery = discovery_record[1] if discovery_record else None
    for data in (discovery, (request or {}).get("provenance")):
        if data and (data.get("job_name") != identity["job_name"] or str(data.get("build_id")) != identity["build_id"]):
            raise ValueError("signed provenance disagrees with artifact path")
    if discovery and (discovery.get("schema_version") != 1 or discovery.get("outcome") not in {"selected", "no_work", "failed"}):
        raise ValueError("invalid discovery outcome")
    if discovery and discovery["outcome"] == "selected" and not isinstance(discovery.get("selected"), dict):
        raise ValueError("selected discovery requires a target")

    steps = [(path, data) for path, data in records if path.rsplit("/", 1)[-1].startswith("nno-step-result")]
    start = timestamp(by_path.get(root + "/started.json", {}).get("timestamp"))
    start = start or (discovery or {}).get("started_at") or next((data.get("recorded_at") for _, data in steps), "")
    run = {
        **{key: identity[key] for key in ("kind", "job_name", "build_id")},
        "started_at": start,
        "status": str(by_path.get(root + "/finished.json", {}).get("result", "unknown")).lower(),
        "ocp_version": "", "planned_modes": ["signed"],
        "source": "manual" if identity["kind"] == "presubmit" else "automatic",
        "provenance": (request or {}).get("provenance", {}),
        "discovery": discovery or {},
        "failed_steps": failed_steps(records, root),
    }
    manifest = {"schema_version": 1, "run": run, "pairs": []}
    if discovery and discovery["outcome"] != "selected":
        if request or steps:
            raise ValueError("unselected discovery has signed request or test results")
        return manifest
    if not request:
        run["note"] = "No normalized signed request was published"
        return manifest
    if request.get("schema_version") != 1 or not isinstance(request.get("pairs"), list) or len(request["pairs"]) != 1:
        raise ValueError("signed request requires exactly one pair and schema_version 1")
    target = request["pairs"][0]
    image = target["driver_requested"]["image"]
    tag = DRIVER_TAG.fullmatch(image.rsplit(":", 1)[-1])
    if not tag:
        return blocked_manifest(records, identity, "cannot extract driver version from requested image tag")
    driver = {**tag.groupdict(), **target["driver_requested"]}
    ocp = re.sub(r"-(?:x86_64|aarch64|amd64|arm64|ppc64le|s390x|multi)$", "", target["openshift_version"])
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9][A-Za-z0-9._-]*)?", ocp):
        raise ValueError("signed request requires an exact OCP version")
    run["ocp_version"] = ocp
    checks: dict[str, str] = {}
    versions, digests, worker_sets = set(), set(), []
    pair = {
        "id": "doca2-signed-" + str(target["id"]), "mode": "signed", "validation": "signed",
        "status": "blocked", "nno_requested": "", "nno_observed": "",
        "driver_requested": {**driver, "version": tag["version"]},
        "driver_observed": {}, "checks": checks, "metrics": {},
        "artifacts": {}, "completed_at": "", "note": "", "stages": {},
    }
    manifest["pairs"].append(pair)

    def artifact(path: str) -> None:
        pair["artifacts"][path.removeprefix(root + "/artifacts/")] = (
            f"https://prow.ci.openshift.org/view/gs/test-platform-results-public/{path}"
        )

    def check(name: str, status: str) -> None:
        # A missing check in either stage must not be hidden by another stage.
        priority = {"passed": 0, "skipped": 1, "blocked": 2, "failed": 3}
        if name not in checks or priority[status] > priority[checks[name]]:
            checks[name] = status

    artifact(request_record[0])
    if discovery_record:
        artifact(discovery_record[0])
    if discovery:
        selected = discovery.get("selected") or {}
        release = selected.get("release") or {}
        if any(selected.get(key) != driver[key] for key in ("image", "kernel", "architecture")) or release.get("installer_version") != target["openshift_version"]:
            check("discovery_target", "failed")
            notes.append("discovery selection disagrees with signed request")
        else:
            check("discovery_target", "passed")
        pair["driver_requested"].update({key: selected[key] for key in
            ("manifest_digest", "config_digest", "index_digest") if selected.get(key)})

    for stage, prow_step in STAGES.items():
        completions = [(path, data) for path, data in records
                       if PurePosixPath(path).name == "finished.json" and PurePosixPath(path).parent.name == prow_step]
        stage_outcome = outcome(completions[0][1] if len(completions) == 1 else None)
        check(stage + "_step", stage_outcome)
        for path, _ in completions:
            artifact(path)
        if stage == "gpu":
            continue
        expected_step = "network-operator-e2e-signed-" + stage
        results = [(path, data) for path, data in steps if data.get("step") == expected_step]
        unique: dict[str, tuple[str, dict[str, Any]]] = {}
        for path, data in results:
            if PurePosixPath(path).parent.parent.name != prow_step:
                raise ValueError("signed result is outside its expected Prow step")
            if data.get("schema_version") != 1 or data.get("job_name") != identity["job_name"] or str(data.get("build_id")) != identity["build_id"]:
                raise ValueError("signed step identity disagrees with artifact path")
            attempt = data.get("attempt")
            if not isinstance(attempt, str) or not attempt:
                raise ValueError("signed step requires an invocation ID")
            if attempt in unique and unique[attempt][1] != data:
                raise ValueError("conflicting signed step results for one invocation")
            unique[attempt] = (path, data)
        if len(unique) != 1:
            check(stage + "_result", "blocked")
            notes.append(f"{stage}: expected one coherent invocation, found {len(unique)}")
            pair["stages"][stage] = {"attempts": sorted(unique), "status": stage_outcome}
            continue
        path, result = next(iter(unique.values()))
        artifact(path)
        attempt = result["attempt"]
        check(stage + "_result", result["status"] if result.get("status") in {"passed", "failed", "skipped"} else "blocked")
        pair["stages"][stage] = {"attempt": attempt, "status": stage_outcome}
        required = ["precompiled_selection", "requested_driver_policy", "requested_driver_running", "ocp_version", "nno_csv"]
        required += ["nno_deployment", "nic_cluster_policy_ready"] if stage == "deploy" else ["rdma_gpudirect"]
        for name in required:
            value = result.get("checks", {}).get(name)
            check(name, value if value in {"passed", "failed", "skipped"} else "blocked")
        tag_status = result.get("checks", {}).get("requested_driver_tag")
        if stage == "deploy" and tag_status is not None:
            check("requested_driver_tag", tag_status if tag_status in {"passed", "failed", "skipped"} else "blocked")
        selection = result.get("precompiled_selection") or {}
        matches = result.get("ocp_version") == ocp and selection.get("pull_spec") == image
        configured = result.get("configured_ofed_driver") or {}
        check("requested_target", "passed" if matches else "failed")
        if not matches:
            notes.append(f"{stage}: observed OCP or driver differs from requested target")
        nno = result.get("nno_csv_version")
        if nno:
            versions.add(nno)
        else:
            check("nno_csv", "blocked")
        image_id = str(configured.get("image_id", ""))
        digest = image_id.rsplit("@", 1)[-1]
        valid_digest = bool(DIGEST.fullmatch(digest))
        check("driver_image_id", "passed" if valid_digest else "blocked")
        if valid_digest:
            digests.add(digest)
        evidence = [(path, data) for path, data in records if path.endswith("/" + WORKERS)
                    and data.get("step") == expected_step and data.get("attempt") == attempt]
        if len(evidence) != 1:
            check("driver_worker_images", "blocked")
        else:
            evidence_path, data = evidence[0]
            if PurePosixPath(evidence_path).parent.parent.name != prow_step:
                raise ValueError("worker evidence is outside its expected Prow step")
            artifact(evidence_path)
            workers = data.get("workers") or {}
            if isinstance(workers, dict) and len(workers) == 2 and all(workers):
                worker_sets.append(set(workers))
                observed = {str(value).rsplit("@", 1)[-1] for value in workers.values()}
                valid = data.get("pull_spec") == image and observed == {digest} and valid_digest
                valid = valid and (not driver.get("digest") or driver["digest"] == digest)
                if discovery:
                    selected_digests = {discovery["selected"][key] for key in ("manifest_digest", "config_digest", "index_digest") if discovery["selected"].get(key)}
                    valid = valid and digest in selected_digests
                check("driver_worker_images", "passed" if valid else "failed")
                pair["stages"][stage]["workers"] = workers
            else:
                check("driver_worker_images", "blocked")
        if stage == "gpudirect":
            pair["metrics"] = result.get("metrics") or {}
        recorded = result.get("recorded_at")
        if recorded:
            parsed = datetime.fromisoformat(recorded)
            if parsed.tzinfo is None:
                raise ValueError("signed result timestamp requires a timezone")
            recorded = parsed.astimezone(UTC).isoformat().replace("+00:00", "Z")
            pair["completed_at"] = max(pair["completed_at"], recorded)

    if len(versions) > 1:
        check("nno_csv", "failed")
        notes.append("NNO CSV version differed between signed stages")
    if versions:
        pair["nno_requested"] = pair["nno_observed"] = min(versions)
    if len(digests) > 1 or (len(worker_sets) == 2 and worker_sets[0] != worker_sets[1]):
        check("driver_worker_images", "failed")
        notes.append("driver digest or worker set differed between signed stages")
    if digests:
        pair["driver_observed"] = {"image": image.rsplit(":", 1)[0], "digest": min(digests)}
    pair["status"] = (
        "failed" if "failed" in checks.values() else
        "blocked" if "blocked" in checks.values() else
        "skipped" if "skipped" in checks.values() else
        "passed"
    )
    pair["note"] = "; ".join(notes)
    return manifest
