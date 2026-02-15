package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/dra"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
