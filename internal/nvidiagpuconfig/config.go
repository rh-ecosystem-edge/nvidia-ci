package nvidiagpuconfig

import (
	"github.com/golang/glog"
	"github.com/kelseyhightower/envconfig"
)

// NvidiaGPUConfig contains environment information related to nvidiagpu tests.
type NvidiaGPUConfig struct {
	InstanceType                       string `envconfig:"NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE"`
	CatalogSource                      string `envconfig:"NVIDIAGPU_CATALOGSOURCE"`
	SubscriptionChannel                string `envconfig:"NVIDIAGPU_SUBSCRIPTION_CHANNEL"`
	CleanupAfterTest                   bool   `envconfig:"NVIDIAGPU_CLEANUP" default:"true"`
	DeployFromBundle                   bool   `envconfig:"NVIDIAGPU_DEPLOY_FROM_BUNDLE" default:"false"`
	BundleImage                        string `envconfig:"NVIDIAGPU_BUNDLE_IMAGE"`
	OperatorUpgradeToChannel           string `envconfig:"NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL"`
	GPUFallbackCatalogsourceIndexImage string `envconfig:"NVIDIAGPU_GPU_FALLBACK_CATALOGSOURCE_INDEX_IMAGE"`
	ClusterPolicyPatch                 string `envconfig:"NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"`
	SingleMIGProfile                   string `envconfig:"NVIDIAGPU_SINGLE_MIG_PROFILE"`
	MIGInstances                       string `envconfig:"NVIDIAGPU_MIG_INSTANCES"`
	MIGStrategy                        string `envconfig:"NVIDIAGPU_MIG_STRATEGY"`
	DelayBetweenPods                   int    `envconfig:"NVIDIAGPU_DELAY_BETWEEN_PODS"`
}

// NewNvidiaGPUConfig returns an instance of NvidiaGPUConfig.
// Logs at V(100) and returns nil on failure.
func NewNvidiaGPUConfig() *NvidiaGPUConfig {
	log := glog.V(100)
	log.Info("Creating new NvidiaGPUConfig")

	cfg := &NvidiaGPUConfig{}
	if err := envconfig.Process("nvidiagpu_", cfg); err != nil {
		glog.V(100).Infof("Failed to instantiate NvidiaGPUConfig: %v", err)
		return nil
	}

	log.Info("NvidiaGPUConfig created successfully")
	return cfg
}
