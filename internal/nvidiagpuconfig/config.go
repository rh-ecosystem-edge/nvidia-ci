package nvidiagpuconfig

import (
	"log"

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
	NFDFallbackCatalogsourceIndexImage string `envconfig:"NVIDIAGPU_NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE"` // should this be here?
}

// NewNvidiaGPUConfig returns instance of NvidiaGPUConfig type.
func NewNvidiaGPUConfig() *NvidiaGPUConfig {
	log.Print("Creating new NvidiaGPUConfig")

	nvidiaGPUConfig := new(NvidiaGPUConfig)

	err := envconfig.Process("nvidiagpu_", nvidiaGPUConfig)
	if err != nil {
		log.Printf("failed to instantiate nvidiaGPUConfig: %v", err)

		return nil
	}

	return nvidiaGPUConfig
}
