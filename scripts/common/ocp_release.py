"""Generic OCP release, DTK and container-registry helpers shared across discovery scripts."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

version_re = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9._-]*)?$")
digest_re = re.compile(r"^sha256:[a-f0-9]{64}$")
installer_suffix_re = re.compile(r"-(?:x86_64|aarch64|amd64|arm64|ppc64le|s390x|multi)$")
architecture_re = re.compile(r"^(?:amd64|arm64|ppc64le|s390x)$")


def strip_installer_suffix(version):
    """Remove trailing architecture suffix from an OCP installer version string."""
    return installer_suffix_re.sub("", version)


def error_message(error):
    """Redact authorization tokens and query parameters from an error message."""
    message = re.sub(r"(?i)\b(?:Basic|Bearer)\s+\S+", "[redacted authorization]", str(error))
    message = re.sub(r"(https?://)[^/\s@]+@", r"\1[redacted]@", message)
    return re.sub(r"(https?://[^\s?]+)\?\S+", r"\1?[redacted]", message)


def command(arguments, cwd=None, env=None):
    """Run a subprocess, returning stdout on success or raising ValueError on failure."""
    result = subprocess.run(arguments, cwd=cwd, env=env, capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise ValueError(f"{' '.join(arguments[:3])} exited {result.returncode}: {error_message(result.stderr)}")
    return result.stdout


def digest(value):
    """Validate and return a sha256 digest string."""
    if not isinstance(value, str) or not digest_re.fullmatch(value):
        raise ValueError(f"invalid image digest: {value!r}")
    return value


def image_info(image, architecture, auth_file):
    """Inspect a container image and return its manifest and index digests."""
    info = json.loads(command(['oc', 'image', 'info', image, '-o', 'json',
                               '--filter-by-os=linux/' + architecture, '--registry-config=' + str(auth_file)]))
    if info['config']['architecture'] != architecture or info['config']['os'] != 'linux':
        raise ValueError(f"registry platform disagrees with requested architecture: {image}")
    return dict(manifest_digest=digest(info['digest']),
                index_digest=digest(info['listDigest']) if info.get('listDigest') else None,
                created_at=info['config']['created'])


def config_digest(image, auth_file):
    """Return the config layer digest of a container image via skopeo."""
    manifest = json.loads(command(['skopeo', 'inspect', '--raw', '--authfile', str(auth_file), 'docker://' + image]))
    return digest(manifest['config']['digest'])


def release_info(image, architecture, auth_file):
    """Extract version, payload digest, DTK image and OS images from an OCP release."""
    info = json.loads(command(['oc', 'adm', 'release', 'info', image, '-o', 'json',
                              '--filter-by-os=linux/' + architecture, '--registry-config=' + str(auth_file)]))
    if info['config']['architecture'] != architecture or info['config']['os'] != 'linux':
        raise ValueError(f'release platform disagrees with requested architecture: {image}')
    manifest = digest(info['digest'])
    repository = re.sub(r'(?::[^/:@]+|@sha256:[a-f0-9]{64})$', '', image)
    toolkit = next((tag['from']['name'] for tag in info['references']['spec']['tags'] if tag['name'] == 'driver-toolkit'), None)
    if not toolkit:
        raise ValueError(f'release has no default driver-toolkit: {image}')
    digest(toolkit.rsplit('@', 1)[-1])
    return dict(version=info['metadata']['version'], payload=repository + '@' + manifest, toolkit=toolkit,
                manifest_digest=manifest, index_digest=digest(info['listDigest']) if info.get('listDigest') else None,
                os_images={tag['name']: tag['from']['name'] for tag in info['references']['spec']['tags']
                           if re.fullmatch(r'rhel-coreos(?:-[0-9]+)?', tag['name'])})


def rpm_info(image, component, architecture, auth_file):
    """Extract RHEL version and kernel package list from an OCP OS image RPM database."""
    with tempfile.TemporaryDirectory(prefix='ocp-rpmdb-') as directory:
        packages = json.loads(command(['oc', 'adm', 'release', 'info', image, '--rpmdb', '-o', 'json',
                                       '--rpmdb-image=' + component, '--rpmdb-cache=' + directory,
                                       '--filter-by-os=linux/' + architecture, '--registry-config=' + str(auth_file)]))
    rhel = re.fullmatch(r'([0-9]+\.[0-9]+)-[A-Za-z0-9._+]+', packages['redhat-release'])
    suffix = {'amd64': 'x86_64', 'arm64': 'aarch64'}.get(architecture, architecture)
    kernels = {packages[name] + '.' + suffix + flavor for name, flavor in
               [('kernel-core', ''), ('kernel-64k-core', '_64k')] if name in packages}
    if not rhel or not kernels or any(not re.fullmatch(
            r'[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z0-9._+]+\.(?:x86_64|aarch64(?:_64k)?|ppc64le|s390x)', kernel)
            for kernel in kernels):
        raise ValueError(f'incomplete OS RPM metadata: {image} ({component})')
    return dict(rhel_version=rhel[1], kernels=kernels)


def toolkit_info(image, architecture, auth_file):
    """Extract kernel and RHEL version from the driver-toolkit release metadata."""
    with tempfile.TemporaryDirectory(prefix='ocp-regctl-') as directory:
        root = Path(directory)
        (root / 'config.json').symlink_to(Path(auth_file).resolve())
        (root / 'regctl.json').write_text('{}')
        return json.loads(command(['regctl', 'image', 'get-file', '--platform', 'linux/' + architecture,
                                   image, '/etc/driver-toolkit-release.json'],
                                  env={**os.environ, 'DOCKER_CONFIG': directory, 'REGCTL_CONFIG': str(root / 'regctl.json')}))


def dtk_labels(image, architecture, auth_file):
    """Read DTK kernel and RHEL version from image labels via skopeo inspect.

    Returns a dict with at least KERNEL_VERSION and optionally RHEL_VERSION,
    matching the keys that toolkit_info returns from the in-image JSON file.
    Does not require regctl.
    """
    info = json.loads(command(['skopeo', 'inspect', '--authfile', str(auth_file),
                               'docker://' + image]))
    labels = info.get('Labels') or {}
    kernel = labels.get('KERNEL_VERSION') or labels.get('kernel-version', '')
    rhel = labels.get('RHEL_VERSION') or labels.get('rhel-version', '')
    if not kernel:
        raise ValueError(f'DTK image has no kernel-version label: {image}')
    return dict(KERNEL_VERSION=kernel, RHEL_VERSION=rhel)


def release_stream_urls(architecture, ocp_version=None):
    """Return the OCP release stream API URLs for a given architecture.

    When *ocp_version* is supplied (e.g. '4.22' or '5.0'), the major part
    determines the stream name ('4-stable' or '5-stable').  Without it the
    function defaults to the '4-stable' stream for backward compatibility.
    """
    major = ocp_version.split('.')[0] if ocp_version else '4'
    stream = f'{major}-stable' if architecture == 'amd64' else f'{major}-stable-{architecture}'
    return [f'https://{architecture}.ocp.releases.ci.openshift.org/api/v1/releasestream/{stream}/tags',
            f'https://multi.ocp.releases.ci.openshift.org/api/v1/releasestream/{major}-stable-multi/tags']
