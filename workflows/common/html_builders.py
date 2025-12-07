"""
Shared HTML building utilities for CI dashboards.
"""

import html
from typing import List, Dict, Any
from datetime import datetime, timezone


def build_toc(ocp_keys: List[str]) -> str:
    """
    Build a Table of Contents (TOC) for OpenShift versions.
    
    Args:
        ocp_keys: List of OCP version strings to include in TOC
        
    Returns:
        HTML string containing the TOC
    """
    toc_links = ", ".join(
        f'<a href="#ocp-{ocp_version}">{ocp_version}</a>' for ocp_version in ocp_keys
    )
    return f"""
<div class="toc">
    <div class="ocp-version-header">OpenShift Versions</div>
    {toc_links}
</div>
    """


def build_notes(notes: List[str]) -> str:
    """
    Build an HTML snippet with manual notes for an OCP version.
    
    Args:
        notes: List of note strings to display
        
    Returns:
        HTML string containing the notes section, or empty string if no notes
    """
    if not notes:
        return ""

    # Escape HTML in notes for safety
    escaped_notes = [html.escape(note) for note in notes]
    items = "\n".join(f'<li class="note-item">{n}</li>' for n in escaped_notes)
    return f"""
  <div class="section-label">Notes</div>
  <div class="note-items">
    <ul>
      {items}
    </ul>
  </div>
    """


def build_history_bar(
    results: List[Dict[str, Any]],
    title: str,
    success_key: str = "test_status",
    success_value: str = "SUCCESS"
) -> str:
    """
    Build a history bar showing test result squares (success/failure/aborted).
    
    Args:
        results: List of test result dictionaries
        title: Title to display above the history bar
        success_key: Key in result dict to check for status
        success_value: Value that indicates success
        
    Returns:
        HTML string containing the history bar
    """
    if not results:
        return ""
    
    # Sort by timestamp, most recent first
    sorted_results = sorted(
        results, key=lambda r: int(r.get("job_timestamp", 0)), reverse=True
    )
    
    leftmost_result = sorted_results[0]
    last_date = datetime.fromtimestamp(
        int(leftmost_result.get("job_timestamp", 0)), timezone.utc
    ).strftime("%Y-%m-%d %H:%M:%S UTC")
    
    history_html = f"""
  <div class="section-label">
    <strong>{html.escape(title)}</strong>
  </div>
  <div class="history-bar-inner history-bar-outer">
    <div style="margin-top: 5px;">
      <strong>Last Job Date:</strong> {last_date}
    </div>
    """
    
    for result in sorted_results:
        status = result.get(success_key, "Unknown").upper()
        if status == success_value:
            status_class = "history-success"
        elif status == "FAILURE":
            status_class = "history-failure"
        else:
            status_class = "history-aborted"
            
        timestamp_str = datetime.fromtimestamp(
            int(result.get("job_timestamp", 0)), timezone.utc
        ).strftime("%Y-%m-%d %H:%M:%S UTC")
        
        prow_url = result.get("prow_job_url", "#")
        history_html += f"""
    <div class='history-square {status_class}'
         onclick='window.open("{prow_url}", "_blank")'>
         <span class="history-square-tooltip">
          Status: {status} | Timestamp: {timestamp_str}
         </span>
    </div>
        """
    
    history_html += "</div>"
    return history_html


def build_last_updated_footer(timestamp: str = None) -> str:
    """
    Build a footer showing when the dashboard was last updated.
    
    Args:
        timestamp: Optional timestamp string. If not provided, uses current time.
        
    Returns:
        HTML string containing the footer
    """
    if timestamp is None:
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    
    return f"""
  <div class="last-updated">
    Last updated: {timestamp} UTC
  </div>
</body>
</html>
    """


def sanitize_id(text: str) -> str:
    """
    Sanitize a string to be used as an HTML ID.
    
    Args:
        text: String to sanitize
        
    Returns:
        Sanitized string safe for use as HTML ID
    """
    # Replace spaces and special characters with hyphens
    sanitized = text.lower().replace(" ", "-").replace("_", "-")
    # Remove any characters that aren't alphanumeric or hyphens
    sanitized = "".join(c for c in sanitized if c.isalnum() or c == "-")
    return sanitized

