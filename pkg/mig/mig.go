package mig

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"
	gpuburn "github.com/rh-ecosystem-edge/nvidia-ci/internal/gpu-burn"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	PodDelay int
)

func init() {
	// Register flags before Ginkgo parses them
	flag.IntVar(&PodDelay, "pod-delay", 0, "delay in seconds between pod creation on mixed-mig testcase")
}

// TestSingleMIGGPUBurn performs the GPU Burn test with single strategy MIG Configuration
// Check mig.capable label (label might not exist after preceding tests, but it should reappear as either true or false)
//
//	therefore have to use the wait.NodeLabelExists() function to check for the label and value
//	If the label is not found, skip the test
//	If the label is found and value is false, skip the test
//
// Clean up existing GPU workload resources, if any
// Read MIG parameters from environment variable, returns -1 for random selection
// Query MIG profiles from hardware and select one of them as a strategy label for the GPU node
// Set the strategy and config labels on the GPU node
// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready
// Waiting for mig.strategy=single label to be present on GPU nodes
// Pulling and updating ClusterPolicy, and waiting for the label to be present on GPU nodes
// Prepare the workload and deploy it (namespace, configmap, 1 single pod for one profile)
// After it has been running and finished, get the logs and analyze them
func TestSingleMIGGPUWorkload(nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig, burn *nvidiagpu.GPUBurnConfig,
	BurnImageName map[string]string, WorkerNodeSelector map[string]string, cleanupAfterTest bool) {
	// select one mig profile from the list of mig profiles
	var useMigProfile string // = "mig-1g.5gb"  // mig profiles are queried from the hardware
	var useMigIndex int      // will be set to random value after migCapabilities is populated
	var migCapabilities []MIGProfileInfo

	By("Check mig.capability on GPU nodes")
	err := wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.capable", "true", labels.Set(WorkerNodeSelector),
		nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error checking MIG capability on nodes: %v", err)

	// ***** Cleaning up previous GPU Burn resources
	By("Cleanup if necessary")
	CleanupWorkloadResources(burn)

	// Read MIG parameter from environment variable, returns -1 for random selection
	// Read Mixed MIG parameter from environment variable, returns slice of instance counts per profile, or default values
	// Query MIG capabilities and select MIG profile and index to be used later.
	// Select MIG profile and index to be used later
	By("Read NVIDIAGPU_SINGLE_MIG_PROFILE environment variable and select MIG profile")
	migStrategy := "single"
	migInstanceCounts := ReadMIGParameter(nvidiaGPUConfig.MIGInstances)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Parsed MIG instance counts: %v", migInstanceCounts)
	useMigIndex = ReadSingleMIGParameter(nvidiaGPUConfig.SingleMIGProfile)
	migCapabilities, useMigIndex = SelectMigProfile(WorkerNodeSelector, useMigIndex, migInstanceCounts)
	Expect(migCapabilities).ToNot(BeNil(), "SelectMigProfile did not return migCapabilities")
	_ = UpdateMIGCapabilities(migCapabilities, migInstanceCounts, migStrategy)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Updated MigCapabilities: %v", migCapabilities)

	// Pull existing ClusterPolicy
	By("Pull existing ClusterPolicy")
	pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy: %v", err)
	initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
	Expect(initialClusterPolicyResourceVersion).ToNot(BeEmpty(), "initialClusterPolicyResourceVersion is empty after pull ClusterPolicy")

	// Configure MIG strategy for the test
	By("Configuring MIG strategy in ClusterPolicy")
	clusterArch, err := configureMIGStrategy(pulledClusterPolicyBuilder, WorkerNodeSelector, nvidiagpuv1.MIGStrategySingle)
	Expect(err).ToNot(HaveOccurred(), "error configuring MIG strategy and getting cluster architecture: %v", err)

	// Set the MIG strategy and mig.config labels on GPU worker nodes
	By("Set the MIG strategy label on GPU worker nodes")
	useMigProfile = SetMIGLabelsOnNodes(migCapabilities, useMigIndex, WorkerNodeSelector, migStrategy)

	// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready
	// error is ignored in case of timeout, if the state transition from ready to notReady and back to ready.
	// It is acceptable to continue after timeout to notReady state if the following state is ready.
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be notReady after node label changes", nvidiagpu.ClusterPolicyNotReadyTimeout))
	_ = wait.ClusterPolicyNotReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyNotReadyCheckInterval, nvidiagpu.ClusterPolicyNotReadyTimeout)

	// Wait for ClusterPolicy to be ready. Changing labels will take a couple of minutes.
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout))
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready: %v", err)

	// Node labels are updated after ClusterPolicy is ready, it takes some time for them to appear.
	By("Check for MIG single strategy capability labels on GPU nodes")
	migSingleLabel := "nvidia.com/mig.strategy"
	expectedLabelValue := "single"
	err = wait.NodeLabelExists(inittools.APIClient, migSingleLabel, expectedLabelValue,
		labels.Set(WorkerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	// In this case test has to proceed even if the label is not found. Strategy will be changed later.
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label '%s' set to '%s'", migSingleLabel, expectedLabelValue)
	glog.V(gpuparams.Gpu10LogLevel).Infof("MIG single strategy label found, proceeding with test")

	defer func() {
		var wait bool
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer1 (set MIG labels to non-mig on GPU nodes)")
		// Check if test has already failed - if so, skip expensive ClusterPolicy wait
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			glog.V(gpuparams.GpuLogLevel).Infof("Test has already failed, skipping ClusterPolicy wait in cleanup")
			wait = false
		} else {
			wait = true
		}
		ResetMIGLabelsToDisabled(WorkerNodeSelector, wait)
	}()

	// Check and create test-gpu-burn namespace if it is missing
	By("Create test-gpu-burn namespace")
	gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, burn.Namespace)
	if !gpuBurnNsBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn namespace '%s'", burn.Namespace)
		_, err = gpuBurnNsBuilder.Create()
		Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
			"namespace '%s' : %v ", burn.Namespace, err)
	}

	// Create GPU Burn configmap in test-gpu-burn namespace
	By("Deploy GPU Burn configmap in test-gpu-burn namespace")
	configmapBuilder := configmap.NewBuilder(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if !configmapBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn configmap '%s' in namespace '%s'", burn.ConfigMapName, burn.Namespace)
		_, err = gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
		Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)
	}

	// Verify that the GPU Burn configmap was created.
	By(" Pulling the created GPU Burn configmap")
	configmapBuilder, err = configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
		"namespace '%s': %v", burn.ConfigMapName, burn.Namespace, err)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer2 (configmapBuilder deleting configmap)")
		if cleanupAfterTest {
			err := configmapBuilder.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn configmap: %v", err)
			err = configmapBuilder.WaitUntilDeleted(15 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "Error waiting for gpu-burn configmap to be deleted: %v", err)
		}
	}()

	// Deploy GPU Burn pod with MIG single strategy configuration
	By("Deploy gpu-burn pod with MIG configuration in test-gpu-burn namespace")
	glog.V(gpuparams.Gpu10LogLevel).Infof("Creating image '%s' pod with MIG profile '%s' in burn: '%s' requesting %d instances",
		BurnImageName[clusterArch], useMigProfile, burn, migCapabilities[useMigIndex].Total)
	// Using total, because nvidia-smi Available field may sometimes be zero (e.g. pods are running for some reason)
	// Using migCapabilities[useMigIndex].MixedCnt could be used to restrict the number of instances to use,
	// but it would cause problems when both single-mig and mixed-mig testcases are run in the same test suite.
	instances := migCapabilities[useMigIndex].Total
	gpuMigPodPulled := DeployGPUWorkload(
		BurnImageName[clusterArch],
		burn.PodName,
		burn.Namespace,
		useMigProfile,
		instances,
		burn.PodLabel)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer3 (gpuMigPodPulled) Deleting gpu-burn pod")
		if cleanupAfterTest {
			_, err := gpuMigPodPulled.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pod: %v", err)
		}
	}()

	// Wait for GPU Burn pod to complete
	By(fmt.Sprintf("Wait for up to %s for gpu-burn pod with MIG to be in Running phase", nvidiagpu.BurnPodRunningTimeout))
	waitForGPUBurnPodToComplete(gpuMigPodPulled, burn.Namespace)

	// Getting the logs, using 0 as a multiplier for calculation of time since pod creation, as there is only one pod.
	By("Get the gpu-burn pod logs")
	gpuBurnMigLogs := GetGPUBurnPodLogs(gpuMigPodPulled, 0)

	// Check the logs for successful execution.
	By("Parse the gpu-burn pod logs and check for successful execution with MIG")
	CheckGPUBurnPodLogs(gpuBurnMigLogs, instances)

	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorGreen+colorBold, "Single MIG Test completed"))
}

