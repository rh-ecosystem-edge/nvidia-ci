"""Collect NNO pair manifests from local Prow artifacts or public GCS.

The report JSON is an index of immutable Prow build manifests. A build can
contribute several NNO/DOCA pairs, including multiple attempts at one pair.
"""

from __future__ import annotations

import argparse
import json
import re
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

BUCKET = "test-platform-results"
MANIFEST_NAME = "nno-pairs.json"
PR_PATH = re.compile(
    r"^pr-logs/pull/(?P<repo>[^/]+)/(?P<pr>\d+)/(?P<job>[^/]+)/(?P<build>[^/]+)/"
)
PERIODIC_PATH = re.compile(r"^logs/(?P<job>[^/]+)/(?P<build>[^/]+)/")
PAIR_STATUSES = {"planned", "running", "passed", "failed", "skipped", "blocked"}
MODES = {"standard", "signed"}


def source_identity(path: str) -> dict[str, str] | None:
    """Read a Prow build identity from an artifact path."""
    path = path.lstrip("/")
    match = PR_PATH.match(path)
    if match:
        return {
            "kind": "presubmit",
            "repo": match["repo"],
            "pr": match["pr"],
            "job_name": match["job"],
            "build_id": match["build"],
            "build_path": path[: match.end()].rstrip("/"),
        }
    match = PERIODIC_PATH.match(path)
    if match:
        return {
            "kind": "periodic",
            "job_name": match["job"],
            "build_id": match["build"],
            "build_path": path[: match.end()].rstrip("/"),
        }
    return None


