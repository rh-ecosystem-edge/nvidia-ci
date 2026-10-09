"""Tagged driver metadata and the CLI adapters shared by discovery and preflight."""

import json
import re
import subprocess

version_re = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9._-]*)?$")
image_re = re.compile(
    r"^registry\.stage\.redhat\.io/nvidia/"
    r"(?P<image>doca-driver-rhel9|doca-driver-rhel10):(?P<tag>[A-Za-z0-9][A-Za-z0-9._-]*)$"
)
tag_re = re.compile(
    r"^(?P<version>[A-Za-z0-9][A-Za-z0-9._-]*)-"
    r"(?P<kernel>[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z0-9._+-]*\.el[0-9]+_[0-9]+\."
    r"(?:x86_64|aarch64(?:_64k)?|ppc64le|s390x))-rhcos(?P<rhcos>[0-9]+\.[0-9]+)-"
    r"(?P<architecture>amd64|arm64|ppc64le|s390x)$"
)
pair_id_re = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
architecture_re = re.compile(r"^(?:amd64|arm64|ppc64le|s390x)$")
digest_re = re.compile(r"^sha256:[a-f0-9]{64}$")


def error_message(error):
    message = re.sub(r"(?i)\b(?:Basic|Bearer)\s+\S+", "[redacted authorization]", str(error))
    message = re.sub(r"(https?://)[^/\s@]+@", r"\1[redacted]@", message)
    return re.sub(r"(https?://[^\s?]+)\?\S+", r"\1?[redacted]", message)


def command(arguments, cwd=None):
    result = subprocess.run(arguments, cwd=cwd, capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise ValueError(f"{' '.join(arguments[:3])} exited {result.returncode}: {error_message(result.stderr)}")
    return result.stdout


def metadata(image):
    match = image_re.fullmatch(image)
    tag = tag_re.fullmatch(match['tag']) if match else None
    if not tag:
        raise ValueError(f"unsupported staging driver tag: {image}")
    return dict(image=image, repository=image.rsplit(':', 1)[0], kernel=tag['kernel'],
                rhcos=tag['rhcos'], architecture=tag['architecture'], rhel_major=match['image'].removeprefix('doca-driver-rhel'))


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


def identity(image):
    parsed = metadata(image['image'])
    repository = image['repository']
    if repository != parsed['repository'] or image['architecture'] != parsed['architecture']:
        raise ValueError('image identity disagrees with its pullspec')
    return repository, image['architecture'], digest(image['manifest_digest'])


def runtime_digests(image):
    return {digest(image[field]) for field in ('manifest_digest', 'config_digest', 'index_digest') if image.get(field)}
