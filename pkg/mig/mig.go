package mig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
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

// ANSI color constants for console output highlighting
// colors are \033[31m - red through \033[37m - white
const (
	colorReset = "\033[0m"
	colorCyan  = "\033[36m"
	colorBold  = "\033[1m"
)

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
// Prepare the workload and deploy it (namespace, configmap, pod)
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
	// Query MIG capabilities and select MIG profile and index to be used later.
	// Select MIG profile and index to be used later
	By("Read NVIDIAGPU_SINGLE_MIG_PROFILE environment variable and select MIG profile")
	useMigIndex = ReadMIGParameter(nvidiaGPUConfig.SingleMIGProfile)
	migCapabilities, useMigIndex = SelectMigProfile(WorkerNodeSelector, useMigIndex)
	Expect(migCapabilities).ToNot(BeNil(), "SelectMigProfile did not return migCapabilities")

	// Set the MIG strategy and mig.config labels on GPU worker nodes
	By("Set the MIG strategy label on GPU worker nodes")
	useMigProfile = SetMIGLabelsOnNodes(migCapabilities, useMigIndex, WorkerNodeSelector)

	// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be notReady after node label changes", nvidiagpu.ClusterPolicyNotReadyTimeout))
	_ = wait.ClusterPolicyNotReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyNotReadyCheckInterval, nvidiagpu.ClusterPolicyNotReadyTimeout)

	// Wait for ClusterPolicy to be ready. Changing labels will take a couple of minutes.
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout))
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready: %v", err)

	By("Check for MIG single strategy capability labels on GPU nodes")
	migSingleLabel := "nvidia.com/mig.strategy"
	expectedLabelValue := "single"
	err = wait.NodeLabelExists(inittools.APIClient, migSingleLabel, expectedLabelValue,
		labels.Set(WorkerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label '%s' set to '%s'", migSingleLabel, expectedLabelValue)
	glog.V(gpuparams.Gpu10LogLevel).Infof("MIG single strategy label found, proceeding with test")

	defer func() {
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer1 (set MIG labels to non-mig on GPU nodes)")
		ResetMIGLabelsToDisabled(WorkerNodeSelector)
	}()

	// Pull existing ClusterPolicy
	By("Pull existing ClusterPolicy")
	pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy: %v", err)
	initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
	Expect(initialClusterPolicyResourceVersion).ToNot(BeEmpty(), "initialClusterPolicyResourceVersion is empty after pull ClusterPolicy")

	// Configure MIG strategy for the test
	By("Configuring MIG strategy in ClusterPolicy")
	clusterArch, err := configureMIGStrategy(pulledClusterPolicyBuilder, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "error configuring MIG strategy and getting cluster architecture: %v", err)

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
	// Sometimes available is zero, so using total instead
	gpuMigPodPulled := DeployGPUWorkload(
		BurnImageName[clusterArch],
		burn.PodName,
		burn.Namespace,
		useMigProfile,
		migCapabilities[useMigIndex].Total,
		burn.PodLabel)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer3 (gpuMigPodPulled) sleeping for 5 seconds	")
		if cleanupAfterTest {
			_, err := gpuMigPodPulled.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pod: %v", err)
		}
	}()

	// Wait for GPU Burn pod to complete
	By(fmt.Sprintf("Wait for up to %s for gpu-burn pod with MIG to be in Running phase", nvidiagpu.BurnPodRunningTimeout))
	waitForGPUBurnPodToComplete(gpuMigPodPulled, burn.Namespace)

	By("Get the gpu-burn pod logs")
	gpuBurnMigLogs := GetGPUBurnPodLogs(gpuMigPodPulled)

	By("Parse the gpu-burn pod logs and check for successful execution with MIG")
	CheckGPUBurnPodLogs(gpuBurnMigLogs, migCapabilities[useMigIndex].Total)
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

// ReadMIGParameter checks the SingleMIGProfile parameter and parses the MIG index if provided.
// It returns the parsed MIG index, or -1 if not set or invalid (i.e. contains no digits)
// -1 translates to random selection of MIG profile
func ReadMIGParameter(singleMIGProfile string) int {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check parameters"))
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

// CleanupWorkloadResources cleans up existing GPU burn pods and configmaps, then waits for cleanup to complete.
func CleanupWorkloadResources(burn *nvidiagpu.GPUBurnConfig) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Cleaning up namespace and workload resources"))
	// Delete any existing gpu-burn pods with the label. There may be none.
	gpuBurnPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found gpu-burn pod '%s' with: %v", gpuBurnPodName, err)
		existingPodBuilder, err := pod.Pull(inittools.APIClient, gpuBurnPodName, burn.Namespace)
		Expect(err).ToNot(HaveOccurred(), "Error pulling workload pod: %v", err)
		_, err = existingPodBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting workload pod: %v", err)
		err = existingPodBuilder.WaitUntilDeleted(30 * time.Second)
		Expect(err).ToNot(HaveOccurred(), "Error waiting for workload pod to be deleted: %v", err)
	}

	// Delete the configmap if it exists
	existingConfigmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found gpu-burn configmap '%s' with: %v", burn.ConfigMapName, err)
		err = existingConfigmapBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting workload configmap: %v", err)
		err = existingConfigmapBuilder.WaitUntilDeleted(30 * time.Second)
		Expect(err).ToNot(HaveOccurred(), "Error waiting for workload configmap to be deleted: %v", err)
	}
}

