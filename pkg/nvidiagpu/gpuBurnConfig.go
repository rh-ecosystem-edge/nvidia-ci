package nvidiagpu

// GPUBurnConfig holds configuration constants for running and managing the GPU burn test.
// This setup is part of the business domain focused on GPU performance verification.
type GPUBurnConfig struct {
	Namespace     string
	PodName       string
	PodLabel      string
	ConfigMapName string
}

// NewDefaultGPUBurnConfig creates a default configuration for the GPU burn test.
// This configuration sets up the necessary resources identified by unique names and labels,
// which are crucial for orchestrating the test within a Kubernetes environment.
func NewDefaultGPUBurnConfig() *GPUBurnConfig {
	return &GPUBurnConfig{
		Namespace:     "test-gpu-burn",
		PodName:       "gpu-burn-pod",
		PodLabel:      "app=gpu-burn-app",
		ConfigMapName: "gpu-burn-entrypoint",
	}
}
