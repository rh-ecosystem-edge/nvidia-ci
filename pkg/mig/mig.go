package mig

import (
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
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"
	gpuburn "github.com/rh-ecosystem-edge/nvidia-ci/internal/gpu-burn"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ANSI color constants for console output highlighting
const (
	colorReset = "\033[0m"
	//	colorRed     = "\033[31m"
	//	colorGreen   = "\033[32m"
	//	colorYellow  = "\033[33m"
	//	colorBlue    = "\033[34m"
	//	colorMagenta = "\033[35m"
	colorCyan = "\033[36m"
	//	colorWhite   = "\033[37m"
	colorBold = "\033[1m"
)

// TestSingleMIGGPUBurn performs the GPU Burn test with single strategy MIG Configuration
func TestSingleMIGGPUBurn(nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig, burn *nvidiagpu.GPUBurnConfig, BurnImageName map[string]string, WorkerNodeSelector map[string]string, cleanupAfterTest bool) {
	// select one mig profile from the list of mig profiles
	var useMigProfile string // = "mig-1g.5gb"  // mig profiles are queried from the hardware
	var useMigIndex int = -1 // will be set to random value after migCapabilities is populated
	var migCapabilities []get.MIGProfileInfo
	// glog.V(gpuparams.Gpu10LogLevel).Infof("Starting GPU Burn with MIG Configuration testcase")
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Starting GPU Burn with MIG Configuration testcase"))

	// ***** Select MIG profile and index to be used later
	By("Starting GPU Burn with single strategy MIG Configuration testcase")
	useMigIndex = checkAndParseMIGProfile(nvidiaGPUConfig.SingleMIGProfile)

	// ***** Cleaning up previous GPU Burn resources
	By("Cleanup if necessary")
	cleanupGPUBurnResources(burn)

	// ***** Query MIG capabilities and select MIG profile and index to be used later.
	// ***** Skip the test if no MIG configurations are found.
	// ***** MIG index is set to random value if not set in the environment variable NVIDIAGPU_SINGLE_MIG_PROFILE
	By("Check if MIG single strategy is available on GPU nodes")
	migCapabilities, useMigIndex = queryAndSelectMIGProfile(WorkerNodeSelector, useMigIndex)
	if migCapabilities == nil {
		return
	}

	// ***** Set the MIG strategy and mig.config labels on GPU worker nodes
	By("Set the MIG strategy label on GPU worker nodes")
	useMigProfile = setMIGLabelsOnNodes(migCapabilities, useMigIndex, WorkerNodeSelector)

	// ***** Wait for ClusterPolicy to be ready. Changing labels will take a couple of minutes.
	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout))
	waitForClusterPolicyReady()

	// ***** Check for MIG single strategy capability labels on GPU nodes.
	By("Check for MIG single strategy capability labels on GPU nodes")
	checkMIGSingleStrategyLabel(WorkerNodeSelector)

	defer func() {
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer1 (set MIG labels to non-mig on GPU nodes)")
		ResetMIGLabelsToDisabled(WorkerNodeSelector)
	}()

	// ***** Pull existing ClusterPolicy
	By("Pull existing ClusterPolicy")
	pulledClusterPolicyBuilder, _ := pullClusterPolicyAndCaptureResourceVersion()

	// ***** Configure MIG strategy for the test
	By("Configuring MIG strategy in ClusterPolicy")
	clusterArch, _ := configureMIGStrategyAndGetClusterArch(pulledClusterPolicyBuilder, WorkerNodeSelector)

	// ***** Check and create test-gpu-burn namespace if it is missing
	By("Ensure test-gpu-burn namespace exists")
	ensureGPUBurnNamespaceExists(burn.Namespace)

	// ***** Create GPU Burn configmap in test-gpu-burn namespace
	By("Deploy GPU Burn configmap in test-gpu-burn namespace")
	configmapBuilder := createAndPullGPUBurnConfigMap(burn.ConfigMapName, burn.Namespace)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer2 (configmapBuilder) sleeping for 15 seconds")
		if cleanupAfterTest {
			err := configmapBuilder.Delete()
			time.Sleep(time.Second * 15)
			if err != nil {
				glog.Errorf("Failed to delete configmap during cleanup: %v", err)
			}
		}
	}()

	// ***** Deploy GPU Burn pod with MIG single strategy configuration
	By("Deploy gpu-burn pod with MIG configuration in test-gpu-burn namespace")
	gpuMigPodPulled := deployGPUBurnPodWithMIGAndPull(
		BurnImageName[clusterArch],
		burn.Namespace,
		useMigProfile,
		migCapabilities[useMigIndex].Available,
		burn.PodLabel)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer3 (gpuMigPodPulled) sleeping for 5 seconds	")
		if cleanupAfterTest {
			time.Sleep(time.Second * 5)
			_, err := gpuMigPodPulled.Delete()
			if err != nil {
				glog.Errorf("Failed to delete gpu-burn pod during cleanup: %v", err)
			}
		}
	}()

	// ***** Wait for GPU Burn pod to complete
	By(fmt.Sprintf("Wait for up to %s for gpu-burn pod with MIG to be in Running phase", nvidiagpu.BurnPodRunningTimeout))
	waitForGPUBurnPodToComplete(gpuMigPodPulled, burn.Namespace)

	By("Get the gpu-burn pod logs")
	gpuBurnMigLogs := getGPUBurnPodLogs(gpuMigPodPulled, burn.Namespace)

	// Need to add checking for other possible GPU's
	By("Parse the gpu-burn pod logs and check for successful execution with MIG")
	parseAndValidateGPUBurnPodLogs(gpuBurnMigLogs, migCapabilities[useMigIndex].Available)
}

