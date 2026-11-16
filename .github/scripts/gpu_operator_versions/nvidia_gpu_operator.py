#!/usr/bin/env python
import re
import requests


from common.utils import logger
from gpu_operator_versions.version_utils import max_version

GPU_OPERATOR_NVCR_AUTH_URL = 'https://nvcr.io/proxy_auth?scope=repository:nvidia/gpu-operator:pull'
GPU_OPERATOR_NVCR_TAGS_URL = 'https://nvcr.io/v2/nvidia/gpu-operator/tags/list'

GPU_OPERATOR_GHCR_AUTH_URL = 'https://ghcr.io/token?scope=repository:nvidia/gpu-operator/gpu-operator-bundle:pull'
GPU_OPERATOR_GHCR_LATEST_URL = 'https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/manifests/main-latest'

version_not_found = '1.0.0'


def get_operator_versions(request_timeout_sec: int) -> dict:
    """Fetch the latest GPU Operator patch version for each minor channel."""

    logger.info('Calling NVCR authentication API')
    auth_req = requests.get(GPU_OPERATOR_NVCR_AUTH_URL,
                            allow_redirects=True,
                            headers={'Content-Type': 'application/json'},
                            timeout=request_timeout_sec)
    auth_req.raise_for_status()
    token = auth_req.json()['token']

    logger.info('Listing tags of the GPU operator image')
    req = requests.get(GPU_OPERATOR_NVCR_TAGS_URL,
                       headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'},
                       timeout=request_timeout_sec)
    req.raise_for_status()

    tags = req.json()['tags']
    logger.debug(f'Received GPU operator image tags: {tags}')

    prog = re.compile(r'^v(?P<minor>2\d\.\d+)\.(?P<patch>\d+)$')

    versions = {}
    for t in tags:
        match = prog.match(t)
        if not match:
            continue

        minor = match.group('minor')
        patch = match.group('patch')
        full_version = f'{minor}.{patch}'
        existing = versions.get(minor, version_not_found)
        versions[minor] = max_version(existing, full_version)

    return versions


def get_sha(request_timeout_sec: int) -> str:
    """Fetch the digest of the current GPU Operator main-latest OLM bundle."""

    logger.info('Calling GHCR token endpoint for anonymous access')
    # No Content-Type is needed for a GET request without a body.
    auth_req = requests.get(GPU_OPERATOR_GHCR_AUTH_URL,
                            allow_redirects=True,
                            timeout=request_timeout_sec)
    auth_req.raise_for_status()
    token = auth_req.json()['token']

    logger.info('Getting digest of the GPU operator OLM bundle')
    # NVIDIA now uses an OCI index (multi-platform manifest). Use HEAD because
    # only the Docker-Content-Digest header is needed; for an OCI index, that
    # header contains the digest.
    req = requests.head(GPU_OPERATOR_GHCR_LATEST_URL,
                        headers={
                            'Accept': 'application/vnd.oci.image.index.v1+json',
                            'Authorization': f'Bearer {token}'
                        },
                        timeout=request_timeout_sec)
    req.raise_for_status()

    digest = req.headers.get('Docker-Content-Digest', '')
    if not digest:
        logger.error(f'Docker-Content-Digest header not found in response headers: {req.headers}')
        msg = 'Digest not found in manifest response headers'
        raise ValueError(msg)

    logger.info(f'Successfully retrieved digest: {digest}')
    return digest
