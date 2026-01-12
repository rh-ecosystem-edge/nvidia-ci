#!/usr/bin/env python3
"""
CI Failure Summarizer - AI-powered TL;DR for Prow job failures.

Uses a local Ollama instance with Llama 3.2 1B to generate concise summaries.
No external API keys required - model runs locally and is cached between runs.
"""

import os
import sys
import re
import urllib.parse
import uuid
from pathlib import Path

import requests

# Add parent directory to path for common utils
sys.path.insert(0, str(Path(__file__).parent.parent))
from common.utils import get_logger

logger = get_logger(__name__)

# Configuration
GCS_BUCKET = "test-platform-results"
DEFAULT_ORG_REPO = "rh-ecosystem-edge_nvidia-ci"
OLLAMA_MODEL = "llama3.2:1b"  # ~1GB, fast CPU inference
OLLAMA_URL = "http://localhost:11434"

SYSTEM_PROMPT = """Summarize this CI failure in 1-2 sentences. State if it's safe to retest or needs a fix. Be concise."""


def fetch_file_from_gcs(bucket: str, path: str) -> str | None:
    """Fetch a file from GCS (replicates prow-analyzer logic)."""
    url = f"https://storage.googleapis.com/storage/v1/b/{bucket}/o/{urllib.parse.quote(path, safe='')}"
    
    try:
        response = requests.get(url, params={"alt": "media"}, timeout=60)
        response.raise_for_status()
        return response.text
    except requests.RequestException:
        logger.exception(f"Failed to fetch {path}")
        return None


def categorize_failure_stage(step_name: str) -> str:
    """
    Categorize which stage of the CI job failed based on step name.
    
    Order matters! Check more specific patterns first since step names often
    contain multiple keywords (e.g., "nvidia-gpu-operator-e2e-master-gather-aws-console"
    contains both "e2e" and "gather").
    """
    if not step_name:
        return "⚙️ CI Execution"
    
    step_lower = step_name.lower()
    
    # Check specific patterns FIRST (order matters!)
    # 1. "src" = Source code build (CI infra)
    if step_lower == 'src' or step_lower.endswith('-src'):
        return "🔨 Source Build"
    
    # 2. "gather" - more specific than "e2e" since gather steps have e2e in prefix
    if 'gather' in step_lower or 'must-gather' in step_lower:
        return "📦 Must-Gather"
    
    # 3. Deprovisioning
    if 'deprovision' in step_lower or 'destroy' in step_lower:
        return "🧹 Cluster Deprovisioning"
    
    # 4. Provisioning (ipi-install)
    if 'ipi-install' in step_lower or 'install-install' in step_lower:
        return "🏗️ Cluster Provisioning"
    
    # 5. GPU operator tests (gpu-operator-e2e, but NOT gather steps)
    if 'gpu-operator-e2e' in step_lower or 'e2e' in step_lower:
        return "🧪 GPU Operator Tests"
    
    return "⚙️ CI Execution"


