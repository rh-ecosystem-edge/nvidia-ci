#!/usr/bin/env python


import os
import time
from typing import Dict, List, Any, Optional
import argparse
import datetime
import json
import urllib
import requests

from gql import Client, gql
from gql.transport.requests import RequestsHTTPTransport

from common.utils import logger
from common.templates import load_template

# For MicroShift versions 4.19+ we are reusing AI Model Serving job which performs basic validation
# of the device plugin and more. For older versions we have dedicated
# Device Plugin jobs, however they are named using different convention.

DEFAULT_VERSION_JOB_NAME = "periodics-e2e-aws-ai-model-serving-nightly"
VERSION_JOB_NAME = {
    "4.14": "e2e-aws-nvidia-device-plugin-nightly",
    "4.15": "e2e-aws-nvidia-device-plugin-nightly",
    "4.16": "e2e-aws-nvidia-device-plugin-nightly",
    "4.17": "e2e-aws-nvidia-device-plugin-nightly",
    "4.18": "periodics-e2e-aws-nvidia-device-plugin-nightly",
}

GCP_BASE_URL = "https://storage.googleapis.com/storage/v1/b/test-platform-results/o/"

GITHUB_PR_QUERY = """
query get_prs($branch: String!, $limit: Int!) {
    repository(owner: "openshift", name: "microshift") {
      pullRequests(baseRefName: $branch, first: $limit,  states:[MERGED], orderBy:{field: CREATED_AT, direction: DESC}) {
        nodes {
          number
          title
          state
          url
          headRefName
          mergedAt
          commits(last: 1) {
            nodes {
              commit {
                statusCheckRollup {
                  state
                  contexts(last: 30) {
                    nodes {
                      ... on StatusContext {
                        context
                        description
                        state
                        targetUrl
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
"""


def gcp_list_dir(path: str) -> List[str]:
    resp = requests.get(url=GCP_BASE_URL, params={
                        "alt": "json", "delimiter": "/", "prefix": f"{path}"}, timeout=60)
    content = json.loads(resp.content.decode("UTF-8"))
    if 'prefixes' not in content:
        return []
    return content['prefixes']


def gcp_get_file(path: str) -> Optional[str]:
    resp = requests.get(url=GCP_BASE_URL + urllib.parse.quote_plus(path),
                        params={"alt": "media"}, timeout=60)
    if resp.status_code not in [200, 404]:
        raise Exception(f"Failed to fetch file {path}: ({resp.status_code}) {resp.content.decode('UTF-8')}")
    return resp.content.decode("UTF-8").strip() if resp.status_code == 200 else None


def get_job_runs_for_version(version: str, job_limit: int) -> List[Dict[str, Any]]:
    """
    Returns a list of job runs for a given version.
    Is it obtained by making an API requests to GCP to get list of subdirs inside 'logs/{job_name}/' dir.
    The subdir list is oldest-first, so we're taking 'job_limit' jobs from the end.
    """
    job_name = f"periodic-ci-openshift-microshift-release-{version}-" + VERSION_JOB_NAME.get(
        version, DEFAULT_VERSION_JOB_NAME)
    prefixes = gcp_list_dir(f"logs/{job_name}/")
    return [{"path": path, "num": int(path.split("/")[2])} for path in prefixes[-job_limit:]]


def get_job_microshift_version(job_path: str) -> str:
    """
    Fetches the microshift-version.txt file for particular job run described by job_path variable
    which is expected to be in the format 'logs/{job_name}/{job_run_number}/'.
    """
    # Each branch uses slightly different job name: find subdir starting with e2e-.
    # There should be only one. Failed runs might not have any.
    files = gcp_list_dir(f"{job_path}artifacts/e2e-")
    if len(files) > 1:
        raise Exception(
            f"Expected only one file starting with 'e2e-' for {job_path=}, got {files}")
    elif len(files) == 0:
        logger.warning(
            f"{job_path} does not contain artifacts that start with 'e2e-'")
        return ""

    return gcp_get_file(f"{files[0]}openshift-microshift-e2e-bare-metal-tests/artifacts/microshift-version.txt")


def get_job_finished_json(job_path: str) -> Dict[str, Any]:
    """
    Fetches the finished.json file for particular job run described by job_path variable
    which is expected to be in the format 'logs/{job_name}/{job_run_number}/'.
    """
    p = f"{job_path}finished.json"
    content = gcp_get_file(p)
    if content is None:
        # When the job results are old enough, the dir might still exists but be empty.
        logger.warning(f"{p} does not exist")
        return None
    return json.loads(content)


def get_job_result(job_run: Dict[str, Any]) -> Dict[str, Any]:
    """
    Fetches the finished.json and returns a complete dictionary with the job results for dashboard creation.
    """
    finished = get_job_finished_json(job_run['path'])
    if finished is None:
        return None
    version = get_job_microshift_version(job_run['path'])
    if version is None:
        return None
    return {
        "num": job_run['num'],
        "timestamp": finished['timestamp'],
        "status": finished['result'],
        "url": f"https://prow.ci.openshift.org/view/gs/test-platform-results/{job_run['path']}",
        "microshift_version": version,
    }


