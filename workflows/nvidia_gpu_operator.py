#!/usr/bin/env python
import os
import re

import requests
import semver

from utils import logger

gpu_operator_nvcr_auth_url = 'https://nvcr.io/proxy_auth?scope=repository:nvidia/gpu-operator:pull'
gpu_operator_nvcr_tags_url = 'https://nvcr.io/v2/nvidia/gpu-operator/tags/list'

gpu_operator_ghcr_auth_url = 'https://ghcr.io/token?scope=repository:nvidia/gpu-operator:pull'
gpu_operator_ghcr_latest_url = 'https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/manifests/main-latest'

version_not_found = '1.0.0'

def get_operator_versions() -> dict:

    logger.info('Calling NVCR authentication API')
    auth_req = requests.get(gpu_operator_nvcr_auth_url, allow_redirects=True, headers={'Content-Type': 'application/json'})
    auth_req.raise_for_status()
    token = auth_req.json()['token']

    logger.info('Listing tags of the operator image')
    req = requests.get(gpu_operator_nvcr_tags_url, headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'})
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
        versions[minor] = semver.max_ver(existing, full_version)

    return versions

def get_sha() -> str:

    token = os.getenv('GH_AUTH_TOKEN') # In a GitHub workflow, set `GH_AUTH_TOKEN=$(echo ${{ secrets.GITHUB_TOKEN }} | base64)`
    if token:
        logger.info('GH_AUTH_TOKEN env variable is available, using it for authentication')
    else:
        logger.info('GH_AUTH_TOKEN is not available, calling authentication API')
        auth_req = requests.get(gpu_operator_ghcr_auth_url, allow_redirects=True, headers={'Content-Type': 'application/json'})
        auth_req.raise_for_status()
        token = auth_req.json()['token']

    req = requests.get(gpu_operator_ghcr_latest_url, headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'})
    req.raise_for_status()
    config = req.json()['config']
    logger.debug(f'Received GPU operator bundle config: {config}')
    return config['digest']