// TestMixedMIGGPUWorkload performs the GPU Burn test with mixed strategy MIG Configuration
// Check mig.capable label
// Clean up existing GPU workload resources, if any
// Read Mixed MIG parameter from environment variable
// Query MIG capabilities and select MIG profiles to be used later.
// Read Mixed MIG strategy to be used (e.g. mixed or flavor based)
// Read the delay to be used between pod launches
// Pull existing ClusterPolicy
// Configure MIG strategy and set the label on GPU nodes
// Wait for quick ClusterPolicy state transition to notReady and back to ready
// Create namespace, configmap before starting creation of the pods
// Launch the GPU Burn pods in a loop for each requested profile with optional sleeping interval.
// Ensure the state of pods end up in Completed state
// After all pods are completed, get and check the logs for each pod.
func TestMixedMIGGPUWorkload(nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig, burn *nvidiagpu.GPUBurnConfig,
	BurnImageName map[string]string, WorkerNodeSelector map[string]string, cleanupAfterTest bool) {
	// Any combination of mig profiles can be selected, by default 2x 1g.5gb + 1x 2g.10gb + 1x 3g.20gb
	// The valid combination for A100 is 2x 1g.5gb + 1x 2g.10gb + 1x 3g.20gb
	// If so wished, 1x can be used insteady of 2x.
	var useMigIndex int // will be set to random value after migCapabilities is populated
	var migCapabilities []MIGProfileInfo

	By("Check mig.capability on GPU nodes")
	err := wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.capable", "true", labels.Set(WorkerNodeSelector),
		nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error checking MIG capability on nodes: %v", err)

	// ***** Cleaning up previous GPU Burn resources
	By("Cleanup if necessary")
	CleanupWorkloadResources(burn)

	// Read Mixed MIG parameter from environment variable, returns slice of instance counts per profile, or default values
	// Query MIG capabilities and select MIG profiles to be used later.
	By("Read NVIDIAGPU_MIG_INSTANCES environment variable and select MIG profile")
	migStrategy := "mixed"
	migInstanceCounts := ReadMIGParameter(nvidiaGPUConfig.MIGInstances)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Parsed MIG instance counts: %v", migInstanceCounts)
	useMigIndex = ReadSingleMIGParameter(nvidiaGPUConfig.SingleMIGProfile)
	migCapabilities, useMigIndex = SelectMigProfile(WorkerNodeSelector, useMigIndex, migInstanceCounts)
	Expect(migCapabilities).ToNot(BeNil(), "SelectMigProfile did not return migCapabilities")
	SumOfMixedCnt := UpdateMIGCapabilities(migCapabilities, migInstanceCounts, migStrategy)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Updated MigCapabilities: %v", migCapabilities)
	// Requesting for specific MIG profile and requesting 0 instances is a dry run (just changing labels etc) without any pod creation.
	if SumOfMixedCnt == 0 {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s strategy=%s instances=%s count=%d", colorLog(colorGreen+colorBold,
			"Dry run, no pod creation because of parameter settings:"),
			migStrategy, nvidiaGPUConfig.MIGInstances, SumOfMixedCnt)
	}

	// Read the delay to be used between pod launches
	// This can be used to have the pods running completely, mostly, slightly or not overlapping.
	By("Read NVIDIAGPU_DELAY_BETWEEN_PODS environment variable and set delay between pods")
	delayBetweenPods := ReadDelayBetweenPods(nvidiaGPUConfig.DelayBetweenPods)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Read Delay between pods: %v seconds", delayBetweenPods)

	// Pull existing ClusterPolicy
	By("Pull existing ClusterPolicy")
	pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy: %v", err)
	initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
	Expect(initialClusterPolicyResourceVersion).ToNot(BeEmpty(), "initialClusterPolicyResourceVersion is empty after pull ClusterPolicy")

	// Configure MIG strategy for the test in ClusterPolicy
	By("Configuring MIG strategy in ClusterPolicy")
	clusterArch, err := configureMIGStrategy(pulledClusterPolicyBuilder, WorkerNodeSelector, nvidiagpuv1.MIGStrategyMixed)
	Expect(err).ToNot(HaveOccurred(), "error configuring MIG strategy and getting cluster architecture: %v", err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Cluster architecture: %v", clusterArch)

	// Set MIG mixed strategy label on GPU nodes
	// return values is irrelevant on mixed strategy testcase.
	By("Set MIG mixed strategy label")
	_ = SetMIGLabelsOnNodes(migCapabilities, useMigIndex, WorkerNodeSelector, migStrategy)

	// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready, timeout is one expected outcome.
	// Checking that mig.config.state gets into success state
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be notReady after node label changes", nvidiagpu.ClusterPolicyNotReadyTimeout))
	_ = wait.ClusterPolicyNotReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyNotReadyCheckInterval, nvidiagpu.ClusterPolicyNotReadyTimeout)
	err = CheckMigConfigState(WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label 'nvidia.com/mig.config.state' set to 'success'")

	// Wait for ClusterPolicy to be ready. Changing labels will take a couple of minutes.
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout))
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready: %v", err)
	err = CheckMigConfigState(WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label 'nvidia.com/mig.config.state' set to 'success'")

	// Waiting for the mig.strategy=mixed label to be present on GPU nodes
	By("Check for MIG mixed strategy capability labels on GPU nodes")
	migSingleLabel := "nvidia.com/mig.strategy"
	expectedLabelValue := "mixed"
	err = wait.NodeLabelExists(inittools.APIClient, migSingleLabel, expectedLabelValue,
		labels.Set(WorkerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label '%s' set to '%s'", migSingleLabel, expectedLabelValue)
	glog.V(gpuparams.Gpu10LogLevel).Infof("MIG mixed strategy label found, proceeding with test")

	// Checking that mig.config.state gets into success state
	err = CheckMigConfigState(WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label 'nvidia.com/mig.config.state' set to 'success'")

	defer func() {
		var wait bool
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer1 (set MIG labels to non-mig on GPU nodes)")
		// Check if test has already failed - if so, skip expensive ClusterPolicy wait
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			glog.V(gpuparams.GpuLogLevel).Infof("Test has already failed, skipping ClusterPolicy wait in cleanup")
			wait = false
		} else {
			wait = true
		}
		ResetMIGLabelsToDisabled(WorkerNodeSelector, wait)
	}()

	// Check and create test-gpu-burn namespace if it is missing
	By("Create test-gpu-burn namespace")
	gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, burn.Namespace)
	if !gpuBurnNsBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn namespace '%s'", burn.Namespace)
		_, err = gpuBurnNsBuilder.Create()
		Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
			"namespace '%s' : %v ", burn.Namespace, err)
	}

	// Create GPU Burn configmap in test-gpu-burn namespace
	By("Deploy GPU Burn configmap in test-gpu-burn namespace")
	configmapBuilder := configmap.NewBuilder(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if !configmapBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn configmap '%s' in namespace '%s'", burn.ConfigMapName, burn.Namespace)
		_, err = gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
		Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)
	}

	// Verify that the GPU Burn configmap was created.
	By(" Pulling the created GPU Burn configmap")
	configmapBuilder, err = configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
		"namespace '%s': %v", burn.ConfigMapName, burn.Namespace, err)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer2 (configmapBuilder deleting configmap)")
		if cleanupAfterTest {
			err := configmapBuilder.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn configmap: %v", err)
			err = configmapBuilder.WaitUntilDeleted(15 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "Error waiting for gpu-burn configmap to be deleted: %v", err)
		}
	}()

	// Deploy GPU Burn pod with MIG mixed strategy configuration in a loop for each profile
	// Collect all created MIG burn pods so they can be cleaned up later
	// Optional sleeping between pod launches to have control on the pods running at the same time or not.
	By("Deploy gpu-burn pod with MIG configuration in test-gpu-burn namespace")
	var migPodInfo []MigPodInfo
	for i, cap := range migCapabilities {
		if cap.MixedCnt > 0 {
			glog.V(gpuparams.Gpu10LogLevel).Infof("Creating image '%s' pod with MIG mixed strategy in burn: '%s' requesting %d instances",
				BurnImageName[clusterArch], burn, migCapabilities[i].MixedCnt)
			burn.PodName = fmt.Sprintf("gpu-burn-pod-%d-of-mig-%s", migCapabilities[i].MixedCnt, migCapabilities[i].MigName)
			gpuMigPodPulled := DeployGPUWorkload(
				BurnImageName[clusterArch],
				burn.PodName,
				burn.Namespace,
				migCapabilities[i].MigName,
				migCapabilities[i].MixedCnt,
				burn.PodLabel)
			migPodInfo = append(migPodInfo, MigPodInfo{
				PodName:        burn.PodName,
				Namespace:      burn.Namespace,
				Pod:            gpuMigPodPulled,
				MigProfileInfo: migCapabilities[i],
			})
			time.Sleep(time.Duration(delayBetweenPods) * time.Second)
		}
	}

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer3 (Deleting gpu-burn pods)")
		if cleanupAfterTest {
			for _, podBuilder := range migPodInfo {
				_, err := podBuilder.Pod.Delete()
				Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pod: %v", err)
			}
		}
	}()

	// Ensure all pods get into Running state, looping through the previously created & collected pods.
	// Competed status is accepted as well in the isRunning function.
	By("Ensure all pods get into Running state")
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Ensure all pods get into Running state"))
	for _, podInfo := range migPodInfo {
		if podInfo.Pod.Exists() {
			isRunning(podInfo.Pod, burn.Namespace)
		}
	}

	// Waiting until the pods are completed. Depending on the delay between the pods, this may take some time in each iteration.
	By("Wait for GPU Burn pods to complete")
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Wait for GPU Burn pods to complete"))
	for _, podInfo := range migPodInfo {
		if podInfo.Pod.Exists() {
			isCompleted(podInfo.Pod, burn.Namespace)
		}
	}

	// After all pods are completed, get and check the logs for each pod.
	// The log retrieval has a validity time period. Second parameter is a multiplier to calculate the validity time.
	By("Get and check the gpu-burn pod logs")
	maxPodIndex := len(migPodInfo) - 1
	i := 0
	for _, podInfo := range migPodInfo {
		if podInfo.Pod.Exists() {
			// Second parameter guides on how old logs can be retrieved.
			gpuBurnMigLogs := GetGPUBurnPodLogs(podInfo.Pod, maxPodIndex-i)
			CheckGPUBurnPodLogs(gpuBurnMigLogs, podInfo.MigProfileInfo.MixedCnt)
		}
		i++
	}
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorGreen+colorBold, "Mixed MIG Test completed"))
}

