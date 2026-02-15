package dra

const (
	// Driver Helm chart constants
	DriverReleaseName = "nvidia-dra-driver-gpu"
	DriverNamespace   = "nvidia-dra-driver-gpu"
	DriverChartName   = "nvidia-dra-driver-gpu"

	// Driver Kubernetes resource constants
	KubeletPluginDaemonSetName = "nvidia-dra-driver-gpu-kubelet-plugin"
	ComponentLabelKey          = "nvidia-dra-driver-gpu-component"
	ComponentController        = "controller"
	ComponentKubeletPlugin     = "kubelet-plugin"

	// API constants
	APIGroup              = "resource.k8s.io"
	DeviceClassesResource = "deviceclasses"
)
