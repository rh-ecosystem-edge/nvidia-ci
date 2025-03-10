import json

from settings import settings
from openshift import latest_ocp_releases, fetch_ocp_versions
from nvidia_gpu_operator import get_operator_versions, get_sha, latest_gpu_releases
from utils import create_tests_commands, calculate_diffs, add_tests_commands, format_gpu_suffix

def main():
    with open(settings.version_file_path, "r+") as json_f:
        old_versions = json.load(json_f)

        new_versions = {
            "gpu-main-latest": get_sha(),
            "gpu-operator": get_operator_versions(),
            "ocp": fetch_ocp_versions()
        }

        ocp_releases = latest_ocp_releases(new_versions["ocp"])
        gpu_releases = latest_gpu_releases(new_versions["gpu-operator"])

        json_f.seek(0)
        json_f.truncate()
        json.dump(new_versions, json_f, indent=4)

        diffs = calculate_diffs(old_versions, new_versions)
        format_gpu_suffix(diffs)
        tests_commands = create_tests_commands(diffs, ocp_releases, gpu_releases)
        add_tests_commands(tests_commands)

if __name__ == '__main__':
    main()