def extract_key_errors(log: str) -> dict:
    """
    Extract specific error information using regex patterns.
    Returns structured data about the failure including the TRUE ROOT CAUSE.
    
    Order of priority for root cause detection:
    1. Source build timeout (CI infra issue)
    2. Image pull failures (CI infra - architecture mismatch, registry issues)
    3. Pod pending timeout (CI infra - scheduling issues)
    4. Cluster initialization failure (OCP install)
    5. Bootstrap failure (OCP install) 
    6. Test assertion failure ([FAILED] with actual message)
    7. Connection errors (cluster became unreachable)
    8. Resource issues (OOM, quota, capacity)
    """
    result = {
        "failed_step": None,
        "root_cause": None,  # The actual error message
        "root_cause_category": None,  # Human-readable category
        "phase": None,
        "stage": None,
    }
    
    # Find phase from CI metadata
    phase_match = re.search(r'"phase":\s*"([^"]+)"', log)
    if phase_match:
        result["phase"] = phase_match.group(1)
    
    # Find the FIRST failed step - in Prow logs, this is the root cause
    step_match = re.search(r'Step\s+(\S+)\s+failed', log, re.IGNORECASE)
    if step_match:
        step_name = step_match.group(1)
        step_name = re.sub(r'[\s\.\,]+$', '', step_name)
        result["failed_step"] = step_name
        result["stage"] = categorize_failure_stage(step_name)
    else:
        result["stage"] = "⚙️ CI Execution"
    
    # === ROOT CAUSE DETECTION (ordered by priority) ===
    
    # 1. Source build timeout/failure (CI infra issue)
    build_timeout = re.search(r"build didn't start running within[^\n]+", log)
    if build_timeout:
        result["root_cause"] = build_timeout.group(0)[:200]
        result["root_cause_category"] = "Source Build Timeout"
        result["stage"] = "🔨 Source Build"
        return result
    
    # 2. Image pull failures (CI infra - architecture mismatch, registry issues)
    # Check for architecture mismatch first (most specific)
    arch_mismatch = re.search(r'no image found in manifest list for architecture[^\n]+', log, re.IGNORECASE)
    if arch_mismatch:
        result["root_cause"] = arch_mismatch.group(0)[:200]
        result["root_cause_category"] = "Image Architecture Mismatch"
        result["stage"] = "⚙️ CI Execution"
        return result
    
    # Check for ImagePullBackOff or ErrImagePull
    img_pull = re.search(r'(ImagePullBackOff|ErrImagePull)[^\n]*', log, re.IGNORECASE)
    if img_pull:
        # Try to get more context about WHY the pull failed
        detailed = re.search(r'Failed to pull image[^\n]+', log, re.IGNORECASE)
        if detailed:
            result["root_cause"] = detailed.group(0)[:200]
        else:
            result["root_cause"] = img_pull.group(0)[:200]
        result["root_cause_category"] = "Image Pull Failed"
        result["stage"] = "⚙️ CI Execution"
        return result
    
    # 3. Pod pending timeout (CI infra - scheduling/resource issues)
    pod_pending = re.search(r'pod pending for more than[^\n]+', log, re.IGNORECASE)
    if pod_pending:
        # Get the reason from container status (reason can have hyphens like "crashloop-backoff")
        container_reason = re.search(r'Container \w+ is not ready with reason ([\w-]+)', log)
        if container_reason:
            result["root_cause"] = f"Pod stuck pending: {container_reason.group(1)}"
        else:
            result["root_cause"] = pod_pending.group(0)[:200]
        result["root_cause_category"] = "Pod Pending Timeout"
        result["stage"] = "⚙️ CI Execution"
        return result
    
    # 4. Cluster initialization failure (during ipi-install)
    cluster_init = re.search(r'level=error msg=failed to initialize the cluster:[^\n]+', log)
    if cluster_init:
        result["root_cause"] = cluster_init.group(0)[:200]
        result["root_cause_category"] = "Cluster Init Failed"
        result["stage"] = "🏗️ Cluster Provisioning"
        return result
    
    # 5. Bootstrap failure (level=fatal with bootstrap)
    bootstrap_fail = re.search(r'level=fatal msg=[^\n]*bootstrap[^\n]*', log, re.IGNORECASE)
    if bootstrap_fail:
        result["root_cause"] = bootstrap_fail.group(0)[:200]
        result["root_cause_category"] = "Bootstrap Failed"
        result["stage"] = "🏗️ Cluster Provisioning"
        return result
    
    # 6. Test assertion failure - [FAILED] with actual error message
    # Skip metadata lines like "[FAILED] in [It]" or "[FAILED] seconds]"
    for match in re.finditer(r'\[FAILED\]([^\n]*)', log):
        clean_msg = re.sub(r'\x1b\[[0-9;]*m', '', match.group(1)).strip()
        # Skip metadata lines, look for actual error messages
        if clean_msg and 'in [It]' not in clean_msg and 'seconds]' not in clean_msg:
            result["root_cause"] = f"[FAILED] {clean_msg[:180]}"
            result["root_cause_category"] = "Test Assertion Failed"
            result["stage"] = "🧪 GPU Operator Tests"
            return result
    
    # 7. Connection error (cluster died)
    conn_error = re.search(r'Unable to connect to the server:[^\n]+', log)
    if conn_error:
        result["root_cause"] = conn_error.group(0)[:200]
        result["root_cause_category"] = "Connection Error"
        # Keep existing stage from step name (could be gather, test, etc)
        return result
    
    # 8. Resource issues (OOM, quota, capacity)
    oom = re.search(r'OOMKilled', log, re.IGNORECASE)
    if oom:
        result["root_cause"] = "Container killed due to Out of Memory (OOMKilled)"
        result["root_cause_category"] = "Out of Memory"
        return result
    
    quota = re.search(r'(Quota exceeded|exceeded quota)[^\n]*', log, re.IGNORECASE)
    if quota:
        result["root_cause"] = quota.group(0)[:200]
        result["root_cause_category"] = "Quota Exceeded"
        result["stage"] = "⚙️ CI Execution"
        return result
    
    capacity = re.search(r'InsufficientInstanceCapacity[^\n]*', log, re.IGNORECASE)
    if capacity:
        result["root_cause"] = capacity.group(0)[:200]
        result["root_cause_category"] = "Insufficient AWS Capacity"
        result["stage"] = "🏗️ Cluster Provisioning"
        return result
    
    # 9. Level=fatal for any other fatal error
    fatal = re.search(r'level=fatal msg=[^\n]+', log)
    if fatal:
        result["root_cause"] = fatal.group(0)[:200]
        result["root_cause_category"] = "Fatal Error"
        return result
    
    # 10. etcd errors
    etcd_error = re.search(r'etcdserver:[^\n]+', log)
    if etcd_error:
        result["root_cause"] = etcd_error.group(0)[:200]
        result["root_cause_category"] = "etcd Error"
        return result
    
    # Fallback - couldn't determine root cause
    result["root_cause"] = "Could not determine specific error"
    result["root_cause_category"] = "Unknown"
    
    return result