def get_results_from_presubmits(version: str, cutoff: datetime.datetime, limit: int) -> List[Dict[str, Any]]:
    """
    Fetches the results from presubmits for a given version.
    """
    token = os.getenv("GITHUB_TOKEN")
    if token is None:
        logger.warning(f"GITHUB_TOKEN env var is not set - GitHub GrapQL API requires authentication - skipping fetching job results from PRs")
        return None

    branch = f"release-{version}"
    query = gql(GITHUB_PR_QUERY)
    query.variable_values = {"branch": branch, "limit": limit}

    client = Client(transport=RequestsHTTPTransport(url="https://api.github.com/graphql", headers={"Authorization": f"Bearer {token}"}))
    result = client.execute(query)

    job_results = []

    prs = result['repository']['pullRequests']['nodes']
    logger.info(f"[{version}] Found {len(prs)} PRs for {branch} branch: " + ', '.join([str(pr['number']) for pr in prs]))
    for pr in prs:
        merged_at = datetime.datetime.fromisoformat(pr['mergedAt'])
        if cutoff and merged_at < cutoff:
            # Response already includes PRs sorted newest first,
            # so if we find a PR older than most recent periodic we can stop the loop.
            logger.info(f"[{version}] PR {pr['number']} is older ({merged_at}) than the most recent periodic ({cutoff}) - stopping collecting results from PRs")
            break

        if len(pr['commits']['nodes']) == 0:
            logger.info(f"[{version}] PR {pr['number']} has no commits? Skipping")
            continue

        last_commit = pr['commits']['nodes'][0]['commit']
        status_rollup = last_commit.get('statusCheckRollup') or {}
        contexts = (status_rollup.get('contexts') or {}).get('nodes') or []
        if not contexts:
            logger.info(f"[{version}] PR {pr['number']} has no status contexts to inspect")
            continue

        nvidia_presubmits = [
            presubmit for presubmit in contexts
            if presubmit.get('context') and (
                'nvidia-device-plugin' in presubmit['context']
                or 'ai-model-serving' in presubmit['context']
            )
        ]
        if len(nvidia_presubmits) == 0:
            logger.info(f"[{version}] PR {pr['number']} has no NVIDIA Device Plugin or AI Model Serving presubmit")
            continue

        nvidia_presubmit = nvidia_presubmits[0]
        if 'Overridden by' in nvidia_presubmit['description']:
            logger.info(f"[{version}] NVIDIA Device Plugin or AI Model Serving job in PR {pr['number']} was overridden")
            continue

        prow_url = nvidia_presubmit['targetUrl']
        if 'https://prow.ci.openshift.org/view/gs/test-platform-results/' not in prow_url:
            logger.warning(f"[{version}] Unexpected targetUrl for a presubmit: {nvidia_presubmit['targetUrl']}. Commit status: {json.dumps(last_commit)}")
            continue

        gcp_path = prow_url.replace("https://prow.ci.openshift.org/view/gs/test-platform-results/", "") + "/"
        num = gcp_path.split("/")[-2]
        result = get_job_result( {"path": gcp_path, "num": int(num)} )
        if result:
            job_results.append(result)

    return job_results


def get_all_results(job_limit: int) -> Dict[str, List[Dict[str, Any]]]:
    """
    Fetches the job results for all versions of MicroShift starting from 4.14 until there are no job runs available for particular version.
    """
    logger.info("Fetching job results")

    periodic_cutoff_diff = datetime.timedelta(days=60) # Most recent periodic job must be older than 60 days in order to inspect presubmits.
    fin_results = {}
    start = time.time()
    got_results_for_at_least_one_version = False
    durations = dict[str, float]()

    # To make the script easier to maintain, we start with oldest version and go up until there are no jobs detected.
    # That way it won't require an update everytime there's a new release.
    for minor in range(14, 100):
        start_version = time.time()
        version = f"4.{minor}"
        periodic_runs = get_job_runs_for_version(version, job_limit)
        logger.info(f"[{version}] Found {len(periodic_runs)} periodic job runs")

        # Pretty soon, there will be no periodic job results for 4.14 but that should not cause the procedure to stop immediately.
        if len(periodic_runs) == 0 and got_results_for_at_least_one_version:
            logger.info(f"[{version}] Assuming that {version} is not being developed yet - stopping collecting the results")
            break

        results = []
        for periodic_run in periodic_runs:
            result = get_job_result(periodic_run)
            if not result:
                continue
            results.append(result)

        # Older versions do not run periodic jobs anymore to save on CI resources.
        # Instead, we can get the results from presubmits against the release branch.

        check_presubmits = False
        most_recent = None
        if len(results) == 0:
            logger.info(f"[{version}] No periodic job results found - collecting results from presubmits")
            check_presubmits = True
        else:
            results = sorted(results, key=lambda x: x["timestamp"], reverse=True)
            most_recent = datetime.datetime.fromtimestamp(int(results[0]["timestamp"]), datetime.timezone.utc)
            cutoff = datetime.datetime.now(datetime.timezone.utc) - periodic_cutoff_diff
            if most_recent < cutoff:
                logger.info(f"[{version}] The most recent periodic job is older ({most_recent.strftime('%Y-%m-%d')}) than 60 days ({cutoff.strftime('%Y-%m-%d')}) - collecting results from presubmits")
                check_presubmits = True

        if check_presubmits:
            pr_results = get_results_from_presubmits(version, most_recent, job_limit)
            if pr_results:
                results = sorted(pr_results + results, key=lambda x: x["timestamp"], reverse=True)[:job_limit]
            else:
                logger.critical(f"[{version}] No presubmits and (no periodics or latest periodic is too old) - assuming that version is no longer supported")
                results = []

        if results:
            got_results_for_at_least_one_version = True
            fin_results[version] = results
        durations[version] = f"{time.time() - start_version:.0f}s"

    duration = time.time() - start
    logger.info(f"Took {duration:.2f} seconds to fetch the job results - {durations}")
    return dict(sorted(fin_results.items(), reverse=True))


