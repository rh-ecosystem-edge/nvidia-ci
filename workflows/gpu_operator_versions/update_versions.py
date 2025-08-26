import json
from workflows.common.utils import logger

from workflows.gpu_operator_versions.settings import Settings
from workflows.gpu_operator_versions.openshift import fetch_ocp_versions
from workflows.gpu_operator_versions.version_utils import get_latest_versions, get_earliest_versions
from workflows.gpu_operator_versions.nvidia_gpu_operator import get_operator_versions, get_sha

# Constants
test_command_template = "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}"


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
        earliest_ocp = get_earliest_versions(ocp_releases, 1)
        for ocp_version in earliest_ocp:
            tests.add((ocp_version, "master"))

    if "ocp" in diffs:
        for ocp_version in diffs["ocp"]:
            if ocp_version not in ocp_releases:
                logger.warning(f'OpenShift version "{ocp_version}" is not in the list of releases: {list(ocp_releases)}. '
                               f'This should not normally happen. Check if there was an update to an old version.')
            for gpu_version in gpu_releases:
                tests.add((ocp_version, gpu_version))

    if "gpu-operator" in diffs:
        for gpu_version in diffs["gpu-operator"]:
            if gpu_version not in gpu_releases:
                logger.warning(f'GPU operator version "{gpu_version}" is not in the list of releases: {list(gpu_releases)}. '
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

def main():
    settings = Settings()
    sha = get_sha(settings)
    gpu_versions = get_operator_versions(settings)
    ocp_versions = fetch_ocp_versions(settings)

    new_versions = {
        "gpu-main-latest": sha,
        "gpu-operator": gpu_versions,
        "ocp": ocp_versions
    }

    with open(settings.version_file_path, "r+") as json_f:
        old_versions = json.load(json_f)
        json_f.seek(0)
        json.dump(new_versions, json_f, indent=4)
        json_f.truncate()

    diffs = calculate_diffs(old_versions, new_versions)
    ocp_releases = ocp_versions.keys()
    gpu_releases = get_latest_versions(gpu_versions.keys(), 2)
    tests_commands = create_tests_commands(diffs, ocp_releases, gpu_releases)
    save_tests_commands(tests_commands, settings.tests_to_trigger_file_path)

if __name__ == '__main__':
    main()
