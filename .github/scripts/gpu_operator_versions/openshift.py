#!/usr/bin/env python

import re
import requests
import semver

from gpu_operator_versions.settings import Settings
from typing import Pattern, AnyStr
from common.utils import logger
from gpu_operator_versions.version_utils import max_version

RELEASE_URL_API = 'https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted'
OPENSHIFT_STREAMS = ('4-stable', '5-stable')


def fetch_ocp_version_stream(request_timeout_sec: int) -> list[str]:
    """
    Fetch raw accepted OpenShift versions from the 4-stable and 5-stable streams.

    Profile-specific ignored-version filtering is intentionally deferred so that
    every profile can consume the same discovered snapshot.
    """
    logger.info('Listing accepted OpenShift versions')
    response = requests.get(RELEASE_URL_API, timeout=request_timeout_sec)
    response.raise_for_status()
    release_streams = response.json()
    accepted_versions = [
        version
        for stream in OPENSHIFT_STREAMS
        for version in release_streams[stream]
    ]
    logger.debug(f'Received OpenShift versions: {accepted_versions}')
    return accepted_versions


def filter_ocp_versions(accepted_versions: list[str], ignored_versions_regex: str) -> dict[str, str]:
    """
    Filter and reduce accepted OpenShift versions for one profile.

    Versions matching the configured regex are excluded. For each minor version
    (for example, 4.12 and 5.0), only the highest patch version is retained.
    """
    logger.info(f'Ignored versions: {ignored_versions_regex}')
    ignored_regex: Pattern[AnyStr] = re.compile(ignored_versions_regex)
    versions: dict[str, str] = {}

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


def fetch_ocp_versions(settings: Settings) -> dict[str, str]:
    """Fetch and filter accepted OpenShift versions for one settings object."""
    return filter_ocp_versions(
        fetch_ocp_version_stream(settings.request_timeout_sec),
        settings.ignored_versions,
    )
