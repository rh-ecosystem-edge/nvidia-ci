"""Tagged driver metadata, digest helpers and CLI adapters for discovery."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

version_re = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9._-]*)?$")
image_re = re.compile(
    r"^registry\.stage\.redhat\.io/nvidia/"
    r"(?P<image>doca-driver-rhel9|doca-driver-rhel10):(?P<tag>[A-Za-z0-9][A-Za-z0-9._-]*)$"
)
tag_re = re.compile(
    r"^(?P<version>[A-Za-z0-9][A-Za-z0-9._-]*)-"
    r"(?P<kernel>[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z0-9._+-]*\.el[0-9]+_[0-9]+\."
    r"(?:x86_64|aarch64(?:_64k)?|ppc64le|s390x))-"
    r"(?P<os_tag>[A-Za-z0-9][A-Za-z0-9._-]*)-"
    r"(?P<architecture>amd64|arm64|ppc64le|s390x)$"
)
pair_id_re = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
architecture_re = re.compile(r"^(?:amd64|arm64|ppc64le|s390x)$")
digest_re = re.compile(r"^sha256:[a-f0-9]{64}$")
installer_suffix_re = re.compile(r"-(?:x86_64|aarch64|amd64|arm64|ppc64le|s390x|multi)$")


def strip_installer_suffix(version):
    return installer_suffix_re.sub("", version)


def error_message(error):
    message = re.sub(r"(?i)\b(?:Basic|Bearer)\s+\S+", "[redacted authorization]", str(error))
    message = re.sub(r"(https?://)[^/\s@]+@", r"\1[redacted]@", message)
    return re.sub(r"(https?://[^\s?]+)\?\S+", r"\1?[redacted]", message)


def command(arguments, cwd=None, env=None):
    result = subprocess.run(arguments, cwd=cwd, env=env, capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise ValueError(f"{' '.join(arguments[:3])} exited {result.returncode}: {error_message(result.stderr)}")
    return result.stdout


def metadata(image):
    match = image_re.fullmatch(image)
    tag = tag_re.fullmatch(match['tag']) if match else None
    if not tag:
        raise ValueError(f"unsupported staging driver tag: {image}")
    return dict(image=image, repository=image.rsplit(':', 1)[0], kernel=tag['kernel'],
                os_tag=tag['os_tag'], architecture=tag['architecture'], rhel_major=match['image'].removeprefix('doca-driver-rhel'))


def digest(value):
    if not isinstance(value, str) or not digest_re.fullmatch(value):
        raise ValueError(f"invalid image digest: {value!r}")
    return value


def image_info(image, architecture, auth_file):
    info = json.loads(command(['oc', 'image', 'info', image, '-o', 'json',
                               '--filter-by-os=linux/' + architecture, '--registry-config=' + str(auth_file)]))
    if info['config']['architecture'] != architecture or info['config']['os'] != 'linux':
        raise ValueError(f"registry platform disagrees with requested architecture: {image}")
    return dict(manifest_digest=digest(info['digest']), index_digest=digest(info['listDigest']) if info.get('listDigest') else None,
                created_at=info['config']['created'])


def config_digest(image, auth_file):
    manifest = json.loads(command(['skopeo', 'inspect', '--raw', '--authfile', str(auth_file), 'docker://' + image]))
    return digest(manifest['config']['digest'])


def release_info(image, architecture, auth_file):
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
    # 4.19+ OS images carry a small metadata layer with the complete RPM list.
    # Name the component explicitly: the default machine-os can be another RHEL family.
    with tempfile.TemporaryDirectory(prefix='nno-rpmdb-') as directory:
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
    # regctl streams layers newest-first and stops at the file, including squashed images.
    with tempfile.TemporaryDirectory(prefix='nno-regctl-') as directory:
        root = Path(directory)
        (root / 'config.json').symlink_to(Path(auth_file).resolve())
        (root / 'regctl.json').write_text('{}')
        return json.loads(command(['regctl', 'image', 'get-file', '--platform', 'linux/' + architecture,
                                   image, '/etc/driver-toolkit-release.json'],
                                  env={**os.environ, 'DOCKER_CONFIG': directory, 'REGCTL_CONFIG': str(root / 'regctl.json')}))


def identity(image):
    parsed = metadata(image['image'])
    repository = image['repository']
    if repository != parsed['repository'] or image['architecture'] != parsed['architecture']:
        raise ValueError('image identity disagrees with its pullspec')
    return repository, image['architecture'], digest(image['manifest_digest'])


def runtime_digests(image):
    return {digest(image[field]) for field in ('manifest_digest', 'config_digest', 'index_digest') if image.get(field)}
