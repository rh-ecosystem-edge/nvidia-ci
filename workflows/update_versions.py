import json

from settings import settings
from openshift import latest_ocp_releases, fetch_ocp_versions
from nvidia_gpu_operator import get_operator_versions, get_sha, latest_gpu_releases
from utils import create_tests_commands, calculate_diffs, add_tests_commands

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
    ocp_releases = latest_ocp_releases(ocp_versions)
    gpu_releases = latest_gpu_releases(gpu_versions)
    tests_commands = create_tests_commands(diffs, ocp_releases, gpu_releases)
    add_tests_commands(tests_commands)

if __name__ == '__main__':
    main()
