Ecosystem Edge NVIDIA-CI - Golang Automation CI
=======
# NVIDIA-CI

## Overview
This repository is an automation/CI framework to test NVIDIA operators, starting with the GPU Operator.
This project is based on golang + [ginkgo](https://onsi.github.io/ginkgo) framework.

### Project requirements
Golang and ginkgo versions based on versions specified in `go.mmod` file.

The framework in this repository is designed to test NVIDIA's operators on a pre-installed OpenShift Container Platform
(OCP) cluster which meets the following requirements:

* OCP cluster installed with version >=4.12

### Supported setups
* Regular cluster 3 master nodes (VMs or BMs) and minimum of 2 workers (VMs or BMs)
* Single Node Cluster (VM or BM)
* Public Clouds Cluster (AWS, GCP and Azure)
* On Premise Cluster

### General environment variables
#### Mandatory:
* `KUBECONFIG` - Path to kubeconfig file.
#### Optional:
* Logging with glog

We use glog library for logging. In order to enable verbose logging the following needs to be done:

1. Make sure to import inittool package in your go script, per this example:

<sup>
    import (
      . "github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
    )
</sup>

2. Need to export the following SHELL variable:
> export VERBOSE_LEVEL=100

##### Notes:

  1. The value for the variable has to be >= 100.
  2. The variable can simply be exported in the shell where you run your automation.
  3. The go file you work on has to be in a directory under github.com/rh-ecosystem-edge/nvidia-ci/tests/ directory for being able to import inittools.
  4. Importing inittool also initializes the api client and it's available via "APIClient" variable.

* Collect logs from cluster with reporter

We use k8reporter library for collecting resource from cluster in case of test failure.
In order to enable k8reporter the following needs to be done:

1. Export DUMP_FAILED_TESTS and set it to true. Use example below
> export DUMP_FAILED_TESTS=true

2. Specify absolute path for logs directory like it appears below. By default /tmp/reports directory is used.
> export REPORTS_DUMP_DIR=/tmp/logs_directory

## How to run

The test-runner [script](scripts/test-runner.sh) is the recommended way for executing tests.

Parameters for the script are controlled by the following environment variables:
- `TEST_FEATURES`: list of features to be tested.  Subdirectories under `tests` dir that match a feature will be included (internal directories are excluded).  When we have more than one subdirectlory ot tests, they can be listed comma separated.- _required_
- `NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE`: Use only when OCP is on a public cloud, and when you need to scale the cluster to add a GPU-enabled compute node. If cluster already has a GPU enabled worker node, this variable should be unset.
  - Example instance type: "g4dn.xlarge" in AWS, or "a2-highgpu-1g" in GCP, or "Standard_NC4as_T4_v3" in Azure - _required when need to scale cluster to add GPU node_
- `NVIDIAGPU_CATALOGSOURCE`: custom catalogsource to be used.  If not specified, the default "certified-operators" catalog is used - _optional_
- `NVIDIAGPU_SUBSCRIPTION_CHANNEL`: specific subscription channel to be used.  If not specified, the latest channel is used - _optional_
- `NVIDIAGPU_BUNDLE_IMAGE`: GPU Operator bundle image to deploy with operator-sdk if NVIDIAGPU_DEPLOY_FROM_BUNDLE variable is set to true.  Default value for bundle image if not set: registry.gitlab.com/nvidia/kubernetes/gpu-operator/staging/gpu-operator-bundle:main-latest - _optional when deploying from bundlle_
- `NVIDIAGPU_DEPLOY_FROM_BUNDLE`: boolean flag to deploy GPU operator from bundle image with operator-sdk - Default value is false - _required when deploying from bundle_
- `NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL`: specific subscription channel to upgrade to from previous version.  _required when running operator-upgrade testcase_
- `NVIDIAGPU_CLEANUP`: boolean flag to cleanup up resources created by testcase after testcase execution - Default value is true - _required only when cleanup is not needed_
- `TEST_LABELS`: ginkgo query passed to the label-filter option for including/excluding tests - _optional_
- `VERBOSE_SCRIPT`: prints verbose script information when executing the script - _optional_
- `TEST_VERBOSE`: executes ginkgo with verbose test output - _optional_
- `TEST_TRACE`: includes full stack trace from ginkgo tests when a failure occurs - _optional_

It is recommended to execute the runner script through the `make run-tests` make target.

Example running the end-to-end test case:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_CATALOGSOURCE="certified-operators"
$ export NVIDIAGPU_SUBSCRIPTION_CHANNEL="v23.9"
$ make run-tests
Executing eco-gotests test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="nvidia-ci,gpu" ./tests/nvidiagpu
```

Example running the GPU operator upgrade testcase (from v23.6 to v24.3) after the end-end testcase.
Note:  you must run the end-to-end testcase first to deploy a previous version, set NVIDIAGPU_CLEANUP=false,
and specify the channel to upgrade to NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL=v24.3, along with the label
'operator-upgrade' in TEST_LABELS.  Otherwise, the upgrade testcase will not be executed:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu,operator-upgrade'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_CATALOGSOURCE="certified-operators"
$ export NVIDIAGPU_SUBSCRIPTION_CHANNEL="v23.9"
$ export NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL=v24.3
$ export NVIDIAGPU_CLEANUP=false
$ make run-tests
Executing eco-gotests test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="nvidia-ci,gpu,operator-upgrade" ./tests/nvidiagpu
```