// reportOpenShiftVersionAndEnsureNFD reports the OpenShift version, writes it to a report file,
// and ensures that Node Feature Discovery (NFD) is installed.
func ReportOpenShiftVersionAndEnsureNFD(nfdInstance *operatorconfig.CustomConfig) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Report OpenShift version and ensure NFD"))
	ocpVersion, err := inittools.GetOpenShiftVersion()
	glog.V(gpuparams.GpuLogLevel).Infof("Current OpenShift cluster version is: '%s'", ocpVersion)

	if err != nil {
		glog.Error("Error getting OpenShift version: ", err)
	} else if err := inittools.GeneralConfig.WriteReport(OpenShiftVersionFile, []byte(ocpVersion)); err != nil {
		glog.Error("Error writing an OpenShift version file: ", err)
	}

	nfd.EnsureNFDIsInstalled(inittools.APIClient, nfdInstance, ocpVersion, gpuparams.GpuLogLevel)
}

// HasLabel checks if a specific label is present in the TEST_LABELS filter
// It parses the comma-separated label filter and does exact matching
func HasLabel(labelToCheck string) bool {
	testLabelsFilter := os.Getenv("TEST_LABELS")
	if testLabelsFilter == "" {
		return false
	}

	// Split by comma and check each label
	labels := strings.Split(testLabelsFilter, ",")
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == labelToCheck {
			return true
		}
	}
	return false
}

