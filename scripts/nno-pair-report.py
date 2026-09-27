#!/usr/bin/env python3
"""Accumulate one NNO/DOCA pair manifest across steps of a Prow build.

The state file belongs in SHARED_DIR; the publish command copies the same
manifest into a post step's ARTIFACT_DIR, including unfinished planned pairs.
"""

from __future__ import annotations

import argparse
import json
import os
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

STATUSES = {"planned", "running", "passed", "failed", "skipped", "blocked"}
MODES = {"standard", "signed"}


def nonempty(value: Any, name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{name} must be a nonempty string")
    return value.strip()


def validate_pair(pair: Any) -> dict[str, Any]:
    if not isinstance(pair, dict):
        raise ValueError("pair must be an object")
    pair_id = nonempty(pair.get("id"), "pair.id")
    mode = nonempty(pair.get("mode"), f"pair {pair_id} mode")
    status = nonempty(pair.get("status"), f"pair {pair_id} status")
    if mode not in MODES or status not in STATUSES:
        raise ValueError(f"pair {pair_id} has invalid mode or status")
    nno_requested = nonempty(pair.get("nno_requested"), f"pair {pair_id} nno_requested")
    driver_requested = pair.get("driver_requested")
    if not isinstance(driver_requested, dict):
        raise ValueError(f"pair {pair_id} driver_requested must be an object")
    driver_version = nonempty(driver_requested.get("version"), f"pair {pair_id} driver_requested.version")
    for field in ("checks", "metrics", "artifacts"):
        if field in pair and not isinstance(pair[field], dict):
            raise ValueError(f"pair {pair_id} {field} must be an object")
    if any(value not in STATUSES for value in pair.get("checks", {}).values()):
        raise ValueError(f"pair {pair_id} has an invalid check status")
    if status == "passed":
        observed = pair.get("driver_observed")
        if pair.get("nno_observed") != nno_requested or not isinstance(observed, dict) or observed.get("version") != driver_version:
            raise ValueError(f"passed pair {pair_id} requires matching observed versions")
        checks = pair.get("checks", {})
        if not checks or any(value != "passed" for value in checks.values()):
            raise ValueError(f"passed pair {pair_id} requires passed checks")
        if mode == "signed" and checks.get("signature_verification") != "passed":
            raise ValueError(f"passed signed pair {pair_id} requires signature verification")
    return pair


def validate_manifest(data: Any) -> dict[str, Any]:
    if not isinstance(data, dict) or data.get("schema_version") != 1:
        raise ValueError("manifest requires schema_version: 1")
    run = data.get("run")
    pairs = data.get("pairs")
    if not isinstance(run, dict) or not isinstance(pairs, list):
        raise ValueError("manifest requires run object and pairs array")
    for field in ("kind", "job_name", "build_id", "started_at"):
        nonempty(run.get(field), f"run.{field}")
    if run["kind"] not in {"presubmit", "periodic"}:
        raise ValueError("run.kind must be presubmit or periodic")
    seen = set()
    for pair in pairs:
        validate_pair(pair)
        if pair["id"] in seen:
            raise ValueError(f"duplicate pair id: {pair['id']}")
        seen.add(pair["id"])
    return data


def atomic_json(path: Path, data: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(data, stream, indent=2, sort_keys=True)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def initialize(args: argparse.Namespace) -> dict[str, Any]:
    if args.state_file.exists():
        raise ValueError(f"state file already exists: {args.state_file}")
    plans = json.loads(args.plan_file.read_text(encoding="utf-8"))
    if not isinstance(plans, list):
        raise ValueError("plan file must contain an array of pairs")
    pairs = []
    for plan in plans:
        if not isinstance(plan, dict) or "status" in plan:
            raise ValueError("planned pairs must be objects without a status")
        pair = {**plan, "status": "planned"}
        pairs.append(validate_pair(pair))
    manifest = {
        "schema_version": 1,
        "run": {
            "kind": args.kind,
            "job_name": args.job_name,
            "build_id": args.build_id,
            "started_at": args.started_at or datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "ocp_version": args.ocp_version,
            "gpu_operator_version": args.gpu_operator_version,
            "environment": args.environment,
            "status": "unknown",
            "planned_modes": sorted({pair["mode"] for pair in pairs}),
        },
        "pairs": pairs,
    }
    validate_manifest(manifest)
    atomic_json(args.state_file, manifest)
    return manifest


def update(args: argparse.Namespace) -> dict[str, Any]:
    manifest = validate_manifest(json.loads(args.state_file.read_text(encoding="utf-8")))
    incoming = validate_pair(json.loads(args.pair_file.read_text(encoding="utf-8")))
    for index, existing in enumerate(manifest["pairs"]):
        if existing["id"] != incoming["id"]:
            continue
        for key in ("id", "mode", "nno_requested", "driver_requested"):
            if existing[key] != incoming[key]:
                raise ValueError(f"pair {incoming['id']} changed planned {key}")
        if not set(existing.get("checks", {})).issubset(incoming.get("checks", {})):
            raise ValueError(f"pair {incoming['id']} omitted a planned check")
        if existing["status"] in {"passed", "failed", "skipped", "blocked"}:
            raise ValueError(f"pair {incoming['id']} already has a final result")
        manifest["pairs"][index] = incoming
        validate_manifest(manifest)
        atomic_json(args.state_file, manifest)
        return manifest
    raise ValueError(f"pair {incoming['id']} was not in the build plan")


def update_run(args: argparse.Namespace) -> dict[str, Any]:
    manifest = validate_manifest(json.loads(args.state_file.read_text(encoding="utf-8")))
    fields = {
        "ocp_version": args.ocp_version,
        "gpu_operator_version": args.gpu_operator_version,
        "environment": args.environment,
    }
    if all(value is None for value in fields.values()):
        raise ValueError("provide at least one run field to update")
    for name, value in fields.items():
        if value is not None:
            manifest["run"][name] = nonempty(value, f"run.{name}")
    atomic_json(args.state_file, manifest)
    return manifest


def publish(args: argparse.Namespace) -> Path:
    manifest = validate_manifest(json.loads(args.state_file.read_text(encoding="utf-8")))
    output = args.artifact_dir / "nno-pairs.json"
    atomic_json(output, manifest)
    return output


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    start = commands.add_parser("init", help="Record planned pairs before tests begin")
    start.add_argument("--state-file", required=True, type=Path)
    start.add_argument("--plan-file", required=True, type=Path, help="JSON array of planned pair objects")
    start.add_argument("--kind", required=True, choices=("presubmit", "periodic"))
    start.add_argument("--job-name", required=True)
    start.add_argument("--build-id", required=True)
    start.add_argument("--started-at", default="")
    start.add_argument("--ocp-version", default="")
    start.add_argument("--gpu-operator-version", default="")
    start.add_argument("--environment", default="")
    change = commands.add_parser("update", help="Record a running or final pair result")
    change.add_argument("--state-file", required=True, type=Path)
    change.add_argument("--pair-file", required=True, type=Path, help="Complete pair result JSON")
    run_update = commands.add_parser("set-run", help="Add observed build context after cluster setup")
    run_update.add_argument("--state-file", required=True, type=Path)
    run_update.add_argument("--ocp-version")
    run_update.add_argument("--gpu-operator-version")
    run_update.add_argument("--environment")
    finish = commands.add_parser("publish", help="Copy the manifest into a Prow artifact directory")
    finish.add_argument("--state-file", required=True, type=Path)
    finish.add_argument("--artifact-dir", required=True, type=Path)
    args = parser.parse_args()
    if args.command == "init":
        initialize(args)
    elif args.command == "update":
        update(args)
    elif args.command == "set-run":
        update_run(args)
    else:
        print(publish(args))


if __name__ == "__main__":
    main()
