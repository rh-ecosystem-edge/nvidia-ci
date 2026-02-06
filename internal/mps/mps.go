package mps

import (
	"fmt"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var (
	isFalse             bool = false
	isTrue              bool = true
	workerConfigMapName      = "mps-test-entrypoint"

	// WorkerPodConfigMapData contains the entrypoint script for worker pods
	WorkerPodConfigMapData = map[string]string{
		"entrypoint.sh": `#!/bin/bash
set -e

echo "Starting MPS worker pod..."

# Check GPU availability
NUM_GPUS=$(nvidia-smi -L | wc -l)
if [ $NUM_GPUS -eq 0 ]; then
  echo "ERROR: No GPUs found"
  exit 1
fi

echo "Found $NUM_GPUS GPUs"

# Set MPS environment variables
export CUDA_VISIBLE_DEVICES=0
export CUDA_DEVICE_ORDER=PCI_BUS_ID
export CUDA_MPS_ACTIVE_THREAD_PERCENTAGE=100

echo "MPS environment variables set:"
echo "CUDA_VISIBLE_DEVICES=$CUDA_VISIBLE_DEVICES"
echo "CUDA_DEVICE_ORDER=$CUDA_DEVICE_ORDER"
echo "CUDA_MPS_ACTIVE_THREAD_PERCENTAGE=$CUDA_MPS_ACTIVE_THREAD_PERCENTAGE"

# Run continuous PyTorch workload
echo "Starting continuous PyTorch workload..."
python3 -c "
import torch
import time
import os

# Print process ID for monitoring
print(f'Process ID: {os.getpid()}')

# Create tensors on GPU
x = torch.randn(1000, 1000, device='cuda')
y = torch.randn(1000, 1000, device='cuda')

# Run matrix multiplication in a loop
for i in range(60):  # Run for 5 minutes
    start = time.time()
    z = torch.matmul(x, y)
    end = time.time()
    print(f'Iteration {i+1}: Matrix multiplication completed in {end - start:.4f} seconds')
    time.sleep(5)  # Sleep for 5 seconds between iterations

print('Continuous workload completed')
"
`,
	}
)

// CreateWorkerPodConfigMap creates a ConfigMap with the worker pod entrypoint script
func CreateWorkerPodConfigMap(apiClient *clients.Settings,
	configMapNamespace string) (*configmap.Builder, error) {
	configMapBuilder := configmap.NewBuilder(apiClient, workerConfigMapName, configMapNamespace)
	configMapBuilderWithData := configMapBuilder.WithData(WorkerPodConfigMapData)

	createdConfigMapBuilderWithData, err := configMapBuilderWithData.Create()
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error creating Worker Pod ConfigMap with Data named %s and for namespace %s",
			createdConfigMapBuilderWithData.Object.Name, createdConfigMapBuilderWithData.Object.Namespace)
		return nil, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"Created Worker Pod ConfigMap with Data named %s and for namespace %s",
		createdConfigMapBuilderWithData.Object.Name, createdConfigMapBuilderWithData.Object.Namespace)

	return createdConfigMapBuilderWithData, nil
}

// CreateMPSTestPod returns a Pod configured for MPS testing.
func CreateMPSTestPod(apiClient *clients.Settings, podName, podNamespace string,
	mpsTestImage string) (*corev1.Pod, error) {
	var volumeDefaultMode int32 = 0777

	configMapVolumeSource := &corev1.ConfigMapVolumeSource{}
	configMapVolumeSource.Name = workerConfigMapName
	configMapVolumeSource.DefaultMode = &volumeDefaultMode

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
			Labels: map[string]string{
				"app": "mps-test-app",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   &isTrue,
				SeccompProfile: &corev1.SeccompProfile{Type: "RuntimeDefault"},
			},
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
				{
					Key:      "nvidia.com/gpu",
					Effect:   corev1.TaintEffectNoSchedule,
					Operator: corev1.TolerationOpExists,
				},
			},
			Containers: []corev1.Container{
				{
					Image:           mpsTestImage,
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &isFalse,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Name: "mps-test-ctr",
					Command: []string{
						"/bin/entrypoint.sh",
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "entrypoint",
							MountPath: "/bin/entrypoint.sh",
							ReadOnly:  true,
							SubPath:   "entrypoint.sh",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "entrypoint",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: configMapVolumeSource,
					},
				},
			},
			NodeSelector: map[string]string{
				"nvidia.com/gpu.present":         "true",
				"node-role.kubernetes.io/worker": "",
			},
		},
	}, nil
}