def _nonempty(value: Any, field: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{field} must be a nonempty string")
    return value.strip()


def _image(value: Any, field: str, *, required: bool) -> dict[str, str]:
    if value is None and not required:
        return {}
    if not isinstance(value, dict):
        raise ValueError(f"{field} must be an object")
    version = _nonempty(value.get("version"), f"{field}.version") if required else str(value.get("version", ""))
    image = {
        "version": version,
        "image": str(value.get("image", "")),
        "digest": str(value.get("digest", "")),
    }
    if not required:
        image["ofed_version"] = str(value.get("ofed_version", ""))
    return image


def normalize_manifest(data: Any, path: str) -> dict[str, Any]:
    """Validate and normalize a build manifest without inventing test results."""
    if not isinstance(data, dict) or data.get("schema_version") != 1:
        raise ValueError("manifest requires schema_version: 1")
    run = data.get("run")
    pairs = data.get("pairs")
    if not isinstance(run, dict) or not isinstance(pairs, list):
        raise ValueError("manifest requires run object and pairs array")

    identity = source_identity(path)
    job_name = _nonempty(run.get("job_name"), "run.job_name")
    build_id = _nonempty(str(run.get("build_id", "")), "run.build_id")
    kind = _nonempty(run.get("kind"), "run.kind")
    if kind not in {"presubmit", "periodic"}:
        raise ValueError("run.kind must be presubmit or periodic")
    if identity and (identity["job_name"], identity["build_id"], identity["kind"]) != (job_name, build_id, kind):
        raise ValueError(f"manifest run identity disagrees with artifact path: {path}")
    if identity is None:
        build_path = f"local/{kind}/{job_name}/{build_id}"
    else:
        build_path = identity["build_path"]

    prow_url = run.get("prow_url")
    if not prow_url and identity:
        prow_url = f"https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/{BUCKET}/{build_path}"
    if prow_url and not str(prow_url).startswith("https://"):
        raise ValueError("run.prow_url must be an HTTPS URL")
    planned_modes = run.get("planned_modes", [])
    if not isinstance(planned_modes, list) or any(mode not in MODES for mode in planned_modes):
        raise ValueError("run.planned_modes must contain standard and/or signed")

    normalized_pairs: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    for index, pair in enumerate(pairs):
        if not isinstance(pair, dict):
            raise ValueError(f"pairs[{index}] must be an object")
        pair_id = _nonempty(pair.get("id"), f"pairs[{index}].id")
        if pair_id in seen_ids:
            raise ValueError(f"duplicate pair attempt id: {pair_id}")
        seen_ids.add(pair_id)
        mode = _nonempty(pair.get("mode"), f"pairs[{index}].mode")
        status = _nonempty(pair.get("status"), f"pairs[{index}].status")
        if mode not in MODES or status not in PAIR_STATUSES:
            raise ValueError(f"invalid mode or status in pair {pair_id}")
        nno_requested = _nonempty(pair.get("nno_requested"), f"pairs[{index}].nno_requested")
        driver_requested = _image(pair.get("driver_requested"), f"pairs[{index}].driver_requested", required=True)
        driver_observed = _image(pair.get("driver_observed"), f"pairs[{index}].driver_observed", required=False)
        nno_observed = str(pair.get("nno_observed", ""))
        checks = pair.get("checks", {})
        metrics = pair.get("metrics", {})
        artifacts = pair.get("artifacts", {})
        if not all(isinstance(item, dict) for item in (checks, metrics, artifacts)):
            raise ValueError(f"checks, metrics and artifacts must be objects in pair {pair_id}")
        if status == "passed":
            if nno_observed != nno_requested or driver_observed.get("version") != driver_requested["version"]:
                raise ValueError(f"passed pair {pair_id} lacks matching observed versions")
            if not checks or any(value != "passed" for value in checks.values()):
                raise ValueError(f"passed pair {pair_id} requires passed checks")
            if mode == "signed" and checks.get("signature_verification") != "passed":
                raise ValueError(f"passed signed pair {pair_id} requires signature verification")
        normalized_pairs.append({
            "id": pair_id,
            "mode": mode,
            "status": status,
            "nno_requested": nno_requested,
            "nno_observed": nno_observed,
            "driver_requested": driver_requested,
            "driver_observed": driver_observed,
            "checks": checks,
            "metrics": metrics,
            "artifacts": artifacts,
            "completed_at": str(pair.get("completed_at", "")),
            "note": str(pair.get("note", "")),
        })

    return {
        "source_path": path,
        "build_path": build_path,
        "run": {
            "kind": kind,
            "job_name": job_name,
            "build_id": build_id,
            "prow_url": str(prow_url or ""),
            "started_at": _nonempty(run.get("started_at"), "run.started_at"),
            "ocp_version": str(run.get("ocp_version", "")),
            "gpu_operator_version": str(run.get("gpu_operator_version", "")),
            "environment": str(run.get("environment", "")),
            "status": str(run.get("status", "unknown")),
            "planned_modes": sorted(set(planned_modes)),
        },
        "pairs": normalized_pairs,
    }


def load_local(root: Path) -> list[dict[str, Any]]:
    """Load all pair manifests under a local artifact tree."""
    if not root.is_dir():
        raise ValueError(f"input directory does not exist: {root}")
    builds = []
    for path in sorted(root.rglob(MANIFEST_NAME)):
        relative_path = path.relative_to(root).as_posix()
        builds.append(normalize_manifest(json.loads(path.read_text()), relative_path))
    return builds


def _gcs_json(params: dict[str, str]) -> dict[str, Any]:
    query = urllib.parse.urlencode(params)
    url = f"https://storage.googleapis.com/storage/v1/b/{BUCKET}/o?{query}"
    with urllib.request.urlopen(url, timeout=30) as response:
        return json.load(response)


def _gcs_manifest(path: str) -> dict[str, Any]:
    quoted = urllib.parse.quote(path, safe="")
    url = f"https://storage.googleapis.com/storage/v1/b/{BUCKET}/o/{quoted}?alt=media"
    with urllib.request.urlopen(url, timeout=30) as response:
        return json.load(response)


def list_gcs_manifests(prefix: str) -> list[str]:
    """List pair artifacts under one PR or periodic job prefix."""
    paths: list[str] = []
    page_token = ""
    while True:
        params = {"prefix": prefix, "matchGlob": f"{prefix}**/{MANIFEST_NAME}", "maxResults": "1000"}
        if page_token:
            params["pageToken"] = page_token
        page = _gcs_json(params)
        paths.extend(item["name"] for item in page.get("items", []) if item["name"].endswith(f"/{MANIFEST_NAME}"))
        page_token = page.get("nextPageToken", "")
        if not page_token:
            return sorted(set(paths))


def load_gcs(pr_numbers: list[str], periodic_jobs: list[str], *, max_periodic_builds: int = 50) -> list[dict[str, Any]]:
    """Read public artifacts from PR rehearsals/presubmits and scheduled jobs."""
    paths: set[str] = set()
    for pr in pr_numbers:
        if not pr.isdigit():
            raise ValueError(f"invalid PR number: {pr}")
        for repo in ("rh-ecosystem-edge_nvidia-ci", "openshift_release"):
            paths.update(list_gcs_manifests(f"pr-logs/pull/{repo}/{pr}/"))
    for job in periodic_jobs:
        if "/" in job or not job.startswith("periodic-"):
            raise ValueError(f"invalid periodic job name: {job}")
        job_paths = list_gcs_manifests(f"logs/{job}/")
        # Prow build IDs are increasing decimal strings. Only inspect recent runs.
        paths.update(sorted(job_paths, key=lambda path: int(source_identity(path)["build_id"]), reverse=True)[:max_periodic_builds])
    return [normalize_manifest(_gcs_manifest(path), path) for path in sorted(paths)]


def merge_builds(existing: dict[str, Any], incoming: list[dict[str, Any]]) -> dict[str, Any]:
    """Replace a build by its immutable Prow path; keep historical builds."""
    if existing and existing.get("schema_version") != 1:
        raise ValueError("baseline report has an unsupported schema version")
    merged = {build["build_path"]: build for build in existing.get("builds", [])}
    incoming_by_path = {build["build_path"]: build for build in incoming}
    if len(incoming_by_path) != len(incoming):
        raise ValueError("multiple pair manifests found for one Prow build")
    merged.update(incoming_by_path)
    return {"schema_version": 1, "builds": [merged[key] for key in sorted(merged)]}


def main() -> None:
    parser = argparse.ArgumentParser(description="Collect NNO pair results")
    parser.add_argument("--input-dir", type=Path, help="Local tree containing nno-pairs.json artifacts")
    parser.add_argument("--pr", action="append", default=[], help="PR number to fetch from public Prow GCS")
    parser.add_argument("--periodic-job", action="append", default=[], help="Periodic Prow job to fetch")
    parser.add_argument("--baseline", type=Path, help="Existing report JSON to merge")
    parser.add_argument("--output", required=True, type=Path, help="Report JSON output path")
    args = parser.parse_args()
    if not args.input_dir and not args.pr and not args.periodic_job:
        parser.error("provide --input-dir, --pr or --periodic-job")
    existing = json.loads(args.baseline.read_text()) if args.baseline and args.baseline.exists() else {}
    incoming = []
    if args.input_dir:
        incoming.extend(load_local(args.input_dir))
    if args.pr or args.periodic_job:
        incoming.extend(load_gcs(args.pr, args.periodic_job))
    result = merge_builds(existing, incoming)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(f"Collected {len(incoming)} manifests; report has {len(result['builds'])} builds: {args.output}")


if __name__ == "__main__":
    main()
