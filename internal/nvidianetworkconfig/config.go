package nvidianetworkconfig

import (
	"github.com/golang/glog"

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
	glog.V(100).Info("Creating new NvidiaNetworkConfig")

	nvidiaNetworkConfig := new(NvidiaNetworkConfig)

	err := envconfig.Process("nvidianetwork", nvidiaNetworkConfig)
	if err != nil {
		glog.V(100).Infof("failed to instantiate NvidiaNetworkConfig: %v", err)

		return nil
	}

	return nvidiaNetworkConfig
}
