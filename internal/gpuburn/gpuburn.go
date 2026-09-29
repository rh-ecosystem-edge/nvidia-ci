package gpuburn

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// github.com/rh-ecosystem-edge/nvidia-ci/tests

const entrypointScript = "entrypoint.sh"

var (
	isFalse bool = false
	isTrue  bool = true
)

// gpuBurnConfigMapDataWithDuration returns the ConfigMap data for the gpu-burn entrypoint
// with the specified burn duration in seconds.
// Optional container args (e.g. time-slicing -m 12%) are appended before the duration via "$@".
func gpuBurnConfigMapDataWithDuration(burnTimeSec int) map[string]string {
	return map[string]string{
		entrypointScript: fmt.Sprintf(`#!/bin/bash
		NUM_GPUS=$(nvidia-smi -L | wc -l)
		if [ $NUM_GPUS -eq 0 ]; then
  			echo "ERROR No GPUs found"
			exit 1
		fi
		./gpu_burn "$@" %d

		if [ ! $? -eq 0 ]; then
		  exit 1
		fi`, burnTimeSec),
	}
}

// CreateGPUBurnConfigMap returns a configmap with data field populated.
// burnTimeSec controls the gpu_burn workload duration in the entrypoint script.
// If a ConfigMap with the same name already exists, it is deleted first so the
// pod always receives the current burn-time setting.
func CreateGPUBurnConfigMap(apiClient *clients.Settings,
	configMapName, configMapNamespace string, burnTimeSec int) (*corev1.ConfigMap, error) {
	existing, pullErr := configmap.Pull(apiClient, configMapName, configMapNamespace)
	if pullErr == nil && existing.Exists() {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"Deleting existing gpu-burn ConfigMap %q in namespace %q to apply current burn-time %ds",
			configMapName, configMapNamespace, burnTimeSec)

		if err := existing.Delete(); err != nil {
			return nil, fmt.Errorf("failed to delete existing ConfigMap %q: %w", configMapName, err)
		}
	}

	configMapBuilder := configmap.NewBuilder(apiClient, configMapName, configMapNamespace)

	configMapBuilderWithData := configMapBuilder.WithData(gpuBurnConfigMapDataWithDuration(burnTimeSec))

	createdConfigMapBuilderWithData, err := configMapBuilderWithData.Create()

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error creating ConfigMap with Data named %s and for namespace %s",
			createdConfigMapBuilderWithData.Object.Name, createdConfigMapBuilderWithData.Object.Namespace)

		return nil, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"Created ConfigMap with Data named %s and for namespace %s",
		createdConfigMapBuilderWithData.Object.Name, createdConfigMapBuilderWithData.Object.Namespace)

	return createdConfigMapBuilderWithData.Object, nil
}

// CreateGPUBurnPodWithMIG returns a Pod configured with MIG resources
func CreateGPUBurnPodWithMIG(apiClient *clients.Settings, podName, podNamespace string,
	gpuBurnImage string, migProfile string, migCount int, timeout time.Duration) (*corev1.Pod, error) {
	var volumeDefaultMode int32 = 0777

	configMapVolumeSource := &corev1.ConfigMapVolumeSource{}
	configMapVolumeSource.Name = "gpu-burn-entrypoint"
	configMapVolumeSource.DefaultMode = &volumeDefaultMode

	// Construct MIG resource name using the migProfile.
	// For single strategy MIGs, migProfile is "gpu" resulting in "nvidia.com/gpu".
	// For other MIG profiles, migProfile is like "1g.5gb", and with required mig prefix: "nvidia.com/mig-1g.5gb".
	var migResourceName string
	var burnContainerArgs []string
	switch migProfile {
	case "gpu":
		migResourceName = fmt.Sprintf("nvidia.com/%s", migProfile)
	case "time-slicing":
		migResourceName = "nvidia.com/gpu" // time-slicing uses gpu resource; pod Args forwarded by entrypoint.sh ("$@")
		burnContainerArgs = []string{"-m", "12%"}
	default:
		migResourceName = fmt.Sprintf("nvidia.com/mig-%s", migProfile)
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
			Labels: map[string]string{
				"app": "gpu-burn-app",
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
					Image:           gpuBurnImage,
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &isFalse,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Name: "gpu-burn-ctr",
					Command: []string{
						"/bin/entrypoint.sh",
					},
					Args: burnContainerArgs,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceName(migResourceName): *resource.NewQuantity(int64(migCount), resource.DecimalSI),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceName(migResourceName): *resource.NewQuantity(int64(migCount), resource.DecimalSI),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "entrypoint",
							MountPath: "/bin/entrypoint.sh",
							ReadOnly:  true,
							SubPath:   entrypointScript,
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

// CreateGPUBurnPod returns a Pod after it is Ready after a timeout periods.
func CreateGPUBurnPod(apiClient *clients.Settings, podName, podNamespace string,
	gpuBurnImage string, timeout time.Duration) (*corev1.Pod, error) {
	var volumeDefaultMode int32 = 0777

	configMapVolumeSource := &corev1.ConfigMapVolumeSource{}
	configMapVolumeSource.Name = "gpu-burn-entrypoint"
	configMapVolumeSource.DefaultMode = &volumeDefaultMode

	var err error = nil

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
			Labels: map[string]string{
				"app": "gpu-burn-app",
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
					Image:           gpuBurnImage,
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &isFalse,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Name: "gpu-burn-ctr",
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
							SubPath:   entrypointScript,
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
	}, err
}