def ai_fallback_detect_error(log: str) -> dict | None:
    """
    AI fallback for when deterministic patterns fail to detect the error.
    Uses a specialized prompt to extract error information from the log.
    
    Returns dict with 'root_cause' and 'root_cause_category' or None if AI unavailable.
    """
    # Valid categories the AI can return
    VALID_CATEGORIES = {
        "Image Pull Failed", "Out of Memory", "Quota Exceeded", "Connection Error",
        "Resource Not Found", "Permission Denied", "Timeout", "Test Failed",
        "Infrastructure Error", "Unknown"
    }
    
    # Check if Ollama is available
    try:
        response = requests.get(f"{OLLAMA_URL}/api/tags", timeout=5)
        if response.status_code != 200:
            logger.warning("Ollama not available for AI fallback")
            return None
    except requests.RequestException:
        logger.warning("Ollama not available for AI fallback")
        return None
    
    # Extract error-focused lines from log
    lines = log.split('\n')
    error_lines = []
    
    # Find error-related lines
    error_keywords = ['error', 'failed', 'fatal', 'unable', 'timeout', 'refused', 
                      'backoff', 'crash', 'panic', 'oom', 'quota', 'exceeded']
    
    for i, line in enumerate(lines):
        if any(kw in line.lower() for kw in error_keywords):
            # Add context
            start = max(0, i - 1)
            end = min(len(lines), i + 2)
            for j in range(start, end):
                clean = re.sub(r'\x1b\[[0-9;]*m', '', lines[j])[:200]
                if clean.strip() and clean not in error_lines:
                    error_lines.append(clean)
    
    # Add last 30 lines for final status
    error_lines.append("\n--- FINAL OUTPUT ---")
    for line in lines[-30:]:
        clean = re.sub(r'\x1b\[[0-9;]*m', '', line)[:200]
        if clean.strip():
            error_lines.append(clean)
    
    log_excerpt = '\n'.join(error_lines[-50:])[:3000]
    
    detect_prompt = """You are analyzing a CI build log to find the root cause of failure.
Look for the MOST SPECIFIC error - not just "failed" but WHY it failed.

Common error patterns to look for:
- ImagePullBackOff / ErrImagePull (image problems)
- OOMKilled (memory issues)
- Quota exceeded
- Connection refused/timeout
- Architecture mismatch
- Pod stuck pending
- Resource not found
- Permission denied

Respond with ONLY these 2 lines (no other text):
CATEGORY: <one of: Image Pull Failed, Out of Memory, Quota Exceeded, Connection Error, Resource Not Found, Permission Denied, Timeout, Test Failed, Infrastructure Error, Unknown>
ERROR: <the specific error message from the log, max 150 chars>"""
    
    payload = {
        "model": OLLAMA_MODEL,
        "messages": [
            {"role": "system", "content": detect_prompt},
            {"role": "user", "content": f"Find the root cause error in this log:\n\n{log_excerpt}"}
        ],
        "stream": False,
        "options": {
            "temperature": 0.1,  # Very low for factual extraction
            "num_predict": 100,  # Short response
        }
    }
    
    try:
        response = requests.post(
            f"{OLLAMA_URL}/api/chat",
            json=payload,
            timeout=120  # Shorter timeout for simple extraction
        )
        response.raise_for_status()
        result = response.json()
        
        if "message" not in result or "content" not in result["message"]:
            return None
        
        content = result["message"]["content"].strip()
        logger.debug(f"AI fallback response: {content[:200]}")
        
        # Parse the response
        category_match = re.search(r'CATEGORY:\s*(.+)', content)
        error_match = re.search(r'ERROR:\s*(.+)', content)
        
        if category_match and error_match:
            category = category_match.group(1).strip()
            # Validate category - if AI returned invalid, use "Infrastructure Error"
            if category not in VALID_CATEGORIES:
                logger.debug(f"AI returned invalid category '{category}', using 'Infrastructure Error'")
                category = "Infrastructure Error"
            return {
                "root_cause_category": f"[AI] {category}",
                "root_cause": error_match.group(1).strip()[:200]
            }
        
    except (requests.RequestException, ValueError, KeyError) as e:
        logger.warning(f"AI fallback detection failed: {e}")
    
    return None


