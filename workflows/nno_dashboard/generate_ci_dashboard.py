#!/usr/bin/env python
"""
NVIDIA Network Operator CI Dashboard Generator

This module extends the GPU Operator CI dashboard generator with Network Operator specific imports.
It reuses all the core logic from the GPU operator dashboard and only overrides the import
for the version field names and template loading.
"""
import argparse
import json
import os
from datetime import datetime, timezone
from typing import Any, Dict, List

from workflows.common.utils import logger
from workflows.common.templates import load_template

# Import helper functions from GPU operator dashboard (reuse everything)
from workflows.gpu_operator_dashboard.generate_ci_dashboard import (
    has_valid_semantic_versions,
    build_catalog_table_rows,
    build_notes,
    build_toc,
    build_bundle_info,
)

# Override: Import network operator specific constants
from workflows.nno_dashboard.fetch_ci_data import (
    OCP_FULL_VERSION,
    NETWORK_OPERATOR_VERSION as GPU_OPERATOR_VERSION,  # Alias for compatibility
    STATUS_ABORTED,
)

# Note: We're aliasing NETWORK_OPERATOR_VERSION as GPU_OPERATOR_VERSION so that
# all the imported functions from GPU operator dashboard work without modification.
# The functions just reference the field name, they don't care about the actual operator type.


def is_valid_ocp_version(version_key: str) -> bool:
    """
    Check if a version key is a valid OpenShift version.
    
    Valid: "4.17.16", "4.16", "4.15.0"
    Invalid: "doca4", "bare-metal", "hosted", "Unknown"
    """
    # Filter out known infrastructure types
    invalid_keys = ["doca4", "bare-metal", "hosted", "unknown"]
    if version_key.lower() in invalid_keys:
        return False
    
    # Valid OCP versions start with a digit and contain dots
    if not version_key or not version_key[0].isdigit():
        return False
    
    # Check if it looks like a semantic version (X.Y or X.Y.Z)
    parts = version_key.split('.')
    if len(parts) < 2:
        return False
    
    try:
        # Try to parse first two parts as numbers
        int(parts[0])
        int(parts[1])
        return True
    except (ValueError, IndexError):
        return False


def build_test_flavors_sections(ocp_key: str, test_flavors: Dict[str, Dict[str, Any]], templates_dir: str) -> str:
    """
    Build HTML sections for each test flavor.
    
    Args:
        ocp_key: OpenShift version key
        test_flavors: Dictionary of test flavors with their results
        templates_dir: Path to templates directory
        
    Returns:
        HTML string with all test flavor sections
    """
    if not test_flavors:
        return ""
    
    test_flavor_template = load_template("test_flavor_section.html", templates_dir)
    html_sections = []
    
    # Sort test flavors for consistent display
    sorted_flavors = sorted(test_flavors.keys())
    
    for flavor in sorted_flavors:
        flavor_data = test_flavors[flavor]
        results = flavor_data.get("results", [])
        
        if not results:
            continue
        
        # Build table rows for this flavor
        flavor_rows_html = build_catalog_table_rows(results)
        
        # Create a safe ID from the flavor name
        flavor_id = flavor.lower().replace(" ", "-").replace("/", "-")
        
        # Fill in the template
        flavor_section = test_flavor_template
        flavor_section = flavor_section.replace("{test_flavor}", flavor)
        flavor_section = flavor_section.replace("{ocp_key}", ocp_key)
        flavor_section = flavor_section.replace("{flavor_id}", flavor_id)
        flavor_section = flavor_section.replace("{flavor_table_rows}", flavor_rows_html)
        
        html_sections.append(flavor_section)
    
    return "\n".join(html_sections)


