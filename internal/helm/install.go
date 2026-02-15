package helm

import (
	"fmt"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

// ChartConfig defines the Helm chart to be installed.
type ChartConfig struct {
	Source    string                 // Chart source: OCI ref (oci://...), repo URL (http(s)://...), or local path (/... or file://...)
	ChartName string                 // Chart name (required for repository sources, ignored for OCI/local)
	Version   string                 // Chart version (use "" for latest version from repositories)
	Values    map[string]interface{} // Helm chart values
}

// InstallConfig defines the installation parameters.
type InstallConfig struct {
	Chart       ChartConfig
	ReleaseName string        // Name for the Helm release
	Namespace   string        // Kubernetes namespace to install into
	Timeout     time.Duration // Maximum time to wait for installation
}

// InstallChart installs a Helm chart according to the provided configuration.
//
// Supported chart source formats:
//   - OCI registry: "oci://ghcr.io/owner/chart"
//   - Local path: "/path/to/chart" or "file:///path/to/chart"
//   - HTTP(S) repository: "https://charts.example.com" (requires ChartName)
func InstallChart(actionConfig *action.Configuration, config InstallConfig) error {
	var chartRef, repoURL, helmVersion string

	if strings.HasPrefix(config.Chart.Source, "oci://") {
		chartRef = config.Chart.Source
		helmVersion = config.Chart.Version
	} else if strings.HasPrefix(config.Chart.Source, "file://") || strings.HasPrefix(config.Chart.Source, "/") {
		chartRef = strings.TrimPrefix(config.Chart.Source, "file://")
		helmVersion = config.Chart.Version
	} else if strings.HasPrefix(config.Chart.Source, "http://") || strings.HasPrefix(config.Chart.Source, "https://") {
		if config.Chart.ChartName == "" {
			return fmt.Errorf("ChartName is required for repository source: %s", config.Chart.Source)
		}
		chartRef = config.Chart.ChartName
		repoURL = config.Chart.Source
		helmVersion = config.Chart.Version
	} else {
		return fmt.Errorf("unsupported chart source format: %s (must be OCI ref 'oci://...', HTTP(S) URL 'http(s)://...', or filesystem path)", config.Chart.Source)
	}

	client := action.NewInstall(actionConfig)
	client.Namespace = config.Namespace
	client.CreateNamespace = true
	client.ReleaseName = config.ReleaseName
	client.Version = helmVersion
	client.Wait = true
	client.Timeout = config.Timeout

	if repoURL != "" {
		client.RepoURL = repoURL
	}

	// LocateChart needs settings with cache directory configured
	settings := cli.New()
	chartPath, err := client.LocateChart(chartRef, settings)
	if err != nil {
		return fmt.Errorf("failed to locate chart: %w", err)
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	_, err = client.Run(chart, config.Chart.Values)
	if err != nil {
		return fmt.Errorf("failed to install chart: %w", err)
	}

	return nil
}

// UninstallChart uninstalls a Helm release.
// Returns nil if the release was not found (idempotent behavior).
func UninstallChart(actionConfig *action.Configuration, releaseName string, timeout time.Duration) error {
	listClient := action.NewList(actionConfig)
	listClient.All = true
	releases, err := listClient.Run()
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	releaseExists := false
	for _, release := range releases {
		if release.Name == releaseName {
			releaseExists = true
			break
		}
	}

	if !releaseExists {
		return nil
	}

	client := action.NewUninstall(actionConfig)
	client.Wait = true
	client.Timeout = timeout

	_, err = client.Run(releaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall release %s: %w", releaseName, err)
	}

	return nil
}