// CleanupGPUOperatorResources performs cleanup of GPU Operator resources
// It checks if cleanup should run based on cleanupAfterTest and cleanup label
func CleanupGPUOperatorResources(cleanupAfterTest bool, burnNamespace string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Cleanup GPU Operator Resources"))
	if !cleanupAfterTest {
		if HasLabel("cleanup") {
			glog.V(gpuparams.GpuLogLevel).Infof("Cleanup is enabled via cleanup label, running cleanup")
		} else {
			glog.V(gpuparams.GpuLogLevel).Infof("Cleanup is disabled, skipping GPU operator cleanup")
			return
		}
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Starting cleanup of GPU Operator Resources")

	By("Deleting ClusterPolicy")
	clusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	if err == nil && clusterPolicyBuilder.Exists() {
		_, err := clusterPolicyBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting ClusterPolicy: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy deleted successfully")
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy not found or already deleted")
	}

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

	By("Deleting Subscription")
	subBuilder, err := olm.PullSubscription(inittools.APIClient, nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace)
	if err == nil && subBuilder.Exists() {
		err := subBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting Subscription: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Subscription deleted successfully")
	}

	By("Deleting OperatorGroup")
	ogBuilder, err := olm.PullOperatorGroup(inittools.APIClient, nvidiagpu.OperatorGroupName, nvidiagpu.SubscriptionNamespace)
	if err == nil && ogBuilder.Exists() {
		err := ogBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting OperatorGroup: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("OperatorGroup deleted successfully")
	}

	By("Deleting GPU Operator Namespace")
	nsBuilder := namespace.NewBuilder(inittools.APIClient, nvidiagpu.SubscriptionNamespace)
	if nsBuilder.Exists() {
		err := nsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", nvidiagpu.SubscriptionNamespace)
	}

	By("Deleting GPU Burn Namespace")
	burnNsBuilder := namespace.NewBuilder(inittools.APIClient, burnNamespace)
	if burnNsBuilder.Exists() {
		err := burnNsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting burn namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", burnNamespace)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Completed cleanup of GPU Operator Resources")
}

// ShouldKeepOperator checks if the operator should be kept based on test labels and upgrade channel
func ShouldKeepOperator(labelsToCheck []string) bool {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "ShouldKeepOperator"))

	// Get the label filter from Ginkgo command line
	filterQuery := GinkgoLabelFilter()
	specReport := CurrentSpecReport()
	currentLabels := specReport.Labels()

	// Log the labels present in the ginkgo command line before the for loop
	glog.V(gpuparams.Gpu10LogLevel).Infof("Ginkgo label filter from command line: %s", filterQuery)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Current test labels from Ginkgo: %v", currentLabels)
	glog.V(gpuparams.Gpu10LogLevel).Infof("CurrentSpecReport: %v", currentLabels)

	// Check if test has any of these labels

	// for _, label := range labelsToCheck {
	// 	glog.V(gpuparams.GpuLogLevel).Infof("Checking if label %s is present in CurrentSpecReport", label)
	// 	if matches, _ := specReport.MatchesLabelFilter(label); matches {
	// 		glog.V(gpuparams.Gpu100LogLevel).Infof("Label %s is present in CurrentSpecReport", label)
	// 		return true
	// 	}
	// }
	for _, label := range labelsToCheck {
		glog.V(gpuparams.GpuLogLevel).Infof("Checking if label %s is present in Ginkgo label filter", label)
		if strings.Contains(filterQuery, label) {
			glog.V(gpuparams.Gpu100LogLevel).Infof("Label %s is present in Ginkgo label filter", label)
			return true
		}
	}

	return false
}

// checkAndParseMIGProfile checks the SingleMIGProfile parameter and parses the MIG index if provided.
// It returns the parsed MIG index, or -1 if not set or invalid.
func checkAndParseMIGProfile(singleMIGProfile string) int {
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

// cleanupGPUBurnResources cleans up existing GPU burn pods and configmaps, then waits for cleanup to complete.
func cleanupGPUBurnResources(burn *nvidiagpu.GPUBurnConfig) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Cleaning up test-gpu-burn namespace resources, if necessary"))
	// Delete any existing gpu-burn pods with the label
	gpuBurnPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found existing gpu-burn pod '%s', deleting it", gpuBurnPodName)
		existingPodBuilder, err := pod.Pull(inittools.APIClient, gpuBurnPodName, burn.Namespace)
		if err == nil {
			_, err = existingPodBuilder.Delete()
			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("Error deleting existing gpu-burn pod: %v", err)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Successfully deleted gpu-burn pod '%s'", gpuBurnPodName)
			}
		}
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("No existing gpu-burn pod found to delete")
	}

	// Delete the configmap if it exists
	existingConfigmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found existing gpu-burn configmap '%s', deleting it", burn.ConfigMapName)
		err = existingConfigmapBuilder.Delete()
		if err != nil {
			glog.V(gpuparams.GpuLogLevel).Infof("Error deleting gpu-burn configmap: %v", err)
		} else {
			glog.V(gpuparams.GpuLogLevel).Infof("Successfully deleted gpu-burn configmap '%s'", burn.ConfigMapName)
		}
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("No existing gpu-burn configmap found to delete")
	}
	// No need to delete namespace, unless we want to ensure it is completely clean

	// Wait a moment for resources to be cleaned up
	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for test-gpu-burn resources cleanup to complete")
	time.Sleep(3 * time.Second)
}

