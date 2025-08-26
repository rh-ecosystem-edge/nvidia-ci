# Workflows for NVIDIA CI Automation

This directory contains multiple workflows for automating various aspects of the NVIDIA CI system, organized into separate subdirectories:

## Directory Structure

- [gpu_operator_versions/](./gpu_operator_versions/) — Automation for updating versions and triggering CI jobs
- [gpu_operator_dashboard/](./gpu_operator_dashboard/) — CI dashboard generation for NVIDIA GPU Operator test results
- [microshift_dashboard/](./microshift_dashboard/) — MicroShift NVIDIA Device Plugin testing dashboard
- Shared modules: [utils.py](./utils.py), [templates.py](./templates.py)

See the individual README files in each subdirectory for detailed information.

## Useful links

* [Workflow syntax for GitHub Actions](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions)
* [How do I simply run a python script from github repo with actions](https://stackoverflow.com/questions/70458458/how-do-i-simply-run-a-python-script-from-github-repo-with-actions)
* [Create Pull Request GitHub Action](https://github.com/marketplace/actions/create-pull-request)
