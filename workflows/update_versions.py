import json

from settings import settings
from openshift import fetch_ocp_versions
from nvidia_gpu_operator import get_operator_versions, get_sha
from utils import create_tests_commands, calculate_diffs, save_tests_commands, get_latest_versions

def main():

    sha = get_sha()
    gpu_versions = get_operator_versions()
    ocp_versions = fetch_ocp_versions()

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