// queryAndSelectMIGProfile queries MIG capabilities from hardware and selects/validates the MIG index.
// It returns the MIG capabilities and the selected/validated MIG index.
// If no MIG configurations are found, it calls Skip to skip the test.
func queryAndSelectMIGProfile(WorkerNodeSelector map[string]string, useMigIndex int) ([]get.MIGProfileInfo, int) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Query and select MIG profile"))

	_, migCapabilities, err := get.MIGCapabilities(inittools.APIClient, WorkerNodeSelector)
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Could not discover MIG configurations: %v", err)
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("Found %d MIG configuration profiles", len(migCapabilities))
		for i, info := range migCapabilities {
			glog.V(gpuparams.GpuLogLevel).Infof("  [%d] Profile name: %s, slices %d/%d", i, info.MigName, info.Available, info.Total)
		}
	}

	if len(migCapabilities) == 0 {
		glog.V(gpuparams.GpuLogLevel).Infof("No MIG configurations available")
		Skip("No MIG configurations found")
		return nil, -1
	}

	// Select random index if not already set
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
func setMIGLabelsOnNodes(migCapabilities []get.MIGProfileInfo, useMigIndex int, WorkerNodeSelector map[string]string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Set MIG labels on nodes"))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG strategy label on GPU worker nodes from %d entry of the list (profile: %s with %d/%d slices)",
		useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
	MigProfile := "all-" + migCapabilities[useMigIndex].MigName
	strategy := "single"
	useMigProfile := migCapabilities[useMigIndex].Flavor

	// use first mig profile from the list, unless specified otherwise
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	if err != nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Failed to list worker nodes: %v", err)
	} else {
		for _, nodeBuilder := range nodeBuilders {
			glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG %s strategy label on node '%s' (overwrite=true)", strategy, nodeBuilder.Definition.Name)
			nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.strategy", strategy)
			_, err = nodeBuilder.Update()
			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("Failed to update node '%s' with MIG label: %v, error: %v", nodeBuilder.Definition.Name, strategy, err)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG %s strategy label on node '%s'", strategy, nodeBuilder.Definition.Name)
			}
			glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG configuration label %s on node '%s' (overwrite=true)", MigProfile, nodeBuilder.Definition.Name)
			nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", MigProfile)
			_, err = nodeBuilder.Update()
			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("Failed to update node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG configuration label on node '%s' with %s", nodeBuilder.Definition.Name, MigProfile)
			}
		}
	}

	glog.V(gpuparams.Gpu10LogLevel).Infof("Sleeping for 30 seconds to allow GPU operator to process node label changes")
	time.Sleep(30 * time.Second)

	return useMigProfile
}

// waitForClusterPolicyReady waits for ClusterPolicy to be ready after node label changes.
func waitForClusterPolicyReady() {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Waiting for ClusterPolicy to be ready after setting MIG node labels"))
	err := wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Warning: ClusterPolicy may not be fully ready after node label changes: %v", err)
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy is ready after node label changes")
	}
}

// checkMIGSingleStrategyLabel checks for MIG single strategy capability labels on GPU nodes.
func checkMIGSingleStrategyLabel(WorkerNodeSelector map[string]string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Check MIG single strategy label"))
	migSingleLabel := "nvidia.com/mig.strategy.single"

	migSingleAvailable, err := check.NodeWithLabel(inittools.APIClient, migSingleLabel, WorkerNodeSelector)
	if err != nil || !migSingleAvailable {
		glog.V(gpuparams.Gpu10LogLevel).Infof("MIG single strategy label not found on GPU nodes: %v", err)
		glog.V(gpuparams.Gpu10LogLevel).Infof("Note: MIG strategy labels may not be available yet even if MIG is configured.")
		glog.V(gpuparams.Gpu10LogLevel).Infof("The test will proceed if MIG configurations are discovered later.")
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("MIG single strategy label found, proceeding with test")
	}
}