// CleanupGPUOperatorResources performs cleanup of GPU Operator resources
// It checks if cleanup should run based on cleanupAfterTest and cleanup label
func CleanupGPUOperatorResources(cleanupAfterTest bool, burnNamespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Cleanup GPU Operator Resources"))
	if !cleanupAfterTest {
		glog.V(gpuparams.GpuLogLevel).Infof("Cleanup is disabled, skipping GPU operator cleanup")
		return
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Starting cleanup of GPU Operator Resources")

	cleanupClusterPolicy()
	cleanupCSV()
	cleanupSubscription()
	cleanupOperatorGroup()
	cleanupGPUOperatorNamespace()
	cleanupGPUBurnNamespace(burnNamespace)

	glog.V(gpuparams.GpuLogLevel).Infof("Completed cleanup of GPU Operator Resources")
}

// cleanupClusterPolicy deletes the ClusterPolicy resource if it exists
func cleanupClusterPolicy() {
	By("Deleting ClusterPolicy")
	clusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	if err == nil && clusterPolicyBuilder.Exists() {
		_, err := clusterPolicyBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting ClusterPolicy: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy deleted successfully")
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy not found or already deleted")
	}
}

// cleanupCSV deletes the ClusterServiceVersion resources if they exist
func cleanupCSV() {
	By("Deleting CSV")
	csvList, err := olm.ListClusterServiceVersion(inittools.APIClient, nvidiagpu.SubscriptionNamespace)
	if err == nil && len(csvList) > 0 {
		for _, csv := range csvList {
			if strings.Contains(csv.Definition.Name, "gpu-operator") {
				err := csv.Delete()
				Expect(err).ToNot(HaveOccurred(), "Error deleting CSV: %v", err)
				glog.V(gpuparams.GpuLogLevel).Infof("CSV %s deleted successfully", csv.Definition.Name)
			}
		}
	}
}

// cleanupSubscription deletes the Subscription resource if it exists
func cleanupSubscription() {
	By("Deleting Subscription")
	subBuilder, err := olm.PullSubscription(inittools.APIClient, nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace)
	if err == nil && subBuilder.Exists() {
		err := subBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting Subscription: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Subscription deleted successfully")
	}
}

// cleanupOperatorGroup deletes the OperatorGroup resource if it exists
func cleanupOperatorGroup() {
	By("Deleting OperatorGroup")
	ogBuilder, err := olm.PullOperatorGroup(inittools.APIClient, nvidiagpu.OperatorGroupName, nvidiagpu.SubscriptionNamespace)
	if err == nil && ogBuilder.Exists() {
		err := ogBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting OperatorGroup: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("OperatorGroup deleted successfully")
	}
}

// cleanupGPUOperatorNamespace deletes the GPU Operator namespace if it exists
func cleanupGPUOperatorNamespace() {
	By("Deleting GPU Operator Namespace")
	nsBuilder := namespace.NewBuilder(inittools.APIClient, nvidiagpu.SubscriptionNamespace)
	if nsBuilder.Exists() {
		err := nsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", nvidiagpu.SubscriptionNamespace)
	}
}

// cleanupGPUBurnNamespace deletes the GPU Burn namespace if it exists
func cleanupGPUBurnNamespace(burnNamespace string) {
	By("Deleting GPU Burn Namespace")
	burnNsBuilder := namespace.NewBuilder(inittools.APIClient, burnNamespace)
	if burnNsBuilder.Exists() {
		err := burnNsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting burn namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", burnNamespace)
	}
}

// IsLabelInFilter checks if a specific label is present in the Ginkgo label filter from command line.
// Returns true if the label is found in the filter, false otherwise.
func IsLabelInFilter(label string) bool {
	filterQuery := GinkgoLabelFilter()
	glog.V(gpuparams.Gpu10LogLevel).Infof("Checking if label '%s' is present in Ginkgo label filter: %s", label, filterQuery)

	// If no filter is set, the label is not in the filter
	if filterQuery == "" {
		glog.V(gpuparams.Gpu10LogLevel).Infof("No label filter set, label '%s' is not in filter", label)
		return false
	}

	// Check if the label is present in the filter string
	// Use word boundaries to avoid partial matches (e.g., "single-mig" should not match "single-mig-test")
	// Simple check: label should appear as a whole word (comma-separated or at boundaries)
	labelInFilter := strings.Contains(filterQuery, label)
	if labelInFilter {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Label '%s' is present in Ginkgo label filter", label)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Label '%s' is not present in Ginkgo label filter", label)
	}
	return labelInFilter
}

