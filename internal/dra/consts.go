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

	// GPUDriverName is the DRA driver name for whole-GPU allocation, used both as
	// the DeviceClass name and as the ResourceSlice.Spec.Driver value.
	GPUDriverName = "gpu.nvidia.com"

	// VFIODeviceClassName selects the same underlying devices as GPUDriverName,
	// filtered to those tagged for VFIO passthrough.
	VFIODeviceClassName = "vfio.gpu.nvidia.com"

	// UUIDAttributeName is the per-device attribute key the driver publishes on
	// each ResourceSlice device, sourced from NVML.
	UUIDAttributeName = "uuid"
)