// ResetMIGLabelsToDisabled sets MIG strategy and configuration labels to "all-disabled" on GPU worker nodes.
func ResetMIGLabelsToDisabled(WorkerNodeSelector map[string]string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Reset MIG labels to disabled"))
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	if err != nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Failed to list worker nodes: %v", err)
	} else {
		for _, nodeBuilder := range nodeBuilders {
			glog.V(gpuparams.Gpu10LogLevel).Infof("Removing MIG strategy label from node '%s'", nodeBuilder.Definition.Name)
			nodeBuilder = nodeBuilder.RemoveLabel("nvidia.com/mig.strategy", "")
			_, err = nodeBuilder.Update()
			if err != nil {
				glog.V(gpuparams.Gpu10LogLevel).Infof("Failed to remove MIG strategy label from node '%s': %v", nodeBuilder.Definition.Name, err)
			} else {
				glog.V(gpuparams.Gpu10LogLevel).Infof("Successfully removed MIG strategy label from node '%s'", nodeBuilder.Definition.Name)
			}
			glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG configuration label to 'all-disabled' on node '%s' (overwrite=true)", nodeBuilder.Definition.Name)
			nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", "all-disabled")
			_, err = nodeBuilder.Update()
			if err != nil {
				glog.V(gpuparams.Gpu10LogLevel).Infof("Failed to update node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
			} else {
				glog.V(gpuparams.Gpu10LogLevel).Infof("Successfully set MIG configuration label on node '%s'", nodeBuilder.Definition.Name)
			}
		}
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Sleeping for 30 seconds to allow GPU operator to process node label changes")
	time.Sleep(30 * time.Second)

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterPolicy to be ready after setting MIG node labels")
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Warning: ClusterPolicy may not be fully ready after node label changes: %v", err)
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy is ready after node label changes")
	}

}

// updateAndWaitForClusterPolicyWithMIG updates ClusterPolicy with MIG configuration, waits for it to be ready, and logs the results.
func updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder *nvidiagpu.Builder) {
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

	// Need sleep to allow ClusterPolicy state changes (otherwise the next wait will go through immediately)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Sleeping 30 seconds to allow ClusterPolicy state changes")
	time.Sleep(30 * time.Second)

	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready with MIG configuration", nvidiagpu.ClusterPolicyReadyTimeout))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Waiting up to %s for ClusterPolicy to be ready with MIG configuration",
		nvidiagpu.ClusterPolicyReadyTimeout)
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)

	glog.V(gpuparams.Gpu10LogLevel).Infof("error waiting for ClusterPolicy to be Ready with MIG: %v", err)
	Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterPolicy to be Ready with MIG: %v", err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Waiting up to %s for ClusterPolicy to be ready with MIG configuration",
		nvidiagpu.ClusterPolicyReadyTimeout)

	By("Pull the ready ClusterPolicy with MIG configuration from cluster")
	pulledMIGReadyClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: %v",
		nvidiagpu.ClusterPolicyName, err)

	migReadyJSON, err := json.MarshalIndent(pulledMIGReadyClusterPolicy, "", " ")
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("The ClusterPolicy with MIG configuration has name: %v",
			pulledMIGReadyClusterPolicy.Definition.Name)
		glog.V(gpuparams.GpuLogLevel).Infof("The ClusterPolicy with MIG configuration marshalled "+
			"in json: %v", string(migReadyJSON))
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Error Marshalling ClusterPolicy with MIG into json: %v", err)
	}
}

// configureMIGStrategyAndGetClusterArch configures MIG strategy in ClusterPolicy and retrieves cluster architecture.
// It sets the MIG strategy to single, updates the ClusterPolicy, and then gets the cluster architecture
// from the first GPU enabled worker node.
func configureMIGStrategyAndGetClusterArch(
	pulledClusterPolicyBuilder *nvidiagpu.Builder,
	WorkerNodeSelector map[string]string) (string, error) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Configure MIG strategy and get cluster architecture"))
	migStrategy := nvidiagpuv1.MIGStrategySingle
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Setting ClusterPolicy MIG strategy to '%s'", migStrategy)

	currentMigStrategy := pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy
	if currentMigStrategy == "" {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"Current MIG strategy is empty, setting to '%s'", migStrategy)
		pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy = migStrategy
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"Current MIG strategy is '%s', updating to '%s'",
			currentMigStrategy, migStrategy)
		pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy = migStrategy
	}

	updateAndWaitForClusterPolicyWithMIG(pulledClusterPolicyBuilder)

	glog.V(gpuparams.Gpu10LogLevel).Infof("Getting cluster architecture from nodes with "+
		"WorkerNodeSelector: %v", WorkerNodeSelector)
	clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
	if err != nil {
		return "", err
	}

	glog.V(gpuparams.Gpu10LogLevel).Infof("cluster architecture for GPU enabled worker node is: %s",
		clusterArch)

	Expect(err).ToNot(HaveOccurred(), "error configuring MIG strategy and getting cluster architecture: %v", err)
	time.Sleep(30 * time.Second)

	return clusterArch, nil
}

