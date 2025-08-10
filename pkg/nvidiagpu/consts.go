package nvidiagpu

import "time"

const (
	NvidiaGPUNamespace = "nvidia-gpu-operator"

	NvidiaGPULabel                   = "feature.node.kubernetes.io/pci-10de.present"
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
)
