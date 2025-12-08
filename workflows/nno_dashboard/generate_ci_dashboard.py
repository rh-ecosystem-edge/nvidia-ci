#!/usr/bin/env python3
"""
Generate Network Operator HTML dashboard from JSON data.
Matrix-style layout organized by Network Operator version.
"""

import argparse
import json
import semver
from typing import Dict, List, Any, Set
from datetime import datetime, timezone
from collections import defaultdict

from workflows.common import (
    logger,
    load_template,
    OCP_FULL_VERSION,
    OPERATOR_VERSION,
    is_valid_ocp_version,
    sanitize_id,
)


# Map test flavors from job names to display columns
FLAVOR_COLUMN_MAP = {
    "DOCA4 - RDMA Legacy SR-IOV": "Legacy SR-IOV Ethernet",
    "Bare Metal - RDMA Legacy SR-IOV": "Legacy SR-IOV Ethernet",
    "Hosted - RDMA Legacy SR-IOV": "Legacy SR-IOV Ethernet",
    
    "DOCA4 - RDMA Legacy SR-IOV with GPU": "Legacy SR-IOV Ethernet + GPU Direct",
    "Bare Metal - RDMA Legacy SR-IOV with GPU": "Legacy SR-IOV Ethernet + GPU Direct",
    
    "DOCA4 - RDMA": "Shared InfiniBand",
    "Bare Metal - RDMA": "Shared InfiniBand",
    "Hosted - RDMA": "Shared InfiniBand",
    
    "DOCA4 - RDMA with GPU": "Shared InfiniBand + GPU Direct",
    "Bare Metal - RDMA with GPU": "Shared InfiniBand + GPU Direct",
    
    "DOCA4 - RDMA Shared Device": "Shared Ethernet",
    "Bare Metal - RDMA Shared Device": "Shared Ethernet",
    "Hosted - RDMA Shared Device": "Shared Ethernet",
    
    "DOCA4 - RDMA Shared Device with GPU": "Shared Ethernet + GPU Direct",
    "Bare Metal - RDMA Shared Device with GPU": "Shared Ethernet + GPU Direct",
    
    "DOCA4 - E2E": "Legacy SR-IOV Ethernet",
    "Bare Metal - E2E": "Legacy SR-IOV Ethernet",
    "Hosted - E2E": "Legacy SR-IOV Ethernet",
}

# Column order for the matrix table
COLUMN_ORDER = [
    "GPU Operator",
    "Shared Ethernet",
    "Shared Ethernet + GPU Direct",
    "Shared InfiniBand",
    "Shared InfiniBand + GPU Direct",
    "Legacy SR-IOV Ethernet",
    "Legacy SR-IOV Ethernet + GPU Direct",
]


def restructure_data_by_nno_version(ocp_data: Dict[str, Dict[str, Any]]) -> Dict[str, Dict[str, Any]]:
    """
    Restructure data from OCP-centric to NNO-centric.
    
    Input structure (OCP-centric):
    {
      "4.17.16": {
        "test_flavors": {
          "DOCA4 - RDMA Legacy SR-IOV": {
            "results": [{operator_version: "25.4.0", ...}]
          }
        }
      }
    }
    
    Output structure (NNO-centric):
    {
      "25.4.0": {
        "ocp_versions": {
          "4.17": {
            "gpu_operators": {
              "25.10.0": {
                "Legacy SR-IOV Ethernet": {
                  "status": "SUCCESS",
                  "url": "...",
                  "hardware": "ConnectX-5 Ex"
                }
              }
            }
          }
        }
      }
    }
    """
    nno_data = defaultdict(lambda: {"ocp_versions": defaultdict(lambda: {"gpu_operators": defaultdict(dict)})})
    
    for ocp_full, ocp_info in ocp_data.items():
        if not is_valid_ocp_version(ocp_full):
            continue
        
        # Get major.minor OCP version (e.g., "4.17.16" -> "4.17")
        ocp_parts = ocp_full.split('.')
        ocp_major_minor = f"{ocp_parts[0]}.{ocp_parts[1]}"
        
        test_flavors = ocp_info.get("test_flavors", {})
        
        for flavor_name, flavor_data in test_flavors.items():
            # Map flavor to column
            column = FLAVOR_COLUMN_MAP.get(flavor_name, "Other")
            
            for result in flavor_data.get("results", []):
                nno_version = result.get("operator_version") or result.get("gpu_operator_version", "Unknown")
                gpu_operator_version = "master-latest"  # TODO: Extract from test data if available
                
                status = result.get("test_status", "UNKNOWN")
                url = result.get("prow_job_url", "#")
                
                # Store result
                if column not in nno_data[nno_version]["ocp_versions"][ocp_major_minor]["gpu_operators"][gpu_operator_version]:
                    nno_data[nno_version]["ocp_versions"][ocp_major_minor]["gpu_operators"][gpu_operator_version][column] = {
                        "status": status,
                        "url": url,
                        "hardware": "ConnectX-6 Dx"  # TODO: Extract from test data if available
                    }
    
    return dict(nno_data)


