import json

from settings import settings
from openshift import latest_ocp_releases, fetch_ocp_versions
from nvidia_gpu_operator import get_operator_versions, get_sha, latest_gpu_releases
from utils import update_key, create_tests_commands, calculate_diffs, add_tests_commands, format_gpu_suffix

def main():
    with open(settings.version_file_path, "r+") as json_f:
        old_versions = json.load(json_f)

    ocp_versions = fetch_ocp_versions()
    update_key(settings.version_file_path, "ocp", ocp_versions)

    gpu_versions = get_operator_versions()
    update_key(settings.version_file_path, "gpu-operator", gpu_versions)

    sha = get_sha()
    update_key(settings.version_file_path, "gpu-main-latest", sha)

    with open(settings.version_file_path, "r+") as json_f:
        new_versions = json.load(json_f)

    ocp_releases = latest_ocp_releases(ocp_versions)
    gpu_releases = latest_gpu_releases(gpu_versions)

    diffs = calculate_diffs(old_versions, new_versions)
    format_gpu_suffix(diffs)
    tests_commands = create_tests_commands(diffs, ocp_releases, gpu_releases)
    add_tests_commands(tests_commands)

if __name__ == '__main__':
    main()
