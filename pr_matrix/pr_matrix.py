import urllib.parse
import requests
import re
import urllib
from utils import *
from logger import logger

# Error handling function
def raise_error(message):
    logger.error(message)
    raise Exception(message)

# Regular expression for matching test pattern
test_pattern = re.compile(r"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/\d+/pull-ci-rh-ecosystem-edge-nvidia-ci-main-(?P<ocp_version>\d+\.\d+)-stable-nvidia-gpu-operator-e2e-(?P<gpu_version>\d+-\d+-x|master)/")



def generate_history():
    try:
        logger.info("Generating history...")
        r = requests.get(url="https://api.github.com/repos/rh-ecosystem-edge/nvidia-ci/pulls",
                         params={"state": "closed", "base": "main", "per_page": "100", "page": "1", "head": "rh-ecosystem-edge:create-pull-request/patch"},
                         headers={"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"})
        r.raise_for_status()
        for pr in r.json():
            num = pr["number"]
            logger.info(f"Processing PR #{num}")
            tests = get_all_pr_tests(num)
            logger.info(tests)
    except requests.exceptions.RequestException as e:
        raise_error(f"Request failed in generate_history: {e}")
    except KeyError as e:
        raise_error(f"Missing expected field in PR response: {e}")
    except Exception as e:
        raise_error(f"An unexpected error occurred in generate_history: {e}")

def get_all_pr_tests(pr_num):
    logger.info(f"Getting all tests for PR #{pr_num}")
    try:
        r = requests.get(url="https://storage.googleapis.com/storage/v1/b/test-platform-results/o",
                         params={"prefix": f"pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_num}/",
                                 "alt": "json", "delimiter": "/", "includeFoldersAsPrefixes": "True", "maxResults": "1000", "projection": "noAcl"},
                         headers={"Accept": "application/json"})
        r.raise_for_status()
        prefixes = r.json().get("prefixes")
        tests = []
        if not prefixes:
            return tests
        for job in prefixes:
            match = test_pattern.match(job)
            if not match:
                continue
            ocp = match.group('ocp_version')
            gpu_suffix = match.group('gpu_version')
            result = get_job_results(pr_num, job, ocp, gpu_suffix)
            save_to_json()
            logger.info(f"Test result for {job}: {result}")
            tests.append(result)
        return tests
    except requests.exceptions.RequestException as e:
        raise_error(f"Request failed in get_all_pr_tests: {e}")
    except KeyError as e:
        raise_error(f"Missing expected field in PR test result: {e}")
    except Exception as e:
        raise_error(f"An unexpected error occurred in get_all_pr_tests: {e}")

def get_job_results(pr_id, prefix, ocp_version, gpu_version_suffix):
    try:
        logger.info(f"Getting job results for {prefix}")
        r = requests.get(url="https://storage.googleapis.com/storage/v1/b/test-platform-results/o",
                         params={"prefix": prefix,
                                 "alt": "json", "delimiter": "/", "includeFoldersAsPrefixes": "True", "maxResults": "1000", "projection": "noAcl"},
                         headers={"Accept": "application/json"})
        r.raise_for_status()
        latest_build = fetch_file_content(r.json()["items"][0]["name"])
        logger.info(f"Job: {prefix}, latest build: {latest_build}")
        status = get_status(prefix, latest_build)
        # TODO: We can't get the exact versions if not success.
        # Probably we should include only successful results in the matrix, may have a separate section for warnings.
        url = get_job_url(pr_id, ocp_version, gpu_version_suffix, latest_build)

        if status == "SUCCESS":
            # exact_versions == (ocp_version, gpu_version)
            exact_versions = get_versions(prefix, latest_build, gpu_version_suffix)
            logger.info(f"Job {prefix} succeeded, exact versions: {exact_versions}")
            store_ocp_data(ocp_version,exact_versions[0], exact_versions[1], status, url)
        else:
            logger.info(f"Job {prefix} didn't succeed with status {status}")
            store_ocp_data(ocp_version, ocp_version, gpu_suffix_to_version(gpu_version_suffix), status, url)
        
    except requests.exceptions.RequestException as e:
        raise_error(f"Request failed in get_job_results: {e}")
    except KeyError as e:
        raise_error(f"Missing expected field in job result: {e}")
    except Exception as e:
        raise_error(f"An unexpected error occurred in get_job_results: {e}")


def gpu_suffix_to_version(gpu):
    return gpu if gpu == "master" else gpu[:-2].replace("-", ".")

def get_job_url(pr_id, ocp_minor, gpu_suffix, job_id):
    return f"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/{pr_id}/pull-ci-rh-ecosystem-edge-nvidia-ci-main-{ocp_minor}-stable-nvidia-gpu-operator-e2e-{gpu_suffix}/{job_id}"

def get_versions(prefix, build_id, gpu_version_suffix):
    try:
        logger.info(f"Fetching versions for build {build_id}")
        ocp_version_file = f"{prefix}{build_id}/artifacts/nvidia-gpu-operator-e2e-{gpu_version_suffix}/gpu-operator-e2e/artifacts/ocp.version"
        ocp_version = fetch_file_content(ocp_version_file)

        gpu_version_file = f"{prefix}{build_id}/artifacts/nvidia-gpu-operator-e2e-{gpu_version_suffix}/gpu-operator-e2e/artifacts/operator.version"
        gpu_version = fetch_file_content(gpu_version_file)
        return (ocp_version, gpu_version)

    except requests.exceptions.RequestException as e:
        raise_error(f"Request failed in get_versions: {e}")
    except KeyError as e:
        raise_error(f"Missing expected field in version fetch: {e}")
    except Exception as e:
        raise_error(f"An unexpected error occurred in get_versions: {e}")

def fetch_file_content(file_path):
    try:
        logger.info(f"Fetching file content for {file_path}")
        r = requests.get(url=f"https://storage.googleapis.com/storage/v1/b/test-platform-results/o/{urllib.parse.quote_plus(file_path)}",
                         params={"alt": "media"})
        r.raise_for_status()
        return r.content.decode("UTF-8")
    except requests.exceptions.RequestException as e:
        raise_error(f"Request failed in fetch_file_content: {e}")
    except Exception as e:
        raise_error(f"An unexpected error occurred in fetch_file_content: {e}")

def get_status(prefix, latest_build_id):
    try:
        logger.info(f"Fetching status for {latest_build_id}")
        finished_file = f"{prefix}{latest_build_id}/finished.json"
        r = requests.get(url=f"https://storage.googleapis.com/storage/v1/b/test-platform-results/o/{urllib.parse.quote_plus(finished_file)}",
                         params={"alt": "media"})
        r.raise_for_status()
        return r.json()['result']
    except requests.exceptions.RequestException as e:
        raise_error(f"Request failed in get_status: {e}")
    except KeyError as e:
        raise_error(f"Missing expected field in finished.json response: {e}")
    except Exception as e:
        raise_error(f"An unexpected error occurred in get_status: {e}")

if __name__ == "__main__":
    generate_history()
