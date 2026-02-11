package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

const (
	defaultTimeout             = 5 * time.Minute
	defaultDevicePluginEnabled = true // On par with the GPU Operator default
)

// DRAValues creates Helm chart values for DRA driver installation.
// Use the helper functions below to configure specific options.
type DRAValues map[string]interface{}

// NewDRAValues creates a new DRAValues map with default configuration.
func NewDRAValues() DRAValues {
	return make(DRAValues)
}

// ensureMap ensures a key in the parent map contains a map[string]interface{}.
// If the key is nil, creates a new map. If the key exists but is not a map, panics.
//
// IMPORTANT: The panic on type mismatch is INTENTIONAL. This function validates internal
// invariants in the DRAValues builder pattern. A type mismatch indicates a programming bug
// (incorrect builder usage or logic error), not a runtime condition that should be handled
// gracefully. Failing fast with glog.Fatalf makes debugging easier by catching bugs immediately
// rather than propagating corrupt state. DO NOT change this to return an error.
func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if parent[key] == nil {
		m := make(map[string]interface{})
		parent[key] = m
		return m
	}
	m, ok := parent[key].(map[string]interface{})
	if !ok {
		// This is a programming bug, not a runtime error - fail fast to aid debugging
		glog.Fatalf("%s field is not a map[string]interface{}", key)
	}
	return m
}

// WithGPUResources sets the resources.gpus.enabled value.
func (v DRAValues) WithGPUResources(enabled bool) DRAValues {
	resources := ensureMap(v, "resources")
	gpus := ensureMap(resources, "gpus")
	gpus["enabled"] = enabled
	return v
}

// WithGPUResourcesOverride sets the gpuResourcesEnabledOverride value.
func (v DRAValues) WithGPUResourcesOverride(override bool) DRAValues {
	v["gpuResourcesEnabledOverride"] = override
	return v
}

// WithImageRegistry sets the image repository.
func (v DRAValues) WithImageRegistry(registry string) DRAValues {
	image := ensureMap(v, "image")
	image["repository"] = registry
	return v
}

// WithImageTag sets the image tag.
func (v DRAValues) WithImageTag(tag string) DRAValues {
	image := ensureMap(v, "image")
	image["tag"] = tag
	return v
}

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

