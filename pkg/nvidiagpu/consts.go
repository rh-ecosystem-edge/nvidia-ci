package nvidiagpu

import (
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NvidiaGPUNamespace = "nvidia-gpu-operator"

	// NvidiaGPULabel is the legacy NFD label for NVIDIA GPU presence (vendor ID only, pre-4.21).
	NvidiaGPULabel = "feature.node.kubernetes.io/pci-10de.present"
	// nfdLabelPrefix is the common prefix for NFD PCI labels.
	nfdLabelPrefix = "feature.node.kubernetes.io/pci-"
	// nfdNvidiaVendorSuffix is the NVIDIA vendor ID suffix in NFD PCI labels.
	nfdNvidiaVendorSuffix = "_10de.present"
	GPUPresentLabel                  = "nvidia.com/gpu.present"
	GPUCapacityKey                   = "nvidia.com/gpu"
	DevicePluginLabel                = "app=nvidia-device-plugin-daemonset"
	OperatorGroupName                = "gpu-og"
	OperatorDeployment               = "gpu-operator"
	SubscriptionName                 = "gpu-subscription"
	SubscriptionNamespace            = "nvidia-gpu-operator"
	CatalogSourceDefault             = "certified-operators"
	CatalogSourceNamespace           = "openshift-marketplace"
	Package                          = "gpu-operator-certified"
	ClusterPolicyName                = "gpu-cluster-policy"
	OperatorDefaultMasterBundleImage = "ghcr.io/nvidia/gpu-operator/gpu-operator-bundle:main-latest"

	CustomCatalogSourcePublisherName = "Red Hat"

	CustomCatalogSourceDisplayName = "Certified Operators Custom"

	SleepDuration = 30 * time.Second

	WaitDuration = 4 * time.Minute

	DeletionPollInterval     = 30 * time.Second
	DeletionTimeoutDuration  = 5 * time.Minute
	MachineReadyWaitDuration = 15 * time.Minute

	NodeLabelingDelay = 2 * time.Minute

	CatalogSourceCreationDelay   = 30 * time.Second
	CatalogSourceReadyTimeout    = 4 * time.Minute
	PackageManifestCheckInterval = 30 * time.Second
	PackageManifestTimeout       = 5 * time.Minute
	GpuBundleDeploymentTimeout   = 5 * time.Minute

	OperatorDeploymentCreationDelay = 2 * time.Minute
	DeploymentCreationCheckInterval = 30 * time.Second
	DeploymentCreationTimeout       = 4 * time.Minute

	OperatorDeploymentReadyTimeout = 4 * time.Minute

	CsvSucceededCheckInterval = 60 * time.Second
	CsvSucceededTimeout       = 15 * time.Minute

	ClusterPolicyReadyCheckInterval = 60 * time.Second
	ClusterPolicyReadyTimeout       = 12 * time.Minute

	BurnPodCreationTimeout = 5 * time.Minute

	BurnPodRunningTimeout = 3 * time.Minute
	BurnPodSuccessTimeout = 8 * time.Minute

	BurnLogCollectionPeriod = 500 * time.Second

	CsvDeploymentSleepInterval = 2 * time.Minute

	BurnPodPostUpgradeCreationTimeout = 5 * time.Minute

	RedeployedBurnPodRunningTimeout   = 3 * time.Minute
	RedeployedBurnPodSuccessTimeout   = 8 * time.Minute
	RedeployedBurnLogCollectionPeriod = 500 * time.Second

	ClusterPolicyNotReadyCheckInterval = 15 * time.Second
	ClusterPolicyNotReadyTimeout       = 3 * time.Minute

	LabelCheckInterval = 15 * time.Second
	LabelCheckTimeout  = 3 * time.Minute
)

// ResolveGPULabel returns the NFD GPU label present on the cluster.
// NFD 4.21+ uses pci-{class}_10de.present (e.g. pci-0302_10de.present),
// older versions use pci-10de.present (vendor only).
// This function checks worker nodes for any label matching the pattern.
func ResolveGPULabel(apiClient *clients.Settings) string {
	nodeList, err := nodes.List(apiClient, metav1.ListOptions{})
	if err != nil {
		glog.V(90).Infof("Error listing nodes: %v, defaulting to '%s'", err, NvidiaGPULabel)

		return NvidiaGPULabel
	}

	for _, node := range nodeList {
		for label := range node.Object.Labels {
			if strings.HasPrefix(label, nfdLabelPrefix) && strings.HasSuffix(label, nfdNvidiaVendorSuffix) {
				glog.V(90).Infof("Resolved GPU label to '%s' on node '%s'", label, node.Object.Name)

				return label
			}
		}
	}

	// Fall back to legacy label for pre-NFD clusters or when no nodes are labeled yet.
	glog.V(90).Infof("No NFD NVIDIA GPU label found on any node, defaulting to '%s'", NvidiaGPULabel)

	return NvidiaGPULabel
}