// ShouldKeepOperator checks if the operator should be kept based on test labels and upgrade channel
func ShouldKeepOperator(labelsToCheck []string) bool {
	glog.V(gpuparams.Gpu100LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "ShouldKeepOperator"))

	// Get the label filter from Ginkgo command line
	filterQuery := GinkgoLabelFilter()
	specReport := CurrentSpecReport()
	currentLabels := specReport.Labels()

	// Log the labels present in the ginkgo command line before the for loop
	glog.V(gpuparams.Gpu100LogLevel).Infof("Ginkgo label filter from command line: %s", filterQuery)
	glog.V(gpuparams.Gpu100LogLevel).Infof("Current test labels from Ginkgo: %v", currentLabels)
	glog.V(gpuparams.Gpu100LogLevel).Infof("CurrentSpecReport: %v", currentLabels)

	// Check if test has any of these labels

	for _, label := range labelsToCheck {
		glog.V(gpuparams.Gpu100LogLevel).Infof("Checking if label %s is present in Ginkgo label filter", label)
		if strings.Contains(filterQuery, label) {
			glog.V(gpuparams.Gpu100LogLevel).Infof("Label %s is present in Ginkgo label filter", label)
			return true
		}
	}

	return false
}

// ReadSingleMIGParameter checks the SingleMIGProfile parameter and parses the MIG index if provided.
// It returns the parsed MIG index, or -1 if not set or invalid (i.e. contains no digits)
// -1 translates to random selection of MIG profile
func ReadSingleMIGParameter(singleMIGProfile string) int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check NVIDIAGPU_SINGLE_MIG_PROFILE parameter"))
	if singleMIGProfile == "" {
		glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_SINGLE_MIG_PROFILE" +
			" is not set, selecting it automatically")
		return -1
	}
	glog.V(gpuparams.Gpu10LogLevel).Infof("env variable NVIDIAGPU_SINGLE_MIG_PROFILE"+
		" is set to '%s', using it as requested MIG profile, if it is a valid number", singleMIGProfile)
	regex := regexp.MustCompile(`\d+`)
	matches := regex.FindStringSubmatch(singleMIGProfile)
	if len(matches) > 0 {
		useMigIndex, _ := strconv.Atoi(matches[0])
		return useMigIndex
	}
	return -1
}

// ReadMIGParameter checks the MixedMIGProfile parameter and parses the MIG instance counts if provided.
// It returns a slice of integers representing the number of instances for each MIG profile.
// If the parameter is not set, it returns the default values for A100 GPU [2,0,1,1,0,0].
// If the parameter is set, it parses all numbers from the string (comma or space separated) and returns them as a slice.
func ReadMIGParameter(MixedMIGProfile string) []int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check NVIDIAGPU_MIG_INSTANCES parameter"))
	defaults := []int{2, 0, 1, 1, 0, 0}
	if MixedMIGProfile == "" {
		glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_MIG_INSTANCES"+
			" is not set, using default values: %v", defaults)
		return defaults
	}
	glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_MIG_INSTANCES"+
		" is set to '%s', parsing it as requested MIG instance counts", MixedMIGProfile)

	// Extract all numbers from the string (handles comma-separated, space-separated, or mixed formats)
	regex := regexp.MustCompile(`\d+`)
	matches := regex.FindAllString(MixedMIGProfile, -1)

	if len(matches) > 0 {
		result := make([]int, 0, len(matches))
		for _, match := range matches {
			value, err := strconv.Atoi(match)
			if err == nil {
				result = append(result, value)
			}
		}
		if len(result) > 0 {
			glog.V(gpuparams.GpuLogLevel).Infof("Parsed MIG instance counts: %v", result)
			return result
		}
	}

	// If no valid numbers found, return default values
	glog.V(gpuparams.GpuLogLevel).Infof("No valid numbers found in NVIDIAGPU_MIG_INSTANCES, using default values %s", defaults)
	return defaults
}

// ReadMixedMIGStrategy checks the MixedMIGStrategy parameter and returns the MIG strategy.
// It returns the MIG strategy, or default value 'mixed' if not set.
func ReadMixedMIGStrategy(MixedMIGStrategy string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check parameter NVIDIAGPU_MIXED_MIG_STRATEGY"))
	if MixedMIGStrategy == "" {
		return "mixed"
	}
	return MixedMIGStrategy
}

// ReadDelayBetweenPods checks the DelayBetweenPods parameter and returns the delay between pods.
// ReadDelayBetweenPods checks the Ginkgo CLI parameter pod-delay and returns the delay between pods.
// Currently setting either will work and bigger value will be used.
// It returns the delay between pods, or 0 if not set.
func ReadDelayBetweenPods(delayBetweenPods int) int {
	podDelay := 0
	switch {
	case delayBetweenPods < 0:
		podDelay = 0
	case delayBetweenPods > 315:
		podDelay = 315
	default:
		podDelay = delayBetweenPods
	}

	switch {
	case PodDelay < 0:
		// Do nothing, value is already 0 or more
	case PodDelay > 315:
		// Exceeding value is reset to maximum value
		podDelay = 315
	case PodDelay > podDelay && PodDelay <= 315:
		podDelay = PodDelay
	default:
		// do nothing, value is already within the range and set accoring to delayBetweenPods
	}

	glog.V(gpuparams.Gpu10LogLevel).Infof("delay-between-pods %d PodDelay %d podDelay %d", delayBetweenPods, PodDelay, podDelay)
	return podDelay
}

