#!/usr/bin/env python

import re
import requests
import semver

from settings import settings
from typing import Pattern, AnyStr
from utils import get_logger, max_version

logger = get_logger(__name__)

RELEASE_URL_API = 'https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted'

def fetch_ocp_versions() -> dict:
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
        sem_ver = semver.VersionInfo.parse(ver)
        minor = f'{sem_ver.major}.{sem_ver.minor}'
        if ignored_regex.fullmatch(minor):
            logger.debug(f'Version {ver} ignored')
            continue

        patches = versions.get(minor)
        versions[minor] = max_version(patches, ver) if patches else ver

    return versions
