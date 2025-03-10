#!/usr/bin/env python

import re
import requests
import semver

from settings import settings
from typing import Pattern, AnyStr

quay_url_api = 'https://quay.io/api/v1/repository/openshift-release-dev/ocp-release/tag/'

def fetch_ocp_versions() -> dict:
    versions: dict = {}
    page_size: int = 100
    has_more: bool = True
    tag_filter: str = 'like:%.%.%-multi-x86_64'
    tag_regex: Pattern[AnyStr] = re.compile(r'^(?P<minor>\d+\.\d+)\.(?P<patch>\d+(?:-rc\.\d+)?)\-multi\-x86_64$')
    page: int = 1

    while has_more:
        response = requests.get(quay_url_api, params={
            'limit': str(page_size), 'page': page, 'filter_tag_name': tag_filter, 'onlyActiveTags': 'true'})
        response.raise_for_status()
        response_json = response.json()
        has_more = response_json.get('has_additional')
        page += 1

        for tag in response_json.get('tags', []):
            tag_name = tag.get('name', '')
            match = tag_regex.match(tag_name)
            if not match:
                continue

            minor = match.group('minor')
            if minor in settings.ignored_versions:
                continue

            full = f"{minor}.{match.group('patch')}"
            patches = versions.get(minor)
            versions[minor] = semver.max_ver(versions[minor], full) if patches else full

    return versions


def latest_ocp_releases(ocp_versions: dict) -> list:
    return list(ocp_versions.keys())
