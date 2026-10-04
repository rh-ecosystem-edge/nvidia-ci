"""Collect NNO step results from Prow artifacts and build the report history.

The report JSON is an index of immutable Prow builds. A build can contribute
several NNO/DOCA pairs, including multiple attempts at one pair. Step result
files are combined into that report schema.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import requests

from common import gcs_utils

BUCKET = gcs_utils.GCS_BUCKET
STEP_RESULT_NAME = "nno-step-result.json"
STEP_RESULT_PREFIX = "nno-step-result-"
PR_PATH = re.compile(
    r"^pr-logs/pull/(?P<repo>[^/]+)/(?P<pr>\d+)/(?P<job>[^/]+)/(?P<build>[^/]+)/"
)
PERIODIC_PATH = re.compile(r"^logs/(?P<job>[^/]+)/(?P<build>[^/]+)/")
PAIR_STATUSES = {"planned", "running", "passed", "failed", "skipped", "blocked"}
MODES = {"standard", "signed"}
STEP_CHECKS = {
    "standard": (
        "ocp_version", "nno_csv", "nno_deployment", "nic_cluster_policy_ready",
        "rdma_gpudirect",
    ),
    "precompiled": ("ocp_version", "nno_csv", "precompiled_selection", "nno_deployment", "nic_cluster_policy_ready"),
}


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
        "doca_version": str(value.get("doca_version", "")),
    }
    if not required:
        image["ofed_version"] = str(value.get("ofed_version", ""))
    return image


def normalize_manifest(data: Any, path: str) -> dict[str, Any]:
    """Validate a report build without inventing test results."""
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

    if identity:
        prow_url = f"https://prow.ci.openshift.org/view/gs/{BUCKET}/{build_path}"
    else:
        prow_url = run.get("prow_url")
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
        validation = pair.get("validation") or (
            "precompiled" if pair_id.startswith("doca2-precompiled-") else
            "signed" if mode == "signed" else "dynamic"
        )
        if not isinstance(validation, str) or validation not in {"dynamic", "precompiled", "signed"}:
            raise ValueError(f"invalid validation in pair {pair_id}")
        if validation == "precompiled":
            mode = "signed"
        elif (mode == "signed") != (validation == "signed"):
            raise ValueError(f"mode and validation disagree in pair {pair_id}")
        nno_requested = str(pair.get("nno_requested", "") or "")
        driver_requested = _image(
            pair.get("driver_requested"), f"pairs[{index}].driver_requested",
            required=status == "passed",
        )
        driver_observed = _image(pair.get("driver_observed"), f"pairs[{index}].driver_observed", required=False)
        nno_observed = str(pair.get("nno_observed", ""))
        checks = pair.get("checks", {})
        metrics = pair.get("metrics", {})
        artifacts = pair.get("artifacts", {})
        if not all(isinstance(item, dict) for item in (checks, metrics, artifacts)):
            raise ValueError(f"checks, metrics and artifacts must be objects in pair {pair_id}")
        if status == "passed":
            if not nno_requested:
                raise ValueError(f"passed pair {pair_id} requires nno_requested")
            observed_driver_version = driver_observed.get("version", "")
            if nno_observed != nno_requested or (
                observed_driver_version and observed_driver_version != driver_requested["version"]
            ):
                raise ValueError(f"passed pair {pair_id} has mismatched NNO or driver versions")
            if not checks or any(value != "passed" for value in checks.values()):
                raise ValueError(f"passed pair {pair_id} requires passed checks")
            if mode == "standard" and validation == "dynamic" and checks.get("rdma_gpudirect") != "passed":
                raise ValueError(f"passed dynamic pair {pair_id} requires GPUDirect RDMA")
        normalized_pairs.append({
            "id": pair_id,
            "mode": mode,
            "validation": validation,
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


def _step_family(result: dict[str, Any]) -> str:
    selection = result.get("precompiled_selection")
    step = str(result.get("step", "")).lower()
    return "precompiled" if isinstance(selection, dict) or "precompiled" in step else "standard"


def _step_attempt(result: dict[str, Any]) -> str:
    value = result.get("attempt") or result.get("build_id") or "default"
    return str(value).strip() or "default"


def _step_check_status(value: Any, result_status: str) -> str:
    status = str(value or "")
    if status in {"passed", "failed", "skipped", "blocked", "planned"}:
        return status
    if status in {"", "not_run", "not_found"}:
        return "planned"
    if status == "running":
        return result_status if result_status in PAIR_STATUSES else "blocked"
    if status == "found":
        return "passed"
    return "blocked"


def _combine_check_status(current: str, incoming: str) -> str:
    priority = {"planned": 0, "passed": 1, "skipped": 2, "blocked": 3, "failed": 4}
    return incoming if priority[incoming] > priority[current] else current


def _image_reference(repository: Any, image: Any) -> str:
    repository = str(repository or "").strip().rstrip("/")
    image = str(image or "").strip().lstrip("/")
    if not repository:
        return image
    if not image:
        return repository
    if image.startswith(f"{repository}/"):
        return image
    return f"{repository}/{image}"


def _image_digest(image_id: Any) -> str:
    value = str(image_id or "").strip()
    if "@" in value:
        return value.rsplit("@", 1)[1]
    return value if value.startswith("sha256:") else ""


def _step_timestamp(value: str) -> datetime:
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError(f"invalid step recorded_at timestamp: {value!r}") from error
    if parsed.tzinfo is None:
        raise ValueError(f"step recorded_at timestamp requires a timezone: {value!r}")
    return parsed.astimezone(timezone.utc)


def _utc_timestamp(value: datetime) -> str:
    return value.isoformat().replace("+00:00", "Z")


def _step_pair(
    family: str,
    attempt: str,
    records: list[tuple[str, dict[str, Any]]],
) -> dict[str, Any]:
    expected_checks = STEP_CHECKS[family]
    pair_id_attempt = re.sub(r"[^A-Za-z0-9_.-]", "-", attempt)
    pair: dict[str, Any] = {
        "id": f"doca2-{family}-{pair_id_attempt}",
        "mode": "standard" if family == "standard" else "signed",
        "validation": "dynamic" if family == "standard" else "precompiled",
        "status": "blocked",
        "nno_requested": "",
        "nno_observed": "",
        "driver_requested": {"version": "", "image": "", "digest": "", "doca_version": ""},
        "driver_observed": None,
        "checks": {name: "planned" for name in expected_checks},
        "metrics": {},
        "artifacts": {},
        "completed_at": "",
        "note": "",
    }
    matching = [
        (path, result) for path, result in records
        if _step_family(result) == family and _step_attempt(result) == attempt
    ]
    if not matching:
        pair["checks"] = {name: "blocked" for name in expected_checks}
        return pair

    statuses: list[str] = []
    notes: list[str] = []
    no_precompiled_match = False
    completed_at: datetime | None = None
    for path, result in matching:
        status = str(result.get("status", "blocked"))
        if status not in {"passed", "failed", "skipped", "blocked"}:
            status = "blocked"
        statuses.append(status)
        selection = result.get("precompiled_selection")
        if isinstance(selection, dict) and selection.get("outcome") == "no_match":
            no_precompiled_match = True

        ocp_version = str(result.get("ocp_version") or "").strip()
        if ocp_version:
            pair["checks"]["ocp_version"] = "passed"
        nno_version = str(result.get("nno_csv_version") or "").strip()
        if nno_version:
            if pair["nno_observed"] and pair["nno_observed"] != nno_version:
                statuses.append("failed")
                notes.append("NNO CSV version differed between step records")
            else:
                pair["nno_observed"] = nno_version
                pair["nno_requested"] = nno_version

        result_checks = result.get("checks", {})
        if isinstance(result_checks, dict):
            for name, value in result_checks.items():
                if name in {"rdma_shared_device", "rdma_gpudirect"}:
                    if "gpudirect" not in str(result.get("step", "")).lower():
                        continue
                    name = "rdma_gpudirect"
                if name not in pair["checks"]:
                    continue
                if name == "precompiled_selection" and no_precompiled_match:
                    value = "skipped"
                normalized = _step_check_status(value, status)
                pair["checks"][name] = _combine_check_status(pair["checks"][name], normalized)

        configured = result.get("configured_ofed_driver")
        selection_data = selection if isinstance(selection, dict) else {}
        configured_data = configured if isinstance(configured, dict) else {}
        configured_image = _image_reference(configured_data.get("repository"), configured_data.get("image"))
        selection_outcome = str(selection_data.get("outcome") or "")
        selected_version = str(selection_data.get("version") or "").strip()
        selected_pullspec = str(selection_data.get("pull_spec") or "").strip()
        if family == "precompiled" and selection_outcome in {"no_match", "selection_error", "not_run"}:
            driver_version = selected_version
            driver_image = selected_pullspec
        else:
            driver_version = selected_version or str(configured_data.get("version") or "").strip()
            driver_image = selected_pullspec or configured_image
        if driver_version:
            current = pair["driver_requested"]["version"]
            if current and current != driver_version:
                statuses.append("failed")
                notes.append("configured OFED version differed between step records")
            else:
                pair["driver_requested"]["version"] = driver_version
        if driver_image:
            current_image = pair["driver_requested"]["image"]
            if current_image and current_image != driver_image:
                statuses.append("failed")
                notes.append("configured OFED image differed between step records")
            else:
                pair["driver_requested"]["image"] = driver_image

        if configured_data and not (
            family == "precompiled" and selection_outcome in {"no_match", "selection_error", "not_run"}
        ):
            digest = _image_digest(configured_data.get("image_id"))
            if digest:
                current_digest = (pair["driver_observed"] or {}).get("digest", "")
                if current_digest and current_digest != digest:
                    statuses.append("failed")
                    notes.append("observed OFED image digest differed between step records")
                else:
                    pair["driver_observed"] = {
                        "version": "", "image": configured_image, "digest": digest,
                        "doca_version": "", "ofed_version": "",
                    }

        metrics = result.get("metrics", {})
        if isinstance(metrics, dict):
            is_standard_rdma = (
                family == "standard" and isinstance(result_checks, dict)
                and "rdma_shared_device" in result_checks
                and "gpudirect" not in str(result.get("step", "")).lower()
            )
            for name, value in metrics.items():
                if is_standard_rdma and name in {"bandwidth_gbps", "message_rate_mpps"}:
                    name = f"standard_{name}"
                pair["metrics"][name] = value
        step_name = str(result.get("step") or Path(path).parent.name or "unknown")
        identity = source_identity(path)
        if identity:
            relative_artifact = path.lstrip("/")
            artifact_key = f"{Path(path).parent.name or step_name} ({Path(path).name})"
            pair["artifacts"][artifact_key] = f"https://prow.ci.openshift.org/view/gs/{BUCKET}/{relative_artifact}"
        recorded_at = str(result.get("recorded_at") or "")
        if recorded_at:
            timestamp = _step_timestamp(recorded_at)
            if completed_at is None or timestamp > completed_at:
                completed_at = timestamp
                pair["completed_at"] = _utc_timestamp(timestamp)
        skip_reason = str(result.get("skip_reason") or "").strip()
        selection_error = str(selection.get("error") or "").strip() if isinstance(selection, dict) else ""
        notes.extend(note for note in (skip_reason, selection_error) if note)

    if no_precompiled_match:
        pair["status"] = "skipped"
        pair["checks"] = {
            name: value if value in {"passed", "failed"} else "skipped"
            for name, value in pair["checks"].items()
        }
        pair["checks"]["precompiled_selection"] = "skipped"
        notes.append("no matching precompiled OFED image")
    elif "failed" in statuses:
        pair["status"] = "failed"
    elif "skipped" in statuses:
        pair["status"] = "skipped"
    elif (
        "blocked" not in statuses
        and all(value == "passed" for value in pair["checks"].values())
        and pair["nno_observed"]
        and pair["driver_requested"]["version"]
    ):
        pair["status"] = "passed"

    if pair["status"] != "passed":
        pair["checks"] = {
            name: "blocked" if value == "planned" else value
            for name, value in pair["checks"].items()
        }
    pair["note"] = "; ".join(dict.fromkeys(notes))
    return pair


def normalize_step_results(
    records: list[tuple[str, dict[str, Any]]], path: str,
) -> dict[str, Any]:
    """Combine step sidecars into the report's existing build/pair shape."""
    if not records:
        raise ValueError("cannot build a report entry without step results")
    identity = source_identity(path)
    first = records[0][1]
    job_name = _nonempty(first.get("job_name") or (identity or {}).get("job_name"), "run.job_name")
    build_id = _nonempty(str(first.get("build_id") or (identity or {}).get("build_id", "")), "run.build_id")
    kind = (identity or {}).get("kind")
    if kind is None:
        kind = "presubmit" if job_name.startswith(("pull-", "rehearse-")) else "periodic"

    unique_records: list[tuple[str, dict[str, Any]]] = []
    seen_steps: dict[tuple[str, str], tuple[str, dict[str, Any]]] = {}
    attempts: set[str] = set()
    recorded_at: list[datetime] = []
    ocp_versions: list[str] = []
    for source_path, result in records:
        if result.get("schema_version") != 1:
            raise ValueError(f"step result requires schema_version: 1: {source_path}")
        if result.get("job_name") and result["job_name"] != job_name:
            raise ValueError(f"step result job disagrees with artifact path: {source_path}")
        if result.get("build_id") and str(result["build_id"]) != build_id:
            raise ValueError(f"step result build disagrees with artifact path: {source_path}")
        step_name = _nonempty(result.get("step"), f"step result name in {source_path}")
        attempt = _step_attempt(result)
        step_key = (attempt, step_name)
        previous = seen_steps.get(step_key)
        if previous:
            previous_path, previous_result = previous
            if previous_result != result:
                raise ValueError(
                    f"conflicting step results for attempt {attempt!r}, step {step_name!r}: "
                    f"{previous_path} and {source_path}"
                )
            continue
        seen_steps[step_key] = (source_path, result)
        unique_records.append((source_path, result))
        attempts.add(attempt)
        timestamp = str(result.get("recorded_at") or "").strip()
        if timestamp:
            recorded_at.append(_step_timestamp(timestamp))
        version = str(result.get("ocp_version") or "").strip()
        if version and version not in ocp_versions:
            ocp_versions.append(version)

    records = unique_records

    if not recorded_at:
        raise ValueError("step results require recorded_at")
    if len(ocp_versions) > 1:
        raise ValueError("step results disagree on the OpenShift version")
    pairs = []
    for attempt in sorted(attempts):
        families = {
            _step_family(result)
            for _, result in records
            if _step_attempt(result) == attempt
        }
        for family in (name for name in ("standard", "precompiled") if name in families):
            pairs.append(_step_pair(family, attempt, records))
    manifest = {
        "schema_version": 1,
        "run": {
            "kind": kind,
            "job_name": job_name,
            "build_id": build_id,
            "started_at": _utc_timestamp(min(recorded_at)),
            "ocp_version": ocp_versions[0] if ocp_versions else "",
            "status": "unknown",
            "planned_modes": sorted({pair["mode"] for pair in pairs}),
        },
        "pairs": pairs,
    }
    return normalize_manifest(manifest, path)


