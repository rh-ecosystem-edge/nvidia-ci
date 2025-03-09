import json
import logging
from logging import Logger

import semver

from typing import Tuple, Any

from settings import settings


def update_key(versions_file: str, version_key: str, version_value: Any):
    logger = get_logger()
    with open(versions_file, "r+") as json_f:
        data = json.load(json_f)
        old_version = data.get(version_key)
        if old_version == version_value:
            logger.info('No changes detected, exit')
            return

        logger.info(f'New version detected: {version_value} (was {old_version})')

        data[version_key] = version_value
        json_f.seek(0)  # rewind
        json.dump(data, json_f, indent=4)
        json_f.truncate()


def get_logger() -> Logger:
    logger = logging.getLogger('update_version')
    logger.setLevel(logging.DEBUG)
    ch = logging.StreamHandler()
    ch.setLevel(logging.DEBUG)
    formatter = logging.Formatter('%(asctime)s [%(levelname)s] %(message)s')
    ch.setFormatter(formatter)
    logger.addHandler(ch)
    return logger


def get_two_highest_gpu_versions(gpu_versions: dict) -> list:
    sorted_versions = sorted(gpu_versions.keys(), key=lambda v: tuple(map(int, v.split('.'))), reverse=True)
    return sorted_versions[:2]


def get_latest_versions_to_test(data: dict) -> Tuple[list[str], list[str]]:
    all_ocp_versions = data.get("ocp", {})

    all_gpu_operator_versions = data.get("gpu-operator", {})
    latest_gpu_versions = get_two_highest_gpu_versions(all_gpu_operator_versions)
    latest_gpu_versions.append("master")
    return latest_gpu_versions, list(all_ocp_versions.keys())  # returning list of latest ocp + gpu versions


def add_tests_commands(tests_commands: set):
    with open(settings.tests_to_trigger_file_path, "w+") as f:
        for command in sorted(tests_commands):
            f.write(command + "\n")


def create_tests_commands(diffs: dict, ocp_releases: list, gpu_releases: list) -> set:
    test_commands = set()
    if "gpu-main-latest" in diffs:
        for ocp_version in ocp_releases:
            test_commands.add(f"/test {ocp_version}-stable-nvidia-gpu-operator-e2e-master")

    if "ocp" in diffs:
        for ocp_version in diffs["ocp"]:
            for gpu_version in gpu_releases:
                test_commands.add(f"/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}")

    if "gpu-operator" in diffs:
        for gpu_version in diffs["gpu-operator"]:
            if gpu_version not in gpu_releases:
                continue
            for ocp_version in ocp_releases:
                test_commands.add(f"/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}")
    return test_commands


def calculate_diffs(old_versions: dict, new_versions: dict) -> dict:
    diffs = {}

    for key, value in new_versions.items():
        if key not in old_versions or old_versions[key] != value:
            diffs[key] = value

    return diffs


def version2suffix(v: str):
    return f'{v.replace(".", "-")}-x'


def get_latest_versions_as_suffix(versions: list) -> list:
    return [version2suffix(v) for v in sorted(versions)[-2:]]


def format_gpu_suffix(diffs: dict) -> dict:
    return {version2suffix(k): v for k, v in diffs.items()}
