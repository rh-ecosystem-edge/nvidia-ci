package nvidiagpu

import "time"

const (
	nfdOperatorNamespace      = "openshift-nfd"
	nfdCatalogSourceDefault   = "redhat-operators"
	nfdCatalogSourceNamespace = "openshift-marketplace"
	nfdOperatorDeploymentName = "nfd-controller-manager"
	nfdPackage                = "nfd"
	nfdCRName                 = "nfd-instance"
	operatorVersionFile       = "operator.version"
	openShiftVersionFile      = "ocp.version"

	nvidiaGPUNamespace                  = "nvidia-gpu-operator"
	nfdRhcosLabel                       = "feature.node.kubernetes.io/system-os_release.ID"
	nfdRhcosLabelValue                  = "rhcos"
	nvidiaGPULabel                      = "feature.node.kubernetes.io/pci-10de.present"
	gpuOperatorGroupName                = "gpu-og"
	gpuOperatorDeployment               = "gpu-operator"
	gpuSubscriptionName                 = "gpu-subscription"
	gpuSubscriptionNamespace            = "nvidia-gpu-operator"
	gpuCatalogSourceDefault             = "certified-operators"
	gpuCatalogSourceNamespace           = "openshift-marketplace"
	gpuPackage                          = "gpu-operator-certified"
	gpuClusterPolicyName                = "gpu-cluster-policy"
	gpuBurnNamespace                    = "test-gpu-burn"
	gpuBurnPodName                      = "gpu-burn-pod"
	gpuBurnPodLabel                     = "app=gpu-burn-app"
	gpuBurnConfigmapName                = "gpu-burn-entrypoint"
	gpuOperatorDefaultMasterBundleImage = "registry.gitlab.com/nvidia/kubernetes/gpu-operator/staging/gpu-operator-bundle:main-latest"

	gpuCustomCatalogSourcePublisherName    = "Red Hat"
	nfdCustomNFDCatalogSourcePublisherName = "Red Hat"

	gpuCustomCatalogSourceDisplayName = "Certified Operators Custom"
	nfdCustomCatalogSourceDisplayName = "Redhat Operators Custom"

	ClusterPolicyTimeout  = 20 * time.Minute
	ClusterPolicyInterval = 60 * time.Second
)
