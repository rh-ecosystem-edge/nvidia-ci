#!/usr/bin/env python
import os
import re
import requests


from workflows.gpu_operator_versions.settings import Settings
from workflows.common.utils import logger
from workflows.gpu_operator_versions.version_utils import max_version

GPU_OPERATOR_NVCR_AUTH_URL = 'https://nvcr.io/proxy_auth?scope=repository:nvidia/gpu-operator:pull'
GPU_OPERATOR_NVCR_TAGS_URL = 'https://nvcr.io/v2/nvidia/gpu-operator/tags/list'

GPU_OPERATOR_GHCR_AUTH_URL = 'https://ghcr.io/token?scope=repository:nvidia/gpu-operator:pull'
GPU_OPERATOR_GHCR_LATEST_URL = 'https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/manifests/main-latest'

version_not_found = '1.0.0'

def get_operator_versions(settings: Settings) -> dict:

    logger.info('Calling NVCR authentication API')
    auth_req = requests.get(GPU_OPERATOR_NVCR_AUTH_URL,
                            allow_redirects=True,
                            headers={'Content-Type': 'application/json'},
                            timeout=settings.request_timeout_sec)
    auth_req.raise_for_status()
    token = auth_req.json()['token']

    logger.info('Listing tags of the GPU operator image')
    req = requests.get(GPU_OPERATOR_NVCR_TAGS_URL,
                       headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'},
                       timeout=settings.request_timeout_sec)
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

def get_sha(settings: Settings) -> str:

    token = os.getenv('GH_AUTH_TOKEN') # In a GitHub workflow, set `GH_AUTH_TOKEN=$(echo ${{ secrets.GITHUB_TOKEN }} | base64)`
    if token:
        logger.info('GH_AUTH_TOKEN env variable is available, using it to authenticate against GitHub')
    else:
        logger.info('GH_AUTH_TOKEN is not available, calling GitHub authentication API')
        auth_req = requests.get(GPU_OPERATOR_GHCR_AUTH_URL,
                                allow_redirects=True,
                                headers={'Content-Type': 'application/json'},
                                timeout=settings.request_timeout_sec)
        auth_req.raise_for_status()
        token = auth_req.json()['token']

    logger.info('Getting digest of the GPU operator OLM bundle')
    req = requests.get(GPU_OPERATOR_GHCR_LATEST_URL,
                       headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'},
                       timeout=settings.request_timeout_sec)
    req.raise_for_status()
    config = req.json()['config']
    logger.debug(f'Received GPU operator bundle config: {config}')
    return config['digest']
