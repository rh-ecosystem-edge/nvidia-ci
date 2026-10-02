package shared

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/dra"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
)

const (
	DriverInstallationTimeout  = 5 * time.Minute
	defaultDevicePluginEnabled = true // On par with the GPU Operator default
)

// VerifyDRAPrerequisites checks that all prerequisites for DRA driver installation are met.
func VerifyDRAPrerequisites(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Verifying GPU Operator ClusterPolicy is ready")
	err := wait.ClusterPolicyReady(apiClient, nvidiagpu.ClusterPolicyName, 1*time.Second, 1*time.Second)
	if err != nil {
		return fmt.Errorf("GPU Operator prerequisite check failed: %w", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Verifying DRA API is available")
	err = VerifyDRAAPIAvailable(apiClient)
	if err != nil {
		return fmt.Errorf("DRA API prerequisite check failed: %w", err)
	}

	return nil
}

// VerifyDRAAPIAvailable checks that the DRA API resource group (resource.k8s.io) is available in the cluster.
func VerifyDRAAPIAvailable(apiClient *clients.Settings) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(apiClient.Config)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	apiGroupList, err := discoveryClient.ServerGroups()
	if err != nil {
		return fmt.Errorf("failed to query API groups: %w", err)
	}

	for _, group := range apiGroupList.Groups {
		if group.Name == dra.APIGroup {
			glog.V(gpuparams.GpuLogLevel).Infof("DRA API group '%s' is available with versions: %v",
				dra.APIGroup, group.Versions)
			return nil
		}
	}

	return fmt.Errorf("DRA API group '%s' not found - DRA feature must be enabled in the cluster", dra.APIGroup)
}

// IsDevicePluginEnabled checks the device plugin state in ClusterPolicy.
// Returns true if device plugin is enabled, false if disabled or not configured.
func IsDevicePluginEnabled(apiClient *clients.Settings) (bool, error) {
	clusterPolicy, err := nvidiagpu.Pull(apiClient, nvidiagpu.ClusterPolicyName)
	if err != nil {
		return false, fmt.Errorf("failed to get ClusterPolicy: %w", err)
	}

	if clusterPolicy.Object.Spec.DevicePlugin.Enabled == nil {
		return defaultDevicePluginEnabled, nil
	}

	return *clusterPolicy.Object.Spec.DevicePlugin.Enabled, nil
}

// SetDevicePluginEnabled enables or disables the device plugin in ClusterPolicy.
// Returns the previous device plugin state and an error.
func SetDevicePluginEnabled(apiClient *clients.Settings, enabled bool) (bool, error) {
	clusterPolicy, err := nvidiagpu.Pull(apiClient, nvidiagpu.ClusterPolicyName)
	if err != nil {
		return false, fmt.Errorf("failed to get ClusterPolicy: %w", err)
	}

	previousState := defaultDevicePluginEnabled
	if clusterPolicy.Object.Spec.DevicePlugin.Enabled != nil {
		previousState = *clusterPolicy.Object.Spec.DevicePlugin.Enabled
	}

	if previousState == enabled {
		glog.V(gpuparams.GpuLogLevel).Infof("Device plugin is already in the desired state: %v", enabled)
		return previousState, nil
	}

	clusterPolicy.Definition.Spec.DevicePlugin.Enabled = &enabled

	_, err = clusterPolicy.Update(true)
	if err != nil {
		return previousState, fmt.Errorf("failed to update ClusterPolicy: %w", err)
	}

	return previousState, nil
}

// WaitForDRADriverReady waits for DRA driver resources to be ready.
func WaitForDRADriverReady(apiClient *clients.Settings, timeout time.Duration) error {

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for DRA driver DaemonSets to be ready")
	err := wait.DaemonSetReady(apiClient, dra.KubeletPluginDaemonSetName, dra.DriverNamespace, 10*time.Second, timeout)
	if err != nil {
		return fmt.Errorf("DaemonSets not ready: %w", err)
	}
	glog.V(gpuparams.GpuLogLevel).Infof("All DRA driver DaemonSets are ready")

	glog.V(gpuparams.GpuLogLevel).Infof("Verifying DRA driver pods exist")
	err = verifyDRADriverPods(apiClient)
	if err != nil {
		return fmt.Errorf("failed to verify DRA driver pods: %w", err)
	}

	return nil
}

// verifyDRADriverPods lists pods with DRA component labels and verifies both types exist.
func verifyDRADriverPods(apiClient *clients.Settings) error {
	labelSelector := fmt.Sprintf("%s in (%s,%s)", dra.ComponentLabelKey, dra.ComponentController, dra.ComponentKubeletPlugin)
	podList, err := apiClient.Pods(dra.DriverNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	hasController := false
	hasKubeletPlugin := false

	for _, pod := range podList.Items {
		switch pod.GetLabels()[dra.ComponentLabelKey] {
		case dra.ComponentController:
			hasController = true
		case dra.ComponentKubeletPlugin:
			hasKubeletPlugin = true
		}
		if hasController && hasKubeletPlugin {
			break
		}
	}

	if !hasController {
		return fmt.Errorf("no controller pods found with label: %s=%s", dra.ComponentLabelKey, dra.ComponentController)
	}

	if !hasKubeletPlugin {
		return fmt.Errorf("no kubelet-plugin pods found with label: %s=%s", dra.ComponentLabelKey, dra.ComponentKubeletPlugin)
	}
	return nil
}

// VerifyDeviceClasses verifies that specific DeviceClass instances exist in the cluster.
// deviceClassNames is a list of DeviceClass names to check (e.g., ["compute-domain-daemon.nvidia.com"]).
func VerifyDeviceClasses(apiClient *clients.Settings, deviceClassNames []string) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(apiClient.Config)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	groups, resources, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return fmt.Errorf("failed to get API groups and resources: %w", err)
	}

	var preferredVersion string
	for _, group := range groups {
		if group.Name == dra.APIGroup {
			preferredVersion = group.PreferredVersion.Version
			break
		}
	}

	if preferredVersion == "" {
		return fmt.Errorf("DRA API group '%s' not found", dra.APIGroup)
	}

	groupVersion := fmt.Sprintf("%s/%s", dra.APIGroup, preferredVersion)
	resourceExists := false
	for _, resourceList := range resources {
		if resourceList.GroupVersion == groupVersion {
			for _, resource := range resourceList.APIResources {
				if resource.Name == dra.DeviceClassesResource {
					resourceExists = true
					break
				}
			}
			break
		}
	}

	if !resourceExists {
		return fmt.Errorf("%s resource not found in %s", dra.DeviceClassesResource, groupVersion)
	}

	gvr := schema.GroupVersionResource{
		Group:    dra.APIGroup,
		Version:  preferredVersion,
		Resource: dra.DeviceClassesResource,
	}

	deviceClassList, err := apiClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to get %s: %w", dra.DeviceClassesResource, err)
	}

	existingNames := make(map[string]bool)
	for _, item := range deviceClassList.Items {
		existingNames[item.GetName()] = true
	}

	for _, expected := range deviceClassNames {
		if !existingNames[expected] {
			return fmt.Errorf("'%s' not found in cluster's %s", expected, dra.DeviceClassesResource)
		}
	}
	return nil
}

