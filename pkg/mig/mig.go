package mig

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"

	// "os"
	"regexp"
	"strconv"
	"strings"
	"time"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuburn"
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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	// "sigs.k8s.io/yaml"
)

// TestSingleMIGGPUBurn performs the GPU Burn test with single strategy MIG Configuration
// Check mig.capable label (label might not exist after preceding tests, but it should reappear as either true or false)
//
//	therefore have to use the wait.NodeLabelExists() function to check for the label and value
//	If the label is not found, skip the test
//	If the label is found and value is false, skip the test
//
// Clean up existing GPU workload resources, if any
// Read MIG parameters from CLI parameter, returns -1 for random selection
// Query MIG profiles from hardware and select one of them as a strategy label for the GPU node
// Set the strategy and config labels on the GPU node
// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready
// Waiting for mig.strategy=single label to be present on GPU nodes
// Pulling and updating ClusterPolicy, and waiting for the label to be present on GPU nodes
// Prepare the workload and deploy it (namespace, configmap, 1 single pod for one profile)
// After it has been running and finished, get the logs and analyze them
func TestSingleMIGGPUWorkload(nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig, burn *nvidiagpu.GPUBurnConfig,
	burnImageName map[string]string, workerNodeSelector map[string]string, cleanupAfterTest bool) {
	// select one mig profile from the list of mig profiles
	var useMigProfile string // = "mig-1g.5gb"  // mig profiles are queried from the hardware
	var useMigIndex int      // will be set to random value after migCapabilities is populated
	var migCapabilities []MIGProfileInfo

	By("Check mig.capability on GPU nodes")
	err := wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.capable", "true", labels.Set(workerNodeSelector),
		nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error checking MIG capability on nodes: %v", err)

	// ***** Cleaning up previous GPU Burn resources
	By("Cleanup if necessary")
	CleanupWorkloadResources(burn)

	// Read MIG parameter from CLI parameter, returns -1 for random selection
	// Read Mixed MIG parameter from CLI parameter, returns slice of instance counts per profile, or default values
	// Query MIG capabilities and select MIG profile and index to be used later.
	// Select MIG profile and index to be used later
	By("Read single.mig.profile parameter and select MIG profile")
	migStrategy := MIGStrategySingle
	migInstanceCounts := ReadMIGParameter()
	glog.V(gpuparams.Gpu10LogLevel).Infof("Parsed MIG instance counts: %v", migInstanceCounts)
	useMigIndex = ReadSingleMIGParameter()
	migCapabilities, useMigIndex = SelectMigProfile(workerNodeSelector, useMigIndex, migInstanceCounts)
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
	clusterArch, err := configureMIGStrategy(pulledClusterPolicyBuilder, workerNodeSelector, nvidiagpuv1.MIGStrategySingle)
	Expect(err).ToNot(HaveOccurred(), "error configuring MIG strategy and getting cluster architecture: %v", err)

	// Set the single MIG strategy and mig.config labels on GPU worker nodes
	By("Set the MIG strategy label on GPU worker nodes")
	useMigProfile = SetMIGLabelsOnNodes(migCapabilities, useMigIndex, workerNodeSelector, migStrategy)

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
	expectedLabelValue := MIGStrategySingle
	err = wait.NodeLabelExists(inittools.APIClient, migSingleLabel, expectedLabelValue,
		labels.Set(workerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
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
		ResetMIGLabelsToDisabled(workerNodeSelector, wait)
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
		burnImageName[clusterArch], useMigProfile, burn, migCapabilities[useMigIndex].Total)
	// Using total, because nvidia-smi Available field may sometimes be zero (e.g. pods are running for some reason)
	// Using migCapabilities[useMigIndex].MixedCnt could be used to restrict the number of instances to use,
	// but it would cause problems when both single-mig and mixed-mig testcases are run in the same test suite.
	instances := migCapabilities[useMigIndex].Total
	gpuMigPodPulled := DeployGPUWorkload(
		burnImageName[clusterArch],
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
// Read Mixed MIG parameter from CLI parameter
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
	burnImageName map[string]string, workerNodeSelector map[string]string, cleanupAfterTest bool) {
	// Any combination of mig profiles can be selected, by default 2x 1g.5gb + 1x 2g.10gb + 1x 3g.20gb
	// The valid combination for A100 is 2x 1g.5gb + 1x 2g.10gb + 1x 3g.20gb
	// If so wished, 1x can be used insteady of 2x and 0x can be used instead of 1x or 2x.
	var useMigIndex int // will be set to random value after migCapabilities is populated
	var migCapabilities []MIGProfileInfo

	By("Check mig.capability on GPU nodes")
	err := wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.capable", "true", labels.Set(workerNodeSelector),
		nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error checking MIG capability on nodes: %v", err)

	// ***** Cleaning up previous GPU Burn resources
	By("Cleanup if necessary")
	CleanupWorkloadResources(burn)

	// Read Mixed MIG parameter from CLI parameter, returns slice of instance counts per profile, or default values
	// Query MIG capabilities and select MIG profiles to be used later.
	By("Read mixed.mig.instances parameter and select MIG profile")
	migStrategy := MIGStrategyMixed
	migInstanceCounts := ReadMIGParameter()
	glog.V(gpuparams.Gpu10LogLevel).Infof("Parsed MIG instance counts: %v", migInstanceCounts)
	useMigIndex = ReadSingleMIGParameter()
	migCapabilities, useMigIndex = SelectMigProfile(workerNodeSelector, useMigIndex, migInstanceCounts)
	Expect(migCapabilities).ToNot(BeNil(), "SelectMigProfile did not return migCapabilities")
	SumOfMixedCnt := UpdateMIGCapabilities(migCapabilities, migInstanceCounts, migStrategy)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Updated MigCapabilities: %v", migCapabilities)
	// Requesting for specific MIG profile and requesting 0 instances is a dry run (just changing labels etc) without any pod creation.
	if SumOfMixedCnt == 0 {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s strategy=%s instances=%s count=%d", colorLog(colorGreen+colorBold,
			"Dry run, no pod creation because of parameter settings:"),
			migStrategy, MigInstances, SumOfMixedCnt)
	}

	// Read the delay to be used between pod launches
	// This can be used to have the pods running completely, mostly, slightly or not overlapping.
	By("Read mixed.mig.pod-delay parameter and set delay between pods")
	delayBetweenPods := ReadDelayBetweenPods()
	glog.V(gpuparams.Gpu10LogLevel).Infof("Read Delay between pods: %v seconds", delayBetweenPods)

	// Pull existing ClusterPolicy
	By("Pull existing ClusterPolicy")
	pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy: %v", err)
	initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
	Expect(initialClusterPolicyResourceVersion).ToNot(BeEmpty(), "initialClusterPolicyResourceVersion is empty after pull ClusterPolicy")

	// Configure MIG strategy for the test in ClusterPolicy
	By("Configuring MIG strategy in ClusterPolicy")
	clusterArch, err := configureMIGStrategy(pulledClusterPolicyBuilder, workerNodeSelector, nvidiagpuv1.MIGStrategyMixed)
	Expect(err).ToNot(HaveOccurred(), "error configuring MIG strategy and getting cluster architecture: %v", err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Cluster architecture: %v", clusterArch)

	// Set MIG mixed strategy and mig.config labels on GPU nodes
	// return values is irrelevant on mixed strategy testcase.
	By("Set MIG mixed strategy label")
	_ = SetMIGLabelsOnNodes(migCapabilities, useMigIndex, workerNodeSelector, migStrategy)

	// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready, timeout is one expected outcome.
	// Checking that mig.config.state gets into success state
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be notReady after node label changes", nvidiagpu.ClusterPolicyNotReadyTimeout))
	_ = wait.ClusterPolicyNotReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyNotReadyCheckInterval, nvidiagpu.ClusterPolicyNotReadyTimeout)
	err = CheckMigConfigState(workerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label 'nvidia.com/mig.config.state' set to 'success'")

	// Wait for ClusterPolicy to be ready. Changing labels will take a couple of minutes.
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout))
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready: %v", err)
	err = CheckMigConfigState(workerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label 'nvidia.com/mig.config.state' set to 'success'")

	// Waiting for the mig.strategy=mixed label to be present on GPU nodes
	By("Check for MIG mixed strategy capability labels on GPU nodes")
	migSingleLabel := "nvidia.com/mig.strategy"
	expectedLabelValue := MIGStrategyMixed
	err = wait.NodeLabelExists(inittools.APIClient, migSingleLabel, expectedLabelValue,
		labels.Set(workerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label '%s' set to '%s'", migSingleLabel, expectedLabelValue)
	glog.V(gpuparams.Gpu10LogLevel).Infof("MIG mixed strategy label found, proceeding with test")

	// Checking that mig.config.state gets into success state
	err = CheckMigConfigState(workerNodeSelector)
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
		ResetMIGLabelsToDisabled(workerNodeSelector, wait)
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
				burnImageName[clusterArch], burn, migCapabilities[i].MixedCnt)
			burn.PodName = fmt.Sprintf("gpu-burn-pod-%d-of-mig-%s", migCapabilities[i].MixedCnt, migCapabilities[i].MigName)
			gpuMigPodPulled := DeployGPUWorkload(
				burnImageName[clusterArch],
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
	// Competed status is accepted as well in the isRunning function (because of mixed.mig.pod-delay parameter,
	//   previous pods may be completed while the later ones are still running).
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

func TestGPUWorkloadWithTimeslicing(nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig, burn *nvidiagpu.GPUBurnConfig,
	burnImageName map[string]string, workerNodeSelector map[string]string, cleanupAfterTest bool) {

	By("Read and validate time.slicing.instances")
	_ = ReadTimeSlicingParameters()

	// ***** Cleaning up previous GPU Burn resources
	By("Cleanup if necessary")
	CleanupWorkloadResources(burn)

	By("Disable MIG if configured on GPU nodes")
	migActive, err := IsMig(workerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "error checking MIG status on GPU nodes: %v", err)
	if migActive {
		DisableMig(workerNodeSelector)
	}

	tsConfigSnap, err := captureTimeSlicingConfigSnapshot(inittools.APIClient)
	Expect(err).ToNot(HaveOccurred(), "error capturing time-slicing config snapshot: %v", err)

	timeSlicingSetupStarted := false
	defer func() {
		defer GinkgoRecover()
		if !timeSlicingSetupStarted {
			return
		}
		alreadyFailed := CurrentSpecReport().Failed()
		waitForReady := !alreadyFailed

		glog.V(gpuparams.Gpu100LogLevel).Infof("defer (remove time-slicing config)")
		if err := DeletePods(inittools.APIClient, burn.Namespace).
			WithLabelSelector(burn.PodLabel).
			Delete(); err != nil {
			glog.Errorf("time-slicing teardown: delete gpu-burn pods: %v", err)
			if !alreadyFailed {
				Expect(err).NotTo(HaveOccurred())
			}
		}
		if err := removeTimeSlicingConfig(inittools.APIClient, workerNodeSelector, tsConfigSnap, waitForReady); err != nil {
			glog.Errorf("time-slicing teardown: remove config: %v", err)
			if !alreadyFailed {
				Expect(err).NotTo(HaveOccurred())
			}
		}
	}()

	By("Create time-slicing config")
	_, err = CreateTimeSlicingConfig(inittools.APIClient, workerNodeSelector, "", MaxTsSlices)
	Expect(err).ToNot(HaveOccurred(), "error creating time-slicing config: %v", err)

	// Check and create test-gpu-burn namespace if it is missing
	By("Create test-gpu-burn namespace")
	gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, burn.Namespace)
	if !gpuBurnNsBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn namespace '%s'", burn.Namespace)
		_, err = gpuBurnNsBuilder.Create()
		Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
			"namespace '%s' : %v ", burn.Namespace, err)
	}

	// GPU burn entrypoint ConfigMap (CleanupWorkloadResources removed it); same name/namespace as other MIG tests.
	By("Deploy GPU Burn configmap for time-slicing in test-gpu-burn namespace")
	gpuBurnEntryCmBuilder := configmap.NewBuilder(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if !gpuBurnEntryCmBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn configmap '%s' in namespace '%s'",
			burn.ConfigMapName, burn.Namespace)
		_, err = gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
		Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)
	}

	gpuBurnEntryCmBuilder, err = configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
		"namespace '%s': %v", burn.ConfigMapName, burn.Namespace, err)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer (gpu-burn entrypoint configmap for time-slicing) deleting configmap")
		if cleanupAfterTest {
			err := gpuBurnEntryCmBuilder.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn configmap: %v", err)
			err = gpuBurnEntryCmBuilder.WaitUntilDeleted(15 * time.Second)
			Expect(err).ToNot(HaveOccurred(), "Error waiting for gpu-burn configmap to be deleted: %v", err)
		}
	}()

	// CreateTimeSlicingConfig already enables GFD and updates ClusterPolicy; avoid a second Update here
	// (stale resourceVersion can trigger delete/recreate in clusterpolicy.go).
	By("Get cluster architecture from GPU worker nodes")
	clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, workerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture: %v", err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Cluster architecture: %v", clusterArch)

	By("Checking pre-testing time-slicing status, no pids should be found")
	cmd := []string{"nvidia-smi", "pmon", "-d", "5", "-c", "5"}
	// 	# gpu         pid   type     sm    mem    enc    dec    jpg    ofa    command
	// # Idx           #    C/G      %      %      %      %      %      %    name
	//     0    1020701     C     19      1      -      -      -      -    gpu_burn
	//     0    1020782     C     11      0      -      -      -      -    gpu_burn
	//     0    1020845     C     11      0      -      -      -      -    gpu_burn
	//     0    1020858     C      7      0      -      -      -      -    gpu_burn
	//     0    1020869     C      7      0      -      -      -      -    gpu_burn

	output := GetCmdOutput(inittools.APIClient, workerNodeSelector, cmd)
	Expect(output).NotTo(BeEmpty(), "Error checking time-slicing status: %v", err)
	// Get the PID (1st column) from the CSV output
	status, pids := GetPidsFromPmon(output, 2)

	glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pids: %v with status: %s", pids, status)

	By("Checking pre-testing time-slicing status, pid status for existing pids")
	cmd = []string{"nvidia-smi", "--query-compute-apps=pid,process_name,used_memory,timestamp,gpu_name,gpu_bus_id,gpu_serial,gpu_uuid", "--format=csv,nounits"}
	// 909847, ./gpu_burn, 36268 MiB, 2026/03/02 10:48:43.256, NVIDIA A100-PCIE-40GB, 00000000:B1:00.0, 1323720014180, GPU-ea8148c0-9fd4-3e84-18a0-19cffe1cecce
	// 909855, ./gpu_burn, 3756 MiB, 2026/03/02 10:48:43.256, NVIDIA A100-PCIE-40GB, 00000000:B1:00.0, 1323720014180, GPU-ea8148c0-9fd4-3e84-18a0-19cffe1cecce

	output = GetCmdOutput(inittools.APIClient, workerNodeSelector, cmd)
	Expect(output).NotTo(BeEmpty(), "Error checking time-slicing status: %v", err)
	// Get the PID (1st column) from the CSV output
	status, pids = GetPidFromCSV(output, 1)

	glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pids: %v with status: %s", pids, status)

	status, podInfo := GetPodsWithPids(inittools.APIClient, workerNodeSelector, pids)
	Expect(status).To(BeTrue(), "Error getting pod info: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod info: %v", podInfo)

	By("Deploy gpu-burn pod with time-slicing in test-gpu-burn namespace")
	var tsPodInfo []TsPodInfo
	// pods := make([]*pod.Builder, 0, TsPodCount)
	for i := 0; i < TsPodCount; i++ {
		slices := TsInstances[i]
		podName := fmt.Sprintf("gpu-burn-pod-%d-slice-%d", i+1, slices)
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating time-slicing gpu-burn pod %s requesting nvidia.com/gpu=%d",
			podName, slices)
		pb := DeployGPUWorkload(burnImageName[clusterArch], podName, burn.Namespace, "time-slicing", slices, burn.PodLabel)
		tsPodInfo = append(tsPodInfo, TsPodInfo{
			PodName:   podName,
			Namespace: burn.Namespace,
			Pod:       pb,
			Checked:   false,
		})
	}
	// allow pods to get into Running state to avoid unnecessary logging
	time.Sleep(10 * time.Second)

	// Ensure all pods get into Running state, looping through the previously created & collected pods.
	// Competed status is accepted as well in the isRunning function (because of mixed.mig.pod-delay parameter,
	//   previous pods may be completed while the later ones are still running).
	By("Ensure required number of pods get into Running state")
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Ensure required number of pods get into Running state before starting monitoring"))
	i := 0
	for _, podInfo := range tsPodInfo {
		if TsMonAfterPod <= i {
			glog.V(gpuparams.GpuLogLevel).Infof("Monitoring will now start after %v pods reached running state", TsMonAfterPod)
			break
		}
		if podInfo.Pod.Exists() {
			isRunning(podInfo.Pod, podInfo.Namespace)
		}
		i++
	}

	By("Monitor time-slicing status until all gpu-burn pods complete; validate each pod log on success")
	if TsMonAfterPod <= len(tsPodInfo) {
		j := 0
		for _, podInfo := range tsPodInfo {
			j++
			if j < TsMonAfterPod {
				// skipping pods before time-slicing.mon-after-pod
				glog.V(gpuparams.Gpu10LogLevel).Infof("Skipping pod %s/%s before time-slicing.mon-after-pod", podInfo.Namespace, podInfo.Pod.Definition.Name)
				continue
			} else {
				glog.V(gpuparams.Gpu10LogLevel).Infof("Monitoring pod %s/%s after time-slicing.mon-after-pod", podInfo.Namespace, podInfo.Pod.Definition.Name)
			}

			if podInfo.Pod.Exists() {
				// skip if the pod was failed, testcase collects logs for the pods anyway
				ret := isFailed(podInfo.Pod, podInfo.Namespace)
				if ret == true {
					continue
				}
				ret1 := isRunningStatus(podInfo.Pod, podInfo.Namespace)
				if ret1 > 1 { // monitor once, and wait until pod is completed
					// skip if the pod was already succeeded
					MonitorTimeslicingGPULoad(burn, podInfo, workerNodeSelector)
					// pmonOut := GetCmdOutput(inittools.APIClient, workerNodeSelector, []string{"nvidia-smi", "pmon", "-d", "1", "-c", "1"})
					// status, pids := GetPidsFromPmon(pmonOut, 2)
					// glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pids: %v with status: %v", pids, status)
					isCompleted(podInfo.Pod, burn.Namespace)
				}
			}
		}
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("No monitoring, parameter mon-after-pod: %v is bigger than the number of pods: %v", TsMonAfterPod, len(tsPodInfo))
	}

	// Waiting until the pods are completed (or failed). Depending on the delay between the pods, this may take some time in each iteration.
	By("Wait for GPU Burn pods to complete")
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Wait for GPU Burn pods to complete"))
	for _, podInfo := range tsPodInfo {
		if podInfo.Pod.Exists() {
			ret := isFailed(podInfo.Pod, podInfo.Namespace)
			if ret == true {
				continue
			}
			isCompleted(podInfo.Pod, podInfo.Namespace)
		}
	}

	// After all pods are completed, get and check the logs for each pod.
	// The log retrieval has a validity time period. Second parameter is a multiplier to calculate the validity time.
	By("Get and check the gpu-burn pod logs")
	maxPodIndex := len(tsPodInfo) - 1
	i = 0
	for _, podInfo := range tsPodInfo {
		if podInfo.Pod.Exists() {
			// Second parameter guides on how old logs can be retrieved.
			gpuBurnMigLogs := GetGPUBurnPodLogs(podInfo.Pod, maxPodIndex-i)
			// CheckGPUBurnPodLogs(gpuBurnMigLogs, 1) // only 1 GPU used for time-slicing
			CheckTimeSlicingGPUBurnPodLogs(gpuBurnMigLogs)
		}
		i++
	}
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorGreen+colorBold, "Time-slicing Test completed"))

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
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Delete ClusterPolicy"))
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
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Delete CSV"))
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
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Delete Subscription"))
	subBuilder, err := olm.PullSubscription(inittools.APIClient, nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace)
	if err == nil && subBuilder.Exists() {
		err := subBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting Subscription: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Subscription deleted successfully")
	}
}