// SelectMigProfile queries MIG profiles from hardware and selects/validates the MIG index.
// It returns the MIG capabilities and the selected/validated MIG index.
// If no MIG configurations are found, it calls Skip to skip the test.
func SelectMigProfile(WorkerNodeSelector map[string]string, useMigIndex int) ([]MIGProfileInfo, int) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Query and select MIG profile"))

	_, migCapabilities, err := MIGProfiles(inittools.APIClient, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Error getting MIG capabilities: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("Found %d MIG configuration profiles", len(migCapabilities))
	for i, info := range migCapabilities {
		glog.V(gpuparams.GpuLogLevel).Infof("  [%d] Profile name: %s, slices %d/%d", i, info.MigName, info.Available, info.Total)
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

// setMIGLabelsOnNodes sets MIG strategy and configuration labels on GPU worker nodes.
// It returns the MIG profile flavor that was set.
func SetMIGLabelsOnNodes(migCapabilities []MIGProfileInfo, useMigIndex int, WorkerNodeSelector map[string]string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Set MIG labels on nodes"))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG strategy label on GPU worker nodes from %d entry of the list (profile: %s with %d/%d slices)",
		useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
	MigProfile := "all-" + migCapabilities[useMigIndex].MigName
	strategy := "single"
	useMigProfile := migCapabilities[useMigIndex].Flavor

	// use first mig profile from the list, unless specified otherwise
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
	for _, nodeBuilder := range nodeBuilders {
		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG %s strategy label on node '%s' (overwrite=true)", strategy, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.strategy", strategy)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG %s strategy label on node '%s'", strategy, nodeBuilder.Definition.Name)

		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG configuration label %s on node '%s' (overwrite=true)", MigProfile, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", MigProfile)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG configuration label on node '%s' with %s", nodeBuilder.Definition.Name, MigProfile)
	}

	return useMigProfile
}

