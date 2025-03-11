import json
import logging
from typing import Any

test_command_template = "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}"

logger = logging.getLogger('update_version')
logger.setLevel(logging.INFO)
ch = logging.StreamHandler()
ch.setLevel(logging.INFO)
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


def get_latest_versions(versions: list, count: int) -> list:
    sorted_versions = sorted(versions, key=lambda v: tuple(map(int, v.split('.'))))
    return sorted_versions[-count:] if len(sorted_versions) > count else sorted_versions


def save_tests_commands(tests_commands: set, file_path: str):
    with open(file_path, "w+") as f:
        for command in sorted(tests_commands):
            f.write(command + "\n")


def create_tests_matrix(diffs: dict, ocp_releases: list, gpu_releases: list) -> set:
    tests = set()
    if "gpu-main-latest" in diffs:
        latest_ocp = get_latest_versions(ocp_releases, 1)
        for ocp_version in latest_ocp:
            tests.add((ocp_version, "master"))

    if "ocp" in diffs:
        for ocp_version in diffs["ocp"]:
            if ocp_version not in ocp_releases:
                logger.warning(f'OpenShift version "{ocp_version}" is not in the list of releases: {ocp_releases}. '
                               f'This should not normally happen. Check if there was an update to an old version.')
            for gpu_version in gpu_releases:
                tests.add((ocp_version, gpu_version))

    if "gpu-operator" in diffs:
        for gpu_version in diffs["gpu-operator"]:
            if gpu_version not in gpu_releases:
                logger.warning(f'GPU operator version "{gpu_version}" is not in the list of releases: {gpu_releases}. '
                               f'This should not normally happen. Check if there was an update to an old version.')
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
            logger.info(f'Comparing versions under "{key}"')
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
