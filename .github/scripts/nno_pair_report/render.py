"""Render standard and signed NNO/DOCA compatibility matrices."""

from __future__ import annotations

import argparse
import html
import json
import re
from pathlib import Path
from typing import Any

if __package__:
    from .signed import is_signed_job
else:
    from signed import is_signed_job

MATRIX_OUTPUT_NAMES = {
    "standard": "nno_doca2_matrix",
    "signed": "nno_doca2_signed_matrix",
}
EXPLORER_OUTPUT_NAMES = {
    "standard": "nno_doca2_explorer",
    "signed": "nno_doca2_signed_explorer",
}


def scheduled_stability(builds: list[dict[str, Any]], mode: str = "standard") -> list[dict[str, Any]]:
    """Group scheduled build outcomes by exact OpenShift version and Prow job."""
    groups: dict[tuple[str, str], list[dict[str, Any]]] = {}
    for build in builds:
        run = build["run"]
        if run["kind"] != "periodic":
            continue
        ocp = run.get("ocp_version") or "Unknown"
        status = run.get("status", "unknown").lower()
        if status not in {"success", "failure", "aborted", "pending", "running"}:
            status = "unknown"
        if status == "success" and run.get("discovery", {}).get("outcome") == "no_work":
            status = "no_work"
        elif mode == "signed" and status not in {"failure", "aborted"}:
            statuses = [pair["status"] for pair in build["pairs"]]
            if "failed" in statuses:
                status = "failure"
            elif status == "success" and (not statuses or any(value != "passed" for value in statuses)):
                status = "blocked"
        groups.setdefault((ocp, run["job_name"]), []).append({
            "build_id": run["build_id"],
            "started_at": run["started_at"],
            "ocp_version": ocp,
            "status": status,
            "prow_status": run.get("status", "unknown"),
            "prow_url": run["prow_url"],
            "passed_pairs": sum(pair["status"] == "passed" for pair in build["pairs"]),
            "failed_pairs": sum(pair["status"] == "failed" for pair in build["pairs"]),
            "outcome": run.get("discovery", {}).get("outcome", ""),
            "reason": run.get("discovery", {}).get("reason", ""),
            "result_note": run.get("result_note", ""),
        })

    return [
        {
            "ocp_version": ocp,
            "job_name": job,
            "runs": sorted(groups[(ocp, job)], key=lambda run: (run["started_at"], run["build_id"]), reverse=True),
        }
        for ocp, job in sorted(groups, reverse=True)
    ]


def precompiled_reached(pair: dict[str, Any]) -> bool:
    """A no-image skip or a failure before image selection is not a tested pair."""
    checks = pair["checks"]
    if checks.get("precompiled_selection") == "passed":
        return True
    driver = pair["driver_requested"]
    return bool(driver.get("image") and driver.get("version") and any(
        checks.get(name) in {"passed", "failed"}
        for name in ("nno_deployment", "nic_cluster_policy_ready")
    ))


def signed_validation_recorded(pair: dict[str, Any]) -> bool:
    """Missing NNO alone does not mean a deployment or verifier never ran."""
    return bool(
        pair.get("nno_observed") or "artifact_consistency" in pair["checks"]
        or any(stage.get("attempt") or stage.get("attempts") for stage in pair.get("stages", {}).values())
        or any(pair["checks"].get(name) in {"passed", "failed", "skipped"} for name in (
            "deploy_step", "deploy_result", "gpudirect_step", "gpudirect_result", "nno_deployment",
        ))
    )


def unvalidated_job_failure(run: dict[str, Any]) -> dict[str, str]:
    """Name a confirmed prerequisite failure; do not infer one from absent artifacts."""
    for step in run.get("failed_steps", []):
        name = step["name"]
        if "deploy-cluster" in name:
            reason = "Cluster deployment failed"
        elif name == "nno-discover-signed-target":
            reason = "Image discovery failed"
        elif name == "nno-read-signed-target":
            reason = "Target preparation failed"
        else:
            continue
        return {"validation_outcome": "not_reached", "result_label": "Failed before validation",
                "result_note": reason + " — precompiled validation not reached"}
    return {"validation_outcome": "not_recorded", "result_label": "Failed — validation not recorded",
            "result_note": "Job failed — no precompiled validation evidence was recorded"}


def validated_precompiled_pair(pair: dict[str, Any], run: dict[str, Any]) -> dict[str, Any]:
    """Require precompiled policy readiness, RDMA and a successful Prow build.

    Apply this to saved history as well as freshly collected sidecars so old
    deployment-only passes cannot remain green after the validation rule changes.
    """
    checks = {**pair["checks"]}
    if pair["validation"] == "precompiled":
        for name in ("ocp_version", "nno_csv", "precompiled_selection", "nno_deployment", "nic_cluster_policy_ready"):
            checks.setdefault(name, "blocked")
        if not any(name in checks for name in ("rdma_shared_device", "rdma_gpudirect")):
            checks["rdma_shared_device"] = "blocked"
    checks["prow_job"] = (
        "passed" if run.get("status") == "success" else
        "failed" if run.get("status") in {"failure", "error", "aborted"} else "blocked"
    )
    if pair["validation"] == "signed" and not signed_validation_recorded(pair) and checks["prow_job"] == "failed":
        return {**pair, "status": "failed", "checks": checks, **unvalidated_job_failure(run)}
    status = pair["status"]
    if "failed" in checks.values():
        status = "failed"
    elif status == "passed" and any(value != "passed" for value in checks.values()):
        status = "blocked"
    return {**pair, "status": status, "checks": checks}


