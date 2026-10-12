"""Render standard and signed NNO/DOCA compatibility matrices."""

from __future__ import annotations

import argparse
import html
import json
from pathlib import Path
from typing import Any

MATRIX_OUTPUT_NAMES = {
    "standard": "nno_doca2_matrix",
    "signed": "nno_doca2_signed_matrix",
}
EXPLORER_OUTPUT_NAMES = {
    "standard": "nno_doca2_explorer",
    "signed": "nno_doca2_signed_explorer",
}


def scheduled_stability(builds: list[dict[str, Any]]) -> list[dict[str, Any]]:
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
        groups.setdefault((ocp, run["job_name"]), []).append({
            "build_id": run["build_id"],
            "started_at": run["started_at"],
            "ocp_version": ocp,
            "status": status,
            "prow_url": run["prow_url"],
            "passed_pairs": sum(pair["status"] == "passed" for pair in build["pairs"]),
            "failed_pairs": sum(pair["status"] == "failed" for pair in build["pairs"]),
        })

    return [
        {
            "ocp_version": ocp,
            "job_name": job,
            "runs": sorted(groups[(ocp, job)], key=lambda run: (run["started_at"], run["build_id"]), reverse=True),
        }
        for ocp, job in sorted(groups, reverse=True)
    ]


def mode_data(history: dict[str, Any], mode: str) -> dict[str, Any]:
    if history.get("schema_version") != 1 or not isinstance(history.get("builds"), list):
        raise ValueError("report history requires schema_version 1 and builds array")
    builds = []
    for build in history["builds"]:
        pairs = []
        for pair in build["pairs"]:
            validation = pair.get("validation") or (
                "precompiled" if pair["id"].startswith("doca2-precompiled-") else
                "signed" if pair["mode"] == "signed" else "dynamic"
            )
            pair_mode = "signed" if validation == "precompiled" else pair["mode"]
            if pair_mode != mode or validation not in ({"signed", "precompiled"} if mode == "signed" else {"dynamic"}):
                continue
            if pair_mode != pair["mode"]:
                pair = {**pair, "mode": pair_mode}
            if mode == "standard" and pair["status"] == "passed" and pair["checks"].get("rdma_gpudirect") != "passed":
                checks = {**pair["checks"]}
                checks.setdefault("rdma_gpudirect", "blocked")
                status = "failed" if "failed" in checks.values() else "blocked"
                pair = {**pair, "status": status, "checks": checks}
            pairs.append(pair)
        if pairs or mode in build["run"].get("planned_modes", []):
            builds.append({**build, "pairs": pairs})
    return {"schema_version": 1, "mode": mode, "builds": builds, "scheduled_stability": scheduled_stability(builds)}


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
                "The signed report currently uses precompiled OFED image selection and deployment results; RDMA and signature checks are not part of that step." if mode == "signed" else
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
