# Ecosystem Edge NVIDIA-CI - Golang Automation CI

# NVIDIA-CI

## Overview

This repository is an automation/CI framework to test NVIDIA operators, the GPU Operator and Network Operator.
This project is based on golang + [ginkgo](https://onsi.github.io/ginkgo) framework.

### Project requirements

Golang and ginkgo versions based on versions specified in `go.mod` file.

The framework in this repository is designed to test NVIDIA's operators on a pre-installed OpenShift Container Platform
(OCP) cluster which meets the following requirements:

- OCP cluster installed with version >=4.12

### Supported setups

- Regular cluster 3 master nodes (VMs or BMs) and minimum of 2 workers (VMs or BMs)
- Single Node Cluster (VM or BM)
- Public Clouds Cluster (AWS, GCP and Azure) - For GPU Operator Only
- On Premise Cluster



### General environment variables



#### Mandatory:

- `KUBECONFIG` - Path to kubeconfig file.



#### Optional:

- Logging with glog

We use glog library for logging. In order to enable verbose logging the following needs to be done:

1. Make sure to import inittool package in your go script, per this example:

import ( . "github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools" )

1. Need to export the following SHELL variable:

> export VERBOSE_LEVEL=100



##### Notes:

1. The value for the variable has to be >= 100.
2. The variable can simply be exported in the shell where you run your automation.
3. The go file you work on has to be in a directory under github.com/rh-ecosystem-edge/nvidia-ci/tests/ directory for being able to import inittools.
4. Importing inittool also initializes the api client and it's available via "APIClient" variable.

- Collect logs from cluster with reporter

We use k8reporter library for collecting resource from cluster in case of test failure.
In order to enable k8reporter the following needs to be done:

1. Export DUMP_FAILED_TESTS and set it to true. Use example below

> export DUMP_FAILED_TESTS=true

1. Specify absolute path for logs directory like it appears below.  By default /tmp/reports directory is used.

> export REPORTS_DUMP_DIR=/tmp/logs_directory



## How to run

The test-runner [script](scripts/test-runner.sh) is the recommended way for executing tests.

### Environment variables

General Parameters for the script are controlled by the following environment variables:

- `TEST_FEATURES`: list of features to be tested.  Subdirectories under `tests` dir that match a feature will be included (internal directories are excluded).  When we have more than one subdirectory ot tests, they can be listed comma-separated.- *required*
- `TEST_LABELS`: ginkgo query passed to the label-filter option for including/excluding tests. Supports comma-separated labels (AND logic) and `||` operator (OR logic). Examples: `'nvidia-ci,gpu'`, `'nvidia-ci,mps'`, `'nvidia-ci,mig'`, `'deploy || rdma-legacy-sriov'` - *optional*
- `TEST_VERBOSE`: executes ginkgo with verbose test output - *optional*
- `TEST_TRACE`: includes full stack trace from ginkgo tests when a failure occurs - *optional*
- `VERBOSE_SCRIPT`: prints verbose script information when executing the script - *optional*
- `NO_COLOR`: `{true|anything else}` when used, omits the coloring of logs that appear on beginning of the functions. However it does not affect on the coloring of the logs that ginkgo framework generates. - *optional*

NVIDIA GPU Operator-specific parameters for the script are controlled by the following environment variables:

- `NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE`: Use only when OCP is on a public cloud, and when you need to scale the cluster to add a GPU-enabled compute node. If cluster already has a GPU enabled worker node, this variable should be unset.
  - Example instance type: "g4dn.xlarge" in AWS, or "a2-highgpu-1g" in GCP, or "Standard_NC4as_T4_v3" in Azure - *required when need to scale cluster to add GPU node*
- `NVIDIAGPU_CATALOGSOURCE`: custom catalogsource to be used.  If not specified, the default "certified-operators" catalog is used - *optional*
- `NVIDIAGPU_SUBSCRIPTION_CHANNEL`: specific subscription channel to be used.  If not specified, the latest channel is used - *optional*
- `NVIDIAGPU_BUNDLE_IMAGE`: GPU Operator bundle image to deploy with operator-sdk if NVIDIAGPU_DEPLOY_FROM_BUNDLE variable is set to true.  Default value for bundle image if not set: ghcr.io/nvidia/gpu-operator/gpu-operator-bundle:main-latest - *optional when deploying from bundlle*
- `NVIDIAGPU_DEPLOY_FROM_BUNDLE`: boolean flag to deploy GPU operator from bundle image with operator-sdk - Default value is false - *required when deploying from bundle*
- `NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL`: specific subscription channel to upgrade to from previous version.  *required when running operator-upgrade testcase*
- `NVIDIAGPU_CLEANUP`: boolean flag to cleanup up resources created by testcase after testcase execution - Default value is true - *required only when cleanup is not needed*
- `NVIDIAGPU_GPU_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`: custom certified-operators catalogsource index image for GPU package - *required when deploying fallback custom GPU catalogsource*
- `NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH`: a JSON patch to apply to a default cluster policy from ALM examples, written according to
 [RFC 6902](http://tools.ietf.org/html/rfc6902) (also see [kubectl patch](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_patch/)) - *optional*
- `NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`:  custom redhat-operators catalogsource index image for NFD package - *required when deploying fallback custom NFD catalogsource*

NVIDIA Network Operator-specific (NNO) parameters for the script are controlled by the following environment variables:

- `NVIDIANETWORK_CATALOGSOURCE`: custom catalogsource to be used.  If not specified, the default "certified-operators" catalog is used - *optional*
- `NVIDIANETWORK_SUBSCRIPTION_CHANNEL`: specific subscription channel to be used.  If not specified, the latest channel is used - *optional*
- `NVIDIANETWORK_BUNDLE_IMAGE`: Network Operator bundle image to deploy with operator-sdk if NVIDIANETWORK_DEPLOY_FROM_BUNDLE variable is set to true.  Default value for bundle image if not set: TBD - *optional when deploying from bundlle*
- `NVIDIANETWORK_DEPLOY_FROM_BUNDLE`: boolean flag to deploy Network Operator from bundle image with operator-sdk - Default value is false - *required when deploying from bundle*
- `NVIDIANETWORK_SUBSCRIPTION_UPGRADE_TO_CHANNEL`: specific subscription channel to upgrade to from previous version.  *required when running operator-upgrade testcase*
- `NVIDIANETWORK_CLEANUP`: boolean flag to cleanup up resources created by testcase after testcase execution - Default value is true - *required only when cleanup is not needed*
- `NVIDIANETWORK_NNO_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`: custom certified-operators catalogsource index image for GPU package - *required when deploying fallback custom NNO catalogsource*
- `NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE`:  custom redhat-operators catalogsource index image for NFD package - *required when deploying fallback custom NFD catalogsource*
- `NVIDIANETWORK_OFED_DRIVER_VERSION`: OFED Driver Version.  If not specified, the default driver version is used - *optional*
- `NVIDIANETWORK_OFED_REPOSITORY`:  OFED Driver Repository.   If not specified, the default repository is used - *optional*
- `NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE`:  RDMA workload pod namespace - *required*
- `NVIDIANETWORK_RDMA_LINK_TYPE` Layer 2 link type, Infinband or Ethernet - *required*
- `NVIDIANETWORK_RDMA_MLX_DEVICE`: mlx5 device ID corresponding to the interface port connected to Spectrum or Infiniband switch - *required*
- `NVIDIANETWORK_RDMA_CLIENT_HOSTNAME`: RDMA Client hostname of first worker node for ib_write_bw test - *required when running the RDMA testcase*
- `NVIDIANETWORK_RDMA_SERVER_HOSTNAME`: RDMA Server hostname of second worker node for ib_write_bw test - *required when running the RDMA testcase*
- `NVIDIANETWORK_RDMA_NETWORK_TYPE`: RDMA network type, e.g. sriov, shared-device.  Defaults to shared-device if not specified - *required when running the RDMA testcase*
- `NVIDIANETWORK_RDMA_TEST_IMAGE`: RDMA Test Container Image that runs the entrypoint.sh script with optional arguments specified in the pod spec.  This container will clone the "[https://github.com/linux-rdma/perftest](https://github.com/linux-rdma/perftest)" repo and builds the ib_write_bw binaries with or without cuda headers.  It will also run the ib_write_bw command with arguments either in CLient or Server mode.  Defaults to "quay.io/wabouham/ecosys-nvidia/rdma-tools:0.0.3" - *optional*
- `NVIDIANETWORK_RDMA_SRIOV_NETWORK_NAME`: sriovnetwork resource name  -  *required when running the Legacy SRIOV RDMA testcase*
- `NVIDIANETWORK_MELLANOX_ETH_INTERFACE_NAME`: Mellanox Ethernet Interface Name - Defaults to "ens8f0np0" if not specified - *optional*
- `NVIDIANETWORK_MELLANOX_IB_INTERFACE_NAME`:  Mellanox Infiniband Interface Name - Defaults to "ens8f0np0" if not specified - *optional*
- `NVIDIANETWORK_MACVLANNETWORK_NAME`: MacvlanNetwork Custom Resource instance name  - Defaults to name from Cluster Service Version alm-examples section if not specified  - *optional*
- `NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE`: MacvlanNetwork Custom Resource instance IPAM or IP Address/Subnet mask range for Eth or IB interface - *required*
- `NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY`: MacvlanNetwork Custom Resource instance IPAM Default Gateway for specified ip address range - *required*
- `NVIDIANETWORK_RDMA_GPUDIRECT`: Boolean flag to run RDMA workload with 1 nvidia.com/gpu resource - *optional*



### CLI parameters:

NVIDIA MIG parameters for the script are controlled by the following ginkgo parameters which are delivered as `ARGS="-- [{parameter}...]"` for the `make run-tests` (check the examples):

- `--single.mig-profile=n`, where n is typically a value of int type between 0-5. The parameter is used to choose the MIG profile from list of available MIG profiles (e.g. 1g.5gb is usually referenced with index 0).  If not specified, a valid random number is used. Typically values 0-5. - *optional*
- `--mixed.mig.instances=xxx`, where xxx is a comma-separated string inside quotation marks (e.g. "2,0,1,1,0,0") The list of numbers represent how many instances are to be used for each profile when creating a pod. The first number indicates how many instances are to be used for the first profile etc. The instances of different profiles consume GPU slices in a different way. The name of the profile (e.g. 2g.10gb) describes the consumption of each instance (each instance would consume 2 slices and 10gb of memory). *optional*
- `--mixed.mig.pod-delay=n`, where n is a number in range 0 - 315 (seconds). In mixed MIG testcase there are usually more than 1 pod launched (depends on available GPU and mixed.mig.instances parameter). Since GPU workload is 300 seconds, this parameter can be used to control the delay between the pod launches so that the pods are running completely simultaneously, mostly overlapping (e.g. 15-80), slightly overlapping (e.g. 200-280 seconds), or non-overlapping (over 300 seconds). Values outside valid range are reset to closest limit (either 0 or 315). *optional*
- --time.slicing.instances="n". Default is 8. There may be a comma-separated list of values, as many as pod-count indicates. The values indicate how many timeslices for each pod is to be created (e.g. "1,2,3,4,5,6,7,8", meaning that first pod would get 1 time-slice, second one would get 2, etc.). They may run simultaneously as long as they don't exceed the maximum value (8). Any pods that is not getting the resources will stay in pending state until the requested resources become available. Any pod with requested 8 time-slices will run alone. The sum of the values is currently restricted to 100.
- --time.slicing.mon-after-pod=n. Default is 0, meaning that process monitoring will start immediately. If there are 5 instances and mon-after-pod=4, the process monitoring would only start after 4th pod. There may be previous pods still running depending on how many time-slices they have been allocated. Any number exceeding the number of instances will prevent the process monitoring




### Testing MPS with GPU Operator

To test the Multi-Process Service (MPS) functionality, you need to first deploy the GPU Operator and then run the MPS tests without cleaning up the GPU Operator deployment between test suites.

It is recommended to execute the runner script through the `make run-tests` make target.

#### Steps to run MPS tests:

1. First, deploy the GPU Operator with cleanup disabled for example:

```bash
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

1. After the GPU Operator deployment completes successfully, run the MPS tests:

```bash
$ export TEST_FEATURES="mps"
$ export TEST_LABELS='nvidia-ci,mps'  # Run MPS-specific tests
$ make run-tests
```

The MPS tests will use the existing GPU Operator deployment that was left in place from the previous test run. This ensures that the MPS tests can properly validate MPS functionality on an already configured GPU environment.

#### Test Suite Ordering:

The test framework ensures that the GPU Operator deployment tests run before MPS tests through Ginkgo's ordering mechanisms. If you need to add new MPS tests, make sure they are organized to run after the GPU Operator deployment by using proper labeling and ordering in your test files.

#### Cleanup:

After completing the MPS tests, you may want to clean up all resources by running:

```bash
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,cleanup'
$ export NVIDIAGPU_CLEANUP=true
$ make run-tests
```

This will remove all resources created by both the GPU Operator deployment and MPS tests.

### Testing MIG with GPU Operator

To test the Multi-Instance GPU (MIG) functionality, you need to first deploy the GPU Operator and then run the MIG tests without cleaning up the GPU Operator deployment between test suites.

It is recommended to execute the runner script through the `make run-tests` make target.

#### Steps to run MIG tests:

1. Run mig testcases (single-mig and mixed-mig) after nvidia-ci on any cluster, while selecting single.mig.profile=1 and 2 time-slicing pods, which do not run at the same time because their sum would exceed the maximum number of time-slices (8).

```bash
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-gpu-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu,single-mig,mixed-mig,time-slicing'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_CLEANUP=false
$ make run-tests ARGS="-- --single.mig.profile=1 --time.slicing.instances=3,8"
```

1. Running only MIG testcases on an existing cluster which has GPU operator installed,

e.g. after executing step 1. MIG testcase(s) can be used from either nvidiagpu or
mig package. MIG is used in this example. In the other case, use `TEST_FEATURES="nvidiagpu"`
to execute the testcase from nvidiagpu package.
With these MIG parameters single-mig testcase would choose a random MIG profile (as the single.mig.profile is not
included) , mixed-mig testcase would use 1 instance amount for A100 GPU (1x 1g.5gb, 1x 2g.10gb and 1x 3g.20gb,
leaving the second profile 1g.10gb unused).
mixed-mig testcase would wait 35 seconds between the pods launching with mixed.mig.pod-delay parameter
time-slicing testcase would launch 8 pods, with 1 time-slice each, allowing them to be executed at the same time
You can deliver the ginkgo cli parameter using ARGS after the "make run-tests"

```bash
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-gpu-logs-dir
$ export TEST_FEATURES="mig"
$ export TEST_LABELS='single-mig,mixed-mig,time-slicing'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_CLEANUP=false
$ make run-mig-tests ARGS="-- --mixed.mig.instances='1,0,1,1' --mixed.mig.pod-delay=35 --time.slicing.instances=1,1,1,1,1,1,1,1"
```



#### Cleanup:

If the GPU operator and gpu burn pod needs to be cleaned up, just set the cleanup parameter to true
in the last execution of either steps 1 or 2

```bash
$ export NVIDIAGPU_CLEANUP=true
```

### Testing Time-Slicing with GPU Operator

To test GPU time-slicing, deploy the GPU Operator first, then run the time-slicing tests.
The test creates a device plugin ConfigMap with time-slicing configuration, deploys a
ClusterPolicy referencing it, and validates that multiple CUDA workloads run concurrently
on a single time-sliced GPU.

#### Steps to run time-slicing tests:

1. Deploy the GPU Operator with cleanup disabled (same as MPS step 1 above)
2. Run the time-slicing tests:
```bash
$ export TEST_FEATURES="timeslicing"
$ export TEST_LABELS='nvidia-ci,timeslicing'
$ make run-tests
```

### Examples of Testing GPU Operator end-to-end

Example running the end-to-end GPU Operator test case:

```bash
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-ci-gpu-logs-dir
$ export TEST_FEATURES="nvidiagpu"
$ export TEST_LABELS='nvidia-ci,gpu,single-mig,mixed-mig,time-slicing'
$ export TEST_TRACE=true
$ export VERBOSE_LEVEL=100
$ export NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE="g4dn.xlarge"
$ export NVIDIAGPU_CATALOGSOURCE="certified-operators"
$ export NVIDIAGPU_SUBSCRIPTION_CHANNEL="v23.9"
$ make run-tests
Executing nvidiagpu test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="nvidia-ci,gpu,single-mig,mixed-mig,time-slicing" ./tests/nvidiagpu
```



### Examples of Testing GPU Operator upgrade

Example running the GPU Operator upgrade testcase (from v23.6 to v24.3) after the end-end testcase.
Note:  you must run the end-to-end testcase first to deploy a previous version, set NVIDIAGPU_CLEANUP=false,
and specify the channel to upgrade to NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL=v24.3, along with the label
'operator-upgrade' in TEST_LABELS.  Otherwise, the upgrade testcase will not be executed:

```bash
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



### Example of running nvidia-ci with custom parameters for NFD and GPU operators

Example running the end-to-end test case and creating custom catalogsources for NFD and GPU Operator packagmanifests
when missing from their default catalogsources.

```bash
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



### Example for end-to-end Network Operator testcase with Legacy SRIOV RDMA testcase

Example running the end-to-end Network Operator test case, with the Legacy SRIOV RDMA testcase.  
Note: both TEST_LABELS "deploy || rdma-legacy-sriov' are specified in examples below:

```bash
$ export KUBECONFIG=/path/to/kubeconfig
$ export DUMP_FAILED_TESTS=true
$ export REPORTS_DUMP_DIR=/tmp/nvidia-nno-ci-logs-dir
$ export TEST_FEATURES="nvidianetwork"
# To run the NNO deploy testcase followed by RDMA Shared Device testcase,
# set: export TEST_LABELS="deploy || rdma-shared-dev"
$ export TEST_LABELS="deploy || rdma-legacy-sriov"
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
$ export NVIDIANETWORK_RDMA_GPUDIRECT=true
# NVIDIANETWORK_RDMA_NETWORK_TYPE supported values are: "sriov", "shared-device"
$ export NVIDIANETWORK_RDMA_NETWORK_TYPE=sriov


$ make run-tests
Executing nvidiagpu test-runner script
scripts/test-runner.sh
ginkgo -timeout=24h --keep-going --require-suite -r -vv --trace --label-filter="deploy || rdma-legacy-sriov" ./tests/nvidianetwork
```

