import json
import os
import argparse
import semver
import re

from typing import Dict, List, Any
from datetime import datetime, timezone
from utils import logger


def load_template(filename: str) -> str:
    """
    Load and return the contents of a template file.
    Uses an absolute path based on the script's location.
    """
    script_dir = os.path.dirname(os.path.abspath(__file__))
    file_path = os.path.join(script_dir, "templates", filename)
    with open(file_path, 'r', encoding='utf-8') as f:
        return f.read()


def generate_test_matrix(ocp_data: Dict[str, List[Dict[str, Any]]]) -> str:
    """
    Build the final HTML report by:
      1. Reading the header template,
      2. Generating the table blocks for each OCP version,
      3. Reading the footer template and injecting the last-updated time.
    """
    header_template = load_template("header.html")
    compatibility_warnings_html = build_compatibility_warnings()
    header_template = header_template.replace("{compatibility_warnings}", compatibility_warnings_html)
    html_content = header_template
    main_table_template = load_template("main_table.html")
    sorted_ocp_keys = sorted(ocp_data.keys(), reverse=True)
    html_content += build_toc(sorted_ocp_keys)
    master_pattern = re.compile(r'^.+\-e2e\-master\/\d+$')
    for ocp_key in sorted_ocp_keys:
        notes = ocp_data[ocp_key].get("notes")
        results = ocp_data[ocp_key]["tests"]
        regular_results = []
        bundle_results = []   
        for r in results:
            if master_pattern.search(r.get("prow_job_url", "")):
                bundle_results.append(r)
            elif (r.get("test_status") == "SUCCESS"):
                regular_results.append(r)
        notes_html = build_notes(notes)
        table_rows_html = build_catalog_table_rows(regular_results)
        bundle_info_html = build_bundle_info(bundle_results)
        table_block = main_table_template
        table_block = table_block.replace("{ocp_key}", ocp_key)
        table_block = table_block.replace("{table_rows}", table_rows_html)
        table_block = table_block.replace("{bundle_info}", bundle_info_html)
        table_block = table_block.replace("{notes}", notes_html)
        html_content += table_block

    footer_template = load_template("footer.html")
    now_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    footer_template = footer_template.replace("{LAST_UPDATED}", now_str)
    html_content += footer_template
    return html_content

def build_catalog_table_rows(regular_results: List[Dict[str, Any]]) -> str:
    """
    Build the <tr> rows for the table, grouped by the full OCP version.
    For each OCP version group, deduplicate by GPU version (keeping only the entry with the latest timestamp)
    and create clickable GPU version links, sorting semantic versions correctly.
    """
    # Group results by full OCP version
    grouped: Dict[str, List[Dict[str, Any]]] = {}
    for result in regular_results:
        ocp_full = result["ocp_full_version"]
        grouped.setdefault(ocp_full, []).append(result)

    rows_html = ""
    # Sort OCP versions semantically (so 4.9.10 > 4.9.9)
    for ocp_full in sorted(
            grouped.keys(),
            key=lambda v: semver.VersionInfo.parse(v),
            reverse=True
    ):
        rows = grouped[ocp_full]

        # Deduplicate by GPU version, keeping latest timestamp
        deduped: Dict[str, Dict[str, Any]] = {}
        for row in rows:
            gpu = row["gpu_operator_version"]
            if gpu not in deduped or row["job_timestamp"] > deduped[gpu]["job_timestamp"]:
                deduped[gpu] = row

        # Sort GPU Operator versions semantically
        deduped_rows = list(deduped.values())
        sorted_rows = sorted(
            deduped_rows,
            key=lambda r: semver.VersionInfo.parse(r["gpu_operator_version"].split("(")[0]),
            reverse=True
        )

        # Build clickable links for GPU versions
        gpu_links = ", ".join(
            f'<a href="{r["prow_job_url"]}" target="_blank">{r["gpu_operator_version"]}</a>'
            for r in sorted_rows
        )

        rows_html += f"""
        <tr>
          <td class="version-cell">{ocp_full}</td>
          <td>{gpu_links}</td>
        </tr>
        """

    return rows_html

def build_notes(notes: List[str]) -> str:
    """
    Build a HTML snipped with manual notes for an OCP version
    """
    if not notes:
        return ""

    items = "\n".join(f'<li class="note-item">{n}</li>' for n in notes)
    return f"""
  <div class="section-label">Notes</div>
  <div class="note-items">
    <ul>
      {items}
    </ul>
  </div>
    """

def load_compatibility_warnings() -> List[Dict[str, Any]]:
    script_dir = os.path.dirname(os.path.abspath(__file__))
    warnings_path = os.path.join(script_dir, "compatibility_warnings.json")
    
    with open(warnings_path, 'r', encoding='utf-8') as f:
        data = json.load(f)
        return [w for w in data.get("warnings", []) if w.get("active", True)]


def build_compatibility_warnings() -> str:
    """
    Build HTML for compatibility warnings and known issues from configuration.
    """
    warnings = load_compatibility_warnings()
    if not warnings:
        return ""
    
    warning_items = ""
    for warning in warnings:
        title = warning.get("title", "")
        message = warning.get("message", "")
        warning_items += f'<li><strong>{title}:</strong> {message}</li>\n'
    
    return f"""
<div class="compatibility-warnings">
    <div class="warning-header">Compatibility Notices</div>
    <ul class="warning-list">
        {warning_items.strip()}
    </ul>
</div>
    """


def build_toc(ocp_keys: List[str]) -> str:
    """
    Build a TOC of OpenShift versions
    """
    toc_links = ", ".join(f'<a href="#ocp-{ocp_version}">{ocp_version}</a>' for ocp_version in ocp_keys)
    return f"""
<div class="toc">
    <div class="ocp-version-header">OpenShift Versions</div>
    {toc_links}
</div>
    """

def build_bundle_info(bundle_results: List[Dict[str, Any]]) -> str:
    """
    Build a small HTML snippet that displays info about GPU bundle statuses
    (shown in a 'history-bar' with colored squares).
    """
    if not bundle_results:
        return ""
    sorted_bundles = sorted(bundle_results, key=lambda r: r["job_timestamp"], reverse=True)
    leftmost_bundle = sorted_bundles[0]
    last_bundle_date = datetime.fromtimestamp(int(leftmost_bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
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
        bundle_timestamp = datetime.fromtimestamp(int(bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        bundle_html += f"""
    <div class='history-square {status_class}'
         onclick='window.open("{bundle["prow_job_url"]}", "_blank")'>
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
    args = parser.parse_args()
    with open(args.dashboard_data_filepath, "r") as f:
        ocp_data = json.load(f)
    logger.info(f"Loaded JSON data with keys: {list(ocp_data.keys())} from {args.dashboard_data_filepath}")

    html_content = generate_test_matrix(ocp_data)

    with open(args.dashboard_html_filepath, "w", encoding="utf-8") as f:
        f.write(html_content)
        logger.info(f"Matrix dashboard generated: {args.dashboard_html_filepath}")


if __name__ == "__main__":
    main()