// CleanupWorkloadResources cleans up existing GPU burn pods and configmaps, then waits for cleanup to complete.
func CleanupWorkloadResources(burn *nvidiagpu.GPUBurnConfig) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Cleaning up namespace and workload resources"))
	// Delete any existing gpu-burn pods with the label. There may be none.
	podList, err := pod.List(inittools.APIClient, burn.Namespace, metav1.ListOptions{LabelSelector: burn.PodLabel})
	if err == nil && len(podList) > 0 {
		glog.V(gpuparams.GpuLogLevel).Infof("Found %d gpu-burn pod(s) with label '%s'", len(podList), burn.PodLabel)
		for _, podBuilder := range podList {
			glog.V(gpuparams.GpuLogLevel).Infof("Deleting gpu-burn pod '%s'", podBuilder.Definition.Name)
			_, err = podBuilder.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting workload pod '%s': %v", podBuilder.Definition.Name, err)
		}
		// Wait for all pods to be deleted
		for _, podBuilder := range podList {
			err = podBuilder.WaitUntilDeleted(30 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "Error waiting for workload pod '%s' to be deleted: %v", podBuilder.Definition.Name, err)
		}
		glog.V(gpuparams.Gpu10LogLevel).Infof("All gpu-burn pods with label '%s' have been deleted", burn.PodLabel)
	} else if err != nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Error listing pods with label '%s': %v", burn.PodLabel, err)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("No gpu-burn pods found with label '%s'", burn.PodLabel)
	}

	// Delete the configmap if it exists
	existingConfigmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if err == nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Found gpu-burn configmap '%s' with: %v", burn.ConfigMapName, err)
		err = existingConfigmapBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting workload configmap: %v", err)
		err = existingConfigmapBuilder.WaitUntilDeleted(30 * time.Second)
		Expect(err).ToNot(HaveOccurred(), "Error waiting for workload configmap to be deleted: %v", err)
	}
}

// SelectMigProfile queries MIG profiles from hardware and selects/validates the MIG index.
// It returns the MIG capabilities and the selected/validated MIG index.
// If no MIG configurations are found, it calls Skip to skip the test.
func SelectMigProfile(WorkerNodeSelector map[string]string, useMigIndex int, migInstanceCounts []int) ([]MIGProfileInfo, int) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Query and select MIG profile"))

	_, migCapabilities, err := MIGProfiles(inittools.APIClient, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Error getting MIG capabilities: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("Found %d MIG configuration profiles", len(migCapabilities))
	for i, info := range migCapabilities {
		if len(migInstanceCounts) > i {
			glog.V(gpuparams.GpuLogLevel).Infof("Parameter requests %d instances, profile [%s] has %d/%d slices", migInstanceCounts[i], info.MigName, info.Available, info.Total)
		} else {
			glog.V(gpuparams.GpuLogLevel).Infof("  [%d] Profile name: %s, slices %d/%d", i, info.MigName, info.Available, info.Total)
		}
	}
	Expect(len(migCapabilities)).ToNot(BeZero(), "No MIG configurations available")

	// Select random index if not already set or if it is out of range
	if useMigIndex < 0 {
		useMigIndex = rand.Intn(len(migCapabilities))
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected random MIG index: %d (available: 0-%d)", useMigIndex, len(migCapabilities)-1)
	} else if useMigIndex >= len(migCapabilities) {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected MIG index %d is out of range (available: 0-%d), using last available index", useMigIndex, len(migCapabilities)-1)
		useMigIndex = len(migCapabilities) - 1
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected MIG index %d is within range (available: 0-%d), using it", useMigIndex, len(migCapabilities)-1)
	}

	return migCapabilities, useMigIndex
}

// CheckMigConfigState checks that mig.config.state gets into success state on GPU nodes.
// It returns an error if the label is not found or does not have the expected value.
func CheckMigConfigState(WorkerNodeSelector map[string]string) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check for MIG config state on GPU nodes"))
	migConfigStateLabel := "nvidia.com/mig.config.state"
	expectedLabelValue := "success"
	err := wait.NodeLabelExists(inittools.APIClient, migConfigStateLabel, expectedLabelValue,
		labels.Set(WorkerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("MIG config state (success) label found, proceeding with test")
	}
	return err
}

// UpdateMIGCapabilities updates the MixedCnt field of each MIGProfileInfo
// in migCapabilities with the corresponding values from migInstanceCounts.
// If migInstanceCounts has fewer elements than migCapabilities, only the available
// counts are applied. If migInstanceCounts has more elements, only the first
// len(migCapabilities) elements are used.
func UpdateMIGCapabilities(migCapabilities []MIGProfileInfo, migInstanceCounts []int, migStrategy string) int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("Updating MIG capabilities MixedCnt with instance counts: %v", migInstanceCounts)

	UsedSlices := 0
	UsedMemory := 0
	MaxSlices := 0
	MaxMemory := 0
	addtext := ""
	SumOfMixedCnt := 0
	// Update MixedCnt for each profile
	for i := 0; i < len(migCapabilities); i++ {
		// If migInstanceCounts has fewer elements, assume missing values are zero
		var instanceCount int
		if i < len(migInstanceCounts) {
			instanceCount = migInstanceCounts[i]
		} else {
			instanceCount = 0
			addtext = "assumed"
		}
		migCapabilities[i].MixedCnt = instanceCount
		SumOfMixedCnt += instanceCount
		UsedSlices += migCapabilities[i].SliceUsage * instanceCount
		UsedMemory += migCapabilities[i].MemUsage * instanceCount
		if MaxSlices < migCapabilities[i].SliceUsage {
			MaxSlices = migCapabilities[i].SliceUsage
		}
		if MaxMemory < migCapabilities[i].MemUsage {
			MaxMemory = migCapabilities[i].MemUsage
		}
		glog.V(gpuparams.Gpu10LogLevel).Infof("Updated profile %d (%s) MixedCnt to %s %d",
			i, migCapabilities[i].MigName, addtext, instanceCount)
	}
	glog.V(gpuparams.Gpu10LogLevel).Infof("UsedSlices: %d, UsedMemory: %d, MaxSlices: %d, MaxMemory: %d", UsedSlices, UsedMemory, MaxSlices, MaxMemory)
	if UsedSlices > MaxSlices && migStrategy == "mixed" {
		glog.V(gpuparams.Gpu10LogLevel).Infof(colorRed + "Warning: UsedSlices is greater than MaxSlices, case may fail" + colorReset)
	}
	if UsedMemory > MaxMemory && migStrategy == "mixed" {
		glog.V(gpuparams.Gpu10LogLevel).Infof(colorRed + "Warning: UsedMemory is greater than MaxMemory, case may fail" + colorReset)
	}

	// Log if there are more profiles than instance counts
	if len(migCapabilities) > len(migInstanceCounts) {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Warning: %d MIG profiles found but only %d instance counts provided. "+
			"Remaining profiles will have MixedCnt=0", len(migCapabilities), len(migInstanceCounts))
	}
	return SumOfMixedCnt
}

// setMIGLabelsOnNodes sets MIG strategy and configuration labels on GPU worker nodes.
// It returns the MIG profile flavor that was set.
func SetMIGLabelsOnNodes(migCapabilities []MIGProfileInfo, useMigIndex int, WorkerNodeSelector map[string]string, migStrategy string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Set MIG labels on nodes"))
	var MigProfile, useMigProfile string

	switch migStrategy {
	case "single":
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG single strategy label on GPU worker nodes from %d entry of the list (profile: %s with %d/%d slices)",
			useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
		MigProfile = "all-" + migCapabilities[useMigIndex].MigName
		useMigProfile = migCapabilities[useMigIndex].Flavor
	case "mixed":
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG mixed strategy label on GPU worker nodes from %d entry of the list (profile: %s with %d/%d slices)",
			useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
		MigProfile = "all-balanced"
		useMigProfile = "mixed"
	default:
		// mig strategy is initially for mixed strategy, so by default using mixed strategy on any other case.
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG strategy label on GPU worker nodes from %d entry of the list (profile: %s with %d/%d slices)",
			useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
		MigProfile = migStrategy
		migStrategy = "mixed"
		useMigProfile = "mixed"
	}

	// use first mig profile from the list, unless specified otherwise
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
	for _, nodeBuilder := range nodeBuilders {
		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG %s strategy label on node '%s' (overwrite=true)", migStrategy, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.strategy", migStrategy)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG %s strategy label on node '%s'", migStrategy, nodeBuilder.Definition.Name)

		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG configuration label %s on node '%s' (overwrite=true)", MigProfile, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", MigProfile)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG configuration label on node '%s' with %s", nodeBuilder.Definition.Name, MigProfile)
	}

	return useMigProfile
}