// pullClusterPolicyAndCaptureResourceVersion pulls the ClusterPolicy from the cluster,
// validates it was pulled successfully, and captures its ResourceVersion.
// It returns the pulled ClusterPolicy builder and the ResourceVersion string.
func pullClusterPolicyAndCaptureResourceVersion() (*nvidiagpu.Builder, string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Pull ClusterPolicy and capture ResourceVersion"))
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Pulling ClusterPolicy builder structure named '%s'", nvidiagpu.ClusterPolicyName)
	pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)

	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy builder object name '%s' "+
		"from cluster: %v", nvidiagpu.ClusterPolicyName, err)

	glog.V(gpuparams.GpuLogLevel).Infof(
		"Pulled ClusterPolicy builder structure named '%s'", pulledClusterPolicyBuilder.Object.Name)

	By("Capturing current clusterPolicy ResourceVersion")
	initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Pulled ClusterPolicy resourceVersion is '%s'", initialClusterPolicyResourceVersion)

	return pulledClusterPolicyBuilder, initialClusterPolicyResourceVersion
}

// ensureGPUBurnNamespaceExists ensures that the GPU burn namespace exists.
// If the namespace doesn't exist, it creates it and labels it with the required labels
// for cluster monitoring and pod security enforcement.
func ensureGPUBurnNamespaceExists(namespaceName string) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Ensure GPU burn namespace exists"))
	gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, namespaceName)
	if gpuBurnNsBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("The namespace '%s' already exists",
			gpuBurnNsBuilder.Object.Name)
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Creating the gpu burn namespace '%s'",
			namespaceName)
		createdGPUBurnNsBuilder, err := gpuBurnNsBuilder.Create()
		Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
			"namespace '%s' : %v ", namespaceName, err)

		glog.V(gpuparams.Gpu10LogLevel).Infof("Successfully created namespace '%s'",
			createdGPUBurnNsBuilder.Object.Name)

		glog.V(gpuparams.Gpu10LogLevel).Infof("Labeling the newly created namespace '%s'",
			createdGPUBurnNsBuilder.Object.Name)

		labeledGPUBurnNsBuilder := createdGPUBurnNsBuilder.WithMultipleLabels(map[string]string{
			"openshift.io/cluster-monitoring":    "true",
			"pod-security.kubernetes.io/enforce": "privileged",
		})

		newGPUBurnLabeledNsBuilder, err := labeledGPUBurnNsBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "error labeling namespace %v : %v ",
			newGPUBurnLabeledNsBuilder.Definition.Name, err)

		glog.V(gpuparams.Gpu10LogLevel).Infof("The gpu-burn labeled namespace has "+
			"labels: %v", newGPUBurnLabeledNsBuilder.Object.Labels)
	}
}

// createAndPullGPUBurnConfigMap creates a GPU burn configmap in the specified namespace
// and then pulls it back to verify it was created successfully.
// It returns the configmap builder for further use (e.g., in defer cleanup functions).
func createAndPullGPUBurnConfigMap(configMapName, namespace string) *configmap.Builder {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Create and pull GPU burn configmap"))
	gpuBurnConfigMap, err := gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, configMapName,
		namespace)
	Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)

	glog.V(gpuparams.Gpu10LogLevel).Infof("The created gpuBurnConfigMap has name: %s",
		gpuBurnConfigMap.Name)

	configmapBuilder, err := configmap.Pull(inittools.APIClient, configMapName, namespace)
	Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
		"namespace '%s': %v", configMapName, namespace, err)

	glog.V(gpuparams.Gpu10LogLevel).Infof("The pulled gpuBurnConfigMap has name: %s",
		configmapBuilder.Definition.Name)

	return configmapBuilder
}