// cleanupOperatorGroup deletes the OperatorGroup resource if it exists
func cleanupOperatorGroup() {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Delete OperatorGroup"))
	ogBuilder, err := olm.PullOperatorGroup(inittools.APIClient, nvidiagpu.OperatorGroupName, nvidiagpu.SubscriptionNamespace)
	if err == nil && ogBuilder.Exists() {
		err := ogBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting OperatorGroup: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("OperatorGroup deleted successfully")
	}
}

// cleanupGPUOperatorNamespace deletes the GPU Operator namespace if it exists
func cleanupGPUOperatorNamespace() {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Delete GPU Operator Namespace"))
	nsBuilder := namespace.NewBuilder(inittools.APIClient, nvidiagpu.SubscriptionNamespace)
	if nsBuilder.Exists() {
		err := nsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", nvidiagpu.SubscriptionNamespace)
	}
}

// cleanupGPUBurnNamespace deletes the GPU Burn namespace if it exists
func cleanupGPUBurnNamespace(burnNamespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Delete GPU Burn Namespace"))
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
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "IsLabelInFilter"))
	filterQuery := GinkgoLabelFilter()
	glog.V(gpuparams.Gpu100LogLevel).Infof("Checking if label '%s' is present in Ginkgo label filter: %s", label, filterQuery)

	// If no filter is set, the label is not in the filter
	if filterQuery == "" {
		glog.V(gpuparams.Gpu100LogLevel).Infof("No label filter set, label '%s' is not in filter", label)
		return false
	}

	// Check if the label is present in the filter string
	// Use word boundaries to avoid partial matches (e.g., "single-mig" should not match "single-mig-test")
	// Simple check: label should appear as a whole word (comma-separated or at boundaries)
	labelInFilter := strings.Contains(filterQuery, label)
	if labelInFilter {
		glog.V(gpuparams.GpuLogLevel).Infof("Label '%s' is present in Ginkgo label filter", label)
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("Label '%s' is not present in Ginkgo label filter", label)
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

// ReadSingleMIGParameter checks the singleMIGProfile parameter and parses the MIG index if provided.
// Function returns the selected MIG index, or -1 if not set or invalid (i.e. contains no digits)
// -1 translates to random selection of MIG profile
func ReadSingleMIGParameter() int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check --single.mig.profile parameter"))
	if SingleMigProfile != defaultSingleMigProfile {
		// CLI parameter is set, use it
		glog.V(gpuparams.Gpu10LogLevel).Infof("CLI parameter --single.mig.profile"+
			" is set to '%d', using it as requested MIG profile", SingleMigProfile)
		return SingleMigProfile
	}
	return -1
}

