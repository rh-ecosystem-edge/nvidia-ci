#!/usr/bin/env python3
"""
Generate DPF HTML dashboard from accumulated JSON data.

Reads dpf_matrix.json and versions.json to produce dpf_matrix.html.
Deduplicates rows so each unique version combination appears only once
(most recent run wins).
"""

import argparse
import html
import json
import os
from typing import Dict, List, Any, Tuple
from datetime import datetime, timezone

from common.utils import logger
from common.html_builders import build_last_updated_footer, sanitize_id


def load_versions_config() -> Dict[str, Any]:
    script_dir = os.path.dirname(os.path.abspath(__file__))
    config_path = os.path.join(script_dir, "versions.json")
    with open(config_path, "r") as f:
        return json.load(f)


def dedup_key(result: Dict[str, Any]) -> Tuple:
    return (
        result.get("ocp_version", ""),
        result.get("branch", ""),
        result.get("dpf_version", ""),
        result.get("ovn_chart_version", ""),
        result.get("bluefield_ocp_image", ""),
        result.get("hcp_provisioner_image", ""),
    )


def build_history_bar(results: List[Dict[str, Any]], branch: str) -> str:
    if not results:
        return ""

    sorted_results = sorted(
        results,
        key=lambda r: int(r.get("job_timestamp", 0)),
        reverse=True,
    )

    squares = ""
    for result in sorted_results:
        status = result.get("networking_status", "FAILURE")
        status_class = (
            "history-success" if status == "SUCCESS" else "history-failure"
        )

        timestamp = int(result.get("job_timestamp", 0))
        date_str = datetime.fromtimestamp(
            timestamp, timezone.utc
        ).strftime("%b %d")
        prow_url = html.escape(result.get("prow_job_url", "#"), quote=True)

        squares += (
            f'<a href="{prow_url}" style="text-decoration:none"'
            f' target="_blank" rel="noopener noreferrer">'
            f'<div class="history-square {status_class}">'
            f'<span class="history-square-tooltip">'
            f"{status} - {date_str}</span>"
            f"</div></a>\n"
        )

    return (
        f'<div class="history-bar-outer">'
        f'<div class="history-bar-inner">'
        f"<span>Recent runs ({html.escape(branch)}):</span>\n"
        f"{squares}</div></div>\n"
    )


def build_branch_badge(branch: str) -> str:
    css_class = "badge-main" if branch == "main" else "badge-release"
    return f'<span class="badge {css_class}">{html.escape(branch)}</span>'


def highlight_image_tag(image: str) -> str:
    if ":" not in image:
        return html.escape(image)
    repo, tag = image.rsplit(":", 1)
    return (
        f'{html.escape(repo)}:'
        f'<span class="image-tag">{html.escape(tag)}</span>'
    )


def pick_representative(group: List[Dict[str, Any]]) -> Dict[str, Any]:
    """Pick the representative result for a version combination.

    If any run succeeded, return the most recent success.
    Otherwise return the most recent failure.
    """
    by_time = sorted(
        group, key=lambda r: int(r.get("job_timestamp", 0)), reverse=True
    )
    for r in by_time:
        if r.get("networking_status") == "SUCCESS":
            return r
    return by_time[0]