def extract_relevant_log(log: str, key_errors: dict, max_chars: int = 2000) -> str:
    """
    Build a focused summary of the log with extracted errors.
    Optimized for fast CPU inference - smaller context = faster response.
    """
    # Build a focused log section
    sections = []
    
    # Add extracted error info at the top (most important for AI)
    sections.append(f"STAGE: {key_errors.get('stage', 'Unknown')}")
    if key_errors.get("failed_step"):
        sections.append(f"FAILED STEP: {key_errors['failed_step']}")
    if key_errors.get("root_cause"):
        sections.append(f"ROOT CAUSE: {key_errors['root_cause']}")
    if key_errors.get("root_cause_category"):
        sections.append(f"CATEGORY: {key_errors['root_cause_category']}")
    
    sections.append("\n--- KEY LOG LINES ---\n")
    
    # Add relevant log portions
    lines = log.split('\n')
    
    # Find lines with actual errors (not just "error" in path names)
    error_keywords = ['refused', 'timeout', 'failed to', 'unable to', 'fatal', 'panic', 'oomkilled', '[failed]']
    important_lines = []
    seen_ranges = set()
    
    for i, line in enumerate(lines):
        # Skip file path lines
        if '->' in line and '/' in line:
            continue
        line_lower = line.lower()
        if any(kw in line_lower for kw in error_keywords):
            # Add context (avoid duplicates)
            start = max(0, i - 1)
            end = min(len(lines), i + 2)
            range_key = (start, end)
            if range_key not in seen_ranges:
                seen_ranges.add(range_key)
                for j in range(start, end):
                    clean = re.sub(r'\x1b\[[0-9;]*m', '', lines[j])  # Strip ANSI
                    important_lines.append(clean[:150])
                important_lines.append("---")
    
    # Add last 10 lines for final status
    important_lines.append("\n--- FINAL STATUS ---")
    for line in lines[-10:]:
        clean = re.sub(r'\x1b\[[0-9;]*m', '', line)
        important_lines.append(clean[:150])
    
    sections.extend(important_lines[:20])  # Limit lines for faster inference
    
    result = '\n'.join(sections)
    
    if len(result) > max_chars:
        result = result[:max_chars]
    
    logger.info(f"Extracted {len(result)} chars: step={key_errors.get('failed_step')}, root_cause={key_errors.get('root_cause_category')}")
    return result


