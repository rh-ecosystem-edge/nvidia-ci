package nvidianetworkconfig

import (
	"log"

	"github.com/kelseyhightower/envconfig"
)

// NvidiaNetworkConfig contains environment information related to nvidianetwork tests.
type NvidiaNetworkConfig struct {
	CatalogSource                      string `envconfig:"NVIDIANETWORK_CATALOGSOURCE"`
	SubscriptionChannel                string `envconfig:"NVIDIANETWORK_SUBSCRIPTION_CHANNEL"`
	CleanupAfterTest                   bool   `envconfig:"NVIDIANETWORK_CLEANUP" default:"true"`
	DeployFromBundle                   bool   `envconfig:"NVIDIANETWORK_DEPLOY_FROM_BUNDLE" default:"false"`
	BundleImage                        string `envconfig:"NVIDIANETWORK_BUNDLE_IMAGE"`
	OperatorUpgradeToChannel           string `envconfig:"NVIDIANETWORK_SUBSCRIPTION_UPGRADE_TO_CHANNEL"`
	NNOFallbackCatalogsourceIndexImage string `envconfig:"NVIDIANETWORK_NNO_FALLBACK_CATALOGSOURCE_INDEX_IMAGE"`
	NFDFallbackCatalogsourceIndexImage string `envconfig:"NVIDIANETWORK_NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE"`
}

// NewNvidiaNetworkConfig returns instance of NvidiaNetworkConfig type.
func NewNvidiaNetworkConfig() *NvidiaNetworkConfig {
	log.Print("Creating new NvidiaGPUConfig")

	nvidiaNetworkConfig := new(NvidiaNetworkConfig)

	err := envconfig.Process("nvidianetwork", nvidiaNetworkConfig)
	if err != nil {
		log.Printf("failed to instantiate nvidiaNetworkConfig: %v", err)

		return nil
	}

	return nvidiaNetworkConfig
}
