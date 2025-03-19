import requests
import re

test_pattern = re.compile(r"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/\d+/pull-ci-rh-ecosystem-edge-nvidia-ci-main-(?P<ocp_version>\d+\.\d+)-stable-nvidia-gpu-operator-e2e-(?P<gpu_version>\d+-\d+-x|master)/")

def generate():
    r = requests.get(url="https://api.github.com/repos/rh-ecosystem-edge/nvidia-ci/pulls",
                 params={"state":"closed", "base":"main", "per_page":"100", "page":"1", "head": "rh-ecosystem-edge:create-pull-request/patch"},
                 headers={"Accept":"application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"})
    r.raise_for_status()
    for pr in r.json():
        num = pr["number"]
        tests = get_tests(num)
        print(f"Collected versions: {tests}")

def get_tests(pr_num):
    print(f"PR #{pr_num}")
    r = requests.get(url="https://storage.googleapis.com/storage/v1/b/test-platform-results/o",
                     params={"prefix": f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_num}/",
                             "alt":"json", "delimiter":"/", "includeFoldersAsPrefixes":"True", "maxResults":"1000", "projection":"noAcl"},
                     headers={"Accept":"application/json"})
    r.raise_for_status()
    subs = r.json().get("prefixes")
    tests = []
    if not subs:
        return tests
    for p in subs:
        print(f"\t{p}")
        match = test_pattern.match(p)
        if not match:
            continue
        ocp = match.group('ocp_version')
        gpu = match.group('gpu_version')
        tests.append((ocp, gpu))
    return tests

r"pull-ci-rh-ecosystem-edge-nvidia-ci-main-(?<ocp_version>\d+\.d+)-stable-nvidia-gpu-operator-e2e-(?<gpu_version>\d+-\d+-x|master)"

if __name__ == "__main__":
    generate()