def build_matrix_table(ocp_version: str, gpu_operators_data: Dict[str, Dict[str, Any]]) -> str:
    """
    Build a matrix table for a specific OCP version.
    Only includes columns that have actual data (removes empty columns).
    
    Args:
        ocp_version: OCP version (e.g., "4.20")
        gpu_operators_data: Dictionary of GPU operator versions and their test results
        
    Returns:
        HTML table string
    """
    if not gpu_operators_data:
        return ""
    
    # Sort GPU operator versions
    sorted_gpu_ops = sorted(gpu_operators_data.keys())
    
    # Determine which columns have data (not all empty)
    columns_with_data = set()
    for gpu_op_version in sorted_gpu_ops:
        test_results = gpu_operators_data[gpu_op_version]
        for column in COLUMN_ORDER[1:]:  # Skip "GPU Operator" column
            if column in test_results:
                columns_with_data.add(column)
    
    # Build list of columns to display (in order)
    active_columns = ["GPU Operator"]  # Always include first column
    for column in COLUMN_ORDER[1:]:
        if column in columns_with_data:
            active_columns.append(column)
    
    # If no data columns, don't build table
    if len(active_columns) == 1:
        return ""
    
    # Build table header
    table_html = f"""
    <div class="section-label">OpenShift {ocp_version} Compatibility</div>
    <table id="table-nno-{sanitize_id(ocp_version)}">
      <thead>
        <tr>
"""
    
    for column in active_columns:
        table_html += f"          <th>{column}</th>\n"
    
    table_html += """        </tr>
      </thead>
      <tbody>
"""
    
    # Build table rows (one per GPU operator version)
    for gpu_op_version in sorted_gpu_ops:
        test_results = gpu_operators_data[gpu_op_version]
        
        table_html += f"""        <tr>
          <td class="version-cell">{gpu_op_version}</td>
"""
        
        # Add cells for each active test flavor column
        for column in active_columns[1:]:  # Skip "GPU Operator" column
            if column in test_results:
                result = test_results[column]
                status = result.get("status", "UNKNOWN")
                url = result.get("url", "#")
                hardware = result.get("hardware", "")
                
                if status == "SUCCESS":
                    icon = "✓"
                    css_class = "success-link"
                elif status == "FAILURE":
                    icon = "✗"
                    css_class = "failed-link"
                else:
                    icon = "⚬"
                    css_class = "pending"
                
                if status in ["SUCCESS", "FAILURE"]:
                    table_html += f'          <td><a href="{url}" target="_blank" class="{css_class}">{icon}</a>'
                else:
                    table_html += f'          <td><span class="{css_class}">{icon}</span>'
                
                if hardware:
                    table_html += f'<br><small>{hardware}</small>'
                table_html += '</td>\n'
            else:
                # This shouldn't happen since we filtered columns, but keep as fallback
                table_html += '          <td class="empty-cell"></td>\n'
        
        table_html += "        </tr>\n"
    
    table_html += """      </tbody>
    </table>
"""
    
    return table_html


def generate_test_matrix(ocp_data: Dict[str, Dict[str, Any]], templates_dir: str) -> str:
    """
    Generate the complete HTML dashboard.
    
    Args:
        ocp_data: Dictionary of OCP versions and their test data (OCP-centric format)
        templates_dir: Path to templates directory
        
    Returns:
        Complete HTML string
    """
    # Load header template
    header_template = load_template("header.html", templates_dir)
    html_content = header_template
    
    # Restructure data to be NNO-centric
    nno_data = restructure_data_by_nno_version(ocp_data)
    
    if not nno_data:
        html_content += "<p>No valid test data found.</p>"
        html_content += """
  <div class="last-updated">
    Last updated: """ + datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S") + """ UTC
  </div>
</body>
</html>"""
        return html_content
    
    # Sort NNO versions
    sorted_nno_versions = sorted(nno_data.keys(), reverse=True)
    
    # Build TOC
    toc_links = []
    for nno_version in sorted_nno_versions:
        version_id = sanitize_id(nno_version)
        toc_links.append(f'<a href="#nno-{version_id}">{nno_version}</a>')
    
    html_content += f"""
<div class="toc">
    <div class="ocp-version-header">Network Operator Versions</div>
    {", ".join(toc_links)}
</div>
"""
    
    # Build sections for each NNO version
    for nno_version in sorted_nno_versions:
        nno_info = nno_data[nno_version]
        version_id = sanitize_id(nno_version)
        
        html_content += f"""      <a id="nno-{version_id}"></a>
  <div class="ocp-version-container">
    <div class="ocp-version-header">
      Network Operator {nno_version}
    </div>
"""
        
        # Sort OCP versions
        ocp_versions = nno_info.get("ocp_versions", {})
        sorted_ocp_versions = sorted(ocp_versions.keys(), reverse=True)
        
        # Build matrix table for each OCP version
        for ocp_version in sorted_ocp_versions:
            gpu_operators_data = ocp_versions[ocp_version].get("gpu_operators", {})
            table_html = build_matrix_table(ocp_version, gpu_operators_data)
            html_content += table_html
        
        html_content += "  </div>\n\n"
    
    # Add footer
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    html_content += f"""  <div class="last-updated">
    Last updated: {timestamp} UTC
  </div>
</body>
</html>
"""
    
    return html_content


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Generate Network Operator CI Dashboard HTML")
    parser.add_argument("--dashboard_data_filepath", required=True, help="Path to JSON data file")
    parser.add_argument("--dashboard_html_filepath", required=True, help="Path to output HTML file")
    
    args = parser.parse_args()
    
    # Load JSON data
    logger.info(f"Loading data from {args.dashboard_data_filepath}")
    with open(args.dashboard_data_filepath, 'r') as f:
        ocp_data = json.load(f)
    
    # Get templates directory
    import os
    script_dir = os.path.dirname(os.path.abspath(__file__))
    templates_dir = os.path.join(script_dir, "templates")
    
    # Generate HTML
    logger.info("Generating HTML dashboard")
    html_content = generate_test_matrix(ocp_data, templates_dir)
    
    # Save HTML
    logger.info(f"Saving HTML to {args.dashboard_html_filepath}")
    with open(args.dashboard_html_filepath, 'w') as f:
        f.write(html_content)
    
    logger.info("Dashboard generation complete")


if __name__ == "__main__":
    main()