def generate_test_matrix(ocp_data: Dict[str, Dict[str, Any]]) -> str:
    """
    Build the final HTML report by:
      1. Reading the header template,
      2. Generating the table blocks for each OCP version,
      3. Reading the footer template and injecting the last-updated time.
    
    This is a Network Operator specific version that loads templates from the NNO templates directory.
    """
    # Load templates from NNO templates directory
    templates_dir = os.path.join(os.path.dirname(__file__), "templates")
    header_template = load_template("header.html", templates_dir)
    html_content = header_template
    main_table_template = load_template("main_table.html", templates_dir)
    
    # Filter to only valid OCP versions (exclude infrastructure types like "doca4", "bare-metal")
    valid_ocp_keys = [key for key in ocp_data.keys() if is_valid_ocp_version(key)]
    sorted_ocp_keys = sorted(valid_ocp_keys, reverse=True)
    
    logger.info(f"Valid OCP versions found: {sorted_ocp_keys}")
    if len(valid_ocp_keys) != len(ocp_data.keys()):
        filtered_keys = set(ocp_data.keys()) - set(valid_ocp_keys)
        logger.warning(f"Filtered out non-OCP version keys: {filtered_keys}")
    
    html_content += build_toc(sorted_ocp_keys)

    for ocp_key in sorted_ocp_keys:
        notes = ocp_data[ocp_key].get("notes", [])
        bundle_results = ocp_data[ocp_key].get("bundle_tests", [])
        release_results = ocp_data[ocp_key].get("release_tests", [])
        test_flavors = ocp_data[ocp_key].get("test_flavors", {})

        # Apply additional filtering for release results (defensive programming)
        # Note: release_tests should already be pre-filtered, but we keep this for safety
        regular_results = []
        for r in release_results:
            # Only include entries with valid semantic versions
            # Ignore ABORTED results for regular (non-bundle) results
            if has_valid_semantic_versions(r) and r.get("test_status") != STATUS_ABORTED:
                regular_results.append(r)
        
        notes_html = build_notes(notes)
        bundle_info_html = build_bundle_info(bundle_results)
        
        # Build test flavor sections
        test_flavors_html = build_test_flavors_sections(ocp_key, test_flavors, templates_dir)
        
        # If no test flavors, fall back to showing all release results in a single table
        if not test_flavors_html and regular_results:
            fallback_section = load_template("test_flavor_section.html", templates_dir)
            table_rows_html = build_catalog_table_rows(regular_results)
            fallback_section = fallback_section.replace("{test_flavor}", "From operator catalog")
            fallback_section = fallback_section.replace("{ocp_key}", ocp_key)
            fallback_section = fallback_section.replace("{flavor_id}", "regular")
            fallback_section = fallback_section.replace("{flavor_table_rows}", table_rows_html)
            test_flavors_html = fallback_section
        
        table_block = main_table_template
        table_block = table_block.replace("{ocp_key}", ocp_key)
        table_block = table_block.replace("{test_flavors_sections}", test_flavors_html)
        table_block = table_block.replace("{bundle_info}", bundle_info_html)
        table_block = table_block.replace("{notes}", notes_html)
        html_content += table_block

    footer_template = load_template("footer.html", templates_dir)
    now_str = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    footer_template = footer_template.replace("{LAST_UPDATED}", now_str)
    html_content += footer_template
    return html_content


def main():
    """Main entry point for Network Operator dashboard generator."""
    parser = argparse.ArgumentParser(description="Network Operator Test Matrix Dashboard Generator")
    parser.add_argument("--dashboard_html_filepath", required=True,
                        help="Path to html file for the dashboard")
    parser.add_argument("--dashboard_data_filepath", required=True,
                        help="Path to the file containing the versions for the dashboard")
    args = parser.parse_args()
    
    with open(args.dashboard_data_filepath, "r") as f:
        ocp_data = json.load(f)
    logger.info(
        f"Loaded JSON data with keys: {list(ocp_data.keys())} from {args.dashboard_data_filepath}")

    html_content = generate_test_matrix(ocp_data)

    with open(args.dashboard_html_filepath, "w", encoding="utf-8") as f:
        f.write(html_content)
        logger.info(
            f"Network Operator dashboard generated: {args.dashboard_html_filepath}")


if __name__ == "__main__":
    main()

