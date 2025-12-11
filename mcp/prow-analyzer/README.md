# Prow CI Analyzer - MCP Server

AI-powered analysis of Prow CI job failures for GitHub pull requests using OpenShift CI infrastructure.

## What It Does

This [MCP](https://modelcontextprotocol.io/) server enables AI assistants (like Claude or Cursor) to:
- Check PR CI job statuses and success rates
- Analyze test failures and identify root causes

**For users:** Just ask questions about your PR failures in natural language.
**For developers:** See [DEVELOPMENT.md](./DEVELOPMENT.md) for technical details.

## Quick Setup

### Installation

**Option 1: Using the setup script (recommended for Cursor users)**
```bash
cd mcp/prow-analyzer
./setup_venv.sh
```

**Option 2: Standard Python virtual environment**
```bash
cd mcp/prow-analyzer
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

> **Note:** If using Cursor's integrated terminal, use the setup script to avoid AppImage environment issues.

## Configuration

```json
{
  "mcpServers": {
    "prow-analyzer": {
      "command": "/absolute/path/to/mcp/prow-analyzer/venv/bin/python",
      "args": ["/absolute/path/to/mcp/prow-analyzer/mcp_server.py"],
      "env": {
        "PROW_REPOSITORIES": "rh-ecosystem-edge/nvidia-ci"
      }
    }
  }
}
```

**Important:** Use absolute paths (not `~` or relative paths).

### Configuration Options

Create `config.yaml` in the `mcp/prow-analyzer` directory (optional):

```yaml
gcs_bucket: "test-platform-results"
gcsweb_base_url: "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs"
path_template: "pr-logs/pull/{org}_{repo}/{pr_number}"
repositories:
  - org: rh-ecosystem-edge
    repo: nvidia-ci
  - org: openshift
    repo: release
```

Or use environment variables:
- `PROW_GCS_BUCKET` - GCS bucket name
- `PROW_GCSWEB_BASE_URL` - Base URL for GCSWeb UI
- `PROW_PATH_TEMPLATE` - Path template for job data
- `PROW_REPOSITORIES` - Comma-separated `org/repo` list (e.g., `"rh-ecosystem-edge/nvidia-ci,openshift/release"`)

## Usage Examples

Once configured, just ask your AI assistant questions about your PRs:

### Check PR Status
```text
You: "What's the status of PR 346?"

AI: PR 346 has 20 jobs: 19 passed, 1 failed (95% success rate)
    The failed job is: pull-ci-rh-ecosystem-edge-nvidia-ci-main-4.20-stable-nvidia-gpu-operator-e2e
```

### Investigate Failures
```text
You: "Why did the e2e tests fail in PR 346?"

AI: The GPU operator e2e test failed because the nvidia-driver-daemonset pod
    was not ready. The test "ValidateGPUOperator" timed out waiting for the
    driver pod to become ready.
```

### Root Cause Analysis
```text
You: "What caused the driver pod to fail?"

AI: Looking at the driver pod logs from the must-gather, the nvidia driver
    failed to load due to an incompatible kernel version. The driver expected
    kernel 5.14+ but found 5.13.
```

### Check for Flaky Tests
```text
You: "Has this test been flaky recently?"

AI: This test has been stable. Looking at recent runs, this is the first
    failure in the last 10 builds. The kernel version incompatibility appears
    to be a new issue introduced in this PR.
```