// ResetMIGLabelsToDisabled sets MIG strategy and configuration labels to "all-disabled" on GPU worker nodes.
// If waitForReady is true, it waits for ClusterPolicy to be ready after setting the labels.
func ResetMIGLabelsToDisabled(WorkerNodeSelector map[string]string, waitForReady bool) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Reset MIG labels to disabled"))
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
	for _, nodeBuilder := range nodeBuilders {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG configuration label to 'all-disabled' on node '%s' (overwrite=true)", nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", "all-disabled")
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.Gpu10LogLevel).Infof("Successfully set MIG configuration label on node '%s'", nodeBuilder.Definition.Name)
		// Nitpick comment: Deleting strategy label does not help, it reappears after a while on its own
	}

	if !waitForReady {
		glog.V(gpuparams.GpuLogLevel).Infof("Skipping ClusterPolicy wait (test may have failed)")
		return
	}

	// Wait for ClusterPolicy to be notReady
	_ = wait.ClusterPolicyNotReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyNotReadyCheckInterval, nvidiagpu.ClusterPolicyNotReadyTimeout)

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterPolicy to be ready after setting MIG node labels")
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready after node label changes: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy is ready after node label changes")
}

// updateAndWaitForClusterPolicyWithMIG updates ClusterPolicy with MIG configuration, waits for it to be ready, and logs the results.
func updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder *nvidiagpu.Builder, WorkerNodeSelector map[string]string, migStrategy nvidiagpuv1.MIGStrategy) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Update and wait for ClusterPolicy with MIG configuration"))
	updatedClusterPolicyBuilder, err := pulledClusterPolicyBuilder.Update(true)

	Expect(err).ToNot(HaveOccurred(), "error updating ClusterPolicy with MIG configuration: %v", err)

	By("Capturing updated clusterPolicy ResourceVersion")
	updatedClusterPolicyResourceVersion := updatedClusterPolicyBuilder.Object.ResourceVersion
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Updated ClusterPolicy resourceVersion is '%s'", updatedClusterPolicyResourceVersion)

	glog.V(gpuparams.Gpu10LogLevel).Infof(
		"After updating ClusterPolicy, MIG strategy is now '%v'",
		updatedClusterPolicyBuilder.Definition.Spec.MIG.Strategy)

	err = wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.strategy", string(migStrategy), labels.Set(WorkerNodeSelector),
		nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error checking MIG capability on nodes: %v", err)

	By("Pull the ready ClusterPolicy with MIG configuration from cluster")
	pulledMIGReadyClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: %v",
		nvidiagpu.ClusterPolicyName, err)

	migReadyJSON, err := json.MarshalIndent(pulledMIGReadyClusterPolicy, "", " ")
	Expect(err).ToNot(HaveOccurred(), "error marshalling ClusterPolicy with MIG into json: %v", err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("The ClusterPolicy with MIG configuration has name: %v",
		pulledMIGReadyClusterPolicy.Definition.Name)
	glog.V(gpuparams.GpuLogLevel).Infof("The ClusterPolicy with MIG configuration marshalled "+
		"in json: %v", string(migReadyJSON))
}

// configureMIGStrategy configures MIG strategy in ClusterPolicy and retrieves cluster architecture.
// It sets the MIG strategy to the provided value, updates the ClusterPolicy, and then gets the cluster architecture
// from the first GPU enabled worker node.
func configureMIGStrategy(
	pulledClusterPolicyBuilder *nvidiagpu.Builder,
	WorkerNodeSelector map[string]string,
	migStrategy nvidiagpuv1.MIGStrategy) (string, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Configure MIG strategy and get cluster architecture"))
	glog.V(gpuparams.Gpu10LogLevel).Infof(
		"Setting ClusterPolicy MIG strategy to '%s'", migStrategy)

	currentMigStrategy := pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Current MIG strategy is '%s', updating to '%s'",
		currentMigStrategy, migStrategy)
	pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy = migStrategy
	updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder, WorkerNodeSelector, migStrategy)

	By(fmt.Sprintf("Getting cluster architecture from nodes with WorkerNodeSelector: %v", WorkerNodeSelector))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Getting cluster architecture from nodes with "+
		"WorkerNodeSelector: %v", WorkerNodeSelector)
	clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Error getting cluster architecture: %v", err)
	return clusterArch, nil
}

// creates and deploys a GPU burn pod with MIG configuration,
// then retrieves it from the cluster. It returns the pulled pod builder for further operations.
// For various reasons, the pod names are used instead of gpu-burn-app label.
func DeployGPUWorkload(
	imageName, podName, namespace, useMigProfile string,
	migInstanceCount int,
	podLabel string) *pod.Builder {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Deploy GPU burn pod with MIG configuration and pull"))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Creating pod with MIG profile '%s' requesting %d instances",
		useMigProfile, migInstanceCount)

	gpuBurnMigPod, err := gpuburn.CreateGPUBurnPodWithMIG(inittools.APIClient, podName, namespace,
		imageName, useMigProfile, migInstanceCount, nvidiagpu.BurnPodCreationTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error creating gpu burn pod with MIG: %v", err)

	_, err = inittools.APIClient.Pods(gpuBurnMigPod.Namespace).Create(context.TODO(), gpuBurnMigPod,
		metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred(), "Error creating gpu-burn '%s' with MIG in "+
		"namespace '%s': %v", gpuBurnMigPod.Name, gpuBurnMigPod.Namespace, err)

	glog.V(gpuparams.Gpu10LogLevel).Infof("The created gpuBurnMigPod has name: %s has status: %v",
		gpuBurnMigPod.Name, gpuBurnMigPod.Status)

	gpuMigPodPulled, err := pod.Pull(inittools.APIClient, gpuBurnMigPod.Name, namespace)
	Expect(err).ToNot(HaveOccurred(), "error pulling gpu-burn pod from "+
		"namespace '%s': %v", namespace, err)

	return gpuMigPodPulled
}

// waitForGPUBurnPodToComplete waits for the GPU burn pod to reach Running phase,
// then waits for it to complete and reach Succeeded phase.
func waitForGPUBurnPodToComplete(gpuMigPodPulled *pod.Builder, namespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Wait for GPU burn pod to complete"))
	err := gpuMigPodPulled.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.BurnPodRunningTimeout)
	Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod with MIG in "+
		"namespace '%s' to go to Running phase: %v", namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG now in Running phase")

	glog.V(gpuparams.Gpu10LogLevel).Infof("Wait for up to %s for gpu-burn pod to complete", nvidiagpu.BurnPodSuccessTimeout)
	err = gpuMigPodPulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.BurnPodSuccessTimeout)

	Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' with MIG in "+
		"namespace '%s' to go Succeeded phase/Completed status: %v", gpuMigPodPulled.Definition.Name, gpuMigPodPulled.Definition.Namespace, err)
}

