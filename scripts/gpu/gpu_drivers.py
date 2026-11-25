"""GPU precompiled driver registry helpers and GitHub issue alerting.

Provides functions for querying precompiled GPU driver image tags from
registry.redhat.io, filtering by kernel version and architecture, and
managing GitHub issues for missing driver availability.
"""

import json
import re
import sys
from urllib.parse import urlencode
from urllib.request import Request, urlopen

from ocp_release import command

PRODUCTION_REGISTRY = 'registry.redhat.io'
GPU_DRIVER_NAMESPACE = 'nvidia'

# Regex to extract the RHEL major version from a kernel string.
# Mirrors the Go pattern in pkg/nvidiagpu/precompiled.go (elMajorRegexp).
EL_MAJOR_RE = re.compile(r'\.el(\d+)')

# Tag format: <driver-version>-<kernel-version>-<rhel>-<arch>
# Example: 550.90.07-5.14.0-427.13.1.el9_4-rhel9.4-amd64
GPU_TAG_RE = re.compile(
    r'^(?P<driver_version>[0-9][A-Za-z0-9._-]*)-'
    r'(?P<kernel>[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z0-9._+-]*\.el[0-9]+_[0-9]+\.'
    r'(?:x86_64|aarch64|ppc64le|s390x))-'
    r'(?P<rhel>rhel[0-9]+\.[0-9]+)-'
    r'(?P<arch>amd64|arm64|ppc64le|s390x)$'
)


def gpu_driver_repo_for_rhel(rhel_major):
    """Return the GPU driver repository name for a RHEL major version.

    Mirrors PrecompiledDriverImageForKernel in pkg/nvidiagpu/precompiled.go.
    """
    return f'{GPU_DRIVER_NAMESPACE}/gpu-driver-rhel{rhel_major}'


def rhel_major_from_kernel(kernel_version):
    """Extract the RHEL major version number from a kernel version string.

    Uses the .el<N> pattern (e.g. '.el9_4.' → '9', '.el10_0.' → '10').
    Returns None if the pattern is not found.
    """
    match = EL_MAJOR_RE.search(kernel_version)
    return match.group(1) if match else None


def rhel_major_from_dtk(dtk_info):
    """Extract the RHEL major version from DTK metadata.

    RHEL_VERSION is e.g. '9.4' or '10.2'; returns the part before the dot.
    Returns None if the field is missing or empty.
    """
    rhel_version = str(dtk_info.get('RHEL_VERSION', ''))
    if rhel_version and '.' in rhel_version:
        return rhel_version.split('.')[0]
    return None


def list_gpu_driver_tags(repository, auth_file):
    """List tags from a production GPU driver repository using skopeo."""
    output = command(['skopeo', 'list-tags',
                      'docker://' + PRODUCTION_REGISTRY + '/' + repository,
                      '--authfile', str(auth_file)])
    data = json.loads(output)
    return data.get('Tags', [])


def filter_matching_tags(tags, kernel_version, architecture):
    """Return driver versions whose tags match the given kernel and architecture."""
    matches = []
    seen = set()
    for tag in tags:
        match = GPU_TAG_RE.fullmatch(tag)
        if not match:
            continue
        if match['kernel'] != kernel_version:
            continue
        if match['arch'] != architecture:
            continue
        if tag.endswith('-source'):
            continue
        driver = match['driver_version']
        if driver not in seen:
            seen.add(driver)
            matches.append(dict(driver_version=driver, tag=tag,
                                kernel=match['kernel'], rhel=match['rhel'],
                                architecture=match['arch']))
    return matches


def deduplicate_by_major(versions):
    """Keep the longest (most specific) version per major prefix, sorted ascending."""
    best = {}
    for entry in versions:
        version = entry['driver_version']
        major = version.split('.')[0] if '.' in version else version
        existing = best.get(major)
        if existing is None or len(version) > len(existing['driver_version']):
            best[major] = entry
    return sorted(best.values(), key=lambda e: e['driver_version'])


# ---------------------------------------------------------------------------
# GitHub issue alerting — optional, skipped when no token is available.
# ---------------------------------------------------------------------------

ALERT_LABEL = 'missing-precompiled-driver'


def _gh_api(method, url, token, body=None):
    """Make a GitHub API request; return parsed JSON or None on error."""
    data = json.dumps(body).encode() if body else None
    request = Request(url, data=data, method=method, headers={
        'Authorization': f'token {token}',
        'Accept': 'application/vnd.github+json',
        'Content-Type': 'application/json',
    })
    try:
        with urlopen(request, timeout=30) as response:
            return json.load(response)
    except (OSError, ValueError) as error:
        print(f'GPU driver alert: GitHub API {method} {url} failed: {error}', file=sys.stderr)
        return None


def _issue_title(ocp_version, architecture):
    return f'Missing precompiled GPU driver: OCP {ocp_version} ({architecture})'


def _find_open_issue(token, github_repo, ocp_version, architecture):
    """Search for an open issue with the alert label matching this OCP version."""
    title = _issue_title(ocp_version, architecture)
    query = f'repo:{github_repo} is:issue is:open label:{ALERT_LABEL} "{title}" in:title'
    url = 'https://api.github.com/search/issues?' + urlencode({'q': query, 'per_page': '5'})
    result = _gh_api('GET', url, token)
    if result and result.get('items'):
        return result['items'][0]
    return None


def alert_missing_driver(token, github_repo, ocp_version, architecture, kernel_version, gpu_repo, timestamp):
    """Create or update a GitHub issue for a missing precompiled driver."""
    if not token:
        return
    existing = _find_open_issue(token, github_repo, ocp_version, architecture)
    if existing:
        comment = (f'### Pre-flight check update\n\n'
                   f'- **Timestamp:** {timestamp}\n'
                   f'- **Kernel:** `{kernel_version}`\n'
                   f'- **Registry:** `{PRODUCTION_REGISTRY}/{gpu_repo}`\n'
                   f'- **Status:** Still no matching driver tags found.\n')
        _gh_api('POST', existing['comments_url'], token, {'body': comment})
        print(f'GPU driver alert: updated existing GitHub issue #{existing["number"]}')
    else:
        title = _issue_title(ocp_version, architecture)
        body = (f'The GPU precompiled driver pre-flight check found no matching driver images.\n\n'
                f'- **OCP version:** {ocp_version}\n'
                f'- **Architecture:** {architecture}\n'
                f'- **Kernel:** `{kernel_version}`\n'
                f'- **Registry:** `{PRODUCTION_REGISTRY}/{gpu_repo}`\n'
                f'- **Checked at:** {timestamp}\n\n'
                f'This issue will be automatically closed when matching driver tags become available.\n')
        issue = _gh_api('POST', f'https://api.github.com/repos/{github_repo}/issues', token,
                         {'title': title, 'body': body, 'labels': [ALERT_LABEL]})
        if issue:
            print(f'GPU driver alert: created GitHub issue #{issue["number"]}')


def alert_driver_found(token, github_repo, ocp_version, architecture, driver_versions, timestamp):
    """Close any open missing-driver issue now that tags are available."""
    if not token:
        return
    existing = _find_open_issue(token, github_repo, ocp_version, architecture)
    if existing:
        comment = (f'### Resolved\n\n'
                   f'Pre-flight check found matching driver versions: {", ".join(driver_versions)}\n'
                   f'- **Timestamp:** {timestamp}\n')
        _gh_api('POST', existing['comments_url'], token, {'body': comment})
        _gh_api('PATCH', existing['url'], token, {'state': 'closed'})
        print(f'GPU driver alert: closed GitHub issue #{existing["number"]}')