// deployGPUBurnPodWithMIGAndPull creates and deploys a GPU burn pod with MIG configuration,
// then retrieves it from the cluster. It returns the pulled pod builder for further operations.
func deployGPUBurnPodWithMIGAndPull(
	imageName, namespace, useMigProfile string,
	migInstanceCount int,
	podLabel string) *pod.Builder {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Deploy GPU burn pod with MIG configuration and pull"))
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod image name is: '%s', in namespace '%s'",
		imageName, namespace)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Creating pod with MIG profile '%s' requesting %d instances",
		useMigProfile, migInstanceCount)

	gpuBurnMigPod, err := gpuburn.CreateGPUBurnPodWithMIG(inittools.APIClient, namespace, namespace,
		imageName, useMigProfile, migInstanceCount, nvidiagpu.BurnPodCreationTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error creating gpu burn pod with MIG: %v", err)

	glog.V(gpuparams.Gpu10LogLevel).Infof("Creating gpu-burn pod '%s' with MIG configuration in namespace '%s'",
		gpuBurnMigPod.Name, gpuBurnMigPod.Namespace)

	_, err = inittools.APIClient.Pods(gpuBurnMigPod.Namespace).Create(context.TODO(), gpuBurnMigPod,
		metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred(), "Error creating gpu-burn '%s' with MIG in "+
		"namespace '%s': %v", gpuBurnMigPod.Namespace, gpuBurnMigPod.Namespace, err)

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
		"namespace '%s' to go Succeeded phase/Completed status: %v", namespace, namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG now in Succeeded Phase/Completed status")
}

// getGPUBurnPodLogs retrieves the logs from the GPU burn pod with MIG configuration.
// It returns the pod logs as a string.
func getGPUBurnPodLogs(gpuMigPodPulled *pod.Builder, namespace string) string {
	glog.V(gpuparams.Gpu10LogLevel).Infof("%s", colorLog(colorCyan+colorBold, "Get GPU burn pod logs with MIG configuration"))
	glog.V(gpuparams.Gpu10LogLevel).Infof("Get the gpu-burn pod logs with MIG configuration")

	gpuBurnMigLogs, err := gpuMigPodPulled.GetLog(nvidiagpu.BurnLogCollectionPeriod, "gpu-burn-ctr")

	Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
		"from gpu burn namespace '%s': %v", gpuMigPodPulled.Definition.Name, gpuMigPodPulled.Definition.Namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Gpu-burn pod '%s' with MIG logs:\n%s",
		gpuMigPodPulled.Definition.Name, gpuBurnMigLogs)

	return gpuBurnMigLogs
}

// parseAndValidateGPUBurnPodLogs parses the GPU burn pod logs and validates that the execution
// was successful. It checks for "GPU X: OK" messages for each MIG instance and verifies
// that the processing completed successfully (100.0% proc'd).
func parseAndValidateGPUBurnPodLogs(gpuBurnMigLogs string, migInstanceCount int) {
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

// colorLog returns a colored log message with ANSI escape codes
// Usage: glog.V(level).Infof("%s", colorLog(colorCyan+colorBold, "Your message"))
func colorLog(color, message string) string {
	return fmt.Sprintf("%s%s%s", color, message, colorReset)
}

// // colorlogs prints sample colored log messages for testing color output.
// // This function demonstrates all available color options for console highlighting.
// func colorlogs() {
// 	glog.V(101).Infof("%s", colorLog(colorCyan+colorBold, "Cyan"))
// 	glog.V(101).Infof("%s", colorLog(colorGreen+colorBold, "Green"))
// 	glog.V(101).Infof("%s", colorLog(colorYellow+colorBold, "Yellow"))
// 	glog.V(101).Infof("%s", colorLog(colorBlue+colorBold, "Blue"))
// 	glog.V(101).Infof("%s", colorLog(colorMagenta+colorBold, "Magenta"))
// 	glog.V(101).Infof("%s", colorLog(colorWhite+colorBold, "White"))
// 	glog.V(101).Infof("%s", colorLog(colorRed+colorBold, "Red"))
// }

// func clog(verbosity int32, color, message string, args ...interface{}) {
// 	glog.V(glog.Level(verbosity)).Infof("%s", Cl(color, fmt.Sprintf(message, args...)))
// }