def _is_step_result_name(name: str) -> bool:
    return name == STEP_RESULT_NAME or (name.startswith(STEP_RESULT_PREFIX) and name.endswith(".json"))


def load_local(root: Path) -> list[dict[str, Any]]:
    """Load per-step artifact trees and combine them into report builds."""
    if not root.is_dir():
        raise ValueError(f"input directory does not exist: {root}")
    grouped: dict[str, list[tuple[str, dict[str, Any]]]] = {}
    step_paths = [path for path in root.rglob("*.json") if _is_step_result_name(path.name)]
    for path in sorted(step_paths):
        relative_path = path.relative_to(root).as_posix()
        result = json.loads(path.read_text())
        identity = source_identity(relative_path)
        if identity:
            build_path = identity["build_path"]
        else:
            job_name = _nonempty(result.get("job_name"), "step.job_name")
            build_id = _nonempty(str(result.get("build_id", "")), "step.build_id")
            kind = "presubmit" if job_name.startswith(("pull-", "rehearse-")) else "periodic"
            build_path = f"local/{kind}/{job_name}/{build_id}"
        grouped.setdefault(build_path, []).append((relative_path, result))

    return [
        normalize_step_results(steps, steps[0][0])
        for _, steps in sorted(grouped.items())
    ]


def _gcs_json(params: dict[str, str]) -> dict[str, Any]:
    return gcs_utils.http_get_json(gcs_utils.GCS_API_BASE_URL, params=params)