// VerifyGPUResourceSliceInventory verifies that the gpu.nvidia.com ResourceSlice for
// the given node reports exactly the same set of GPU UUIDs that nvidia-smi reports on
// the classic GPU-Operator driver pod for that node. This proves the DRA driver
// actually discovered the physical hardware correctly, not just that its process is
// running - a driver that's alive but silently under/over-reporting GPUs (or
// duplicating one GPU's UUID while missing another) would otherwise pass every
// allocation-based test as long as at least one claim happens to get satisfied.
func VerifyGPUResourceSliceInventory(apiClient *clients.Settings, nodeName string) error {
	groundTruth, err := gpuGroundTruthUUIDs(apiClient, nodeName)
	if err != nil {
		return fmt.Errorf("failed to get ground-truth GPU UUIDs from node '%s': %w", nodeName, err)
	}

	return wait.GPUResourceSliceUUIDsMatch(apiClient, nodeName, groundTruth, 5*time.Second, DriverInstallationTimeout)
}

// gpuGroundTruthUUIDs finds the classic GPU-Operator driver pod on the given node and
// execs nvidia-smi in it to get the set of GPU UUIDs it reports, independent of the DRA
// driver's own device enumeration. Both the pod lookup and the exec are polled: a
// rolling update of the classic driver DaemonSet can transiently leave no Running pod
// on the node, or leave a matched pod not yet exec-capable, and that's unrelated to
// whatever the DRA driver is doing.
func gpuGroundTruthUUIDs(apiClient *clients.Settings, nodeName string) (map[string]bool, error) {
	var (
		groundTruth map[string]bool
		lastErr     error
	)

	pollErr := k8swait.PollUntilContextTimeout(context.TODO(), 5*time.Second, time.Minute, true,
		func(ctx context.Context) (bool, error) {
			driverPod, err := findRunningPodOnNode(apiClient, nvidiagpu.NvidiaGPUNamespace, nvidiagpu.DriverComponentLabelSelector, nodeName)
			if err != nil {
				lastErr = err

				return false, nil
			}

			uuids, err := nvidiaSMIGPUUUIDs(driverPod)
			if err != nil {
				lastErr = err

				return false, nil
			}

			groundTruth = uuids

			return true, nil
		})
	if pollErr != nil {
		return nil, fmt.Errorf("could not get ground-truth GPU UUIDs on node '%s': %w", nodeName, lastErr)
	}

	return groundTruth, nil
}

