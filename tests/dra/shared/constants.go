package shared

const (
	DRADriverNamespace                  = "nvidia-dra-driver-gpu"
	DRADriverKubeletPluginDaemonSetName = "nvidia-dra-driver-gpu-kubelet-plugin"
	DRADriverChartName                  = "nvidia-dra-driver-gpu"
	DRADriverReleaseName                = DRADriverChartName
	DRADriverHelmRepo                   = "https://helm.ngc.nvidia.com/nvidia"
	LatestVersion                       = "latest"
	DRAAPIGroup                         = "resource.k8s.io"
	DRADeviceClassesResource            = "deviceclasses"
	DevicePluginLabel                   = "app=nvidia-device-plugin-daemonset"
	DRAComponentLabelKey                = "nvidia-dra-driver-gpu-component"
	DRAComponentController              = "controller"
	DRAComponentKubeletPlugin           = "kubelet-plugin"
	GPUPresentLabel                     = "nvidia.com/gpu.present"
	GPUCapacityKey                      = "nvidia.com/gpu"
)