def _fetch_gcs_json(path: str) -> dict[str, Any]:
    return json.loads(gcs_utils.fetch_gcs_file_content(path))


def _is_not_found(error: requests.HTTPError) -> bool:
    return isinstance(error, gcs_utils.GCSFileNotFoundError) or (
        error.response is not None and error.response.status_code == 404
    )


def list_gcs_report_artifacts(prefix: str) -> list[str]:
    """List step result files below one Prow prefix."""
    pattern = f"{prefix}**/nno-step-result*.json"
    if gcs_utils.gcsweb_enabled():
        paths = [item["name"] for item in gcs_utils.list_gcsweb_objects(prefix, pattern)]
    else:
        paths = []
        page_token = ""
        while True:
            params = {"prefix": prefix, "matchGlob": pattern, "maxResults": "1000"}
            if page_token:
                params["pageToken"] = page_token
            page = _gcs_json(params)
            paths.extend(item["name"] for item in page.get("items", []))
            page_token = page.get("nextPageToken", "")
            if not page_token:
                break
    return sorted(path for path in paths if _is_step_result_name(path.rsplit("/", 1)[-1]))


def _matches_presubmit_job(path: str, job_name: str) -> bool:
    identity = source_identity(path)
    if identity is None or identity["kind"] != "presubmit":
        return False
    observed = identity["job_name"]
    return observed == job_name or bool(re.fullmatch(r"rehearse-\d+-" + re.escape(job_name), observed))