def summarize_with_ollama(job_name: str, build_log: str, key_errors: dict) -> str:
    """Generate a TL;DR summary using local Ollama instance."""
    
    relevant_log = extract_relevant_log(build_log, key_errors)
    
    logger.info(f"Sending {len(relevant_log):,} chars to Ollama ({OLLAMA_MODEL}) for summarization")
    
    payload = {
        "model": OLLAMA_MODEL,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": f"Job: {job_name}\n\nLog:\n{relevant_log}\n\nBriefly explain why this failed and if it's safe to retest:"}
        ],
        "stream": False,
        "options": {
            "temperature": 0.3,  # Balanced temperature
            "num_predict": 75,   # Very short for fast CPU inference
        }
    }
    
    try:
        response = requests.post(
            f"{OLLAMA_URL}/api/chat",
            json=payload,
            timeout=180  # 3 min timeout - 1B model is fast on CPU
        )
        response.raise_for_status()
    except requests.RequestException:
        logger.exception("Ollama API request failed")
        raise
    
    result = response.json()
    logger.debug(f"Ollama response keys: {list(result.keys())}")
    
    if "message" not in result or "content" not in result["message"]:
        # Don't log full response - may contain sensitive log content from prompt
        logger.error(f"Unexpected Ollama response structure. Keys: {list(result.keys())}")
        raise ValueError("Unexpected Ollama response format: missing 'message.content'")
    
    return result["message"]["content"].strip()


def build_log_path(org_repo: str, pr_number: str, job_name: str, build_id: str) -> str:
    """Construct the GCS path for a build log."""
    return f"pr-logs/pull/{org_repo}/{pr_number}/{job_name}/{build_id}/build-log.txt"


def build_prow_url(org_repo: str, pr_number: str, job_name: str, build_id: str) -> str:
    """Construct the Prow UI URL for a build."""
    return (
        f"https://prow.ci.openshift.org/view/gs/{GCS_BUCKET}/"
        f"pr-logs/pull/{org_repo}/{pr_number}/{job_name}/{build_id}"
    )


def format_comment(job_name: str, build_id: str, summary: str, prow_url: str, 
                   key_errors: dict) -> str:
    """Format the PR comment with prominent error display."""
    stage = key_errors.get("stage", "Unknown")
    failed_step = key_errors.get("failed_step")
    root_cause = key_errors.get("root_cause")
    root_cause_category = key_errors.get("root_cause_category")
    
    # Sanitize values that will be in backticks (prevent markdown injection)
    safe_job_name = job_name.replace("`", "'") if job_name else "Unknown"
    safe_failed_step = failed_step.replace("`", "'") if failed_step else None
    
    # Build info lines
    info_lines = [f"**Stage:** {stage}"]
    if safe_failed_step:
        info_lines.append(f"**Failed Step:** `{safe_failed_step}`")
    if root_cause_category:
        info_lines.append(f"**Failure Type:** {root_cause_category}")
    
    info_section = "\n".join(info_lines)
    
    # Format root cause prominently (escape backticks to avoid markdown injection)
    root_cause_section = ""
    if root_cause and root_cause != "Could not determine specific error":
        # Replace triple backticks with escaped version to prevent breaking markdown
        safe_root_cause = root_cause.replace("```", "``\u200b`")
        root_cause_section = f"\n\n**Error:**\n```\n{safe_root_cause}\n```"
    
    return f"""## 🔴 CI Failure: `{safe_job_name}`

{info_section}{root_cause_section}

**Analysis:** {summary}

---
<sub>🤖 [View full logs]({prow_url}) | Build: `{build_id}`</sub>
"""


def parse_prow_url(url: str) -> dict | None:
    """
    Parse a Prow URL to extract job info.
    
    Expected formats:
    - https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/org_repo/PR/job-name/build-id
    - gs://test-platform-results/pr-logs/pull/org_repo/PR/job-name/build-id
    """
    # Match: /pr-logs/pull/org_repo/PR/job-name/build-id
    match = re.search(r'pr-logs/pull/([^/]+)/(\d+)/([^/]+)/(\d+)', url)
    if match:
        return {
            "org_repo": match.group(1),
            "pr_number": match.group(2),
            "job_name": match.group(3),
            "build_id": match.group(4),
        }
    return None


