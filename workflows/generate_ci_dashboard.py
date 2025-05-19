import json
import os
import argparse
import semver

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
    html_content = header_template

    main_table_template = load_template("main_table.html")
    sorted_ocp_keys = sorted(ocp_data.keys(), reverse=True)
    for ocp_key in sorted_ocp_keys:
        results = ocp_data[ocp_key]
        regular_results = [
            r for r in results
            if ("bundle" not in r["gpu_operator_version"].lower())
               and ("master" not in r["gpu_operator_version"].lower())
               and (r.get("test_status") == "SUCCESS")
        ]
        bundle_results = [r for r in results if r not in regular_results]
        table_rows_html = build_catalog_table_rows(regular_results)
        bundle_info_html = build_bundle_info(bundle_results)
        table_block = main_table_template
        table_block = table_block.replace("{ocp_key}", ocp_key)
        table_block = table_block.replace("{table_rows}", table_rows_html)
        table_block = table_block.replace("{bundle_info}", bundle_info_html)
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
          <td style="min-width:150px; white-space:nowrap;">{ocp_full}</td>
          <td>{gpu_links}</td>
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
    sorted_bundles = sorted(bundle_results, key=lambda r: r["job_timestamp"], reverse=True)
    leftmost_bundle = sorted_bundles[0]
    last_bundle_date = datetime.fromtimestamp(int(leftmost_bundle["job_timestamp"]), timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    bundle_html = f"""
  <div style="margin-top: 20px; font-size: 0.9em; color: #009688; padding: 10px; border-radius: 4px;">
    <strong>From main branch (OLM bundle)</strong>
  </div>
  <div class="history-bar" style="opacity: 0.7;">
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
         onclick='window.open("{bundle["prow_job_url"]}", "_blank")'
         title='Status: {status} | Timestamp: {bundle_timestamp}'>
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