def load_gcs(
    pr_numbers: list[str], periodic_jobs: list[str], *,
    presubmit_job: str | None = None, max_periodic_builds: int = 7,
) -> list[dict[str, Any]]:
    """Read step result files from public Prow artifacts.

    Re-read one week of daily builds so a late upload or a missed run is still
    collected. Older builds remain in the saved history and are not fetched again.
    """
    paths: set[str] = set()
    for pr in pr_numbers:
        if not pr.isdigit():
            raise ValueError(f"invalid PR number: {pr}")
        for repo in ("rh-ecosystem-edge_nvidia-ci", "openshift_release"):
            prefix = f"pr-logs/pull/{repo}/{pr}/"
            job_prefixes = (
                (f"{prefix}{presubmit_job}/", f"{prefix}rehearse-{pr}-{presubmit_job}/")
                if presubmit_job else (prefix,)
            )
            for job_prefix in job_prefixes:
                pr_paths = list_gcs_report_artifacts(job_prefix)
                paths.update(path for path in pr_paths if not presubmit_job or _matches_presubmit_job(path, presubmit_job))
    for job in periodic_jobs:
        if "/" in job or not job.startswith("periodic-"):
            raise ValueError(f"invalid periodic job name: {job}")
        if gcs_utils.gcsweb_enabled():
            # Avoid traversing old builds and their large must-gather trees.
            build_dirs, _ = gcs_utils.list_gcsweb_directory(f"logs/{job}/")
            recent = sorted((build for build in build_dirs if build.isdigit()), key=int, reverse=True)[:max_periodic_builds]
            for build in recent:
                paths.update(list_gcs_report_artifacts(f"logs/{job}/{build}/"))
        else:
            job_paths = list_gcs_report_artifacts(f"logs/{job}/")
            # Prow build IDs are increasing decimal strings. Keep all report artifacts
            # from the most recent builds, not only the first artifact per build.
            build_paths = {
                identity["build_path"]
                for path in job_paths
                if (identity := source_identity(path)) is not None
            }
            recent_builds = set(sorted(
                build_paths,
                key=lambda path: int(path.rsplit("/", 1)[-1]),
                reverse=True,
            )[:max_periodic_builds])
            paths.update(
                path for path in job_paths
                if (identity := source_identity(path)) is not None and identity["build_path"] in recent_builds
            )

    grouped: dict[str, list[str]] = {}
    for path in sorted(paths):
        identity = source_identity(path)
        if identity is None:
            continue
        grouped.setdefault(identity["build_path"], []).append(path)

    builds = []
    for build_path, step_paths in sorted(grouped.items()):
        try:
            step_records = [(path, _fetch_gcs_json(path)) for path in sorted(step_paths)]
            build = normalize_step_results(step_records, step_records[0][0])
        except ValueError as error:
            print(f"Skipping malformed build {build_path}: {error}", file=sys.stderr)
            continue
        try:
            started = _fetch_gcs_json(f"{build_path}/started.json")
        except requests.HTTPError as error:
            if not _is_not_found(error):
                raise
        else:
            timestamp = _started_timestamp(started.get("timestamp"))
            if timestamp:
                build["run"]["started_at"] = timestamp

        finished_path = f"{build_path}/finished.json"
        try:
            finished = _fetch_gcs_json(finished_path)
        except requests.HTTPError as error:
            if not _is_not_found(error):
                raise
        else:
            result = str(finished.get("result", "")).lower()
            if result in {"success", "failure", "aborted"}:
                build["run"]["status"] = result
        builds.append(build)
    return builds