// ReadMIGParameter returns the value of the --mixed.mig.instances parameter or defaults if the parameter was not set.
// It returns a slice of integers representing the number of instances for each MIG profile.
// If the parameter is not set, it returns the hardcoded default values for A100 GPU [2,0,1,1,0,0].
func ReadMIGParameter() []int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Get value of --mixed.mig.instances parameter"))
	defaults := []int{2, 0, 1, 1, 0, 0}

	if MixedMigInstances != nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("CLI parameter --mixed.mig.instances is set to: '%v', "+
			"using it as requested MIG instance counts", MixedMigInstances)
		return MixedMigInstances
	}
	// If no valid numbers found, return default values
	glog.V(gpuparams.GpuLogLevel).Infof("No valid numbers found in --mixed.mig.instances, using default values %v", defaults)
	return defaults
}

// ReadDelayBetweenPods returns the value of mixed.mig.pod-delay.
// ReadDelayBetweenPods checks the Ginkgo CLI parameter mixed.mig.pod-delay and returns the value.
func ReadDelayBetweenPods() int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "ReadDelayBetweenPods"))
	var podDelay int
	switch {
	case PodDelay < 0:
		podDelay = 0
	case PodDelay > 315:
		podDelay = 315
	default:
		podDelay = PodDelay
	}

	glog.V(gpuparams.Gpu10LogLevel).Infof("--mixed.mig.pod-delay parameter value: %d", podDelay)
	return podDelay
}

// ReadTimeSlicingParameters parses and validates and validates --time.slicing.instances
// and --time.slicing.mon-after-pod parameters.
// It sets global TsInstances to the first TsPodCount slice counts (time slices requested per pod).
func ReadTimeSlicingParameters() []int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Validate time-slicing CLI parameters"))

	parsed := parseMigInstances(TsInstancesCSV, defaultTsInstancesCSV)
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s %d", colorLog(colorCyan+colorBold, "parsed length"), len(parsed))
	TsPodCount = len(parsed)

	out := make([]int, TsPodCount)
	copy(out, parsed[:TsPodCount])

	sum := 0
	for i, v := range out {
		Expect(v).To(BeNumerically(">=", 0),
			"time.slicing.instances value at index %d must be non-negative (got %d)", i, v)
		sum += v
	}
	Expect(sum).To(BeNumerically("<=", MaxTsInstance),
		"sum of time-slicing instance counts %v is %d; must not exceed %d", out, sum, MaxTsInstance)

	TsInstances = out
	glog.V(gpuparams.Gpu10LogLevel).Infof("Time-slicing: pod-count=%d per-pod instances=%v sum=%d (max=%d), mon-after-pod=%d",
		TsPodCount, out, sum, MaxTsInstance, TsMonAfterPod)
	return out
}

// CleanupWorkloadResources cleans up existing GPU burn pods and configmaps, then waits for cleanup to complete.
func CleanupWorkloadResources(burn *nvidiagpu.GPUBurnConfig) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Cleaning up namespace and workload resources"))
	if err := DeletePods(inittools.APIClient, burn.Namespace).WithLabelSelector(burn.PodLabel).Delete(); err != nil {
		Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pods: %v", err)
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

const defaultPodDeleteWait = 30 * time.Second

// PodCleanup deletes pods in a namespace that match a label selector.
type PodCleanup struct {
	apiClient     *clients.Settings
	namespace     string
	labelSelector string
	waitTimeout   time.Duration
}

// DeletePods starts a pod cleanup for the given namespace.
func DeletePods(apiClient *clients.Settings, namespace string) *PodCleanup {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "DeletePods"))
	return &PodCleanup{
		apiClient:   apiClient,
		namespace:   namespace,
		waitTimeout: defaultPodDeleteWait,
	}
}

// WithLabel adds a label requirement (key=value) to the pod label selector.
func (p *PodCleanup) WithLabel(labelKey, labelValue string) *PodCleanup {
	add := labelKey + "=" + labelValue
	if p.labelSelector == "" {
		p.labelSelector = add
	} else {
		p.labelSelector = p.labelSelector + "," + add
	}
	return p
}

// WithLabelSelector sets the pod label selector directly (e.g. "app=gpu-burn-app").
func (p *PodCleanup) WithLabelSelector(labelSelector string) *PodCleanup {
	p.labelSelector = labelSelector
	return p
}

// Delete lists matching pods, deletes them, and waits until they are gone.
func (p *PodCleanup) Delete() error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, " Deleting all previously selected pods"))
	if p.apiClient == nil {
		return fmt.Errorf("pod cleanup apiClient is nil")
	}
	if p.namespace == "" {
		return fmt.Errorf("pod cleanup namespace is empty")
	}
	if p.labelSelector == "" {
		return fmt.Errorf("pod cleanup label selector is empty")
	}

	podList, err := pod.List(p.apiClient, p.namespace, metav1.ListOptions{LabelSelector: p.labelSelector})
	if err != nil {
		return fmt.Errorf("list pods in namespace %s with label selector %q: %w", p.namespace, p.labelSelector, err)
	}
	if len(podList) == 0 {
		glog.V(gpuparams.Gpu10LogLevel).Infof("No pods found in namespace %s with label selector %q", p.namespace, p.labelSelector)
		return nil
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Found %d pod(s) in namespace %s with label selector %q", len(podList), p.namespace, p.labelSelector)
	for _, podBuilder := range podList {
		glog.V(gpuparams.GpuLogLevel).Infof("Deleting pod %q", podBuilder.Definition.Name)
		if _, err := podBuilder.Delete(); err != nil {
			return fmt.Errorf("delete pod %s/%s: %w", p.namespace, podBuilder.Definition.Name, err)
		}
	}
	for _, podBuilder := range podList {
		if err := podBuilder.WaitUntilDeleted(p.waitTimeout); err != nil {
			return fmt.Errorf("wait for pod %s/%s deletion: %w", p.namespace, podBuilder.Definition.Name, err)
		}
	}
	glog.V(gpuparams.GpuLogLevel).Infof("All pods in namespace %s with label selector %q have been deleted", p.namespace, p.labelSelector)
	return nil
}

// baseGPUProductName returns the stable device-plugin ConfigMap key: GFD may suffix gpu.product with -SHARED
// after time-slicing is active, but device-plugin.config and ConfigMap keys must not use that suffix.
func baseGPUProductName(product string) string {
	return strings.TrimSuffix(strings.TrimSpace(product), gpuProductSharedSuffix)
}

