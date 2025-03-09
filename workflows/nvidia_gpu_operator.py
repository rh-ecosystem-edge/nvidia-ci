#!/usr/bin/env python
import os
import re

import requests

from utils import get_logger, get_latest_versions_as_suffix


def get_operator_versions() -> dict:
    logger = get_logger()
    logger.info('Calling NVCR authentication API')
    auth_req = requests.get('https://nvcr.io/proxy_auth?scope=repository:nvidia/gpu-operator:pull', allow_redirects=True,
                            headers={'Content-Type': 'application/json'})
    auth_req.raise_for_status()
    token = auth_req.json()['token']

    logger.info('Listing tags of the operator image')
    req = requests.get('https://nvcr.io/v2/nvidia/gpu-operator/tags/list', headers={
        'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'})
    req.raise_for_status()

    tags = req.json()['tags']
    prog = re.compile(r'^v(2\d\.\d+)\.\d+$')
    versions = {}
    for t in tags:
        match = prog.match(t)
        if match:
            minor = match.group(1)
            existing = versions.get(minor)
            if not existing or existing < t:
                versions[minor] = t
    return versions

def get_sha() -> str:

    logger = get_logger()
    token = os.getenv('GH_AUTH_TOKEN') # In a GitHub workflow, set `GH_AUTH_TOKEN=$(echo ${{ secrets.GITHUB_TOKEN }} | base64)`
    if token:
        logger.info('GH_AUTH_TOKEN env variable is available, using it for authentication')
    else:
        logger.info('GH_AUTH_TOKEN is not available, calling authentication API')
        auth_req = requests.get('https://ghcr.io/token?scope=repository:nvidia/gpu-operator:pull', allow_redirects=True,
                                headers={'Content-Type': 'application/json'})
        auth_req.raise_for_status()
        token = auth_req.json()['token']

    req = requests.get('https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/manifests/main-latest', headers={
        'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'})
    req.raise_for_status()
    return req.json()['config']['digest']


def latest_gpu_releases(gpu_versions: dict) -> list:
    releases = sorted(gpu_versions.keys(), key=float, reverse=True)[:2]
    releases = get_latest_versions_as_suffix(releases)
    releases.append("master")
    return releases