package dra

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/kelseyhightower/envconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/helm"
	"helm.sh/helm/v3/pkg/action"
)

// Driver holds DRA driver configuration and provides installation/uninstallation methods.
// Configuration is loaded from environment variables and starts with immutable defaults.
// Specific parameters can be overridden using With* methods.
type Driver struct {
	chartSource  string
	chartVersion string
	values       map[string]interface{}
}

// NewDriver creates a new DRA driver instance with configuration loaded from environment variables.
// If environment variables are not set, uses the defaults specified in struct tags.
//
// Environment Variable Examples:
//
//	DRA_CHART_SOURCE:
//	  - "https://helm.ngc.nvidia.com/nvidia" (default - Helm repository)
//	  - "https://custom-repo.com/charts" (custom Helm repository)
//	  - "oci://ghcr.io/nvidia/k8s-dra-driver-gpu" (OCI registry)
//	  - "/path/to/chart" or "file:///path/to/chart" (local filesystem)
//	DRA_CHART_VERSION:
//	  - "" (latest, default)
//	  - "v25.8.1" (specific release version)
//	  - "25.12.0-dev-39e21b3c-chart" (specific development version)
//	DRA_IMAGE_REGISTRY:
//	  - "" (default - use chart's default)
//	  - "ghcr.io/nvidia/k8s-dra-driver-gpu" (override image registry)
//	DRA_IMAGE_TAG:
//	  - "" (default - use chart's default)
//	  - "v1.2.3" (override image tag)
func NewDriver() (*Driver, error) {
	// Temporary struct for envconfig (requires exported fields)
	temp := struct {
		ChartSource   string `envconfig:"DRA_CHART_SOURCE" default:"https://helm.ngc.nvidia.com/nvidia"`
		ChartVersion  string `envconfig:"DRA_CHART_VERSION" default:""`
		ImageRegistry string `envconfig:"DRA_IMAGE_REGISTRY" default:""`
		ImageTag      string `envconfig:"DRA_IMAGE_TAG" default:""`
	}{}

	err := envconfig.Process("", &temp)
	if err != nil {
		return nil, err
	}

	driver := &Driver{
		chartSource: temp.ChartSource,
		values: map[string]interface{}{
			"nvidiaDriverRoot": "/run/nvidia/driver",
			"resources": map[string]interface{}{
				"gpus": map[string]interface{}{
					"enabled": true,
				},
			},
		},
	}
	if temp.ChartVersion != "" {
		driver.chartVersion = temp.ChartVersion
	}
	if temp.ImageRegistry != "" {
		image := ensureMap(driver.values, "image")
		image["repository"] = temp.ImageRegistry
	}
	if temp.ImageTag != "" {
		image := ensureMap(driver.values, "image")
		image["tag"] = temp.ImageTag
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Created DRA driver configuration (source: %s, version: %s)",
		driver.chartSource, driver.chartVersion)

	return driver, nil
}

// ensureMap ensures a key in the parent map contains a map[string]interface{}.
// If the key is nil, creates a new map. If the key exists but is not a map, exits the
// process via glog.Fatalf.
//
// IMPORTANT: The process exit on type mismatch is INTENTIONAL. This function validates
// internal invariants in the DRAConfig builder pattern. A type mismatch indicates a
// programming bug (incorrect builder usage or logic error), not a runtime condition that
// should be handled gracefully. Exiting immediately with glog.Fatalf makes debugging easier
// by catching bugs immediately rather than propagating corrupt state. DO NOT change this to
// return an error.
func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if parent[key] == nil {
		m := make(map[string]interface{})
		parent[key] = m
		return m
	}
	m, ok := parent[key].(map[string]interface{})
	if !ok {
		glog.Fatalf("%s field is not a map[string]interface{}", key)
	}
	return m
}

// WithGPUResources sets the resources.gpus.enabled value.
func (d *Driver) WithGPUResources(enabled bool) *Driver {
	resources := ensureMap(d.values, "resources")
	gpus := ensureMap(resources, "gpus")
	gpus["enabled"] = enabled
	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver GPU resources set to: %v", enabled)
	return d
}

// WithGPUResourcesOverride sets the gpuResourcesEnabledOverride value.
func (d *Driver) WithGPUResourcesOverride(override bool) *Driver {
	d.values["gpuResourcesEnabledOverride"] = override
	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver GPU resources override set to: %v", override)
	return d
}

// WithImageRegistry sets the image repository in the values map.
func (d *Driver) WithImageRegistry(registry string) *Driver {
	image := ensureMap(d.values, "image")
	image["repository"] = registry
	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver image registry set to: %s", registry)
	return d
}

// WithImageTag sets the image tag in the values map.
func (d *Driver) WithImageTag(tag string) *Driver {
	image := ensureMap(d.values, "image")
	image["tag"] = tag
	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver image tag set to: %s", tag)
	return d
}

// WithChartSource sets the chart source location.
func (d *Driver) WithChartSource(source string) *Driver {
	d.chartSource = source
	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver chart source set to: %s", source)
	return d
}

// WithChartVersion sets the chart version.
func (d *Driver) WithChartVersion(version string) *Driver {
	d.chartVersion = version
	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver chart version set to: %s", version)
	return d
}

// Install installs the DRA driver using the configured parameters.
// The installation method is determined by the ChartSource.
// timeout specifies how long to wait for the installation to complete.
func (d *Driver) Install(actionConfig *action.Configuration, timeout time.Duration) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Installing DRA driver (source: %s, version: %s, values: %+v)",
		d.chartSource, d.chartVersion, d.values)

	installConfig := helm.InstallConfig{
		Chart: helm.ChartConfig{
			Source:    d.chartSource,
			ChartName: DriverChartName,
			Version:   d.chartVersion,
			Values:    d.values,
		},
		ReleaseName: DriverReleaseName,
		Namespace:   DriverNamespace,
		Timeout:     timeout,
	}

	err := helm.InstallChart(actionConfig, installConfig)
	if err != nil {
		return fmt.Errorf("failed to install DRA driver: %w", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver installation completed successfully")

	return nil
}

// Uninstall uninstalls the DRA driver.
// Returns nil if the release was not found (idempotent behavior).
// timeout specifies how long to wait for the uninstallation to complete.
func (d *Driver) Uninstall(actionConfig *action.Configuration, timeout time.Duration) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Uninstalling DRA driver")

	err := helm.UninstallChart(actionConfig, DriverReleaseName, timeout)
	if err != nil {
		return fmt.Errorf("failed to uninstall DRA driver: %w", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("DRA driver uninstalled successfully")

	return nil
}
