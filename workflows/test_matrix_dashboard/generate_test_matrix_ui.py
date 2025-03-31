import argparse
import json
import os
from datetime import datetime, timezone
from typing import Any, Dict, List

from logger import logger

def load_template(filename: str) -> str:
    """
    Load and return the contents of a template file.
    This version uses an absolute path based on the script's location,
    so it doesn't depend on the current working directory.
    """
    script_dir = os.path.dirname(os.path.abspath(__file__))
    file_path = os.path.join(script_dir, "templates", filename)
    with open(file_path, 'r', encoding='utf-8') as f:
        return f.read()

def parse_gpu_version(version_str: str) -> tuple:
    """
    Convert a GPU version string into a tuple for numeric comparison.
    If the version is "master" (case-insensitive), it's treated as the highest version.
    For other version strings, split by '.' and convert to integers.
    If conversion fails, returns (0,).
    """
    if version_str.lower() == "master":
        return (float('inf'),)
    try:
        parts = version_str.split('.')
        return tuple(int(x) for x in parts)
    except ValueError:
        return (0,)

def build_table_rows(regular_results: List[Dict[str, Any]]) -> str:
    """
    Build the <tr> rows for the table, grouped by the full OCP version.
    Each OCP version row includes clickable GPU version links.
    """
    grouped = {}
    # Group results by the full OCP version
    for result in regular_results:
        ocp_full = result["ocp"]
        grouped.setdefault(ocp_full, []).append(result)

    rows_html = ""
    # Sort OCP versions descending
    for ocp_full in sorted(grouped.keys(), reverse=True):
        rows = grouped[ocp_full]
        # Sort GPU versions from largest to smallest
        sorted_rows = sorted(rows, key=lambda r: parse_gpu_version(r["gpu"]), reverse=True)
        gpu_links = ", ".join(
            f'<a href="{r["link"]}" target="_blank">{r["gpu"]}</a>'
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
    Build the small HTML snippet that displays info about GPU bundle statuses.
    (Shown in a 'history-bar' with colored squares.)
    """
    if not bundle_results:
        return ""  # If no bundle results, return empty string

    # De-emphasize GPU bundle info
    first_timestamp = bundle_results[0]["timestamp"]
    last_bundle_date = datetime.fromtimestamp(first_timestamp, timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")

    bundle_html = """
  <div style="margin-top: 20px; font-size: 0.9em; color: #888; background-color: #f7f7f7; padding: 10px; border-radius: 4px;">
    <strong>From main branch GPU bundle (OLM bundle)</strong>
  </div>
  <div class="history-bar" style="opacity: 0.7;">
    <div style="margin-top: 5px;">
      <strong>Last Bundle Job Date:</strong> {last_bundle_date}
    </div>
    """.format(last_bundle_date=last_bundle_date)

    for bundle in bundle_results:
        status = bundle.get("status", "Unknown").upper()
        if status == "SUCCESS":
            status_class = "history-success"
        elif status == "FAILURE":
            status_class = "history-failure"
        else:
            status_class = "history-aborted"
        bundle_timestamp = datetime.fromtimestamp(bundle["timestamp"], timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")

        bundle_html += f"""
    <div class='history-square {status_class}'
         onclick='window.open("{bundle["link"]}", "_blank")'
         title='Status: {status} | Timestamp: {bundle_timestamp}'>
    </div>
        """
    bundle_html += "</div>"  # end .history-bar
    return bundle_html

def generate_test_matrix(ocp_data: Dict[str, List[Dict[str, Any]]]) -> str:
    """
    Build up the final HTML by:
      1. Reading header.html
      2. Looping over ocp_data to build many "main_table" blocks
      3. Reading footer.html and injecting the last-updated time
    """
    # 1. Load the header template (now via absolute path)
    header_template = load_template("header.html")

    # We'll accumulate everything in a string
    html_content = header_template

    # 2. Build the dynamic blocks for each OCP version
    main_table_template = load_template("main_table.html")
    sorted_ocp_keys = sorted(ocp_data.keys(), reverse=True)

    for ocp_key in sorted_ocp_keys:
        results = ocp_data[ocp_key]

        # Filter out "regular" vs "bundle" results
        regular_results = [
            r for r in results
            if ("bundle" not in r["gpu"].lower())
               and ("master" not in r["gpu"].lower())
               and (r.get("status") == "SUCCESS")
        ]
        bundle_results = [r for r in results if r not in regular_results]

        table_rows_html = build_table_rows(regular_results)
        bundle_info_html = build_bundle_info(bundle_results)

        # Replace placeholders in main_table.html
        table_block = main_table_template
        table_block = table_block.replace("{ocp_key}", ocp_key)
        table_block = table_block.replace("{table_rows}", table_rows_html)
        table_block = table_block.replace("{bundle_info}", bundle_info_html)

        html_content += table_block

    # 3. Load and add footer (with last-updated time)
    footer_template = load_template("footer.html")
    now_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    footer_template = footer_template.replace("{LAST_UPDATED}", now_str)

    html_content += footer_template

    return html_content

def main() -> None:
    parser = argparse.ArgumentParser(description="Generate test matrix HTML UI")
    parser.add_argument("--output_dir", required=True,
                        help="Output directory where JSON is stored and HTML will be generated")
    parser.add_argument("--data_file", required=True,
                        help="Name of the JSON file")
    parser.add_argument("--output_file", required=True,
                        help="Name of the generated HTML file")
    args = parser.parse_args()

    json_path = os.path.join(args.output_dir, args.data_file)
    with open(json_path, "r") as f:
        ocp_data = json.load(f)

    logger.info(f"Loaded JSON data with keys: {list(ocp_data.keys())} from {json_path}")

    # Generate the complete HTML content
    html_content = generate_test_matrix(ocp_data)

    # Write to output file
    output_path = os.path.join(args.output_dir, args.output_file)
    with open(output_path, "w", encoding="utf-8") as f:
        f.write(html_content)

    logger.info(f"Matrix report generated: {output_path}")

if __name__ == "__main__":
    main()
