import html
import json
import argparse
import semver

from typing import Dict, List, Any
from datetime import datetime, timezone

from common.utils import logger
from common.templates import load_template
from common.html_builders import build_toc, build_notes
from common.validation import has_valid_semantic_versions
from gpu_operator_dashboard.fetch_ci_data import (
    OCP_FULL_VERSION, GPU_OPERATOR_VERSION, DRIVER_BRANCH, STATUS_ABORTED)


DEFAULT_TITLE = "Test Matrix: NVIDIA GPU Operator on Red Hat OpenShift"


TOPBAR_LINKS = [
    ("https://drivers.okoyl.xyz/groups/nvidia-gpu", "Driver delivery (VPN)"),
]


def build_topbar(sibling_url: str, sibling_label: str) -> str:
    """Build a navigation bar with back link on the left and external links on the right."""
    back_link = '<a href="index.html" style="color:#009688; text-decoration:underline; text-underline-offset:2px;">&#8592; Back</a>'
    links = []
    for url, label in TOPBAR_LINKS:
        safe_url = html.escape(url, quote=True)
        safe_label = html.escape(label)
        links.append(
            f'<a href="{safe_url}" style="color:#009688; text-decoration:underline; text-underline-offset:2px;">'
            f'{safe_label} →</a>')
    if sibling_url and sibling_label:
        safe_url = html.escape(sibling_url, quote=True)
        safe_label = html.escape(sibling_label)
        links.append(
            f'<a href="{safe_url}" style="color:#009688; text-decoration:underline; text-underline-offset:2px;">'
            f'View {safe_label} matrix →</a>')
    return (
        f'  <div style="display:flex; justify-content:space-between; gap:20px; margin-bottom:10px; font-size:14px;">'
        f'{back_link}<span style="display:flex; gap:20px;">{"".join(links)}</span></div>'
    )


def sort_ocp_minor_versions(ocp_keys: List[str]) -> List[str]:
    """Sort major.minor OpenShift versions numerically, newest first."""
    return sorted(
        ocp_keys,
        key=lambda version: tuple(int(part) for part in version.split('.')),
        reverse=True,
    )


def generate_test_matrix(ocp_data: Dict[str, Dict[str, Any]], title: str = DEFAULT_TITLE,
                         sibling_url: str = "", sibling_label: str = "") -> str:
    """
    Build the final HTML report by:
      1. Reading the header template,
      2. Generating the table blocks for each OCP version,
      3. Reading the footer template and injecting the last-updated time.
    """
    header_template = load_template("header.html")
    header_template = header_template.replace(DEFAULT_TITLE, title)
    header_template = header_template.replace("{sibling_link}", build_topbar(sibling_url, sibling_label))
    html_content = header_template
    main_table_template = load_template("main_table.html")
    sorted_ocp_keys = sort_ocp_minor_versions(ocp_data.keys())
    html_content += build_toc(sorted_ocp_keys)

    # Check if any renderable result has driver_branch data
    show_driver_branch = any(
        r.get(DRIVER_BRANCH)
        for version_data in ocp_data.values()
        for r in version_data.get("release_tests", [])
        if has_valid_semantic_versions(r, operator_key=GPU_OPERATOR_VERSION)
        and r.get("test_status") != STATUS_ABORTED
    )

    for ocp_key in sorted_ocp_keys:
        notes = ocp_data[ocp_key].get("notes", [])
        bundle_results = ocp_data[ocp_key].get("bundle_tests", [])
        release_results = ocp_data[ocp_key].get("release_tests", [])

        # Apply additional filtering for release results (defensive programming)
        # Note: release_tests should already be pre-filtered, but we keep this for safety
        regular_results = []
        for r in release_results:
            # Only include entries with valid semantic versions
            # Ignore ABORTED results for regular (non-bundle) results
            if has_valid_semantic_versions(r, operator_key=GPU_OPERATOR_VERSION) and r.get("test_status") != STATUS_ABORTED:
                regular_results.append(r)
        notes_html = build_notes(notes)
        table_rows_html = build_catalog_table_rows(regular_results, show_driver_branch)
        bundle_info_html = build_bundle_info(bundle_results)
        table_block = main_table_template
        table_block = table_block.replace("{ocp_key}", ocp_key)
        table_block = table_block.replace("{table_rows}", table_rows_html)
        table_block = table_block.replace("{bundle_info}", bundle_info_html)
        table_block = table_block.replace("{notes}", notes_html)
        extra_headers = "<th>Driver branch</th>" if show_driver_branch else ""
        table_block = table_block.replace("{extra_headers}", extra_headers)
        html_content += table_block

    footer_template = load_template("footer.html")
    now_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    footer_template = footer_template.replace("{LAST_UPDATED}", now_str)
    html_content += footer_template
    return html_content