// logPodEvents logs events related to a specific pod in the given namespace.
// This is used to give more info about the pod when it exists, but it is in unexpected state.
func logPodEvents(podName, namespace string) {
	events, err := inittools.APIClient.Events(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Pod", podName),
	})
	if err != nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Failed to retrieve events for pod %s in namespace %s: %v", podName, namespace, err)
		return
	}

	if len(events.Items) == 0 {
		glog.V(gpuparams.Gpu10LogLevel).Infof("No events found for pod %s in namespace %s", podName, namespace)
		return
	}

	glog.V(gpuparams.Gpu10LogLevel).Infof("Events for pod %s in namespace %s:", podName, namespace)
	for _, event := range events.Items {
		glog.V(gpuparams.Gpu10LogLevel).Infof("  [%s] %s: %s - %s",
			event.LastTimestamp.Format(time.RFC3339),
			colorLog(colorRed+colorBold, event.Type),
			event.Reason,
			event.Message)
	}
}

// isRunning checks and waits for the GPU burn pod to reach the Running phase.
// It first checks it quickly and if necessary, it waits for it to reach the Running phase.
// Log validation ensures that the logs are from the pod that was created at the start of the test.
func isRunning(GpuPod *pod.Builder, namespace string) {
	// This is to avoid waiting, if the pod is already in Running or Succeeded phase.
	// If pod was Completed (or Running) already, there's no need to wait.
	// Avoiding the timeout in case it is Completed already is preferred.
	_, err := pod.Pull(inittools.APIClient, GpuPod.Definition.Name, namespace)
	Expect(err).ToNot(HaveOccurred(), "Pod %s does not exist in namespace %s with error: %v", GpuPod.Definition.Name, namespace, err)
	if GpuPod.Object.Status.Phase == corev1.PodRunning || GpuPod.Object.Status.Phase == corev1.PodSucceeded {
		return
	}
	// Waiting for the pod to reach Running phase, if it was not already.
	// If the pod is left in Pending state, timeout will occur.
	err = GpuPod.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.BurnPodRunningTimeout)
	if err != nil {
		// pod exists, but is not running
		// Using pod2 to avoid confusion with previous pod pull
		pod2, _ := pod.Pull(inittools.APIClient, GpuPod.Definition.Name, namespace)
		glog.V(gpuparams.Gpu10LogLevel).Infof("Pod %s is likely Pending for some reason: %s (%s)",
			pod2.Definition.Name, pod2.Object.Status.Phase, pod2.Object.Status.Reason)
		logPodEvents(pod2.Definition.Name, namespace)
	}
	Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod with MIG in "+
		"namespace '%s' to go to Running phase: %v\n Pod is likely Pending for some reason", namespace, err)
}

// isCompleted checks if the GPU burn pod reaches the Completed phase.
func isCompleted(gpuMigPodPulled *pod.Builder, namespace string) {
	err := gpuMigPodPulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.BurnPodSuccessTimeout)
	Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod with MIG in "+
		"namespace '%s' to go to Completed phase: %v", namespace, err)
}

// GetGPUBurnPodLogs retrieves the logs from the GPU burn pod with MIG configuration.
// It returns the pod logs as a string.
// multiplier is used to calculate the time since pod creation to retrieve the logs (to ensure validity of the logs)
func GetGPUBurnPodLogs(gpuMigPodPulled *pod.Builder, multiplier int) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s %s", colorLog(colorCyan+colorBold, "Get GPU burn pod logs for:"), gpuMigPodPulled.Definition.Name)

	var BurnLogTimer time.Duration = 0

	// although multiplier is supposed to be positive integer, it's better to check for the negative as well.
	switch {
	case multiplier <= 0:
		BurnLogTimer = nvidiagpu.BurnLogCollectionPeriod
	case multiplier > 0:
		BurnLogTimer = nvidiagpu.BurnPodCreationTimeout + nvidiagpu.BurnLogCollectionPeriod*time.Duration(multiplier)
		glog.V(gpuparams.Gpu100LogLevel).Infof("Using BurnLogTimer: %v for log validation", BurnLogTimer)
	}
	gpuBurnMigLogs, err := gpuMigPodPulled.GetLog(BurnLogTimer, "gpu-burn-ctr")

	Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
		"from gpu burn namespace '%s': %v", gpuMigPodPulled.Definition.Name, gpuMigPodPulled.Definition.Namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Gpu-burn pod '%s' with MIG logs:\n%s",
		gpuMigPodPulled.Definition.Name, gpuBurnMigLogs)

	return gpuBurnMigLogs
}

// CheckGPUBurnPodLogs parses the GPU burn pod logs and validates that the execution
// was successful. It checks for "GPU X: OK" messages for each MIG instance and verifies
// that the processing completed successfully (100.0% proc'd).
func CheckGPUBurnPodLogs(gpuBurnMigLogs string, migInstanceCount int) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Parse and validate GPU burn pod logs with MIG configuration"))
	for i := 0; i < migInstanceCount; i++ {
		match1Mig := strings.Contains(gpuBurnMigLogs, fmt.Sprintf("GPU %d: OK", i))
		glog.V(gpuparams.Gpu10LogLevel).Infof("Checking if GPU %d: OK is present in logs: %v", i, match1Mig)
		Expect(match1Mig).ToNot(BeFalse(), "gpu-burn pod execution with MIG was FAILED for GPU %d", i)
	}
	match2Mig := strings.Contains(gpuBurnMigLogs, "100.0%  proc'd:")

	Expect(match2Mig).ToNot(BeFalse(), "gpu-burn pod execution with MIG was FAILED for not getting 100.0%")
	glog.V(gpuparams.Gpu10LogLevel).Infof("Gpu-burn pod execution with MIG configuration was successful")
}

func colorLog(color, message string) string {
	if !useColors {
		return message
	}
	return fmt.Sprintf("%s%s%s", color, message, colorReset)
}