// timeSlicingDevicePluginConfigData returns optional static ConfigMap entries (per-GPU keys are added via tsDPCD).
func timeSlicingDevicePluginConfigData() map[string]string {
	return map[string]string{}
}

// tsDPCD returns a minimal device-plugin ConfigMap .data fragment for time-slicing: only nvidia.com/gpu with the given replica count (no MIG entries).
// gpuProduct must be a base product name (no -SHARED suffix); it becomes the ConfigMap data key and device-plugin.config label value.
func tsDPCD(gpuProduct string, replicas int) map[string]string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Returns minimal device-plugin ConfigMap"))
	if gpuProduct == "" || replicas <= 0 {
		return nil
	}
	yaml := fmt.Sprintf(`version: v1
sharing:
  timeSlicing:
    resources:
      - name: nvidia.com/gpu
        replicas: %d
`, replicas)
	return map[string]string{gpuProduct: yaml}
}

func firstGPUProductFromNodes(apiClient *clients.Settings, nodeSelector map[string]string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "firstGPUProductFromNodes"))
	nodeBuilders, err := nodes.List(apiClient, metav1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})
	if err != nil || len(nodeBuilders) == 0 {
		return ""
	}
	for _, nb := range nodeBuilders {
		if nb.Definition.Labels == nil {
			continue
		}
		if v := nb.Definition.Labels[gpuProductLabelKey]; v != "" {
			return v
		}
	}
	return ""
}

func waitForGPUProductLabel(apiClient *clients.Settings, nodeSelector map[string]string, poll, timeout time.Duration) (string, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "waitForGPUProductLabel"))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p := firstGPUProductFromNodes(apiClient, nodeSelector); p != "" {
			return p, nil
		}
		time.Sleep(poll)
	}
	return "", fmt.Errorf("timeout after %v waiting for label %s on nodes matching %v",
		timeout, gpuProductLabelKey, nodeSelector)
}

// applyDevicePluginConfigNodeLabels sets nvidia.com/device-plugin.config to the stable ConfigMap key on GPU
// worker nodes. Nodes are matched when base(nvidia.com/gpu.product) equals the ConfigMap key, so labeling
// works whether or not GFD has already appended -SHARED to gpu.product.
func applyDevicePluginConfigNodeLabels(apiClient *clients.Settings, cmData map[string]string, nodeSelector map[string]string) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "applyDevicePluginConfigNodeLabels"))
	const devicePluginConfigLabel = "nvidia.com/device-plugin.config"

	nodeBuilders, err := nodes.List(apiClient, metav1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})
	if err != nil {
		return fmt.Errorf("list nodes matching %v: %w", nodeSelector, err)
	}
	for _, nb := range nodeBuilders {
		if nb.Definition.Labels == nil {
			continue
		}
		gpuProduct := nb.Definition.Labels[gpuProductLabelKey]
		if gpuProduct == "" {
			glog.V(gpuparams.GpuLogLevel).Infof("Node %s has no %s label; skipping %s",
				nb.Definition.Name, gpuProductLabelKey, devicePluginConfigLabel)
			continue
		}
		configKey := baseGPUProductName(gpuProduct)
		if _, ok := cmData[configKey]; !ok {
			glog.V(gpuparams.GpuLogLevel).Infof("Node %s %s=%q (base %q) has no device-plugin ConfigMap entry; skipping %s",
				nb.Definition.Name, gpuProductLabelKey, gpuProduct, configKey, devicePluginConfigLabel)
			continue
		}
		nb = nb.WithLabel(devicePluginConfigLabel, configKey)
		if _, err := nb.Update(); err != nil {
			return fmt.Errorf("label node %s with %s=%s: %w", nb.Definition.Name, devicePluginConfigLabel, configKey, err)
		}
		glog.V(gpuparams.GpuLogLevel).Infof("Labeled node %s: %s=%s (%s=%q)",
			nb.Definition.Name, devicePluginConfigLabel, configKey, gpuProductLabelKey, gpuProduct)
	}
	return nil
}

// CreateTimeSlicingConfig enables GFD, creates/updates the device-plugin ConfigMap and node labels, then sets
// ClusterPolicy spec.devicePlugin.config (name + default). ConfigMap and labels are applied before the device-plugin
// config reference is set so daemonset pods do not start with an empty nvidia.com/device-plugin.config label.
func CreateTimeSlicingConfig(apiClient *clients.Settings, workerNodeSelector map[string]string, gpuProduct string, replicas int) (*corev1.ConfigMap, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "CreateTimeSlicingConfig"))
	if replicas <= 0 {
		return nil, fmt.Errorf("time-slicing replicas must be > 0, got %d", replicas)
	}
	if len(workerNodeSelector) == 0 {
		return nil, fmt.Errorf("workerNodeSelector must not be empty (needed to discover %s when gpuProduct is empty)", gpuProductLabelKey)
	}

	cpBuilder, err := nvidiagpu.Pull(apiClient, nvidiagpu.ClusterPolicyName)
	if err != nil {
		return nil, fmt.Errorf("pull ClusterPolicy %s: %w", nvidiagpu.ClusterPolicyName, err)
	}
	gfdEnabled := true
	cpBuilder.Definition.Spec.GPUFeatureDiscovery.Enabled = &gfdEnabled
	if _, err := cpBuilder.Update(true); err != nil {
		return nil, fmt.Errorf("update ClusterPolicy %s spec.gfd.enabled=true: %w", nvidiagpu.ClusterPolicyName, err)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("Set ClusterPolicy %s spec.gfd.enabled to true", nvidiagpu.ClusterPolicyName)

	product := baseGPUProductName(gpuProduct)
	if product == "" {
		var werr error
		discovered, werr := waitForGPUProductLabel(apiClient, workerNodeSelector, nvidiagpu.LabelCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
		if werr != nil {
			return nil, werr
		}
		product = baseGPUProductName(discovered)
		glog.V(gpuparams.GpuLogLevel).Infof("Discovered %s=%q (stable key %q)", gpuProductLabelKey, discovered, product)
	}
	if product == "" {
		return nil, fmt.Errorf("empty GPU product name after stripping %q suffix", gpuProductSharedSuffix)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("Using stable device-plugin ConfigMap key %q", product)

	out, err := updateTimeSlicingDevicePluginConfigMap(apiClient, product, replicas)
	if err != nil {
		return nil, err
	}
	if err := applyDevicePluginConfigNodeLabels(apiClient, out.Data, workerNodeSelector); err != nil {
		return nil, err
	}

	cpBuilder, err = nvidiagpu.Pull(apiClient, nvidiagpu.ClusterPolicyName)
	if err != nil {
		return nil, fmt.Errorf("pull ClusterPolicy %s before devicePlugin.config: %w", nvidiagpu.ClusterPolicyName, err)
	}
	if cpBuilder.Definition.Spec.DevicePlugin.Config == nil {
		cpBuilder.Definition.Spec.DevicePlugin.Config = &nvidiagpuv1.DevicePluginConfig{}
	}
	cpBuilder.Definition.Spec.DevicePlugin.Config.Name = timeSlicingDevicePluginConfigMapName
	cpBuilder.Definition.Spec.DevicePlugin.Config.Default = product
	if _, err := cpBuilder.Update(true); err != nil {
		return nil, fmt.Errorf("update ClusterPolicy %s devicePlugin.config: %w", nvidiagpu.ClusterPolicyName, err)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("Set ClusterPolicy %s spec.devicePlugin.config.name=%q default=%q",
		nvidiagpu.ClusterPolicyName, timeSlicingDevicePluginConfigMapName, product)

	if err := applyDevicePluginConfigNodeLabels(apiClient, out.Data, workerNodeSelector); err != nil {
		return nil, err
	}
	return out, nil
}

func updateTimeSlicingDevicePluginConfigMap(apiClient *clients.Settings, gpuProduct string, replicas int) (*corev1.ConfigMap, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "updateTimeSlicingDevicePluginConfigMap"))
	ns := nvidiagpu.NvidiaGPUNamespace
	data := timeSlicingDevicePluginConfigData()
	for k, v := range tsDPCD(gpuProduct, replicas) {
		data[k] = v
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      timeSlicingDevicePluginConfigMapName,
			Namespace: ns,
		},
		Data: data,
	}
	cms := apiClient.CoreV1Interface.ConfigMaps(ns)
	created, err := cms.Create(context.TODO(), cm, metav1.CreateOptions{})
	if err == nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Created ConfigMap %s/%s for device-plugin time-slicing", ns, timeSlicingDevicePluginConfigMapName)
		return created, nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, err)
	}
	existing, getErr := cms.Get(context.TODO(), timeSlicingDevicePluginConfigMapName, metav1.GetOptions{})
	if getErr != nil {
		return nil, fmt.Errorf("get existing ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, getErr)
	}
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	for k, v := range data {
		existing.Data[k] = v
	}
	// Drop stale -SHARED keys when the base product entry is present.
	for k := range existing.Data {
		if !strings.HasSuffix(k, gpuProductSharedSuffix) {
			continue
		}
		if _, ok := existing.Data[baseGPUProductName(k)]; ok {
			delete(existing.Data, k)
		}
	}
	updated, updErr := cms.Update(context.TODO(), existing, metav1.UpdateOptions{})
	if updErr != nil {
		return nil, fmt.Errorf("update ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, updErr)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("Updated ConfigMap %s/%s for device-plugin time-slicing", ns, timeSlicingDevicePluginConfigMapName)
	return updated, nil
}

func captureTimeSlicingConfigSnapshot(apiClient *clients.Settings) (*timeSlicingConfigSnapshot, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "captureTimeSlicingConfigSnapshot"))
	snap := &timeSlicingConfigSnapshot{}

	cpBuilder, err := nvidiagpu.Pull(apiClient, nvidiagpu.ClusterPolicyName)
	if err != nil {
		return nil, fmt.Errorf("pull ClusterPolicy %s: %w", nvidiagpu.ClusterPolicyName, err)
	}
	if cfg := cpBuilder.Definition.Spec.DevicePlugin.Config; cfg != nil &&
		(cfg.Name != "" || cfg.Default != "") {
		snap.hadDevicePluginConfig = true
		cfgCopy := *cfg
		snap.devicePluginConfig = &cfgCopy
	}
	if enabled := cpBuilder.Definition.Spec.GPUFeatureDiscovery.Enabled; enabled != nil {
		v := *enabled
		snap.gfdEnabled = &v
	}

	ns := nvidiagpu.NvidiaGPUNamespace
	existing, getErr := apiClient.CoreV1Interface.ConfigMaps(ns).Get(
		context.TODO(), timeSlicingDevicePluginConfigMapName, metav1.GetOptions{})
	if getErr == nil {
		snap.configMapExisted = true
		snap.configMapData = copyStringMap(existing.Data)
	} else if !k8serrors.IsNotFound(getErr) {
		return nil, fmt.Errorf("get ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, getErr)
	}
	return snap, nil
}

func removeDevicePluginConfigNodeLabels(apiClient *clients.Settings, nodeSelector map[string]string) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "removeDevicePluginConfigNodeLabels"))
	const devicePluginConfigLabel = "nvidia.com/device-plugin.config"

	nodeBuilders, err := nodes.List(apiClient, metav1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})
	if err != nil {
		return fmt.Errorf("list nodes matching %v: %w", nodeSelector, err)
	}
	for _, nb := range nodeBuilders {
		if nb.Definition.Labels == nil {
			continue
		}
		value, ok := nb.Definition.Labels[devicePluginConfigLabel]
		if !ok {
			continue
		}
		nb = nb.RemoveLabel(devicePluginConfigLabel, value)
		if _, err := nb.Update(); err != nil {
			return fmt.Errorf("remove %s from node %s: %w", devicePluginConfigLabel, nb.Definition.Name, err)
		}
		glog.V(gpuparams.GpuLogLevel).Infof("Removed label %s from node %s", devicePluginConfigLabel, nb.Definition.Name)
	}
	return nil
}

