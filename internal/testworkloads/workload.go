package testworkloads

import (
	corev1 "k8s.io/api/core/v1"
)

// Workload defines workload-specific behavior that varies between different test workloads.
type Workload interface {
	// BuildPodSpec creates the pod specification for this workload.
	// Returns the pod definition with containers, volumes, etc.
	BuildPodSpec() (*corev1.Pod, error)

	// CheckSuccess performs comprehensive success validation.
	// The builder provides access to logs, pod state, and other data needed for validation.
	// Called by WaitUntilSuccess() after the pod reaches Succeeded phase.
	// Return nil if the workload completed successfully.
	CheckSuccess(builder *Builder) error
}