// InstallDRADriver installs the DRA driver and verifies the installation.
// customValues can be nil or a DRAValues object with custom Helm chart values.
func InstallDRADriver(actionConfig *action.Configuration, version string, customValues DRAValues) error {
	apiClient := GetAPIClient(actionConfig)
	if apiClient == nil {
		return fmt.Errorf("failed to retrieve APIClient from action configuration")
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Starting DRA driver installation from Helm repository (version: %s)", version)
	err := InstallDRADriverFromRepo(actionConfig, version, customValues)
	if err != nil {
		return fmt.Errorf("failed to install DRA driver from Helm repository: %w", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver Helm installation completed successfully")

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for DRA driver pods to become ready")
	err = WaitForDRADriverReady(apiClient, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for DRA driver pods to become ready: %w", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("All DRA driver pods are ready")
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
		if group.Name == DRAAPIGroup {
			glog.V(gpuparams.GpuLogLevel).Infof("DRA API group '%s' is available with versions: %v",
				DRAAPIGroup, group.Versions)
			return nil
		}
	}

	return fmt.Errorf("DRA API group '%s' not found - DRA feature must be enabled in the cluster", DRAAPIGroup)
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

	// Capture previous state
	previousState := defaultDevicePluginEnabled
	if clusterPolicy.Object.Spec.DevicePlugin.Enabled != nil {
		previousState = *clusterPolicy.Object.Spec.DevicePlugin.Enabled
	}

	// If the previous state is the desired state
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

// installChartOptions holds parameters for installing a Helm chart
type installChartOptions struct {
	actionConfig  *action.Configuration
	releaseName   string
	repoURL       string // For repo-based installations
	chartRef      string // Chart name or path
	version       string // Chart version (for repo installations)
	imageRegistry string // Optional custom image registry
	imageTag      string // Optional custom image tag
	customValues  map[string]interface{}
}

// InstallDRADriverFromRepo installs the DRA driver from the NVIDIA Helm repository.
// version can be a specific version (e.g., "25.8.1") or LatestVersion to use the latest published release.
// customValues can be nil or a DRAValues object with custom Helm chart values.
func InstallDRADriverFromRepo(actionConfig *action.Configuration, version string, customValues DRAValues) error {
	helmVersion := ""
	if version != LatestVersion {
		helmVersion = version
	}

	return installChart(installChartOptions{
		actionConfig: actionConfig,
		releaseName:  DRADriverReleaseName,
		repoURL:      DRADriverHelmRepo,
		chartRef:     DRADriverChartName,
		version:      helmVersion,
		customValues: customValues,
	})
}

// InstallDRADriverFromLocal installs the DRA driver from a local Helm chart.
// customValues can be nil or a DRAValues object with custom Helm chart values.
func InstallDRADriverFromLocal(actionConfig *action.Configuration, chartPath, imageRegistry, imageTag string, customValues DRAValues) error {
	return installChart(installChartOptions{
		actionConfig:  actionConfig,
		releaseName:   DRADriverReleaseName,
		chartRef:      chartPath,
		imageRegistry: imageRegistry,
		imageTag:      imageTag,
		customValues:  customValues,
	})
}

func installChart(opts installChartOptions) error {
	client := action.NewInstall(opts.actionConfig)
	client.Namespace = DRADriverNamespace
	client.CreateNamespace = true
	client.ReleaseName = opts.releaseName
	client.Version = opts.version
	client.Wait = true
	client.Timeout = defaultTimeout

	// Set repository URL if provided (for repo installations)
	if opts.repoURL != "" {
		client.RepoURL = opts.repoURL
	}

	// Start with default values
	values := map[string]interface{}{
		"nvidiaDriverRoot": "/run/nvidia/driver",
		"resources": map[string]interface{}{
			"gpus": map[string]interface{}{
				"enabled": true,
			},
		},
	}

	if opts.imageRegistry != "" {
		values["image"] = map[string]interface{}{
			"repository": opts.imageRegistry,
		}
	}

	if opts.imageTag != "" {
		if imgMap, ok := values["image"].(map[string]interface{}); ok {
			imgMap["tag"] = opts.imageTag
		} else {
			values["image"] = map[string]interface{}{
				"tag": opts.imageTag,
			}
		}
	}

	// Deep merge custom values into defaults using Helm's CoalesceTables
	// Note: CoalesceTables(dst, src) considers dst authoritative, so we pass
	// customValues first to ensure custom values override defaults
	if len(opts.customValues) > 0 {
		values = chartutil.CoalesceTables(opts.customValues, values)
	}

	// LocateChart needs settings with cache directory configured
	settings := cli.New()
	chartPath, err := client.LocateChart(opts.chartRef, settings)
	if err != nil {
		return fmt.Errorf("failed to locate chart: %w", err)
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	_, err = client.Run(chart, values)
	if err != nil {
		return fmt.Errorf("failed to install chart: %w", err)
	}

	return nil
}

// UninstallDRADriver uninstalls the DRA driver.
// Returns nil if the release was not found (idempotent behavior).
func UninstallDRADriver(actionConfig *action.Configuration) error {
	listClient := action.NewList(actionConfig)
	listClient.All = true
	releases, err := listClient.Run()
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	releaseExists := false
	for _, release := range releases {
		if release.Name == DRADriverReleaseName {
			releaseExists = true
			break
		}
	}

	if !releaseExists {
		glog.V(gpuparams.GpuLogLevel).Infof("DRA driver release not found, nothing to uninstall")
		return nil
	}

	client := action.NewUninstall(actionConfig)
	client.Wait = true
	client.Timeout = defaultTimeout

	_, err = client.Run(DRADriverReleaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall DRA driver: %w", err)
	}

	return nil
}

// WaitForDRADriverReady waits for all DaemonSets to be ready, then verifies expected pods exist.
func WaitForDRADriverReady(apiClient *clients.Settings, timeout time.Duration) error {

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for all DaemonSets to be ready")
	err := wait.DaemonSetReady(apiClient, DRADriverKubeletPluginDaemonSetName, DRADriverNamespace, 10*time.Second, timeout)
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
	// List only pods with DRA component label
	labelSelector := fmt.Sprintf("%s in (%s,%s)", DRAComponentLabelKey, DRAComponentController, DRAComponentKubeletPlugin)
	podList, err := apiClient.Pods(DRADriverNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	hasController := false
	hasKubeletPlugin := false

	for _, pod := range podList.Items {
		switch pod.GetLabels()[DRAComponentLabelKey] {
		case DRAComponentController:
			hasController = true
		case DRAComponentKubeletPlugin:
			hasKubeletPlugin = true
		}
		if hasController && hasKubeletPlugin {
			break
		}
	}

	if !hasController {
		return fmt.Errorf("no controller pods found with label: %s=%s", DRAComponentLabelKey, DRAComponentController)
	}

	if !hasKubeletPlugin {
		return fmt.Errorf("no kubelet-plugin pods found with label: %s=%s", DRAComponentLabelKey, DRAComponentKubeletPlugin)
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

	// Get all groups and resources in a single API call
	groups, resources, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return fmt.Errorf("failed to get API groups and resources: %w", err)
	}

	// Find the DRA API group and its preferred version
	var preferredVersion string
	for _, group := range groups {
		if group.Name == DRAAPIGroup {
			preferredVersion = group.PreferredVersion.Version
			break
		}
	}

	if preferredVersion == "" {
		return fmt.Errorf("DRA API group '%s' not found", DRAAPIGroup)
	}

	// Verify deviceclasses resource exists in the discovered resources
	groupVersion := fmt.Sprintf("%s/%s", DRAAPIGroup, preferredVersion)
	resourceExists := false
	for _, resourceList := range resources {
		if resourceList.GroupVersion == groupVersion {
			for _, resource := range resourceList.APIResources {
				if resource.Name == DRADeviceClassesResource {
					resourceExists = true
					break
				}
			}
			break
		}
	}

	if !resourceExists {
		return fmt.Errorf("%s resource not found in %s", DRADeviceClassesResource, groupVersion)
	}

	gvr := schema.GroupVersionResource{
		Group:    DRAAPIGroup,
		Version:  preferredVersion,
		Resource: DRADeviceClassesResource,
	}

	// List all DeviceClasses
	deviceClassList, err := apiClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to get %s: %w", DRADeviceClassesResource, err)
	}

	// Build set of existing DeviceClass names for efficient lookup
	existingNames := make(map[string]bool)
	for _, item := range deviceClassList.Items {
		existingNames[item.GetName()] = true
	}

	// Verify all expected DeviceClasses exist
	for _, expected := range deviceClassNames {
		if !existingNames[expected] {
			return fmt.Errorf("'%s' not found in cluster's %s", expected, DRADeviceClassesResource)
		}
	}
	return nil
}
