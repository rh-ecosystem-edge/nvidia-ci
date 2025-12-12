package get

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// InstalledCSVFromSubscription returns installedCSV from Subscription.
func InstalledCSVFromSubscription(apiClient *clients.Settings, gpuSubscriptionName,
	gpuSubscriptionNamespace string) (string, error) {
	subPulled, err := olm.PullSubscription(apiClient, gpuSubscriptionName, gpuSubscriptionNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error pulling Subscription %s from cluster in namespace %s", gpuSubscriptionName,
			gpuSubscriptionNamespace)

		return "", err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"InstalledCSV %s extracted from Subscription %s from cluster in namespace %s",
		subPulled.Object.Status.InstalledCSV, gpuSubscriptionName, gpuSubscriptionNamespace)

	return subPulled.Object.Status.InstalledCSV, nil
}

// CurrentCSVFromSubscription returns installedCSV from Subscription.
func CurrentCSVFromSubscription(apiClient *clients.Settings, gpuSubscriptionName,
	gpuSubscriptionNamespace string) (string, error) {
	subPulled, err := olm.PullSubscription(apiClient, gpuSubscriptionName, gpuSubscriptionNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error pulling Subscription %s from cluster in namespace %s", gpuSubscriptionName,
			gpuSubscriptionNamespace)

		return "", err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"CurrentCSV %s extracted from Subscription %s from cluster in namespace %s",
		subPulled.Object.Status.CurrentCSV, gpuSubscriptionName, gpuSubscriptionNamespace)

	return subPulled.Object.Status.CurrentCSV, nil
}

// GetFirstPodNameWithLabel returns a the first pod name matching pod labelSelector in specified namespace.
func GetFirstPodNameWithLabel(apiClient *clients.Settings, podNamespace, podLabelSelector string) (string, error) {
	podList, err := pod.List(apiClient, podNamespace, v1.ListOptions{LabelSelector: podLabelSelector})

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error listing pods in namespace %s with label selector %s: %v", podNamespace, podLabelSelector, err)
		return "", err
	}

	if len(podList) == 0 {
		glog.V(gpuparams.GpuLogLevel).Infof("no pods found in namespace %s with label selector %s", podNamespace, podLabelSelector)
		return "", fmt.Errorf("no pods found in namespace %s with label selector %s", podNamespace, podLabelSelector)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("Length of podList matching podLabelSelector is '%v'", len(podList))
	glog.V(gpuparams.GpuLogLevel).Infof("podList[0] matching podLabelSelector is '%v'",
		podList[0].Definition.Name)

	return podList[0].Definition.Name, err
}

// GetClusterArchitecture returns first node architecture of the nodes that match nodeSelector (e.g. worker nodes).
func GetClusterArchitecture(apiClient *clients.Settings, nodeSelector map[string]string) (string, error) {
	nodeBuilder, err := nodes.List(apiClient, v1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})

	// Check if at least one node matching the nodeSelector has the specific nodeLabel label set to true
	// For example, look in all the worker nodes for specific label
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("could not discover %v nodes", nodeSelector)

		return "", err
	}

	nodeLabel := "kubernetes.io/arch"

	for _, node := range nodeBuilder {
		labelValue, ok := node.Object.Labels[nodeLabel]

		if ok {
			glog.V(gpuparams.GpuLogLevel).Infof("Found label '%v' with label value '%v' on node '%v'",
				nodeLabel, labelValue, node.Object.Name)

			return labelValue, nil
		}
	}

	err = fmt.Errorf("could not find one node with label '%s'", nodeLabel)

	return "", err
}