func restoreTimeSlicingDevicePluginConfigMap(apiClient *clients.Settings, snap *timeSlicingConfigSnapshot) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "restoreTimeSlicingDevicePluginConfigMap"))
	ns := nvidiagpu.NvidiaGPUNamespace
	cms := apiClient.CoreV1Interface.ConfigMaps(ns)

	if !snap.configMapExisted {
		err := cms.Delete(context.TODO(), timeSlicingDevicePluginConfigMapName, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("delete ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, err)
		}
		glog.V(gpuparams.GpuLogLevel).Infof("Deleted ConfigMap %s/%s (created by time-slicing test)", ns, timeSlicingDevicePluginConfigMapName)
		return nil
	}

	existing, getErr := cms.Get(context.TODO(), timeSlicingDevicePluginConfigMapName, metav1.GetOptions{})
	if getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			return nil
		}
		return fmt.Errorf("get ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, getErr)
	}
	existing.Data = copyStringMap(snap.configMapData)
	if _, updErr := cms.Update(context.TODO(), existing, metav1.UpdateOptions{}); updErr != nil {
		return fmt.Errorf("restore ConfigMap %s/%s: %w", ns, timeSlicingDevicePluginConfigMapName, updErr)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("Restored ConfigMap %s/%s to pre-test state", ns, timeSlicingDevicePluginConfigMapName)
	return nil
}

// removeTimeSlicingConfig reverses CreateTimeSlicingConfig: restores ClusterPolicy devicePlugin.config and GFD,
// removes nvidia.com/device-plugin.config node labels, and restores or deletes the device-plugin ConfigMap.
func removeTimeSlicingConfig(apiClient *clients.Settings, workerNodeSelector map[string]string, snap *timeSlicingConfigSnapshot, waitForReady bool) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "removeTimeSlicingConfig"))
	if snap == nil {
		return fmt.Errorf("time-slicing snapshot is nil")
	}

	if err := removeDevicePluginConfigNodeLabels(apiClient, workerNodeSelector); err != nil {
		return err
	}

	cpBuilder, cpErr := nvidiagpu.Pull(apiClient, nvidiagpu.ClusterPolicyName)
	if cpErr != nil {
		glog.Warningf("ClusterPolicy %s not found, skipping devicePlugin.config restore: %v", nvidiagpu.ClusterPolicyName, cpErr)
	} else {
		if snap.hadDevicePluginConfig {
			cpBuilder.Definition.Spec.DevicePlugin.Config = snap.devicePluginConfig
		} else {
			cpBuilder.Definition.Spec.DevicePlugin.Config = nil
		}
		cpBuilder.Definition.Spec.GPUFeatureDiscovery.Enabled = snap.gfdEnabled
		if _, err := cpBuilder.Update(false); err != nil {
			return fmt.Errorf("restore ClusterPolicy %s devicePlugin.config and GFD: %w", nvidiagpu.ClusterPolicyName, err)
		}
		glog.V(gpuparams.GpuLogLevel).Infof("Restored ClusterPolicy %s devicePlugin.config and GFD to pre-test state", nvidiagpu.ClusterPolicyName)
	}

	if err := restoreTimeSlicingDevicePluginConfigMap(apiClient, snap); err != nil {
		return err
	}

	if err := restartGPUOperatorDevicePluginDaemonsets(apiClient); err != nil {
		return err
	}

	if cpErr != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Skipping ClusterPolicy wait after time-slicing cleanup (ClusterPolicy missing)")
		return nil
	}

	if !waitForReady {
		glog.V(gpuparams.GpuLogLevel).Infof("Skipping ClusterPolicy wait after time-slicing cleanup (test may have failed)")
		return nil
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterPolicy to be ready after time-slicing cleanup")
	if err := wait.ClusterPolicyReady(apiClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout); err != nil {
		return fmt.Errorf("wait for ClusterPolicy ready after time-slicing cleanup: %w", err)
	}
	return nil
}

