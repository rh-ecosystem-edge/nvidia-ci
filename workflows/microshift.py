#!/usr/bin/env python


from sys import version
import time
from typing import Dict, List, Any
import argparse
import datetime
import json
import urllib
import requests


from utils import logger
from generate_ci_dashboard import load_template

# For MicroShift versions 4.19+ we are reusing AI Model Serving job which performs basic validation of the device plugin and more. 
# For older versions we have dedicated Device Plugin jobs, however they are named using different convention.

DEFAULT_VERSION_JOB_NAME = "periodics-e2e-aws-ai-model-serving-nightly"
VERSION_JOB_NAME = {
    "4.14": "e2e-aws-nvidia-device-plugin-nightly",
    "4.15": "e2e-aws-nvidia-device-plugin-nightly",
    "4.16": "e2e-aws-nvidia-device-plugin-nightly",
    "4.17": "e2e-aws-nvidia-device-plugin-nightly",
    "4.18": "periodics-e2e-aws-nvidia-device-plugin-nightly",
}

GCP_BASE_URL = "https://storage.googleapis.com/storage/v1/b/test-platform-results/o/"

def get_job_runs_for_version(version, job_limit):
    """
    Returns a list of job runs for a given version.
    Is it obtained by making an API requests to GCP to get list of subdirs inside 'logs/{job_name}/' dir.
    The subdir list is oldest-first, so we're taking 'job_limit' jobs from the end.
    """
    job_name = f"periodic-ci-openshift-microshift-release-{version}-" + VERSION_JOB_NAME.get(version, DEFAULT_VERSION_JOB_NAME)
    req = requests.get(url=GCP_BASE_URL, params={"alt":"json", "delimiter":"/", "prefix":f"logs/{job_name}/"})
    resp = json.loads(req.content.decode("UTF-8"))
    if 'prefixes' in resp:
        return [ {"path": path, "num": int(path.split("/")[2]) } for path in resp['prefixes'][-job_limit:] ]
    return []


def get_job_finished_json(job_path):
    """
    Fetches the finished.json file for particular job run described by job_path variable
    which is expected to be in the format 'logs/{job_name}/{job_run_number}/'.
    """
    url = GCP_BASE_URL + urllib.parse.quote_plus(job_path + "finished.json")
    req = requests.get(url=url, params={"alt":"media"})
    return json.loads(req.content.decode("UTF-8"))


def get_job_result(job_run):
    """
    Fetches the finished.json and returns a complete dictionary with the job results for dashboard creation.
    """
    finished = get_job_finished_json(job_run['path'])
    return {
            "num": job_run['num'],
            "timestamp": finished['timestamp'],
            "datetime": datetime.datetime.fromtimestamp(finished['timestamp']).strftime("%Y-%m-%d %H:%M:%S UTC"),
            "status": finished['result'],
            "url": f"https://prow.ci.openshift.org/view/gs/test-platform-results/{job_run['path']}"
        }


def get_all_results(job_limit: int):
    """
    Fetches the job results for all versions of MicroShift starting from 4.14 until there are no job runs available for particular version.
    """
    logger.info(f"Fetching job results")
    fin_results = {}
    start = time.time()

    # To make the script easier to maintain, we start with oldest version and go up until there are no jobs detected.
    # That way it won't require an update everytime there's a new release.
    for minor in range(14, 100):
        version = f"4.{minor}"
        runs = get_job_runs_for_version(version, job_limit)
        logger.info(f"Found {len(runs)} job runs for version {version}")

        if len(runs) == 0:
            logger.info(f"Assuming that {version} is not being developed yet - stopping collecting the results")
            break
        
        results = [get_job_result(run) for run in runs]
        fin_results[version] = results

    duration = time.time() - start
    logger.info(f"Took {duration:.2f} seconds to fetch the job results")
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

    sorted_results = sorted(results, key=lambda r: r["timestamp"], reverse=True)
    latest_result = sorted_results[0]
    latest_result_date = datetime.datetime.fromtimestamp(int(latest_result["timestamp"]), datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")

    output = f"""
        <tr>
          <td style="min-width:150px; white-space:nowrap;">MicroShift {version}</td>
          <td>
            <div class="history-bar" style="border: none; margin: 0; padding: 0; background-color: transparent; box-shadow: none;">
              <div style="margin-top: 5px;">
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
        result_date = datetime.datetime.fromtimestamp(int(result["timestamp"]), datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
        output += f"""
              <div class='history-square {status_class}'
                onclick='window.open("{result["url"]}", "_blank")'
                title='Status: {status} | Timestamp: {result_date}'>
              </div>
        """

    output += """
            </div>
          </td>
        </tr>
    """
    return output

def generate_microshift_dashboard(fin_results: Dict[str, List[Dict[str, Any]]]) -> str:
    logger.info(f"Generating dashboard")
    template = load_template("microshift-tpl.html")

    table_rows = build_microshift_table_rows(fin_results)
    template = template.replace("{TABLE_ROWS}", table_rows)

    now_str = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    template = template.replace("{LAST_UPDATED}", now_str)
    return template


def main() -> None:
    parser = argparse.ArgumentParser(description="Microshift x NVIDIA Device Plugin CI Dashboard")
    parser.add_argument("--job-limit", type=int, default=15, help="Amount of the latest job results to fetch")

    subparser = parser.add_subparsers(dest="command")
    parser_fetch = subparser.add_parser("fetch", help="Fetch the job results")
    parser_fetch.add_argument("--output", help="Path to save the results file")

    parser_dashboard = subparser.add_parser("generate-dashboard", help="Generate dashboard")
    parser_dashboard.add_argument("--input", help="Path to the results file. If missing, the data will be fetched first.")
    parser_dashboard.add_argument("--output", help="Path to save the dashboard HTML file")

    args = parser.parse_args()

    if args.command == "fetch":
        results = get_all_results(args.job_limit)
        if args.output:
            with open(args.output, "w") as f:
                json.dump(results, f, indent=2)
        else:
            print(json.dumps(results, indent=2))

    elif args.command == "generate-dashboard":
        if not args.input:
            results = get_all_results(args.job_limit)
        else:
            with open(args.input, "r") as f:
                results = json.load(f)

        dashboard = generate_microshift_dashboard(results)
        if args.output:
            with open(args.output, "w") as f:
                f.write(dashboard)
                logger.info(f"Dashboard saved to {args.output}")
        else:
            print(dashboard)

if __name__ == "__main__":
    main()
