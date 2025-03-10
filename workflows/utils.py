import json
import logging
from logging import Logger
from settings import settings
from typing import Tuple, Any

test_command_template = "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}"

logger = logging.getLogger('update_version')
logger.setLevel(logging.DEBUG)
ch = logging.StreamHandler()
ch.setLevel(logging.DEBUG)
formatter = logging.Formatter('%(asctime)s [%(levelname)s] %(message)s')
ch.setFormatter(formatter)
logger.addHandler(ch)


def update_key(versions_file: str, version_key: str, version_value: Any):
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

def create_tests_matrix(diffs: dict, ocp_releases: list, gpu_releases: list) -> set:
    tests = set()
    if "gpu-main-latest" in diffs:
        for ocp_version in ocp_releases:
            tests.add((ocp_version, "master"))

    if "ocp" in diffs:
        for ocp_version in diffs["ocp"]:
            for gpu_version in gpu_releases:
                tests.add((ocp_version, gpu_version))

    if "gpu-operator" in diffs:
        for gpu_version in diffs["gpu-operator"]:
            if gpu_version not in gpu_releases:
                # TODO: Why do we even care?
                logger.warning(f'Changed "{gpu_version}" is not in the list of releases')
                continue
            for ocp_version in ocp_releases:
                tests.add((ocp_version, gpu_version))

    return tests


def create_tests_commands(diffs: dict, ocp_releases: list, gpu_releases: list) -> set:
    tests_commands = set()
    tests = create_tests_matrix(diffs, ocp_releases, gpu_releases)
    for t in tests:
        gpu_version_suffix = version2suffix(t[1])
        tests_commands.add(test_command_template.format(ocp_version=t[0], gpu_version=gpu_version_suffix))
    return tests_commands


def calculate_diffs(old_versions: dict, new_versions: dict) -> dict:
    diffs = {}
    for key, value in new_versions.items():
        if isinstance(value, dict):
            sub_diff = calculate_diffs(old_versions.get(key, {}), value)
            if sub_diff:
                diffs[key] = sub_diff
        else:
            if key not in old_versions or old_versions[key] != value:
                logger.info(f'Key "{key}" has changed: {old_versions.get(key)} > {value}')
                diffs[key] = value

    return diffs


def version2suffix(v: str):
    return v if v == 'master' else f'{v.replace(".", "-")}-x'