func restartGPUOperatorDevicePluginDaemonsets(apiClient *clients.Settings) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "restartGPUOperatorDevicePluginDaemonsets"))
	ns := nvidiagpu.NvidiaGPUNamespace
	names := []string{"nvidia-device-plugin-daemonset", "gpu-feature-discovery"}
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`,
		time.Now().Format(time.RFC3339),
	))
	for _, name := range names {
		_, err := apiClient.DaemonSets(ns).Patch(
			context.TODO(), name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				glog.V(gpuparams.GpuLogLevel).Infof("DaemonSet %s/%s not found, skipping rollout restart", ns, name)
				continue
			}
			return fmt.Errorf("rollout restart DaemonSet %s/%s: %w", ns, name, err)
		}
		glog.V(gpuparams.GpuLogLevel).Infof("Triggered rollout restart of DaemonSet %s/%s", ns, name)
	}
	return nil
}

// SelectMigProfile queries MIG profiles from hardware and selects/validates the MIG index.
// It returns the MIG capabilities and the selected/validated MIG index.
// If no MIG configurations are found, it calls Skip to skip the test.
func SelectMigProfile(workerNodeSelector map[string]string, useMigIndex int, migInstanceCounts []int) ([]MIGProfileInfo, int) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Query and select MIG profile"))

	_, migCapabilities, err := MIGProfiles(inittools.APIClient, workerNodeSelector)
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

	switch {
	case useMigIndex < 0:
		useMigIndex = rand.Intn(len(migCapabilities))
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected random MIG index: %d (available: 0-%d)", useMigIndex, len(migCapabilities)-1)
	case useMigIndex >= len(migCapabilities):
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected MIG index %d is out of range (available: 0-%d), using last available index", useMigIndex, len(migCapabilities)-1)
		useMigIndex = len(migCapabilities) - 1
	default:
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected MIG index %d is within range (available: 0-%d), using it", useMigIndex, len(migCapabilities)-1)
	}

	return migCapabilities, useMigIndex
}

// CheckMigConfigState checks that mig.config.state gets into success state on GPU nodes.
// It returns an error if the label is not found or does not have the expected value.
func CheckMigConfigState(workerNodeSelector map[string]string) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check for MIG config state on GPU nodes"))
	expectedLabelValue := "success"
	err := wait.NodeLabelExists(inittools.APIClient, migConfigStateLabel, expectedLabelValue,
		labels.Set(workerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
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
	if UsedSlices > MaxSlices && migStrategy == MIGStrategyMixed {
		glog.V(gpuparams.Gpu10LogLevel).Infof(colorRed + "Warning: UsedSlices is greater than MaxSlices, case may fail" + colorReset)
	}
	if UsedMemory > MaxMemory && migStrategy == MIGStrategyMixed {
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
func SetMIGLabelsOnNodes(migCapabilities []MIGProfileInfo, useMigIndex int, workerNodeSelector map[string]string, migStrategy string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Set MIG labels on nodes"))
	var MigProfile, useMigProfile string

	switch migStrategy {
	case MIGStrategySingle:
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG single strategy label on GPU worker nodes from entry # %d of the list (profile: %s with %d/%d slices)",
			useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
		MigProfile = "all-" + migCapabilities[useMigIndex].MigName
		useMigProfile = migCapabilities[useMigIndex].Flavor
	case MIGStrategyMixed:
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG mixed strategy label on GPU worker nodes from entry # %d of the list (profile: %s with %d/%d slices)",
			useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
		MigProfile = "all-balanced"
		useMigProfile = MIGStrategyMixed
	default:
		// mig strategy is initially for mixed strategy, so by default using mixed strategy on any other case.
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG strategy label on GPU worker nodes from entry # %d of the list (profile: %s with %d/%d slices)",
			useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
		MigProfile = migStrategy
		migStrategy = MIGStrategyMixed
		useMigProfile = MIGStrategyMixed
	}

	// use first mig profile from the list, unless specified otherwise
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
	for _, nodeBuilder := range nodeBuilders {
		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG %s strategy label on node '%s' (overwrite=true)", migStrategy, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.strategy", migStrategy)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG %s strategy label on node '%s'", migStrategy, nodeBuilder.Definition.Name)

		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG configuration label %s on node '%s' (overwrite=true)", MigProfile, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel(migConfigLabel, MigProfile)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG configuration label on node '%s' with %s", nodeBuilder.Definition.Name, MigProfile)
	}

	return useMigProfile
}

// IsMig reports whether any GPU worker node has active or failed MIG configuration.
// It returns true when nvidia.com/mig.config.state is "failed", or when nvidia.com/mig.config
// is set to a value other than "all-disabled".
func IsMig(workerNodeSelector map[string]string) (bool, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "IsMig test"))
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerNodeSelector).String()})
	if err != nil {
		return false, fmt.Errorf("list nodes for MIG check: %w", err)
	}
	for _, nodeBuilder := range nodeBuilders {
		nodeLabels := nodeBuilder.Object.Labels
		if nodeLabels[migConfigStateLabel] == "failed" {
			glog.V(gpuparams.GpuLogLevel).Infof("Node '%s' has MIG config state 'failed'", nodeBuilder.Object.Name)
			return true, nil
		}
		if cfg, ok := nodeLabels[migConfigLabel]; ok && cfg != migConfigDisabled {
			glog.V(gpuparams.GpuLogLevel).Infof("Node '%s' has MIG config '%s' (not %s)", nodeBuilder.Object.Name, cfg, migConfigDisabled)
			return true, nil
		}
	}
	return false, nil
}

// DisableMig resets MIG labels to all-disabled on GPU worker nodes and waits for ClusterPolicy to be ready.
func DisableMig(workerNodeSelector map[string]string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Disable MIG on GPU nodes"))
	ResetMIGLabelsToDisabled(workerNodeSelector, true)
}

// ResetMIGLabelsToDisabled sets MIG strategy and configuration labels to "all-disabled" on GPU worker nodes.
// If waitForReady is true, it waits for ClusterPolicy to be ready after setting the labels.
func ResetMIGLabelsToDisabled(workerNodeSelector map[string]string, waitForReady bool) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Reset MIG labels to disabled"))
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
	for _, nodeBuilder := range nodeBuilders {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG configuration label to '%s' on node '%s' (overwrite=true)", migConfigDisabled, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel(migConfigLabel, migConfigDisabled)
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
func updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder *nvidiagpu.Builder, workerNodeSelector map[string]string, migStrategy nvidiagpuv1.MIGStrategy) {
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

	err = wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.strategy", string(migStrategy), labels.Set(workerNodeSelector),
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
	workerNodeSelector map[string]string,
	migStrategy nvidiagpuv1.MIGStrategy) (string, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Configure MIG strategy and get cluster architecture"))
	glog.V(gpuparams.Gpu10LogLevel).Infof(
		"Setting ClusterPolicy MIG strategy to '%s'", migStrategy)

	currentMigStrategy := pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Current MIG strategy is '%s', updating to '%s'",
		currentMigStrategy, migStrategy)
	pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy = migStrategy
	updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder, workerNodeSelector, migStrategy)

	By(fmt.Sprintf("Getting cluster architecture from nodes with workerNodeSelector: %v", workerNodeSelector))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Getting cluster architecture from nodes with "+
		"workerNodeSelector: %v", workerNodeSelector)
	clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, workerNodeSelector)
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

// DeployTimeslicingGPUBurnPods creates TsPodCount GPU burn pods for time-slicing.
// Pod k (1-based) is named gpu-burn-pod-{k}-slice-{n} where n is TsInstances[k-1],
// each requesting n units of nvidia.com/gpu (device-plugin time-slicing replicas).
// ReadTimeSlicingParameters must have run so TsPodCount and TsInstances are populated.
func DeployTimeslicingGPUBurnPods(imageName, namespace, podLabel string) []*pod.Builder {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "DeployTimeslicingGPUBurnPods"))
	Expect(TsPodCount).To(BeNumerically(">", 0), "TsPodCount must be set for time-slicing pods")
	Expect(len(TsInstances)).To(BeNumerically(">=", TsPodCount),
		"TsInstances must have at least TsPodCount entries (got %d, need %d)", len(TsInstances), TsPodCount)

	pods := make([]*pod.Builder, 0, TsPodCount)
	for i := 0; i < TsPodCount; i++ {
		slices := TsInstances[i]
		podName := fmt.Sprintf("gpu-burn-pod-%d-slice-%d", i+1, slices)
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating time-slicing gpu-burn pod %s requesting nvidia.com/gpu=%d",
			podName, slices)
		pb := DeployGPUWorkload(imageName, podName, namespace, "time-slicing", slices, podLabel)
		pods = append(pods, pb)
	}
	return pods
}

// waitForGPUBurnPodToComplete waits for the GPU burn pod to reach Running phase,
// then waits for it to complete and reach Succeeded phase.
// It uses a two-phase timeout: Phase 1 checks scheduling (fast fail if no GPU node),
// Phase 2 waits for Running (tolerates slow image pulls).
func waitForGPUBurnPodToComplete(gpuMigPodPulled *pod.Builder, namespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Wait for GPU burn pod to complete"))

	// Refresh pod state — the caller's snapshot may be stale.
	gpuMigPodPulled, err := pod.Pull(inittools.APIClient, gpuMigPodPulled.Definition.Name, namespace)
	Expect(err).ToNot(HaveOccurred(), "failed to pull gpu-burn pod in namespace '%s': %v", namespace, err)

	// Early-exit: pod already completed before we started waiting.
	if gpuMigPodPulled.Object.Status.Phase == corev1.PodSucceeded {
		glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod already in Succeeded phase, skipping wait")

		return
	}

	// Phase 1: confirm the pod is scheduled within the timeout window.
	err = gpuMigPodPulled.WaitUntilScheduled(nvidiagpu.BurnPodScheduledTimeout)
	Expect(err).ToNot(HaveOccurred(), "gpu-burn MIG pod in namespace '%s' was not scheduled "+
		"within the timeout (scheduler may be busy or pod may have failed early): %v", namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG is scheduled onto a GPU node")

	// Phase 2: wait for Running or Succeeded, tolerating time needed for image pull and fast completions.
	err = gpuMigPodPulled.WaitUntilRunningOrSucceeded(nvidiagpu.BurnPodRunningTimeout)
	if err != nil {
		// Pull a fresh snapshot for diagnostic logging only.
		pod2, err2 := pod.Pull(inittools.APIClient, gpuMigPodPulled.Definition.Name, namespace)
		if err2 == nil {
			glog.V(gpuparams.Gpu10LogLevel).Infof("Pod %s did not reach Running or Succeeded: %s (%s). Error: %v",
				pod2.Definition.Name, pod2.Object.Status.Phase, pod2.Object.Status.Reason, err)
			logPodEvents(pod2.Definition.Name, namespace)
		}

		Expect(err).ToNot(HaveOccurred(), "gpu-burn pod with MIG in namespace '%s' did not reach "+
			"Running or Succeeded phase (pod may have failed or image pull may have taken too long): %v", namespace, err)
	}

	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG now in Running or Succeeded phase")

	glog.V(gpuparams.Gpu10LogLevel).Infof("Wait for up to %s for gpu-burn pod to complete", nvidiagpu.BurnPodSuccessTimeout)
	err = gpuMigPodPulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.BurnPodSuccessTimeout)

	Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' with MIG in "+
		"namespace '%s' to go Succeeded phase/Completed status: %v", gpuMigPodPulled.Definition.Name, gpuMigPodPulled.Definition.Namespace, err)
}

// logPodEvents logs events related to a specific pod in the given namespace.
// This is used to give more info about the pod when it exists, but it is in unexpected state.
func logPodEvents(podName, namespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "logPodEvents"))
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

// isRunning checks and waits for the GPU burn pod to reach the Running phase using a two-phase
// timeout: Phase 1 checks that the pod is scheduled (fast fail if no GPU node), Phase 2 waits
// for Running (tolerates slow image pulls).
// It first checks quickly and if the pod is already Running or Succeeded, it returns immediately.
// Log validation ensures that the logs are from the pod that was created at the start of the test.
func isRunning(gpuPod *pod.Builder, namespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "isRunning"))
	// This is to avoid waiting, if the pod is already in Running or Succeeded phase.
	// If pod was Completed (or Running) already, there's no need to wait.
	// Avoiding the timeout in case it is Completed already is preferred.
	// Save the pod name before the pull so we can reference it safely even if Pull returns nil.
	podName := gpuPod.Definition.Name
	gpuPod, err := pod.Pull(inittools.APIClient, podName, namespace)
	Expect(err).ToNot(HaveOccurred(), "Pod %s does not exist in namespace %s with error: %v", podName, namespace, err)
	if gpuPod.Object.Status.Phase == corev1.PodRunning || gpuPod.Object.Status.Phase == corev1.PodSucceeded {
		return
	}

	// Phase 1: confirm the pod is scheduled within the timeout window.
	err = gpuPod.WaitUntilScheduled(nvidiagpu.BurnPodScheduledTimeout)
	Expect(err).ToNot(HaveOccurred(), "gpu-burn MIG pod in namespace '%s' was not scheduled "+
		"within the timeout (scheduler may be busy or pod may have failed early): %v", namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG is scheduled onto a GPU node")

	// Phase 2: wait for Running or Succeeded, tolerating time needed for image pull and fast completions.
	err = gpuPod.WaitUntilRunningOrSucceeded(nvidiagpu.BurnPodRunningTimeout)
	if err != nil {
		// Pull a fresh snapshot for diagnostic logging only.
		pod2, err2 := pod.Pull(inittools.APIClient, gpuPod.Definition.Name, namespace)
		if err2 == nil {
			glog.V(gpuparams.Gpu10LogLevel).Infof("Pod %s did not reach Running or Succeeded: %s (%s). Error: %v",
				pod2.Definition.Name, pod2.Object.Status.Phase, pod2.Object.Status.Reason, err)
			logPodEvents(pod2.Definition.Name, namespace)
		}

		Expect(err).ToNot(HaveOccurred(), "gpu-burn pod with MIG in namespace '%s' did not reach "+
			"Running or Succeeded phase (pod may have failed or image pull may have taken too long): %v", namespace, err)
	}
}

// isRunningStatus checks pod phase for time-slicing monitoring without the full two-phase wait.
// Return values:
// 0: Succeeded
// 1: Running
// 2: Pending
// 3: any other
func isRunningStatus(gpuPod *pod.Builder, namespace string) int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "isRunningStatus"))
	pulledPod, err := pod.Pull(inittools.APIClient, gpuPod.Definition.Name, namespace)
	Expect(err).ToNot(HaveOccurred(), "Pod %s does not exist in namespace %s with error: %v", gpuPod.Definition.Name, namespace, err)
	switch pulledPod.Object.Status.Phase {
	case corev1.PodSucceeded:
		return 0
	case corev1.PodRunning:
		return 1
	case corev1.PodPending, corev1.PodUnknown:
		// fall through to WaitUntilInStatus
	case corev1.PodFailed:
		isFailed(gpuPod, namespace)
		return 3
	}

	err = gpuPod.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.BurnPodRunningTimeout)
	var err2 error
	var pod2 *pod.Builder
	if err != nil {
		isFailed(gpuPod, namespace)
		pod2, err2 = pod.Pull(inittools.APIClient, gpuPod.Definition.Name, namespace)
		Expect(err2).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod with MIG in "+
			"namespace '%s' to go to Running phase: %v\n Pod is likely Pending for some reason", namespace, err)
		glog.V(gpuparams.Gpu10LogLevel).Infof("Pod %s is likely Pending for some reason: %s (%s). Error: %v, Error2: %v",
			pod2.Definition.Name, pod2.Object.Status.Phase, pod2.Object.Status.Reason, err, err2)
		logPodEvents(pod2.Definition.Name, namespace)
		return 2
	}
	return 3
}

// isFailed checks whether the GPU burn pod is in Failed phase.
// It pulls the latest pod state; when Failed, logs pod events and fails the test (same style as isCompleted on timeout).
func isFailed(gpuPod *pod.Builder, namespace string) bool {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "isFailed"))
	pulled, err := pod.Pull(inittools.APIClient, gpuPod.Definition.Name, namespace)
	Expect(err).ToNot(HaveOccurred(), "Pod %s does not exist in namespace %s with error: %v",
		gpuPod.Definition.Name, namespace, err)
	if pulled.Object.Status.Phase != corev1.PodFailed {
		return false
	}

	logPodEvents(pulled.Definition.Name, namespace)
	return true
	// Expect(pulled.Object.Status.Phase).To(Equal(corev1.PodSucceeded),
	// 	"gpu-burn pod %s/%s is Failed; status=%+v", namespace, pulled.Definition.Name, pulled.Object.Status)
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

// CheckTimeSlicingGPUBurnPodLogs validates gpu-burn output for a completed time-slicing pod (one visible GPU per pod).
func CheckTimeSlicingGPUBurnPodLogs(logs string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "CheckTimeSlicingGPUBurnPodLogs"))
	Expect(strings.Contains(logs, "Tested 1 GPUs")).To(BeTrue(),
		"gpu-burn logs should contain 'Tested 1 GPUs'; logs excerpt: %.500s", logs)
	CheckGPUBurnPodLogs(logs, 1)
}

// MonitorTimeslicingGPULoad polls one time-slicing gpu-burn pod has succeeded
// nvidia-smi checks (pmon and query-compute-apps) and logs their output.
func MonitorTimeslicingGPULoad(burn *nvidiagpu.GPUBurnConfig, podInfo TsPodInfo,
	workerNodeSelector map[string]string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s %v", colorLog(colorCyan+colorBold, "Monitor GPU load with nvidia-smi pmon and dmon, running pod:"), podInfo.PodName)
	pollInterval := 30 * time.Second

	pmonCmd := []string{"nvidia-smi", "pmon", "-d", "1", "-c", "1"}
	csvCmd := []string{"nvidia-smi", "--query-compute-apps=pid,process_name,used_memory,timestamp,gpu_name,gpu_bus_id,gpu_serial,gpu_uuid", "--format=csv,nounits"}

	activePendingOrRunning := false
	pulled, err := pod.Pull(inittools.APIClient, podInfo.PodName, podInfo.Namespace)
	Expect(err).ToNot(HaveOccurred(), "error pulling gpu-burn pod %s in %s: %v",
		podInfo.PodName, podInfo.Namespace, err)
	name := podInfo.PodName
	phase := pulled.Object.Status.Phase
	glog.V(gpuparams.Gpu100LogLevel).Infof("Pod: %s at phase: %s", name, phase)

	isFailed(pulled, podInfo.Namespace)

	switch phase {
	case corev1.PodPending, corev1.PodRunning:
		activePendingOrRunning = true
	case corev1.PodSucceeded:
		By(fmt.Sprintf("Completed pod %s", name))
	default:
		// PodUnknown, etc. — treat as still in progress
		activePendingOrRunning = true
	}

	if activePendingOrRunning {
		labelPods, errList := pod.List(inittools.APIClient, burn.Namespace, metav1.ListOptions{LabelSelector: burn.PodLabel})
		if errList != nil {
			glog.V(gpuparams.Gpu10LogLevel).Infof("listing pods with %s: %v", burn.PodLabel, errList)
		} else {
			glog.V(gpuparams.GpuLogLevel).Infof("time-slicing monitor: %d pod(s) with label %s in namespace %s",
				len(labelPods), burn.PodLabel, burn.Namespace)
		}

		outPmon := GetCmdOutput(inittools.APIClient, workerNodeSelector, pmonCmd)
		status, pids := GetPidsFromPmon(outPmon, 2)
		glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pids: %v with status: %v", pids, status)
		glog.V(gpuparams.GpuLogLevel).Infof("\ntime-slicing monitor: nvidia-smi pmon:\n%s\n",
			outPmon)

		outCSV := GetCmdOutput(inittools.APIClient, workerNodeSelector, csvCmd)
		status, pids = GetPidFromCSV(outCSV, 1)
		glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pids: %v with status: %v", pids, status)
		glog.V(gpuparams.GpuLogLevel).Infof("\ntime-slicing monitor: nvidia-smi CSV:\n%s\n",
			outCSV)

		status, podInfo := GetPodsWithPids(inittools.APIClient, workerNodeSelector, pids)
		Expect(status).To(BeTrue(), "Error getting pod info: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod info: %v", podInfo)
	}
	time.Sleep(pollInterval)
}

// findDriverPodOnNode returns the nvidia-driver pod on the first node matching nodeSelector.
func findDriverPodOnNode(apiClient *clients.Settings, nodeSelector map[string]string) (podName, namespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "findDriverPodOnNode"))
	nodeBuilder, err := nodes.List(apiClient, metav1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing nodes: %v", err)
	Expect(len(nodeBuilder)).ToNot(BeZero(), "no nodes found matching selector")

	nodeName := nodeBuilder[0].Object.Name

	driverPods, err := apiClient.Pods(nvidiagpu.NvidiaGPUNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=nvidia-driver",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	Expect(err).ToNot(HaveOccurred(), "Error listing driver pods: %v", err)
	Expect(len(driverPods.Items)).ToNot(BeZero(), "No driver pods found on node %s", nodeName)

	driverPod := driverPods.Items[0]
	return driverPod.Name, driverPod.Namespace
}

// MIGCapabilities queries GPU hardware directly using nvidia-smi
// to discover MIG capabilities. This is a fallback when GFD labels are not available.
// Returns true if MIG is supported, along with available MIG instance profiles.
func MIGProfiles(apiClient *clients.Settings, nodeSelector map[string]string) (bool, []MIGProfileInfo, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "MIGProfiles"))
	podName, namespace := findDriverPodOnNode(apiClient, nodeSelector)

	// Query MIG capabilities using nvidia-smi
	// First, try to get MIG instance profiles directly (works even if MIG mode is not enabled)
	cmd := []string{"nvidia-smi", "mig", "-lgip"}
	glog.V(gpuparams.Gpu10LogLevel).Infof("oc rsh -n %s pod/%s %v %v %v", namespace, podName, cmd[0], cmd[1], cmd[2])
	profileOutput, err := ExecCmdInPod(apiClient, podName, namespace, cmd, 30*time.Second)
	Expect(err).ToNot(HaveOccurred(), "Error getting MIG profiles: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("Available MIG instance profiles: \n%s", profileOutput)
	// Parse profiles from output (e.g., "1g.5gb", "2g.10gb", etc.)
	profiles := parseMIGProfiles(profileOutput)
	for _, profile := range profiles {
		glog.V(gpuparams.GpuLogLevel).Infof("profile: %s with gpu_id: %d, slices: %d/%d, p2p: %s, sm:%d, dec: %d, enc: %d, CE=%d, JPEG=%d, OFA=%d, MixedCnt=%d, SliceUsage=%d, MemUsage=%d",
			profile.MigName, profile.GpuID, profile.SliceUsage, profile.Total, profile.P2P, profile.SM, profile.DEC, profile.ENC,
			profile.CE, profile.JPEG, profile.OFA, profile.MixedCnt, profile.SliceUsage, profile.MemUsage)
	}
	return true, profiles, nil
}

// Internal functions
// ParseCLIParameters parses CLI parameters and sets the global variables.
// This must be called after flags are parsed (e.g., in a BeforeSuite or BeforeAll hook).
func ParseCLIParameters() {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "ParseCLIParameters"))
	wasProvided := isFlagProvided("mixed.mig.instances")
	if wasProvided {
		MixedMigInstances = parseMigInstances(MigInstances, strconv.Itoa(defaultMigInstances))
	} else {
		MixedMigInstances = nil
	}
}

// isFlagProvided checks if a flag was explicitly set on the command line.
// Returns true if the flag was provided, false if it's using the default value.
func isFlagProvided(flagName string) bool {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s %v", colorLog(colorCyan+colorBold, "isFlagProvided:"), flagName)
	provided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			provided = true
		}
	})
	return provided
}

func parseMigInstances(s string, defaults string) []int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "parseMigInstances"))
	regex := regexp.MustCompile(`\d+`)
	matches := regex.FindAllString(s, -1)
	if len(matches) == 0 {
		s = defaults
		matches = regex.FindAllString(s, -1)
	}
	result := []int{}
	for _, match := range matches {
		instance, _ := strconv.Atoi(match)
		result = append(result, instance)
	}
	return result
}

func LogCLIParameterValues() {
	// Check if the flags were explicitly provided on the command line
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "LogCLIParameterValues"))
	wasProvided := isFlagProvided("single.mig.profile")
	if !wasProvided {
		GinkgoWriter.Printf("Flag --single.mig.profile not provided, using default: %d\n", SingleMigProfile)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s %d", colorLog(colorCyan+colorBold, "Value of --single.mig.profile parameter: "), SingleMigProfile)
	}

	wasProvided = isFlagProvided("mixed.mig.pod-delay")
	if !wasProvided {
		GinkgoWriter.Printf("Flag --mixed.mig.pod-delay not provided, using default: %d\n", PodDelay)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s %d", colorLog(colorCyan+colorBold, "Value of --mixed.mig.pod-delay parameter: "), PodDelay)
	}

	wasProvided = isFlagProvided("mixed.mig.instances")
	if !wasProvided {
		GinkgoWriter.Printf("Flag --mixed.mig.instances not provided, using default: %v\n", defaultMigInstances)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s %v, parsed values: %v",
			colorLog(colorCyan+colorBold, "Value of --mixed.mig.instances parameter: "), MigInstances,
			parseMigInstances(MigInstances, strconv.Itoa(defaultMigInstances)))
	}

	wasProvided = isFlagProvided("no-color")
	if !wasProvided {
		GinkgoWriter.Printf("Flag --no-color not provided, using default: %v\n", NoColor)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s %v", colorLog(colorCyan+colorBold, "Value of --no-color parameter: "), NoColor)
	}

	// wasProvided = isFlagProvided("time.slicing.pod-count")
	// if !wasProvided {
	// 	GinkgoWriter.Printf("Flag --time.slicing.pod-count not provided, using default: %d\n", TsPodCount)
	// } else {
	// 	glog.V(gpuparams.Gpu10LogLevel).Infof("%s %d", colorLog(colorCyan+colorBold, "Value of --time.slicing.pod-count parameter: "), TsPodCount)
	// }

	wasProvided = isFlagProvided("time.slicing.instances")
	if !wasProvided {
		GinkgoWriter.Printf("Flag --time.slicing.instances not provided, using default: %q\n", defaultTsInstancesCSV)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s %q, parsed: %v\n",
			colorLog(colorCyan+colorBold, "Value of --time.slicing.instances parameter: "), TsInstancesCSV,
			parseMigInstances(TsInstancesCSV, defaultTsInstancesCSV))
	}

	wasProvided = isFlagProvided("time.slicing.mon-after-pod")
	if !wasProvided {
		GinkgoWriter.Printf("Flag --time.slicing.mon-after-pod not provided, using default: %d\n", TsMonAfterPod)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("%s %d", colorLog(colorCyan+colorBold, "Value of --time.slicing.mon-after-pod parameter: "), TsMonAfterPod)
	}

}

// ExecCmdInPod executes a command (e.g. nvidia-smi mig -lgip) in a pod and returns the output
// If similar function is needed for other purposes, consider renaming
func ExecCmdInPod(apiClient *clients.Settings, podName, namespace string, command []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "ExecCmdInPod"))
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
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "parseMIGProfiles"))

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

func GetCmdOutput(apiClient *clients.Settings, nodeSelector map[string]string, cmd []string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "GetCmdOutput"))
	podName, namespace := findDriverPodOnNode(apiClient, nodeSelector)

	// send command to driver pod (usually nvidia-smi)
	// e.g. cmd := []string{"nvidia-smi", "pmon", "-c", "1"}
	var cmd1 string
	for _, cmd := range cmd {
		cmd1 = cmd1 + cmd + " "
	}
	glog.V(gpuparams.Gpu10LogLevel).Infof("oc rsh -n %s pod/%s %v", namespace, podName, cmd1)
	output, err := ExecCmdInPod(apiClient, podName, namespace, cmd, 30*time.Second)
	Expect(err).ToNot(HaveOccurred(), "Error getting command output: %v", err)
	glog.V(gpuparams.Gpu100LogLevel).Infof("Command output: \n%s", output)

	return output
}

func CheckGPUBurnPids(apiClient *clients.Settings, nodeSelector map[string]string, cmd []string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "CheckGPUBurnPids"))
	podName, namespace := findDriverPodOnNode(apiClient, nodeSelector)

	// send command to driver pod (usually nvidia-smi)
	// e.g. cmd := []string{"nvidia-smi", "pmon", "-c", "1"}
	var cmd1 string
	for _, cmd := range cmd {
		cmd1 = cmd1 + cmd + " "
	}
	glog.V(gpuparams.Gpu10LogLevel).Infof("oc rsh -n %s pod/%s %v", namespace, podName, cmd1)
	output, err := ExecCmdInPod(apiClient, podName, namespace, cmd, 30*time.Second)
	Expect(err).ToNot(HaveOccurred(), "Error getting MIG profiles: %v", err)
	glog.V(gpuparams.Gpu100LogLevel).Infof("Available MIG instance profiles: \n%s", output)

	return output
}

// CSVLineRe matches one data line from "nvidia-smi --<cmd> --format=csv,nounits" at the time.
// FindAllStringSubmatch returns one element per line: matches[0] = [fullMatch, cap1, cap2, ...].
// So capture groups are matches[0][1]=pid, matches[0][2]=process_name, matches[0][3]=used_memory, ..., matches[0][8]=gpu_uuid.
var CSVLineRe = regexp.MustCompile(`^(\d+),.(.+?),.(\d+),.(.+?),.(.+?),.(.+?),.(.+?),.(.+?)$`)

// pmonLineRe matches one data line from "nvidia-smi pmon -c 1".
// With -c values bigger than 1, duplicate PIDs can appear in the output.
// FindAllStringSubmatch returns one element per line: matches[0] = [fullMatch, cap1, cap2, ...].
// So capture groups are matches[0][1]=gpuIdx, matches[0][2]=pid, matches[0][3]=type, ..., matches[0][10]=command.
var pmonLineRe = regexp.MustCompile(`^\s*(\d+)\s+(\d+|-)\s+([CG]|-)\s+(\d+|-)\s+(\d+|-)\s+(\d+|-)\s+(\d+|-)\s+(\d+|-)\s+(\d+|-)\s+(\S+)\s*$`)

// PidParseConfig configures how to extract PIDs from regex-matched lines (e.g. nvidia-smi CSV or pmon output).
type PidParseConfig struct {
	Re         *regexp.Regexp // line regex; capture groups form row[1], row[2], ...
	PidColumn  int            // index into row for the PID value (row[PidColumn])
	SkipColumn int            // if >= 0, skip line when row[SkipColumn] == "-"
}

// GetPidsWithRegex parses output line-by-line, matches each line with re, and collects integer values from the configured column.
func GetPidsWithRegex(output string, cfg PidParseConfig) (bool, []int) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "GetPidsWithRegex"))
	pids := []int{}
	glog.V(gpuparams.GpuLogLevel).Infof("regex: %v", cfg)
	glog.V(gpuparams.GpuLogLevel).Infof("output: %q", output)
	for _, line := range strings.Split(output, "\n") {
		glog.V(gpuparams.GpuLogLevel).Infof("line: %q", line)
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		matches := cfg.Re.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			glog.V(gpuparams.GpuLogLevel).Infof("line did not match: %q", line)
			continue
		}
		row := matches[0]
		if cfg.SkipColumn >= 0 && cfg.SkipColumn < len(row) && row[cfg.SkipColumn] == "-" {
			continue
		}
		if cfg.PidColumn >= len(row) {
			continue
		}
		pid, err := strconv.Atoi(row[cfg.PidColumn])
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}

	pids = uniqueInts(pids)
	return true, pids
}

func uniqueInts(s []int) []int {
	seen := make(map[int]struct{})
	out := make([]int, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func GetPidFromCSV(output string, index int) (bool, []int) {
	return GetPidsWithRegex(output, PidParseConfig{
		Re:         CSVLineRe,
		PidColumn:  index,
		SkipColumn: 2,
	})
}

func GetPidsFromPmon(output string, index int) (bool, []int) {
	return GetPidsWithRegex(output, PidParseConfig{
		Re:         pmonLineRe,
		PidColumn:  index,
		SkipColumn: 2,
	})
}

// pid vs pod
type PodInfo struct {
	PodName string
	pid     int
}

func GetPodsWithPids(apiClient *clients.Settings, nodeSelector map[string]string, pids []int) (bool, []PodInfo) {
	glog.V(gpuparams.Gpu100LogLevel).Infof("GetPodsWithPids")
	// var podInfo []PodInfo
	// return true, []PodInfo(PodInfo{PodName: "pod1", pid: 1})
	glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pids: %v", pids)
	for _, pid := range pids {
		glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod pid: %d", pid)
		podName := fmt.Sprintf("pod-%d", pid)
		glog.V(gpuparams.GpuLogLevel).Infof("Time-slicing pod name: %s", podName)
	}
	return true, []PodInfo{}
}