// MIGCapabilities queries GPU hardware directly using nvidia-smi
// to discover MIG capabilities. This is a fallback when GFD labels are not available.
// Returns true if MIG is supported, along with available MIG instance profiles.
func MIGCapabilities(apiClient *clients.Settings, nodeSelector map[string]string) (bool, []MIGProfileInfo, error) {
	nodeBuilder, err := nodes.List(apiClient, v1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})
	if err != nil {
		return false, nil, err
	}

	if len(nodeBuilder) == 0 {
		return false, nil, fmt.Errorf("no nodes found matching selector")
	}

	// Get the first GPU node
	firstNode := nodeBuilder[0]
	nodeName := firstNode.Object.Name

	// Find a driver pod or GFD pod on this node to query hardware
	// Try driver pod first
	driverPods, err := apiClient.Pods("nvidia-gpu-operator").List(context.TODO(), v1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=nvidia-driver",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil || len(driverPods.Items) == 0 {
		// Try GFD pod as fallback
		gfdPods, err2 := apiClient.Pods("nvidia-gpu-operator").List(context.TODO(), v1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=gpu-feature-discovery",
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		})
		if err2 != nil || len(gfdPods.Items) == 0 {
			return false, nil, fmt.Errorf("no driver or GFD pod found on node %s to query MIG capabilities", nodeName)
		}
		driverPods = gfdPods
	}

	driverPod := driverPods.Items[0]
	podName := driverPod.Name
	namespace := driverPod.Namespace

	// Query MIG capabilities using nvidia-smi
	// First, try to get MIG instance profiles directly (works even if MIG mode is not enabled)
	cmd := []string{"nvidia-smi", "mig", "-lgip"}
	glog.V(gpuparams.Gpu10LogLevel).Infof("oc rsh -n %s pod/%s %v %v %v", namespace, podName, cmd[0], cmd[1], cmd[2])
	profileOutput, err := execCommandInPod(apiClient, podName, namespace, cmd)
	if err == nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Available MIG instance profiles: %s", profileOutput)
		// Parse profiles from output (e.g., "1g.5gb", "2g.10gb", etc.)
		profiles := parseMIGProfiles(profileOutput)
		for _, profile := range profiles {
			glog.V(gpuparams.GpuLogLevel).Infof("profile: %s with gpu_id: %d, slices: %d/%d, p2p: %s, sm:%d, dec: %d, enc: %d, CE=%d, JPEG=%d, OFA=%d",
				profile.MigName, profile.GpuID, profile.Available, profile.Total, profile.P2P, profile.SM, profile.DEC, profile.ENC,
				profile.CE, profile.JPEG, profile.OFA)
		}
		return true, profiles, nil
	} else {
		// Even if command failed, check if we got any output (non-zero exit codes still produce output)
		if profileOutput != "" {
			glog.V(gpuparams.GpuLogLevel).Infof("nvidia-smi mig -lgip failed but produced output: %s, error: %v", profileOutput, err)
			profiles := parseMIGProfiles(profileOutput)
			for _, profile := range profiles {
				glog.V(gpuparams.Gpu10LogLevel).Infof("profile: %s with gpu_id: %d, slices: %d/%d, p2p: %s, sm:%d, dec: %d, enc: %d, CE=%d, JPEG=%d, OFA=%d",
					profile.MigName, profile.GpuID, profile.Available, profile.Total, profile.P2P, profile.SM, profile.DEC, profile.ENC,
					profile.CE, profile.JPEG, profile.OFA)
			}
			return true, profiles, nil
		} else {
			glog.V(gpuparams.Gpu10LogLevel).Infof("nvidia-smi mig -lgip failed with no output (this is expected if MIG mode is not enabled): %v", err)
		}
	}

	return false, nil, nil
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

// execCommandInPod executes a command in a pod and returns the output
func execCommandInPod(apiClient *clients.Settings, podName, namespace string, command []string) (string, error) {
	// Pull the pod using the pod builder
	podBuilder, err := pod.Pull(apiClient, podName, namespace)
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s/%s: %v", namespace, podName, err)
	}

	// Check pod status
	if podBuilder.Object.Status.Phase != corev1.PodRunning {
		return "", fmt.Errorf("pod %s/%s is not running (phase: %s)", namespace, podName, podBuilder.Object.Status.Phase)
	}

	// Check if pod has containers
	if len(podBuilder.Object.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod %s/%s has no containers", namespace, podName)
	}

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
	if !containerRunning {
		return "", fmt.Errorf("container %s in pod %s/%s is not running (pod phase: %s)", containerName, namespace, podName, podBuilder.Object.Status.Phase)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Executing command %v in pod %s/%s container %s", command, namespace, podName, containerName)

	// Use ExecCommand method from pod builder
	outputBuffer, err := podBuilder.ExecCommand(command, containerName)
	outputStr := outputBuffer.String()

	if err != nil {
		// Check for non-zero exit code which may occur in exceptional cases
		// vs an actual API/connection error
		if outputStr != "" {
			// Command produced output but exited with non-zero code
			// This may happen if "nvidia-smi mig -lgc" is executed when MIG is not enabled)
			glog.V(gpuparams.GpuLogLevel).Infof("Command exited with error but produced output (exit code may be non-zero): %v, output: %s", err, outputStr)
			return outputStr, err
		}
		return outputStr, fmt.Errorf("failed to execute command %v in pod %s/%s: %v, output: %q", command, namespace, podName, err, outputStr)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Command executed successfully, output length: %d bytes", len(outputStr))
	return outputStr, nil
}

// parseMIGProfiles parses MIG profile names from nvidia-smi mig -lgip output
// Handles formats like "MIG 1g.5gb", "MIG 1g.5gb+me", "1g.5gb", etc.
func parseMIGProfiles(output string) []MIGProfileInfo {
	var profiles []MIGProfileInfo
	// Regex to match MIG profile patterns from first line, e.g.:
	// |   0  MIG 1g.5gb          19     7/7        4.75       No     14     0     0   |
	// Captures: GPU, MIG, name, ID, available/total, memory, P2P, SM, DEC, ENC
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
			// Parse the fields, most of them are integers
			gpuID, _ := strconv.Atoi(matches[1])
			migID, _ := strconv.Atoi(matches[4])
			available, _ := strconv.Atoi(matches[5])
			total, _ := strconv.Atoi(matches[6])
			sm, _ := strconv.Atoi(matches[9])
			dec, _ := strconv.Atoi(matches[10])
			enc, _ := strconv.Atoi(matches[11])
			exclude = excludeRegex.MatchString(line)

			// exclude if the +me is present
			if exclude {
				// no entry in the profile
				glog.V(gpuparams.Gpu100LogLevel).Infof("Line 1: Ignoring profile: %s with gpu_id: %d",
					matches[3], matches[1])
				continue
			} else {
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
	return profiles
}
