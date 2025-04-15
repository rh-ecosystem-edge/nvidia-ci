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
	OfedDriverVersion                  string `envconfig:"NVIDIANETWORK_OFED_DRIVER_VERSION"`
	OfedDriverRepository               string `envconfig:"NVIDIANETWORK_OFED_REPOSITORY"`
	RdmaClientHostname                 string `envconfig:"NVIDIANETWORK_RDMA_CLIENT_HOSTNAME"`
	RdmaServerHostname                 string `envconfig:"NVIDIANETWORK_RDMA_SERVER_HOSTNAME"`
	RdmaTestImage                      string `envconfig:"NVIDIANETWORK_RDMA_TEST_IMAGE"`
	MellanoxEthernetInterfaceName      string `envconfig:"NVIDIANETWORK_MELLANOX_ETH_INTERFACE_NAME"`
	MellanoxInfinibandInterfaceName    string `envconfig:"NVIDIANETWORK_MELLANOX_IB_INTERFACE_NAME"`
	MacvlanNetworkName                 string `envconfig:"NVIDIANETWORK_MACVLANNETWORK_NAME"`
	MacvlanNetworkIPAMRange            string `envconfig:"NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE"`
	MacvlanNetworkIPAMGateway          string `envconfig:"NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY"`
	OperatorUpgradeToChannel           string `envconfig:"NVIDIANETWORK_SUBSCRIPTION_UPGRADE_TO_CHANNEL"`
	NNOFallbackCatalogsourceIndexImage string `envconfig:"NVIDIANETWORK_NNO_FALLBACK_CATALOGSOURCE_INDEX_IMAGE"`
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
