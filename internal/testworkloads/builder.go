package testworkloads

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
)

// Builder provides lifecycle management for test workloads.
// It wraps pod.Builder and adds workload-specific validation and success criteria.
type Builder struct {
	workload   Workload
	podBuilder *pod.Builder
	errorMsg   string
	// Stored for lazy initialization
	apiClient *clients.Settings
	namespace string
}

// NewBuilder creates a new Builder for managing workload lifecycle.
// Actual pod spec building and validation is deferred until methods are called.
func NewBuilder(apiClient *clients.Settings, namespace string, workload Workload) *Builder {
	glog.V(100).Infof("Initializing new workload builder in namespace: %s", namespace)

	return &Builder{
		workload:  workload,
		apiClient: apiClient,
		namespace: namespace,
	}
}

// ensureInitialized builds the pod spec and initializes the pod.Builder if not already done.
func (b *Builder) ensureInitialized() error {
	if b.podBuilder != nil {
		return nil
	}

	if b.workload == nil {
		return fmt.Errorf("workload cannot be nil")
	}

	// Build pod spec from workload
	podSpec, err := b.workload.BuildPodSpec()
	if err != nil {
		return fmt.Errorf("failed to build pod spec: %w", err)
	}

	// Ensure namespace is set
	podSpec.Namespace = b.namespace

	// Create pod.Builder from the complete spec
	b.podBuilder = pod.NewBuilderFromDefinition(b.apiClient, podSpec)

	return nil
}

// validate checks if the builder is in a valid state.
func (b *Builder) validate() (bool, error) {
	if b.errorMsg != "" {
		return false, errors.New(b.errorMsg)
	}

	// Initialize pod builder if needed
	if err := b.ensureInitialized(); err != nil {
		b.errorMsg = err.Error()
		return false, err
	}

	return true, nil
}

// Create deploys the workload pod to the cluster.
// Returns the Builder for method chaining.
func (b *Builder) Create() *Builder {
	if valid, _ := b.validate(); !valid {
		return b
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Creating workload pod in namespace %s", b.podBuilder.Definition.Namespace)

	// Delegate to pod.Builder
	var err error
	b.podBuilder, err = b.podBuilder.Create()
	if err != nil {
		b.errorMsg = fmt.Sprintf("failed to create pod: %v", err)
		return b
	}

	return b
}

// WaitUntilStatus waits for the pod to reach a specific phase.
// Returns the Builder for method chaining.
func (b *Builder) WaitUntilStatus(phase corev1.PodPhase, timeout time.Duration) *Builder {
	if valid, _ := b.validate(); !valid {
		return b
	}

	// Delegate to pod.Builder
	err := b.podBuilder.WaitUntilInStatus(phase, timeout)
	if err != nil {
		b.errorMsg = fmt.Sprintf("timeout waiting for pod to reach %s phase: %v", phase, err)
		return b
	}

	return b
}

// WaitUntilRunning waits for the pod to reach Running phase.
// Convenience method for WaitUntilStatus(corev1.PodRunning, timeout).
func (b *Builder) WaitUntilRunning(timeout time.Duration) *Builder {
	return b.WaitUntilStatus(corev1.PodRunning, timeout)
}

// WaitUntilSucceeded waits for the pod to reach Succeeded phase.
// This only checks the pod phase, not workload-specific success criteria.
// Use WaitUntilSuccess() for comprehensive validation.
func (b *Builder) WaitUntilSucceeded(timeout time.Duration) *Builder {
	return b.WaitUntilStatus(corev1.PodSucceeded, timeout)
}

// WaitUntilSuccess waits for the pod to reach Succeeded phase and validates success criteria.
// This combines pod phase check with workload-specific validation via CheckSuccess().
func (b *Builder) WaitUntilSuccess(timeout time.Duration) *Builder {
	if valid, _ := b.validate(); !valid {
		return b
	}

	// First wait for pod to succeed
	b.WaitUntilSucceeded(timeout)

	if b.errorMsg != "" {
		return b
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pod succeeded, validating workload success criteria")

	// Then validate workload-specific success criteria
	err := b.workload.CheckSuccess(b)
	if err != nil {
		b.errorMsg = fmt.Sprintf("workload validation failed: %v", err)
		return b
	}

	return b
}

// GetLogs retrieves logs from the specified container in the workload pod.
func (b *Builder) GetLogs(collectionPeriod time.Duration, containerName string) (string, error) {
	if valid, err := b.validate(); !valid {
		return "", err
	}

	// Delegate to pod.Builder
	logs, err := b.podBuilder.GetLog(collectionPeriod, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return logs, nil
}

// GetFullLogs retrieves all logs from the specified container in the workload pod.
func (b *Builder) GetFullLogs(containerName string) (string, error) {
	if valid, err := b.validate(); !valid {
		return "", err
	}

	// Delegate to pod.Builder
	logs, err := b.podBuilder.GetFullLog(containerName)
	if err != nil {
		return "", fmt.Errorf("failed to get full logs: %w", err)
	}

	return logs, nil
}

// Delete removes the workload pod from the cluster.
func (b *Builder) Delete() error {
	if b.podBuilder == nil {
		// Check if there was a prior error that prevented initialization
		if err := b.Error(); err != nil {
			return fmt.Errorf("cannot delete: %w", err)
		}
		return nil
	}

	// Delegate to pod.Builder
	_, err := b.podBuilder.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	return nil
}

// Error returns any error that occurred during builder operations.
func (b *Builder) Error() error {
	if b.errorMsg == "" {
		return nil
	}
	return errors.New(b.errorMsg)
}