def build_catalog_table_rows(regular_results: List[Dict[str, Any]], show_driver_branch: bool = False) -> str:
    """
    Build the <tr> rows for the table, grouped by the full OCP version.

    Note: regular_results should already be pre-processed (one result per version combination),
    but we still need to group by exact OCP version for display purposes and handle cases
    where multiple OCP patch versions exist for the same GPU version.

    For each OCP version group, determine the final status for each GPU version combination:
    - If there are any successful results for a combination, mark as successful
    - If there are only failed results for a combination, mark as failed
    Display failed combinations with different styling.
    """
    # Group results by full OCP version
    grouped: Dict[str, List[Dict[str, Any]]] = {}
    for result in regular_results:
        ocp_full = result[OCP_FULL_VERSION]
        grouped.setdefault(ocp_full, []).append(result)

    rows_html = ""
    # Sort OCP versions semantically (so 4.9.10 > 4.9.9)
    for ocp_full in sorted(
            grouped.keys(),
            key=lambda v: semver.VersionInfo.parse(v),
            reverse=True
    ):
        rows = grouped[ocp_full]

        # Group by GPU version to analyze all results for each combination
        gpu_groups: Dict[str, List[Dict[str, Any]]] = {}
        for row in rows:
            gpu = row[GPU_OPERATOR_VERSION]
            gpu_groups.setdefault(gpu, []).append(row)

        # Determine final status for each GPU version and keep latest result
        final_results: Dict[str, Dict[str, Any]] = {}
        for gpu, gpu_results in gpu_groups.items():
            # Check if there are any successful results for this GPU version
            has_success = any(r["test_status"] == "SUCCESS" for r in gpu_results)

            # Get the latest result for this GPU version
            latest_result = max(gpu_results, key=lambda r: int(r["job_timestamp"]))

            # If we have any successful result, use a successful one (latest successful)
            # Otherwise, use the latest result (which will be failed)
            if has_success:
                successful_results = [r for r in gpu_results if r["test_status"] == "SUCCESS"]
                chosen = max(successful_results, key=lambda r: int(r["job_timestamp"]))
                final_result = {**chosen, "final_status": "SUCCESS"}
            else:
                final_result = {**latest_result, "final_status": "FAILURE"}

            final_results[gpu] = final_result

        # Sort GPU Operator versions semantically
        sorted_results = sorted(
            final_results.values(),
            key=lambda r: semver.VersionInfo.parse(
                r[GPU_OPERATOR_VERSION].split("(")[0]),
            reverse=True
        )

        # Build clickable links for GPU versions with appropriate styling
        gpu_links = []
        for r in sorted_results:
            if r["final_status"] == "SUCCESS":
                link = f'<a href="{r["prow_job_url"]}" target="_blank" class="success-link">{r[GPU_OPERATOR_VERSION]}</a>'
            else:
                link = f'<a href="{r["prow_job_url"]}" target="_blank" class="failed-link">{r[GPU_OPERATOR_VERSION]} (Failed)</a>'
            gpu_links.append(link)

        gpu_links_html = ", ".join(gpu_links)

        driver_branch_cell = ""
        if show_driver_branch:
            branch_failed: dict[str, bool] = {}
            for r in rows:
                for b in r.get(DRIVER_BRANCH, "").split(", "):
                    b = b.strip()
                    if not b:
                        continue
                    failed = b.endswith(":failed")
                    name = b.removesuffix(":failed")
                    branch_failed[name] = branch_failed.get(name, False) or failed

            rendered = []
            for name in sorted(branch_failed):
                escaped = html.escape(name)
                if branch_failed[name]:
                    rendered.append(f'<span class="failed-branch">{escaped}</span>')
                else:
                    rendered.append(escaped)
            driver_branch_cell = f'\n          <td>{", ".join(rendered)}</td>'

        rows_html += f"""
        <tr>
          <td class="version-cell">{ocp_full}</td>
          <td>{gpu_links_html}</td>{driver_branch_cell}
        </tr>
        """

    return rows_html