// MIGCapabilities queries GPU hardware directly using nvidia-smi
// to discover MIG capabilities. This is a fallback when GFD labels are not available.
// Returns true if MIG is supported, along with available MIG instance profiles.
func MIGProfiles(apiClient *clients.Settings, nodeSelector map[string]string) (bool, []MIGProfileInfo, error) {
	nodeBuilder, err := nodes.List(apiClient, metav1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing nodes: %v", err)
	Expect(len(nodeBuilder)).ToNot(BeZero(), "no nodes found matching selector")

	// Get the first GPU node
	firstNode := nodeBuilder[0]
	nodeName := firstNode.Object.Name

	// Find a driver pod on this node to query hardware
	driverPods, err := apiClient.Pods("nvidia-gpu-operator").List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=nvidia-driver",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	Expect(err).ToNot(HaveOccurred(), "Error listing driver pods: %v", err)
	Expect(len(driverPods.Items)).ToNot(BeZero(), "No driver pods found on node %s", nodeName)

	driverPod := driverPods.Items[0]
	podName := driverPod.Name
	namespace := driverPod.Namespace

	// Query MIG capabilities using nvidia-smi
	// First, try to get MIG instance profiles directly (works even if MIG mode is not enabled)
	cmd := []string{"nvidia-smi", "mig", "-lgip"}
	glog.V(gpuparams.Gpu10LogLevel).Infof("oc rsh -n %s pod/%s %v %v %v", namespace, podName, cmd[0], cmd[1], cmd[2])
	profileOutput, err := ExecCmdInPod(apiClient, podName, namespace, cmd, 30*time.Second)
	Expect(err).ToNot(HaveOccurred(), "Error getting MIG profiles: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("Available MIG instance profiles: %s", profileOutput)
	// Parse profiles from output (e.g., "1g.5gb", "2g.10gb", etc.)
	profiles := parseMIGProfiles(profileOutput)
	for _, profile := range profiles {
		glog.V(gpuparams.GpuLogLevel).Infof("profile: %s with gpu_id: %d, slices: %d/%d, p2p: %s, sm:%d, dec: %d, enc: %d, CE=%d, JPEG=%d, OFA=%d, MixedCnt=%d, SliceUsage=%d, MemUsage=%d",
			profile.MigName, profile.GpuID, profile.SliceUsage, profile.Total, profile.P2P, profile.SM, profile.DEC, profile.ENC,
			profile.CE, profile.JPEG, profile.OFA, profile.MixedCnt, profile.SliceUsage, profile.MemUsage)
	}
	return true, profiles, nil
}

// Internal functions serving the external functions

// ExecCmdInPod executes a command (e.g. nvidia-smi mig -lgip) in a pod and returns the output
// If similar function is needed for other purposes, consider renaming
func ExecCmdInPod(apiClient *clients.Settings, podName, namespace string, command []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Pull the pod using the pod builder
	podBuilder, err := pod.Pull(apiClient, podName, namespace)
	Expect(err).ToNot(HaveOccurred(), "Error pulling pod %s/%s: %v", namespace, podName, err)
	Expect(podBuilder.Object.Status.Phase).To(BeEquivalentTo(corev1.PodRunning), "Pod %s/%s is not running (phase: %s)", namespace, podName, podBuilder.Object.Status.Phase)
	Expect(len(podBuilder.Object.Spec.Containers)).ToNot(BeZero(), "Pod %s/%s has no containers", namespace, podName)

	// Check container status
	containerName := podBuilder.Object.Spec.Containers[0].Name
	containerRunning := false
	for _, status := range podBuilder.Object.Status.ContainerStatuses {
		if status.Name == containerName {
			if status.Ready && status.State.Running != nil {
				containerRunning = true
				break
			}
		}
	}
	Expect(containerRunning).ToNot(BeFalse(), "container %s in pod %s/%s is not running (pod phase: %s)", containerName, namespace, podName, podBuilder.Object.Status.Phase)
	glog.V(gpuparams.GpuLogLevel).Infof("Executing command %v in pod %s/%s container %s with timeout %v", command, namespace, podName, containerName, timeout)

	// Execute command with timeout using goroutine and channel
	type result struct {
		buffer bytes.Buffer
		err    error
	}
	resultChan := make(chan result, 1)

	// Note: On timeout, the spawned goroutine continues until ExecCommand completes,
	// but its result is discarded. This is acceptable in test contexts.
	go func() {
		outputBuffer, err := podBuilder.ExecCommand(command, containerName)
		resultChan <- result{buffer: outputBuffer, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("command execution timed out after %v: %w", timeout, ctx.Err())
	case res := <-resultChan:
		Expect(res.err).ToNot(HaveOccurred(), "Error executing command %v in pod %s/%s container %s: %v", command, namespace, podName, containerName, res.err)
		outputStr := res.buffer.String()
		Expect(outputStr).ToNot(BeEmpty(), "Output from command %v in pod %s/%s container %s is empty", command, namespace, podName, containerName)
		glog.V(gpuparams.GpuLogLevel).Infof("Command executed successfully, output length: %d bytes", len(outputStr))
		return outputStr, nil
	}
}

// parseMIGProfiles parses MIG profile names from nvidia-smi mig -lgip output
// Handles formats like "MIG 1g.5gb", "MIG 1g.5gb+me", "1g.5gb", etc.
func parseMIGProfiles(output string) []MIGProfileInfo {
	var profiles []MIGProfileInfo
	// Regex to match MIG profile patterns from first line, e.g.:
	// |   0  MIG 1g.5gb          19     7/7        4.75       No     14     0     0   |
	// Captures: GPU, MIG, name, ID, available/total, memory, P2P, SM, DEC, ENC
	// NOTE: Available is zero when mig.strategy is single or mixed
	line1Regex := regexp.MustCompile(`\|\s+(\d+)\s+(MIG)\s+(\d+g\.\d+gb(?:\+[a-z]+)?)\s+(\d+)\s+(\d+)\/(\d+)\s+(\d+\.\d+)\s+(\w+)\s+(\d+)\s+(\d+)\s+(\d+)\s+\|`)
	// Regex to match second line with CE, JPEG, OFA, e.g:
	// |                                                               1     0     0   |
	line2Regex := regexp.MustCompile(`\|\s+(\d+)\s+(\d+)\s+(\d+)\s+\|`)
	excludeRegex := regexp.MustCompile(`\|\s+\d+\s+MIG\s+\d+g\.\d+gb\+me`)
	flavor := "gpu"
	exclude := true

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		matches := line1Regex.FindStringSubmatch(line)
		if len(matches) > 0 {
			exclude = excludeRegex.MatchString(line)
			// exclude if the +me is present
			if exclude {
				// no entry in the profile
				glog.V(gpuparams.Gpu100LogLevel).Infof("Line 1: Ignoring profile: %s with gpu_id: %d",
					matches[3], matches[1])
				continue
			} else {
				// Parse the fields, most of them are integers
				gpuID, _ := strconv.Atoi(matches[1])
				migID, _ := strconv.Atoi(matches[4])
				available, _ := strconv.Atoi(matches[5])
				total, _ := strconv.Atoi(matches[6])
				sm, _ := strconv.Atoi(matches[9])
				dec, _ := strconv.Atoi(matches[10])
				enc, _ := strconv.Atoi(matches[11])
				profile := MIGProfileInfo{
					GpuID:     gpuID,
					MigType:   matches[2],
					MigName:   matches[3],
					MigID:     migID,
					Available: available,
					Total:     total,
					Memory:    matches[7],
					P2P:       matches[8],
					SM:        sm,
					DEC:       dec,
					ENC:       enc,
					Flavor:    flavor,
				}
				profiles = append(profiles, profile)
				glog.V(gpuparams.Gpu100LogLevel).Infof("Line 1: found profile: %s with gpu_id: %d, slices: %d/%d, p2p: %s, sm:%d, dec: %d, enc: %d",
					profile.MigName, profile.GpuID, profile.Available, profile.Total, profile.P2P, profile.SM, profile.DEC, profile.ENC)
			}
			// Get the slice and memory usage to calculate resource usage later.
			nameRegex := regexp.MustCompile(`(\d+)g\.(\d+)gb`)
			nameMatches := nameRegex.FindStringSubmatch(line)
			if len(nameMatches) > 0 {
				sliceUsage, _ := strconv.Atoi(nameMatches[1])
				memUsage, _ := strconv.Atoi(nameMatches[2])
				profiles[len(profiles)-1].SliceUsage = sliceUsage
				profiles[len(profiles)-1].MemUsage = memUsage
			}
		}

		// Check for second line (CE, JPEG, OFA) - should immediately follow first line
		matches2 := line2Regex.FindStringSubmatch(line)
		if len(matches2) > 0 && len(profiles) > 0 {
			if exclude {
				// no entry in the profile
				exclude = false
				glog.V(gpuparams.Gpu100LogLevel).Infof("Line 2: Ignoring")
				continue
			} else {
				// Update the last profile with CE, JPEG, OFA values
				ce, _ := strconv.Atoi(matches2[1])
				jpeg, _ := strconv.Atoi(matches2[2])
				ofa, _ := strconv.Atoi(matches2[3])
				profiles[len(profiles)-1].CE = ce
				profiles[len(profiles)-1].JPEG = jpeg
				profiles[len(profiles)-1].OFA = ofa
				glog.V(gpuparams.Gpu100LogLevel).Infof("Line 2: updated profile %s with CE=%d, JPEG=%d, OFA=%d", profiles[len(profiles)-1].MigName, ce, jpeg, ofa)
			}
		}
	}
	Expect(len(profiles)).ToNot(BeZero(), "no profiles found")
	return profiles
}
