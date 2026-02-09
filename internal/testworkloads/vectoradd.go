package testworkloads

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// DefaultImage is the default container image for VectorAdd workload
	DefaultImage = "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubi8"

	// ContainerName is the name of the VectorAdd container
	ContainerName = "vectoradd-ctr"

	// SuccessIndicator is the string that indicates successful completion in logs
	SuccessIndicator = "Test PASSED"
)

// VectorAddWorkload implements the Workload interface for CUDA vector addition sample.
type VectorAddWorkload struct {
	podName      string
	image        string
	resources    corev1.ResourceRequirements
	nodeSelector map[string]string
	tolerations  []corev1.Toleration
}

// NewVectorAdd creates a VectorAdd workload with sensible defaults.
func NewVectorAdd(podName string) *VectorAddWorkload {
	glog.V(100).Infof("Creating VectorAdd workload: %s", podName)
	return &VectorAddWorkload{
		podName: podName,
		image:   DefaultImage,
		resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
		},
		nodeSelector: map[string]string{
			"nvidia.com/gpu.present":         "true",
			"node-role.kubernetes.io/worker": "",
		},
		tolerations: []corev1.Toleration{
			{
				Key:      "nvidia.com/gpu",
				Effect:   corev1.TaintEffectNoSchedule,
				Operator: corev1.TolerationOpExists,
			},
		},
	}
}

// WithImage sets a custom container image.
func (v *VectorAddWorkload) WithImage(image string) *VectorAddWorkload {
	v.image = image
	return v
}

// WithResources sets custom resource requirements.
func (v *VectorAddWorkload) WithResources(resources corev1.ResourceRequirements) *VectorAddWorkload {
	v.resources = resources
	return v
}

// WithNodeSelector sets a custom node selector.
func (v *VectorAddWorkload) WithNodeSelector(selector map[string]string) *VectorAddWorkload {
	v.nodeSelector = selector
	return v
}

// WithTolerations sets custom tolerations.
func (v *VectorAddWorkload) WithTolerations(tolerations []corev1.Toleration) *VectorAddWorkload {
	v.tolerations = tolerations
	return v
}

// BuildPodSpec creates the pod specification for VectorAdd workload.
func (v *VectorAddWorkload) BuildPodSpec() (*corev1.Pod, error) {
	glog.V(gpuparams.GpuLogLevel).Infof("Building pod spec for VectorAdd workload: %s", v.podName)

	if v.podName == "" {
		return nil, fmt.Errorf("pod name cannot be empty")
	}

	if v.image == "" {
		return nil, fmt.Errorf("container image cannot be empty")
	}

	container := NewUnprivilegedContainer(ContainerName, v.image, v.resources)

	pod := NewUnprivilegedPod(
		v.podName,
		[]corev1.Container{container},
		v.nodeSelector,
		v.tolerations,
		map[string]string{"app": "vectoradd-app"},
	)

	return pod, nil
}

// CheckSuccess performs comprehensive success validation for VectorAdd.
// For VectorAdd, this validates that the logs contain the success indicator.
func (v *VectorAddWorkload) CheckSuccess(builder *Builder) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Checking VectorAdd success criteria")

	logs, err := builder.GetFullLogs(ContainerName)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	if !strings.Contains(logs, SuccessIndicator) {
		return fmt.Errorf("logs do not contain success indicator '%s'", SuccessIndicator)
	}

	return nil
}