def build_microshift_table_rows(version_results: Dict[str, List[Dict[str, Any]]]) -> str:
    output = ""
    for version, results in version_results.items():
        output += build_microshift_table_row(version, results)
    return output


def build_microshift_table_row(version: str, results: List[Dict[str, Any]]) -> str:
    """
    Build a small HTML snippet that displays info about GPU bundle statuses
    (shown in a 'history-bar' with colored squares).
    """
    if len(results) == 0:
        return ""

    sorted_results = sorted(
        results, key=lambda r: r["timestamp"], reverse=True)
    latest_result = sorted_results[0]
    latest_result_date = datetime.datetime.fromtimestamp(int(
        latest_result["timestamp"]), datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")

    output = f"""
        <tr>
          <td class="version-cell">MicroShift {version}</td>
          <td>
            <div class="history-bar-inner">
              <div>
                <strong>Latest run:</strong> {latest_result_date}
              </div>
"""

    for result in sorted_results:
        status = result.get("status", "Unknown").upper()
        if status == "SUCCESS":
            status_class = "history-success"
        elif status == "FAILURE":
            status_class = "history-failure"
        else:
            status_class = "history-aborted"
        result_date = datetime.datetime.fromtimestamp(
            int(result["timestamp"]), datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        microshift_version = result["microshift_version"] or version
        output += f"""
              <div class='history-square {status_class}'
                onclick='window.open("{result["url"]}", "_blank")'>
                <span class="history-square-tooltip">
                  <b>Status</b>: {status}
                  <br>
                  <b>Timestamp</b>: {result_date}
                  <br>
                  <b>MicroShift</b>: {microshift_version}
                </span>
              </div>
"""

    output += """
            </div>
          </td>
        </tr>
"""
    return output


def generate_microshift_dashboard(fin_results: Dict[str, List[Dict[str, Any]]]) -> str:
    logger.info("Generating dashboard")
    template = load_template("microshift.html")

    table_rows = build_microshift_table_rows(fin_results)
    template = template.replace("{TABLE_ROWS}", table_rows)

    now_str = datetime.datetime.now(
        datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    template = template.replace("{LAST_UPDATED}", now_str)
    return template


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Microshift x NVIDIA Device Plugin CI Dashboard")
    subparsers = parser.add_subparsers(dest="command")

    parser_fetch = subparsers.add_parser(
        "fetch-data", help="Fetch the job results")
    parser_fetch.add_argument("--job-limit", type=int, default=15,
                              help="Amount of the latest job results to fetch")
    parser_fetch.add_argument(
        "--output-data", help="Path to save the results file", required=True)

    parser_generate = subparsers.add_parser(
        "generate-dashboard", help="Generate the dashboard")
    parser_generate.add_argument(
        "--input-data", help="Path to the results file", required=True)
    parser_generate.add_argument(
        "--output-dashboard", help="Path to save the dashboard HTML file", required=True)

    args = parser.parse_args()

    if args.command == "fetch-data":
        results = get_all_results(args.job_limit)
        with open(args.output_data, "w", encoding="utf-8") as f:
            json.dump(results, f, indent=2)

    elif args.command == "generate-dashboard":
        with open(args.input_data, "r", encoding="utf-8") as f:
            results = json.load(f)

        dashboard = generate_microshift_dashboard(results)
        with open(args.output_dashboard, "w", encoding="utf-8") as f:
            f.write(dashboard)
            logger.info(f"Dashboard saved to {args.output_dashboard}")

    else:
        parser.print_help()


if __name__ == "__main__":
    main()
