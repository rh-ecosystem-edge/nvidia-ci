#!/usr/bin/env python

import re
import requests
import semver

from gpu_operator_versions.settings import Settings
from typing import Pattern, AnyStr
from common.utils import logger
from gpu_operator_versions.version_utils import max_version

RELEASE_URL_API = 'https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted'

def fetch_ocp_versions(settings: Settings) -> dict[str, str]:
    """
    Fetches accepted OpenShift versions from the release API.

    The function filters out versions based on the regex pattern defined in settings.ignored_versions.
    For each minor version (e.g., 4.12), only the highest patch version is kept.

    Returns:
        dict: A dictionary mapping minor versions (e.g., '4.12') to their highest patch version (e.g., '4.12.3').
    """

    logger.info(f'Ignored versions: {settings.ignored_versions}')
    ignored_regex: Pattern[AnyStr] = re.compile(settings.ignored_versions)
    versions: dict = {}

    logger.info('Listing accepted OpenShift versions')
    response = requests.get(RELEASE_URL_API, timeout=settings.request_timeout_sec)
    response.raise_for_status()
    accepted_versions = response.json()['4-stable']
    logger.debug(f'Received OpenShift versions: {accepted_versions}')

    for ver in accepted_versions:
        if ignored_regex.fullmatch(ver):
            logger.debug(f'Exact version {ver} is ignored')
            continue

        sem_ver = semver.VersionInfo.parse(ver)
        minor = f'{sem_ver.major}.{sem_ver.minor}'
        if ignored_regex.fullmatch(minor):
            logger.debug(f'Version {ver} is ignored because all {minor} are ignored')
            continue

        patches = versions.get(minor)
        versions[minor] = max_version(patches, ver) if patches else ver

    return versions