def _started_timestamp(value: Any) -> str:
    """Convert Prow started.json timestamps to the report's UTC timestamp format."""
    if isinstance(value, (int, float)):
        return datetime.fromtimestamp(value, timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
    if isinstance(value, str) and value.strip():
        raw = value.strip()
        if raw.isdigit():
            return datetime.fromtimestamp(int(raw), timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
        try:
            parsed = datetime.fromisoformat(raw.replace("Z", "+00:00"))
        except ValueError:
            return ""
        if parsed.tzinfo is None:
            parsed = parsed.replace(tzinfo=timezone.utc)
        return parsed.astimezone(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
    return ""


def merge_builds(existing: dict[str, Any], incoming: list[dict[str, Any]]) -> dict[str, Any]:
    """Replace a build by its immutable Prow path; keep historical builds."""
    if existing and existing.get("schema_version") != 1:
        raise ValueError("baseline report has an unsupported schema version")
    merged = {build["build_path"]: build for build in existing.get("builds", [])}
    incoming_by_path = {build["build_path"]: build for build in incoming}
    if len(incoming_by_path) != len(incoming):
        raise ValueError("multiple results found for one Prow build")
    merged.update(incoming_by_path)
    return {"schema_version": 1, "builds": [merged[key] for key in sorted(merged)]}


def main() -> None:
    parser = argparse.ArgumentParser(description="Collect NNO pair results")
    parser.add_argument("--input-dir", type=Path, help="Local Prow artifact tree containing step results")
    parser.add_argument("--pr", action="append", default=[], help="PR number to fetch from public Prow GCS")
    parser.add_argument("--presubmit-job", help="Limit PR artifacts to this Prow job (including rehearsals)")
    parser.add_argument("--periodic-job", action="append", default=[], help="Periodic Prow job to fetch")
    parser.add_argument("--baseline", type=Path, help="Existing report JSON to merge")
    parser.add_argument("--output", required=True, type=Path, help="Report JSON output path")
    parser.add_argument("--summary-output", type=Path, help="JSON summary of artifacts fetched in this run")
    args = parser.parse_args()
    if not args.input_dir and not args.pr and not args.periodic_job:
        parser.error("provide --input-dir, --pr or --periodic-job")
    existing = json.loads(args.baseline.read_text()) if args.baseline and args.baseline.exists() else {}
    incoming = []
    if args.input_dir:
        incoming.extend(load_local(args.input_dir))
    if args.pr or args.periodic_job:
        incoming.extend(load_gcs(args.pr, args.periodic_job, presubmit_job=args.presubmit_job))
    result = merge_builds(existing, incoming)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    if args.summary_output:
        args.summary_output.parent.mkdir(parents=True, exist_ok=True)
        args.summary_output.write_text(json.dumps({"fetched_builds": len(incoming)}) + "\n")
    print(f"Collected {len(incoming)} build artifacts; report has {len(result['builds'])} builds: {args.output}")


if __name__ == "__main__":
    main()