// CreateDevicePluginConfigMap creates a ConfigMap with the device plugin configuration for MPS
func CreateDevicePluginConfigMap(apiClient *clients.Settings, replicas int, configMapName, configMapNamespace string, renameByDefault bool) (*configmap.Builder, error) {
	// Define the device plugin configuration
	config := map[string]interface{}{
		"version": "v1",
		"sharing": map[string]interface{}{
			"mps": map[string]interface{}{
				"renameByDefault": renameByDefault,
				"resources": []map[string]interface{}{
					{
						"name":     "nvidia.com/gpu",
						"replicas": replicas,
					},
				},
			},
		},
	}

	yamlData, err := yaml.Marshal(config)
	if err != nil {
		glog.Error("unable to marshal map %v", err)
	}
	devicePluginConfig := map[string]string{
		"plugin-config.yaml": string(yamlData),
	}
	// Create the ConfigMap
	configMapBuilder := configmap.NewBuilder(apiClient, configMapName, configMapNamespace)
	configMapBuilderWithData := configMapBuilder.WithData(devicePluginConfig)

	createdConfigMapBuilderWithData, err := configMapBuilderWithData.Create()
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error creating Device Plugin ConfigMap with Data named %s and for namespace %s",
			createdConfigMapBuilderWithData.Object.Name, createdConfigMapBuilderWithData.Object.Namespace)
		return nil, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"Created Device Plugin ConfigMap with Data named %s and for namespace %s",
		createdConfigMapBuilderWithData.Object.Name, createdConfigMapBuilderWithData.Object.Namespace)

	return createdConfigMapBuilderWithData, nil
}

// CreateClusterPolicyFromCSV creates a new cluster policy from the CSV ALM example
func CreateClusterPolicyFromCSV(apiClient *clients.Settings, gpuOperatorNamespace, clusterPolicyName string) (*nvidiagpu.Builder, error) {
	glog.V(gpuparams.GpuLogLevel).Infof("Creating new ClusterPolicy %s from CSV ALM example", clusterPolicyName)

	// Get the CSV containing the ALM example
	csvList, err := olm.ListClusterServiceVersion(apiClient, gpuOperatorNamespace, metav1.ListOptions{
		LabelSelector: "operators.coreos.com/gpu-operator-certified.nvidia-gpu-operator",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get CSV: %v", err)
	}

	if len(csvList) == 0 {
		return nil, fmt.Errorf("no matching CSV found")
	}

	// Extract ALM example from CSV annotations
	almExample, ok := csvList[0].Object.Annotations["alm-examples"]
	if !ok {
		return nil, fmt.Errorf("CSV does not contain alm-examples annotation")
	}

	// Create cluster policy from ALM example string
	clusterPolicy := nvidiagpu.NewBuilderFromObjectString(apiClient, almExample)

	// Set the name
	clusterPolicy.Definition.Name = clusterPolicyName
	// Enable device plugin
	enabled := true
	clusterPolicy.Definition.Spec.DevicePlugin.Enabled = &enabled

	// Enable MPS in the device plugin configuration
	if clusterPolicy.Definition.Spec.DevicePlugin.MPS == nil {
		clusterPolicy.Definition.Spec.DevicePlugin.MPS = &nvidiagpuv1.MPSConfig{}
	}

	// Set MPS root directory
	clusterPolicy.Definition.Spec.DevicePlugin.MPS.Root = "/run/nvidia/mps"

	// Set device plugin configuration
	if clusterPolicy.Definition.Spec.DevicePlugin.Config == nil {
		clusterPolicy.Definition.Spec.DevicePlugin.Config = &nvidiagpuv1.DevicePluginConfig{}
	}
	clusterPolicy.Definition.Spec.DevicePlugin.Config.Name = "plugin-config"
	clusterPolicy.Definition.Spec.DevicePlugin.Config.Default = "plugin-config.yaml"

	glog.V(gpuparams.GpuLogLevel).Infof("Creating ClusterPolicy %s from CSV ALM example", clusterPolicyName)
	// Create the cluster policy
	createdPolicy, err := clusterPolicy.Create()
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster policy: %v", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Successfully created ClusterPolicy %s from CSV ALM example", clusterPolicyName)
	return createdPolicy, nil
}
