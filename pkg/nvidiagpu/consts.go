package nvidiagpu

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
	BurnNamespace                    = "test-gpu-burn"
	BurnPodName                      = "gpu-burn-pod"
	BurnPodLabel                     = "app=gpu-burn-app"
	BurnConfigmapName                = "gpu-burn-entrypoint"
	OperatorDefaultMasterBundleImage = "registry.gitlab.com/nvidia/kubernetes/gpu-operator/staging/gpu-operator-bundle:main-latest"

	CustomCatalogSourcePublisherName = "Red Hat"

	CustomCatalogSourceDisplayName = "Certified Operators Custom"
)