def mode_data(history: dict[str, Any], mode: str) -> dict[str, Any]:
    if history.get("schema_version") != 1 or not isinstance(history.get("builds"), list):
        raise ValueError("report history requires schema_version 1 and builds array")
    builds = []
    for build in history["builds"]:
        run = build["run"]
        if (mode == "signed" and is_signed_job(run["job_name"])
                and run.get("status") in {"failure", "error", "aborted"}
                and not any(signed_validation_recorded(pair) for pair in build["pairs"])):
            run = {**run, **unvalidated_job_failure(run)}
        pairs = []
        for pair in build["pairs"]:
            validation = pair.get("validation") or (
                "precompiled" if pair["id"].startswith("doca2-precompiled-") else
                "signed" if pair["mode"] == "signed" else "dynamic"
            )
            pair_mode = "signed" if validation == "precompiled" else pair["mode"]
            if pair_mode != mode or validation not in ({"signed", "precompiled"} if mode == "signed" else {"dynamic"}):
                continue
            if pair_mode != pair["mode"] or pair.get("validation") != validation:
                pair = {**pair, "mode": pair_mode, "validation": validation}
            if mode == "signed":
                if validation == "precompiled" and not precompiled_reached(pair):
                    continue
                pair = validated_precompiled_pair(pair, run)
            image_target = re.search(
                r"-((?:rhcos|rhel)\d+(?:\.\d+)+)-(amd64|arm64|ppc64le|s390x)$",
                pair["driver_requested"].get("image", ""),
            )
            if image_target:
                pair = {**pair, "image_target": f"{image_target[1]} / {image_target[2]}"}
            if mode == "standard" and pair["status"] == "passed" and pair["checks"].get("rdma_gpudirect") != "passed":
                checks = {**pair["checks"]}
                checks.setdefault("rdma_gpudirect", "blocked")
                status = "failed" if "failed" in checks.values() else "blocked"
                pair = {**pair, "status": status, "checks": checks}
            pairs.append(pair)
        include_planned = (
            is_signed_job(build["run"]["job_name"]) if mode == "signed" else
            mode in build["run"].get("planned_modes", [])
        )
        if pairs or include_planned:
            builds.append({**build, "run": run, "pairs": pairs})
    return {"schema_version": 1, "mode": mode, "builds": builds, "scheduled_stability": scheduled_stability(builds, mode)}


def safe_report_data(data: dict[str, Any]) -> str:
    safe_payload = json.dumps(data, separators=(",", ":"), ensure_ascii=False)
    return safe_payload.replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")


def render_explorer_html(data: dict[str, Any], label: str) -> str:
    template = Path(__file__).with_name("report_template.html").read_text()
    mode = data["mode"]
    other = "signed" if mode == "standard" else "standard"
    return (template.replace("__REPORT_LABEL__", html.escape(label))
            .replace("__REPORT_TITLE__", html.escape("Signed compatibility explorer" if mode == "signed" else "Compatibility explorer"))
            .replace("__MATRIX_HEADING__", html.escape("NNO and signed DOCA/OFED combinations" if mode == "signed" else "NNO and DOCA/OFED combinations"))
            .replace("__MODE__", html.escape(mode))
            .replace("__OTHER_FILE__", html.escape(EXPLORER_OUTPUT_NAMES[other] + ".html"))
            .replace("__MATRIX_FILE__", html.escape(MATRIX_OUTPUT_NAMES[mode] + ".html"))
            .replace("__OTHER_LABEL__", html.escape("Signed DOCA/OFED" if other == "signed" else "Standard"))
            .replace("__VALIDATION_NOTE__", html.escape(
                "Daily jobs appear only after reaching precompiled testing. Passing requires precompiled image selection, deployment, NIC policy readiness, RDMA and a successful Prow job. Signed workflows always appear and require GPUDirect RDMA. No cryptographic signature or Secure Boot check is recorded." if mode == "signed" else
                "Dynamic OFED pairs require GPUDirect RDMA to pass."
            ))
            .replace("__REPORT_DATA__", safe_report_data(data)))


def render_matrix_html(data: dict[str, Any], label: str) -> str:
    template = Path(__file__).with_name("gpu_style_template.html").read_text()
    mode = data["mode"]
    other = "signed" if mode == "standard" else "standard"
    return (template.replace("__REPORT_LABEL__", html.escape(label))
            .replace("__REPORT_MODE__", html.escape("Signed " if mode == "signed" else ""))
            .replace("__EXPLORER_FILE__", html.escape(EXPLORER_OUTPUT_NAMES[mode] + ".html"))
            .replace("__OTHER_FILE__", html.escape(MATRIX_OUTPUT_NAMES[other] + ".html"))
            .replace("__OTHER_LABEL__", html.escape("Signed DOCA/OFED" if other == "signed" else "Standard"))
            .replace("__REPORT_DATA__", safe_report_data(data)))


def write_reports(history: dict[str, Any], output_dir: Path, label: str = "Prow results") -> list[Path]:
    output_dir.mkdir(parents=True, exist_ok=True)
    written = []
    for mode, name in MATRIX_OUTPUT_NAMES.items():
        data = mode_data(history, mode)
        json_path = output_dir / f"{name}.json"
        html_path = output_dir / f"{name}.html"
        explorer_path = output_dir / f"{EXPLORER_OUTPUT_NAMES[mode]}.html"
        json_path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")
        html_path.write_text(render_matrix_html(data, label))
        explorer_path.write_text(render_explorer_html(data, label))
        written.extend((json_path, html_path, explorer_path))
    return written


def main() -> None:
    parser = argparse.ArgumentParser(description="Render NNO pair reports")
    parser.add_argument("--data", required=True, type=Path, help="Collected NNO pair history JSON")
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--label", default="Prow results", help="Source label shown in the report")
    args = parser.parse_args()
    history = json.loads(args.data.read_text())
    for path in write_reports(history, args.output_dir, args.label):
        print(path)


if __name__ == "__main__":
    main()