def set_github_output(name: str, value: str):
    """Set a GitHub Actions output variable (handles multiline)."""
    github_output = os.environ.get("GITHUB_OUTPUT")
    if github_output:
        # Use unique delimiter to prevent content injection
        delimiter = f"ghadelimiter_{uuid.uuid4().hex}"
        with open(github_output, "a") as f:
            f.write(f"{name}<<{delimiter}\n{value}\n{delimiter}\n")
    else:
        # Fallback for local testing (just log it)
        preview = value[:100] + "..." if len(value) > 100 else value
        print(f"[LOCAL OUTPUT] {name}={preview}")


def main():
    """Main entry point."""
    
    # Get job info from environment (set by GitHub Action)
    pr_number = os.environ.get("PR_NUMBER")
    job_name = os.environ.get("JOB_NAME")
    build_id = os.environ.get("BUILD_ID")
    org_repo = os.environ.get("ORG_REPO", DEFAULT_ORG_REPO)
    
    # Alternatively, parse from a Prow URL
    prow_url_input = os.environ.get("PROW_URL")
    if prow_url_input and not all([pr_number, job_name, build_id]):
        parsed = parse_prow_url(prow_url_input)
        if parsed:
            pr_number = parsed["pr_number"]
            job_name = parsed["job_name"]
            build_id = parsed["build_id"]
            org_repo = parsed["org_repo"]
    
    if not all([pr_number, job_name, build_id]):
        logger.error("Missing required parameters. Need PR_NUMBER, JOB_NAME, BUILD_ID or PROW_URL")
        sys.exit(1)
    
    logger.info(f"Analyzing failure for PR #{pr_number}, job: {job_name}, build: {build_id}")
    
    # Fetch the build log
    log_path = build_log_path(org_repo, pr_number, job_name, build_id)
    logger.info(f"Fetching log from gs://{GCS_BUCKET}/{log_path}")
    
    build_log = fetch_file_from_gcs(GCS_BUCKET, log_path)
    
    if not build_log:
        error_msg = f"Could not fetch build log from {log_path}"
        logger.error(error_msg)
        set_github_output("error", error_msg)
        sys.exit(1)
    
    logger.info(f"Fetched {len(build_log):,} bytes of log content")
    
    # Extract failure info (deterministic - not AI)
    key_errors = extract_key_errors(build_log)
    logger.info(f"Detected: step={key_errors.get('failed_step')}, "
                f"root_cause={key_errors.get('root_cause_category')}")
    
    # Use AI fallback if deterministic detection failed
    if key_errors.get("root_cause_category") == "Unknown":
        logger.info("Deterministic detection returned Unknown, trying AI fallback...")
        ai_result = ai_fallback_detect_error(build_log)
        if ai_result:
            key_errors["root_cause"] = ai_result["root_cause"]
            key_errors["root_cause_category"] = ai_result["root_cause_category"]
            logger.info(f"AI fallback found: {ai_result['root_cause_category']}")
    
    # Generate AI summary for error explanation
    try:
        summary = summarize_with_ollama(job_name, build_log, key_errors)
        logger.info(f"Generated summary: {summary[:100]}...")
    except Exception as e:
        error_msg = f"Failed to generate summary: {type(e).__name__}: {e}"
        logger.exception(error_msg)
        set_github_output("error", error_msg)
        sys.exit(1)
    
    # Format the comment with root cause + AI summary
    prow_url = build_prow_url(org_repo, pr_number, job_name, build_id)
    comment = format_comment(job_name, build_id, summary, prow_url, key_errors)
    
    # Output for GitHub Actions
    set_github_output("summary", comment)
    set_github_output("pr_number", pr_number)
    
    # Also print for local testing
    print("\n" + "=" * 60)
    print(comment)
    print("=" * 60 + "\n")


if __name__ == "__main__":
    main()
