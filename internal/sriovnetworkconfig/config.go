package sriovnetworkconfig

import (
	"github.com/golang/glog"

	"github.com/kelseyhightower/envconfig"
)

// SriovNetworkConfig contains environment information related to sriovnetwork tests.
type SriovNetworkConfig struct {
	CatalogSource       string `envconfig:"SRIOVNETWORK_CATALOGSOURCE"`
	SubscriptionChannel string `envconfig:"SRIOVNETWORK_SUBSCRIPTION_CHANNEL"`
	CleanupAfterTest    bool   `envconfig:"SRIOVNETWORK_CLEANUP" default:"true"`
	NicVendor           string `envconfig:"SRIOVNETWORK_NIC_VENDOR" default:"15b3"`
	PfNames             string `envconfig:"SRIOVNETWORK_PF_NAMES" default:"eth_rail0#0-7"`
	NumVfs              int    `envconfig:"SRIOVNETWORK_NUM_VFS" default:"8"`
	Mtu                 int    `envconfig:"SRIOVNETWORK_MTU" default:"1500"`
	ResourceName        string `envconfig:"SRIOVNETWORK_RESOURCE_NAME" default:"sriovlegacy"`
	Priority            int    `envconfig:"SRIOVNETWORK_PRIORITY" default:"90"`
	NodePolicyName      string `envconfig:"SRIOVNETWORK_NETWORK_NODE_POLICY_NAME" default:"sriov-legacy-policy"`
	NetworkName         string `envconfig:"SRIOVNETWORK_NETWORK_NAME" default:"sriov-network"`
	NetworkNamespace    string `envconfig:"SRIOVNETWORK_NETWORK_NAMESPACE" default:"default"`
	IPAMRange           string `envconfig:"SRIOVNETWORK_IPAM_RANGE" default:"192.168.2.225/28"`
	IPAMExcludeIP1      string `envconfig:"SRIOVNETWORK_IPAM_EXCLUDE_IP1" default:"192.168.2.229/30"`
	IPAMExcludeIP2      string `envconfig:"SRIOVNETWORK_IPAM_EXCLUDE_IP2" default:"192.168.2.236/32"`
	Vlan                int    `envconfig:"SRIOVNETWORK_VLAN" default:"0"`
}

// NewSriovNetworkConfig returns instance of SriovNetworkConfig type.
func NewSriovNetworkConfig() *SriovNetworkConfig {
	glog.V(100).Info("Creating new SriovNetworkConfig")

	sriovNetworkConfig := new(SriovNetworkConfig)

	err := envconfig.Process("sriovnetwork", sriovNetworkConfig)
	if err != nil {
		glog.V(100).Infof("failed to instantiate SriovNetworkConfig: %v", err)

		return nil
	}

	return sriovNetworkConfig
}