def build_table(results: List[Dict[str, Any]]) -> str:
    if not results:
        return ""

    groups: Dict[tuple, List[Dict[str, Any]]] = {}
    for result in results:
        key = dedup_key(result)
        groups.setdefault(key, []).append(result)

    unique_results = [pick_representative(g) for g in groups.values()]
    unique_results.sort(
        key=lambda r: int(r.get("job_timestamp", 0)), reverse=True
    )

    header = """
    <table>
        <thead>
            <tr>
                <th onclick="sortTable(this.closest('table'), 0)" style="cursor:pointer">OpenShift &#x25B4;&#x25BE;</th>
                <th onclick="sortTable(this.closest('table'), 1)" style="cursor:pointer">Branch &#x25B4;&#x25BE;</th>
                <th onclick="sortTable(this.closest('table'), 2)" style="cursor:pointer">DPF Version &#x25B4;&#x25BE;</th>
                <th onclick="sortTable(this.closest('table'), 3)" style="cursor:pointer">OVN Chart &#x25B4;&#x25BE;</th>
                <th>BlueField OCP Image</th>
                <th>HCP Provisioner</th>
                <th onclick="sortTable(this.closest('table'), 6)" style="cursor:pointer">Status &#x25B4;&#x25BE;</th>
            </tr>
        </thead>
        <tbody>"""

    rows = ""
    for result in unique_results:
        status = result.get("networking_status", "FAILURE")
        prow_url = html.escape(
            result.get("prow_job_url", "#"), quote=True
        )
        timestamp = int(result.get("job_timestamp", 0))
        date_str = datetime.fromtimestamp(
            timestamp, timezone.utc
        ).strftime("%b %d, %Y")

        if status == "SUCCESS":
            status_class = "status-success"
            link_class = "success-link"
        else:
            status_class = "status-failure"
            link_class = "failed-link"

        branch_badge = build_branch_badge(
            result.get("branch", "unknown")
        )
        bf_image = highlight_image_tag(
            result.get("bluefield_ocp_image", "")
        )
        hcp_image = highlight_image_tag(
            result.get("hcp_provisioner_image", "")
        )

        rows += f"""
            <tr>
                <td class="version-cell">{html.escape(result.get("ocp_version", ""))}</td>
                <td>{branch_badge}</td>
                <td class="version-cell">{html.escape(result.get("dpf_version", ""))}</td>
                <td class="version-cell">{html.escape(result.get("ovn_chart_version", ""))}</td>
                <td class="image-cell">{bf_image}</td>
                <td class="image-cell">{hcp_image}</td>
                <td class="{status_class}">
                    <a class="{link_class}" href="{prow_url}" target="_blank" rel="noopener noreferrer">{status}</a>
                    <span class="run-date">{date_str}</span>
                </td>
            </tr>"""

    return f"{header}{rows}\n        </tbody>\n    </table>"


def generate_dashboard(
    data: Dict[str, Dict[str, Any]],
    config: Dict[str, Any],
) -> str:
    script_dir = os.path.dirname(os.path.abspath(__file__))
    templates_dir = os.path.join(script_dir, "templates")

    with open(os.path.join(templates_dir, "header.html"), "r") as f:
        html_content = f.read()

    all_ocp_versions = set(data.keys())
    ocp_config = config.get("ocp_versions", {})
    for ocp_v in ocp_config:
        all_ocp_versions.add(ocp_v)

    sorted_ocp = sorted(
        all_ocp_versions,
        key=lambda v: tuple(int(x) for x in v.split(".")),
        reverse=True,
    )

    toc_links = ", ".join(
        f'<a href="#ocp-{html.escape(ocp_v, quote=True)}">'
        f"{html.escape(ocp_v)}</a>"
        for ocp_v in sorted_ocp
    )

    html_content += (
        '<div class="toc">\n'
        '    <div class="ocp-version-header">OpenShift Versions</div>\n'
        f"    {toc_links}\n"
        "</div>\n"
    )

    for ocp_v in sorted_ocp:
        is_planned = ocp_config.get(ocp_v, {}).get("status") == "planned"
        has_data = ocp_v in data and data[ocp_v].get("results")

        html_content += (
            f'\n<div class="ocp-version-container"'
            f' id="ocp-{html.escape(ocp_v)}">\n'
            f'    <div class="ocp-version-header">'
            f"OpenShift {html.escape(ocp_v)}</div>\n"
        )

        if is_planned and not has_data:
            html_content += (
                '    <div class="note-items"><ul>\n'
                '        <li class="note-item">'
                "No periodic jobs configured for this OCP version yet."
                "</li>\n"
                "    </ul></div>\n"
            )
        elif has_data:
            results = data[ocp_v]["results"]
            branches = {r.get("branch", "main") for r in results}
            primary_branch = (
                "main" if "main" in branches else sorted(branches)[0]
            )
            primary_results = [
                r for r in results if r.get("branch") == primary_branch
            ]
            html_content += build_table(results)
            html_content += build_history_bar(
                primary_results, primary_branch
            )
        else:
            html_content += (
                '    <div class="note-items"><ul>\n'
                '        <li class="note-item">'
                "No test results available yet.</li>\n"
                "    </ul></div>\n"
            )

        html_content += "</div>\n"

    html_content += build_last_updated_footer()

    return html_content


def main():
    parser = argparse.ArgumentParser(
        description="Generate DPF CI Dashboard HTML"
    )
    parser.add_argument(
        "--input-data", required=True, help="Path to JSON data file"
    )
    parser.add_argument(
        "--output-html", required=True, help="Path to output HTML file"
    )
    args = parser.parse_args()

    with open(args.input_data, "r") as f:
        data = json.load(f)

    config = load_versions_config()

    html_content = generate_dashboard(data, config)

    with open(args.output_html, "w") as f:
        f.write(html_content)

    logger.info(f"Dashboard saved to {args.output_html}")


if __name__ == "__main__":
    main()
