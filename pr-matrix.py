import requests
from bs4 import BeautifulSoup
import re
import logging


# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')


def get_pr_numbers():
   url = "https://api.github.com/repos/rh-ecosystem-edge/nvidia-ci/pulls?state=closed&base=main&per_page=100&page=1&head=rh-ecosystem-edge:create-pull-request/patch"
   logging.info("Fetching PR numbers...")
   response = requests.get(url, headers={
       "Accept": "application/vnd.github+json",
       "X-GitHub-Api-Version": "2022-11-28"
   })
   response.raise_for_status()
   prs = response.json()
   pr_numbers = [pr["number"] for pr in prs[:100]]  # Limit to 100 PRs or less
   logging.info(f"Fetched {len(pr_numbers)} PRs.")
   return pr_numbers


def construct_test_urls(pr_numbers):
   base_url = "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/rh-ecosystem-edge_nvidia-ci/"
   return [f"{base_url}{pr}" for pr in pr_numbers]


def extract_test_names(pr_url):
   logging.info(f"Fetching test names from {pr_url}...")
   response = requests.get(pr_url)
   response.raise_for_status()
   soup = BeautifulSoup(response.text, "html.parser")
   tests = [a.text.strip() for a in soup.select("ul.resource-grid a")]  # Extract test names
   logging.info(f"Found {len(tests)} tests for {pr_url}.")
   return tests


def extract_versions(test_name):
    ocp_match = re.search(r"4\.\d+", test_name)  
    gpu_operator_match = re.search(r"e2e-(\d+-\d+(-\d+)?|master)", test_name)  
    
    ocp_version = ocp_match.group(0) if ocp_match else "Unknown"
    gpu_operator_version = gpu_operator_match.group(1) if gpu_operator_match else "Unknown"
    
    return ocp_version, gpu_operator_version



def main():
   pr_numbers = get_pr_numbers()
   test_urls = construct_test_urls(pr_numbers)
   with open("pr_tests.txt", "w") as file:
       for url in test_urls:
           file.write(f"Tests for {url}:\n")
           tests = extract_test_names(url)
           for test in tests:
               ocp_version, gpu_operator_version = extract_versions(test)
               file.write(f"  - {test} (OCP: {ocp_version}, GPU Operator: {gpu_operator_version})\n")
           file.write("\n")
   logging.info("Test results saved to pr_tests.txt.")


if __name__ == "__main__":
   main()




