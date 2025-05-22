Ecosystem Edge NVIDIA-CI - Golang Automation CI
=======
# NVIDIA-CI

## Overview
This repository is an automation/CI framework to test NVIDIA operators, the GPU Operator and Network Operator.
This project is based on golang + [ginkgo](https://onsi.github.io/ginkgo) framework.

### Project requirements
Golang and ginkgo versions based on versions specified in `go.mod` file.

The framework in this repository is designed to test NVIDIA's operators on a pre-installed OpenShift Container Platform
(OCP) cluster which meets the following requirements:

* OCP cluster installed with version >=4.12

### Supported setups
* Regular cluster 3 master nodes (VMs or BMs) and minimum of 2 workers (VMs or BMs)
* Single Node Cluster (VM or BM)
* Public Clouds Cluster (AWS, GCP and Azure) - For GPU Operator Only
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

2. Specify absolute path for logs directory like it appears below.  By default /tmp/reports directory is used.
> export REPORTS_DUMP_DIR=/tmp/logs_directory

## How to run

The test-runner [script](scripts/test-runner.sh) is the recommended way for executing tests.

General Parameters for the script are controlled by the following environment variables:
- `TEST_FEATURES`: list of features to be tested.  Subdirectories under `tests` dir that match a feature will be included (internal directories are excluded).  When we have more than one subdirectlory ot tests, they can be listed comma separated.- _required_
- `TEST_LABELS`: ginkgo query passed to the label-filter option for including/excluding tests - _optional_
- `TEST_VERBOSE`: executes ginkgo with verbose test output - _optional_
- `TEST_TRACE`: includes full stack trace from ginkgo tests when a failure occurs - _optional_
- `VERBOSE_SCRIPT`: prints verbose script information when executing the script - _optional_

NVIDIA GPU Operator-specific parameters for the script are controlled by the following environment variables:
- `NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE`: Use only when OCP is on a public cloud, and when you need to scale the cluster to add a GPU-enabled compute node. If cluster already has a GPU enabled worker node, this variable should be unset.
  - Example instance type: "g4dn.xlarge" in AWS, or "a2-highgpu-1g" in GCP, or "Standard_NC4as_T4_v3" in Azure - _required when need to scale cluster to add GPU node_
- `NVIDIAGPU_CATALOGSOURCE`: custom catalogsource to be used.  If not specified, the default "certified-operators" catalog is used - _optional_
- `NVIDIAGPU_SUBSCRIPTION_CHANNEL`: specific subscription channel to be used.  If not specified, the latest channel is used - _optional_
- `NVIDIAGPU_BUNDLE_IMAGE`: GPU Operator bundle image to deploy with operator-sdk if NVIDIAGPU_DEPLOY_FROM_BUNDLE variable is set to true.  Default value for bundle image if not set: ghcr.io/nvidia/gpu-operator/gpu-operator-bundle:main-latest - _optional when deploying from bundlle_
- `NVIDIAGPU_DEPLOY_FROM_BUNDLE`: boolean flag to deploy GPU operator from bundle image with operator-sdk - Default value is false - _required when deploying from bundle_
- `NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL`: specific subscription channel to upgrade to from previous version.  _required when running operator-upgrade testcase_
- `NVIDIAGPU_CLEANUP`: boolean flag to cleanup up resources created by testcase after testcase execution - Default value is true - _required only when cleanup is not needed_
- `NVIDIAGPU_GPU_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`: custom certified-operators catalogsource index image for GPU package - _required when deploying fallback custom GPU catalogsource_
- `NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`:  custom redhat-operators catalogsource index image for NFD package - _required when deploying fallback custom NFD catalogsource_
- `NVIDIAGPU_GPU_DRIVER_IMAGE`: specific GPU driver image specified in clusterPolicy - _optional_
- `NVIDIAGPU_GPU_DRIVER_REPO`: specific GPU driver image repository specified in clusterPolicy - _optional_
- `NVIDIAGPU_GPU_DRIVER_VERSION`: specific GPU driver version specified in clusterPolicy - _optional_
- `NVIDIAGPU_GPU_DRIVER_ENABLE_RDMA`: option to enable GPUDirect RDMA in clusterpolicy.  Default value is false - _optional_


NVIDIA Network Operator-specific (NNO) parameters for the script are controlled by the following environment variables:
- `NVIDIANETWORK_CATALOGSOURCE`: custom catalogsource to be used.  If not specified, the default "certified-operators" catalog is used - _optional_
- `NVIDIANETWORK_SUBSCRIPTION_CHANNEL`: specific subscription channel to be used.  If not specified, the latest channel is used - _optional_
- `NVIDIANETWORK_BUNDLE_IMAGE`: Network Operator bundle image to deploy with operator-sdk if NVIDIANETWORK_DEPLOY_FROM_BUNDLE variable is set to true.  Default value for bundle image if not set: TBD - _optional when deploying from bundlle_
- `NVIDIANETWORK_DEPLOY_FROM_BUNDLE`: boolean flag to deploy Network Operator from bundle image with operator-sdk - Default value is false - _required when deploying from bundle_
- `NVIDIANETWORK_SUBSCRIPTION_UPGRADE_TO_CHANNEL`: specific subscription channel to upgrade to from previous version.  _required when running operator-upgrade testcase_
- `NVIDIANETWORK_CLEANUP`: boolean flag to cleanup up resources created by testcase after testcase execution - Default value is true - _required only when cleanup is not needed_
- `NVIDIANETWORK_NNO_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`: custom certified-operators catalogsource index image for GPU package - _required when deploying fallback custom NNO catalogsource_
- `NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`:  custom redhat-operators catalogsource index image for NFD package - _required when deploying fallback custom NFD catalogsource_
- `NVIDIANETWORK_OFED_DRIVER_VERSION`: OFED Driver Version.  If not specified, the default driver version is used - _optional_    
- `NVIDIANETWORK_OFED_REPOSITORY`:  OFED Driver Repository.   If not specified, the default repository is used - _optional_            
- `NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE`:  RDMA workload pod namespace - _required_
- `NVIDIANETWORK_RDMA_LINK_TYPE` Layer 2 link type, Infinband or Ethernet - _required_
- `NVIDIANETWORK_RDMA_MLX_DEVICE:` mlx5 device ID corresponding to the interface port connected to Spectrum or Infiniband switch - _required_
- `NVIDIANETWORK_RDMA_CLIENT_HOSTNAME:` RDMA Client hostname of first worker node for ib_write_bw test - _required when running the RDMA testcase_ 
- `NVIDIANETWORK_RDMA_SERVER_HOSTNAME:` RDMA Server hostname of second worker node for ib_write_bw test - _required when running the RDMA testcase_   
- `NVIDIANETWORK_RDMA_TEST_IMAGE:` RDMA Test Container Image that runs the entrypoint.sh script with optional arguments specified in the pod spec.  This container will clone the "https://github.com/linux-rdma/perftest" repo and builds the ib_write_bw binaries with or without cuda headers.  It will also run the ib_write_bw command with arguments either in CLient or Server mode.  Defaults to "quay.io/wabouham/ecosys-nvidia/rdma-tools:0.0.2" - _optional_
- `NVIDIANETWORK_MELLANOX_ETH_INTERFACE_NAME`: Mellanox Ethernet Interface Name - Defaults to "ens8f0np0" if not specified - _optional_  
- `NVIDIANETWORK_MELLANOX_IB_INTERFACE_NAME`:  Mellanox Infiniband Interface Name - Defaults to "ens8f0np0" if not specified - _optional_   
- `NVIDIANETWORK_MACVLANNETWORK_NAME`: MacvlanNetwork Custom Resource instance name  - Defaults to name from Cluster Service Version alm-examples section if not specified  - _optional_ 
- `NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE`: MacvlanNetwork Custom Resource instance IPAM or IP Address/Subnet mask range for Eth or IB interface - _required_    
- `NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY`: MacvlanNetwork Custom Resource instance IPAM Default Gateway for specified ip address range - _required_

### Testing MPS with GPU Operator

To test the Multi-Process Service (MPS) functionality, you need to first deploy the GPU Operator and then run the MPS tests without cleaning up the GPU Operator deployment between test suites.

It is recommended to execute the runner script through the `make run-tests` make target.

#### Steps to run MPS tests:

1. First, deploy the GPU Operator with cleanup disabled for example:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-gpu-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_CATALOGSOURCE="certified-operators"
$ export NVIDIAGPU_SUBSCRIPTION_CHANNEL="v23.9"
$ export NVIDIAGPU_CLEANUP=false  # Important: don't clean up after deployment
$ make run-tests
```
2. After the GPU Operator deployment completes successfully, run the MPS tests:
```
$ export TEST_FEATURES="mps"
$ export TEST_LABELS='nvidia-ci,mps'  # Run MPS-specific tests
$ make run-tests
```
The MPS tests will use the existing GPU Operator deployment that was left in place from the previous test run. This ensures that the MPS tests can properly validate MPS functionality on an already configured GPU environment.

#### Test Suite Ordering:

The test framework ensures that the GPU Operator deployment tests run before MPS tests through Ginkgo's ordering mechanisms. If you need to add new MPS tests, make sure they are organized to run after the GPU Operator deployment by using proper labeling and ordering in your test files.

#### Cleanup:

After completing the MPS tests, you may want to clean up all resources by running:

```
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,cleanup'
$ export NVIDIAGPU_CLEANUP=true
$ make run-tests
```
This will remove all resources created by both the GPU Operator deployment and MPS tests.

Example running the end-to-end GPU Operator test case:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-gpu-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_CATALOGSOURCE="certified-operators"
$ export NVIDIAGPU_SUBSCRIPTION_CHANNEL="v23.9"
# Optional variables are commented below, used as needed:
# export NVIDIAGPU_GPU_DRIVER_IMAGE="driver" 
# export NVIDIAGPU_GPU_DRIVER_REPO="nvcr.io/nvidia"
# for OCP 4.12
# export NVIDIAGPU_GPU_DRIVER_VERSION="570.130.20"
# export NVIDIAGPU_GPU_DRIVER_ENABLE_RDMA=true

$ make run-tests
Executing nvidiagpu test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="nvidia-ci,gpu" ./tests/nvidiagpu
```

Example running the GPU Operator upgrade testcase (from v23.6 to v24.3) after the end-end testcase.
Note:  you must run the end-to-end testcase first to deploy a previous version, set NVIDIAGPU_CLEANUP=false,
and specify the channel to upgrade to NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL=v24.3, along with the label
'operator-upgrade' in TEST_LABELS.  Otherwise, the upgrade testcase will not be executed:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-gpu-logs-dir
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
Executing nvidiagpu test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="nvidia-ci,gpu,operator-upgrade" ./tests/nvidiagpu
```

Example running the end-to-end test case and creating custom catalogsources for NFD and GPU Operator packagmanifests
when missing from their default catalogsources.
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-gpu-ci-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_GPU_FALLBACK_CATALOGSOURCE_INDEX_IMAGE="registry.redhat.io/redhat/certified-operator-index:v4.16"
$ export NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE="registry.redhat.io/redhat/redhat-operator-index:v4.17"
$ make run-tests
```

Example running the end-to-end Network Operator test case, with the rdma testcase.  Note both TEST_LABELS "nno,rdma' are specified in examples below:
```
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-nno-ci-logs-dir
$ export TEST_FEATURES="nvidianetwork"
$ export TEST_LABELS='nno,rdma'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIANETWORK_CATALOGSOURCE="certified-operators"
$ export NVIDIANETWORK_SUBSCRIPTION_CHANNEL="v24.7"
$ export NVIDIANETWORK_NNO_FALLBACK_CATALOGSOURCE_INDEX_IMAGE="registry.redhat.io/redhat/certified-operator-index:v4.17"
$ export NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE="registry.redhat.io/redhat/redhat-operator-index:v4.17"
$ export NVIDIANETWORK_OFED_DRIVER_VERSION="25.01-0.6.0.0-0"
$ export NVIDIANETWORK_OFED_REPOSITORY="quay.io/wabouham/ecosys-nvidia"
$ export NVIDIANETWORK_RDMA_CLIENT_HOSTNAME=nvd-srv-3.nvidia.eng.redhat.com
$ export NVIDIANETWORK_RDMA_SERVER_HOSTNAME=nvd-srv-2.nvidia.eng.redhat.com
$ export NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE=192.168.2.0/24
$ export NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY=192.168.2.1
$ export NVIDIANETWORK_DEPLOY_FROM_BUNDLE=true
$ export NVIDIANETWORK_BUNDLE_IMAGE="nvcr.io/.../network-operator-bundle:v25.1.0-rc.2"
$ export NVIDIANETWORK_MELLANOX_ETH_INTERFACE_NAME="ens8f0np0"
$ export NVIDIANETWORK_MELLANOX_IB_INTERFACE_NAME="ibs2f0"
$ export NVIDIANETWORK_MACVLANNETWORK_NAME="rdmashared-net"
$ export NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE="default"
$ export NVIDIANETWORK_RDMA_LINK_TYPE="ethernet"
$ export NVIDIANETWORK_RDMA_MLX_DEVICE="mlx5_2"


$ make run-tests
Executing nvidiagpu test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="nno,rdma" ./tests/nvidianetwork
```
