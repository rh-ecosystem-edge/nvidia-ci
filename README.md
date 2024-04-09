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
- `NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE`: Use only when OCP is on a public cloud, and you need to scale the cluster to add a GPU-enabled compute node. Example instance type: "g4dn.xlarge" in AWS, or "a2-highgpu-1g" in GCP, or "Standard_NC4as_T4_v3" in Azure - _optional_
- `NVIDIAGPU_CATALOGSOURCE`: custom catalogsource to be used.  If not specified, the default "certified-operators" catalog is used - _optional_
- `NVIDIAGPU_SUBSCRIPTION_CHANNEL`: specific subscription channel to be used.  If not specified, the latest channel is used - _optional_
- `TEST_LABELS`: ginkgo query passed to the label-filter option for including/excluding tests - _optional_ 
- `VERBOSE_SCRIPT`: prints verbose script information when executing the script - _optional_
- `TEST_VERBOSE`: executes ginkgo with verbose test output - _optional_
- `TEST_TRACE`: includes full stack trace from ginkgo tests when a failure occurs - _optional_

It is recommended to execute the runner script through the `make run-tests` make target.

Example:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu'
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_CATALOGSOURCE="certified-operators"
$ export NVIDIAGPU_SUBSCRIPTION_CHANNEL="v23.9"
$ make run-tests                    
Executing eco-gotests test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r --label-filter="nvidia-ci,gpu" ./tests/nvidiagpu
```