// ResetMIGLabelsToDisabled sets MIG strategy and configuration labels to "all-disabled" on GPU worker nodes.
func ResetMIGLabelsToDisabled(WorkerNodeSelector map[string]string) {
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
func updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder *nvidiagpu.Builder, WorkerNodeSelector map[string]string) {
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

	err = wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.strategy", "single", labels.Set(WorkerNodeSelector),
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
// It sets the MIG strategy to single, updates the ClusterPolicy, and then gets the cluster architecture
// from the first GPU enabled worker node.
func configureMIGStrategy(
	pulledClusterPolicyBuilder *nvidiagpu.Builder,
	WorkerNodeSelector map[string]string) (string, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Configure MIG strategy and get cluster architecture"))
	migStrategy := nvidiagpuv1.MIGStrategySingle
	glog.V(gpuparams.Gpu10LogLevel).Infof(
		"Setting ClusterPolicy MIG strategy to '%s'", migStrategy)

	currentMigStrategy := pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Current MIG strategy is '%s', updating to '%s'",
		currentMigStrategy, migStrategy)
	pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy = migStrategy
	updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder, WorkerNodeSelector)

	By(fmt.Sprintf("Getting cluster architecture from nodes with WorkerNodeSelector: %v", WorkerNodeSelector))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Getting cluster architecture from nodes with "+
		"WorkerNodeSelector: %v", WorkerNodeSelector)
	clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Error getting cluster architecture: %v", err)
	return clusterArch, nil
}

// creates and deploys a GPU burn pod with MIG configuration,
// then retrieves it from the cluster. It returns the pulled pod builder for further operations.
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

	By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
	gpuMigPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, namespace, podLabel)
	Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod with label "+
		"'app=gpu-burn-app' from namespace '%s': %v", namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpuMigPodName is %s", gpuMigPodName)

	By("Pull the gpu-burn pod object from the cluster")
	gpuMigPodPulled, err := pod.Pull(inittools.APIClient, gpuMigPodName, namespace)
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
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG now in Succeeded Phase/Completed status")
}

// GetGPUBurnPodLogs retrieves the logs from the GPU burn pod with MIG configuration.
// It returns the pod logs as a string.
func GetGPUBurnPodLogs(gpuMigPodPulled *pod.Builder) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Get GPU burn pod logs with MIG configuration"))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Get the gpu-burn pod logs with MIG configuration")

	gpuBurnMigLogs, err := gpuMigPodPulled.GetLog(nvidiagpu.BurnLogCollectionPeriod, "gpu-burn-ctr")

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
		Expect(match1Mig).ToNot(BeFalse(), "gpu-burn pod execution with MIG was FAILED")
	}
	match2Mig := strings.Contains(gpuBurnMigLogs, "100.0%  proc'd:")

	Expect(match2Mig).ToNot(BeFalse(), "gpu-burn pod execution with MIG was FAILED")
	glog.V(gpuparams.Gpu10LogLevel).Infof("Gpu-burn pod execution with MIG configuration was successful")
}

var useColors = os.Getenv("NO_COLOR") != "true"

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
		glog.V(gpuparams.GpuLogLevel).Infof("profile: %s with gpu_id: %d, slices: %d/%d, p2p: %s, sm:%d, dec: %d, enc: %d, CE=%d, JPEG=%d, OFA=%d",
			profile.MigName, profile.GpuID, profile.Available, profile.Total, profile.P2P, profile.SM, profile.DEC, profile.ENC,
			profile.CE, profile.JPEG, profile.OFA)
	}
	return true, profiles, nil
}

// MIGProfileInfo represents information about a MIG profile
type MIGProfileInfo struct {
	GpuID     int    // Physical GPU index
	MigType   string // always MIG, probably unnecessary
	MigName   string // e.g., 1g.5gb, 2g.10gb, 3g.20gb
	MigID     int    // Profile identifier used when creating instances
	Available int    // number of available instances
	Total     int    // total number of instances
	Memory    string // memory in GB, need to be converted to float64
	P2P       string // Peer-to-peer support between instances (No = not supported)
	SM        int    // SM: Streaming Multiprocessors per instance (compute units)
	DEC       int    // DEC: Video decode units per instance
	ENC       int    // ENC: Video encode units per instance
	CE        int    // CE: Copy Engine units per instance (second row)
	JPEG      int    // JPEG: JPEG decoder units per instance (second row)
	OFA       int    // OFA: Optical Flow Accelerator units per instance (second row)
	Flavor    string // single strategy: nvidia.com/gpu or all-balanced: nvidia.com/mig-*
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