def build_bundle_info(bundle_results: List[Dict[str, Any]]) -> str:
    """
    Build a small HTML snippet that displays info about GPU bundle statuses
    (shown in a 'history-bar' with colored squares).
    """
    if not bundle_results:
        return ""
    sorted_bundles = sorted(
        bundle_results, key=lambda r: int(r["job_timestamp"]), reverse=True)
    leftmost_bundle = sorted_bundles[0]
    last_bundle_date = datetime.fromtimestamp(int(
        leftmost_bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    bundle_html = f"""
  <div class="section-label">
    <strong>From main branch (OLM bundle)</strong>
  </div>
  <div class="history-bar-inner history-bar-outer">
    <div style="margin-top: 5px;">
      <strong>Last Bundle Job Date:</strong> {last_bundle_date}
    </div>
    """
    for bundle in sorted_bundles:
        status = bundle.get("test_status", "Unknown").upper()
        if status == "SUCCESS":
            status_class = "history-success"
        elif status == "FAILURE":
            status_class = "history-failure"
        else:
            status_class = "history-aborted"
        bundle_timestamp = datetime.fromtimestamp(
            int(bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        prow_url = html.escape(bundle["prow_job_url"], quote=True)
        bundle_html += f"""
    <div class='history-square {status_class}'
         data-url='{prow_url}'
         onclick='window.open(this.dataset.url, "_blank")'>
         <span class="history-square-tooltip">
          Status: {status} | Timestamp: {bundle_timestamp}
         </span>
    </div>
        """
    bundle_html += "</div>"
    return bundle_html


def main():
    parser = argparse.ArgumentParser(description="Test Matrix Utility")
    parser.add_argument("--dashboard_html_filepath", required=True,
                        help="Path to to html file for the dashboard")
    parser.add_argument("--dashboard_data_filepath", required=True,
                        help="Path to the file containing the versions for the dashboard")
    parser.add_argument("--title", default=DEFAULT_TITLE,
                        help="Dashboard title (default: %(default)s)")
    parser.add_argument("--sibling-url", default="",
                        help="URL of the sibling dashboard (e.g. precompiled/signed or standard)")
    parser.add_argument("--sibling-label", default="",
                        help="Label for the sibling dashboard link (e.g. 'Precompiled/Signed' or 'Standard')")
    args = parser.parse_args()
    with open(args.dashboard_data_filepath, "r") as f:
        ocp_data = json.load(f)
    logger.info(
        f"Loaded JSON data with keys: {list(ocp_data.keys())} from {args.dashboard_data_filepath}")

    html_content = generate_test_matrix(ocp_data, title=args.title,
                                        sibling_url=args.sibling_url,
                                        sibling_label=args.sibling_label)

    with open(args.dashboard_html_filepath, "w", encoding="utf-8") as f:
        f.write(html_content)
        logger.info(
            f"Matrix dashboard generated: {args.dashboard_html_filepath}")


if __name__ == "__main__":
    main()