// findRunningPodOnNode finds the first Running pod matching labelSelector on the given
// node, in the given namespace, reusing the shared pod.List primitive rather than
// calling the Kubernetes API directly. Filtering to Running matters because more than
// one pod can match the same label+node selector during a DaemonSet rolling update (an
// old Terminating pod alongside a new Pending one) - picking an arbitrary match risks
// exec'ing into a pod that isn't ready or is about to be torn down.
func findRunningPodOnNode(apiClient *clients.Settings, namespace, labelSelector, nodeName string) (*pod.Builder, error) {
	podBuilders, err := pod.List(apiClient, namespace, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods with selector '%s' on node '%s': %w", labelSelector, nodeName, err)
	}

	for _, podBuilder := range podBuilders {
		if podBuilder.Object.Status.Phase == corev1.PodRunning {
			return podBuilder, nil
		}
	}

	return nil, fmt.Errorf("no Running pod found with selector '%s' on node '%s' (%d candidates)",
		labelSelector, nodeName, len(podBuilders))
}

// nvidiaSMIGPUUUIDs execs nvidia-smi in the given pod and returns the set of GPU
// UUIDs it reports, independent of the DRA driver's own device enumeration.
func nvidiaSMIGPUUUIDs(podBuilder *pod.Builder) (map[string]bool, error) {
	output, err := podBuilder.ExecCommand([]string{"nvidia-smi", "--query-gpu=uuid", "--format=csv,noheader"})
	if err != nil {
		return nil, fmt.Errorf("failed to exec nvidia-smi in pod '%s/%s': %w",
			podBuilder.Object.Namespace, podBuilder.Object.Name, err)
	}

	uuids := make(map[string]bool)

	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			uuids[line] = true
		}
	}

	if len(uuids) == 0 {
		return nil, fmt.Errorf("nvidia-smi in pod '%s/%s' reported no GPU UUIDs",
			podBuilder.Object.Namespace, podBuilder.Object.Name)
	}

	return uuids, nil
}
