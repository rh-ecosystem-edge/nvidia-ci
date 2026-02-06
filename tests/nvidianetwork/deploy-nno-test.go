package nvidianetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidianetworkconfig"
	rdmatest "github.com/rh-ecosystem-edge/nvidia-ci/internal/rdma"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfdcheck"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"                          //nolint:revive,staticcheck // Dot import is standard for Ginkgo tests
	. "github.com/onsi/gomega"                             //nolint:revive,staticcheck // Dot import is standard for Gomega assertions
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"  //nolint:revive,staticcheck // Dot import for global constants
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
	internalNFD "github.com/rh-ecosystem-edge/nvidia-ci/internal/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidianetwork"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
)

var (
	nfdInstance = operatorconfig.NewCustomConfig()

	WorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel: "",
		nvidiaNetworkLabel:                  "true",
	}

	// NvidiaNetworkConfig provides access to general configuration parameters.
	nvidiaNetworkConfig *nvidianetworkconfig.NvidiaNetworkConfig
	nfdConfig           *internalNFD.NFDConfig
	CatalogSource                         = UndefinedValue
	SubscriptionChannel                   = UndefinedValue
	InstallPlanApproval v1alpha1.Approval = "Automatic"

	DefaultSubscriptionChannel           = UndefinedValue
	networkOperatorUpgradeToChannel      = UndefinedValue
	cleanupAfterTest                bool = true
	deployFromBundle                bool = false
	networkOperatorBundleImage           = ""
	clusterArchitecture                  = UndefinedValue

	CustomCatalogSource = UndefinedValue

	createNNOCustomCatalogsource  bool = false
	CustomCatalogsourceIndexImage      = UndefinedValue

	rdmaWorkloadNamespace = UndefinedValue
	rdmaLinkType          = UndefinedValue
	rdmaMlxDevice         = UndefinedValue
	rdmaClientHostname    = UndefinedValue
	rdmaServerHostname    = UndefinedValue
	// rdmaTestImage              = UndefinedValue
	rdmaNetworkType      = "shared-device"
	rdmaGPUDirect   bool = false

	mellanoxEthernetInterfaceName   = UndefinedValue
	mellanoxInfinibandInterfaceName = UndefinedValue

	macvlanNetworkName        = UndefinedValue
	macvlanNetworkIPAMRange   = UndefinedValue
	macvlanNetworkIPAMGateway = UndefinedValue

	ipoibNetworkName           = UndefinedValue
	ipoibNetworkIPAMRange      = UndefinedValue
	ipoibNetworkIPAMExcludeIP1 = UndefinedValue
	ipoibNetworkIPAMExcludeIP2 = UndefinedValue

	ofedDriverVersion    = UndefinedValue
	ofedDriverRepository = UndefinedValue

	sriovNetworkName = UndefinedValue

	rdmaTestImage = UndefinedValue

	// Default image based on cluster architecture
	rdmaTestImageDefault = map[string]string{
		"amd64": "quay.io/wabouham/ecosys-nvidia/rdma-tools:0.0.3",
		"arm64": "quay.io/wabouham/ecosys-nvidia/rdma-tools-aarch64:0.0.3",
	}

	withCuda = "no"
)

const (
	nvidiaNetworkLabel                      = "feature.node.kubernetes.io/pci-15b3.present"
	networkOperatorDefaultMasterBundleImage = "registry.gitlab.com/nvidia/kubernetes/network-operator/staging/network-operator-bundle:main-latest"

	nnoNamespace                        = "nvidia-network-operator"
	nnoOperatorGroupName                = "nno-og"
	nnoDeployment                       = "nvidia-network-operator-controller-manager"
	nnoSubscriptionName                 = "nno-subscription"
	nnoSubscriptionNamespace            = "nvidia-network-operator"
	nnoCatalogSourceDefault             = "certified-operators"
	nnoCatalogSourceNamespace           = nfd.CatalogSourceNamespace
	nnoPackage                          = "nvidia-network-operator"
	nnoNicClusterPolicyName             = "nic-cluster-policy"
	nnoMacvlanNetworkNameDefault        = "rdmashared-net"
	nnoIPoIBNetworkNameDefault          = "example-ipoibnetwork"
	nnoCustomCatalogSourcePublisherName = "Red Hat"
	nnoCustomCatalogSourceDisplayName   = "Certified Operators Custom"

	mellanoxEthernetInterfaceNameDefault   = "ens1f0np0"
	mellanoxInfinibandInterfaceNameDefault = "ibs1f1"
)

var _ = Describe("NNO", Ordered, Label(tsparams.LabelSuite), func() {

	var (
		deployBundle       deploy.Deploy
		deployBundleConfig deploy.BundleConfig
	)

	if mellanoxEthernetInterfaceName == "" {
		mellanoxEthernetInterfaceName = mellanoxEthernetInterfaceNameDefault
	}

	if mellanoxInfinibandInterfaceName == "" {
		mellanoxInfinibandInterfaceName = mellanoxEthernetInterfaceNameDefault
	}

	nvidiaNetworkConfig = nvidianetworkconfig.NewNvidiaNetworkConfig()
	nfdConfig, _ = internalNFD.NewNFDConfig()

	Context("DeployNNO", Label("deploy-nno-with-dtk"), func() {

		BeforeAll(func() {

			if nvidiaNetworkConfig.CatalogSource == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_CATALOGSOURCE"+
					" is not set, using default NNO catalogsource '%s'", nnoCatalogSourceDefault)
				CatalogSource = nnoCatalogSourceDefault
			} else {
				CatalogSource = nvidiaNetworkConfig.CatalogSource
				glog.V(networkparams.LogLevel).Infof("NNO catalogsource now set to env variable "+
					"NVIDIANETWORK_CATALOGSOURCE value '%s'", CatalogSource)
			}

			if nvidiaNetworkConfig.SubscriptionChannel == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_SUBSCRIPTION_CHANNEL" +
					" is not set, will deploy latest channel")
				SubscriptionChannel = UndefinedValue
			} else {
				SubscriptionChannel = nvidiaNetworkConfig.SubscriptionChannel
				glog.V(networkparams.LogLevel).Infof("NNO Subscription Channel now set to env variable "+
					"NVIDIANETWORK_SUBSCRIPTION_CHANNEL value '%s'", SubscriptionChannel)
			}

			if nvidiaNetworkConfig.CleanupAfterTest {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_CLEANUP" +
					" is not set or is set to True, will cleanup resources after test case execution")
				cleanupAfterTest = true
			} else {
				cleanupAfterTest = nvidiaNetworkConfig.CleanupAfterTest
				glog.V(networkparams.LogLevel).Infof("Flag to cleanup after test is set to env variable "+
					"NVIDIANETWORK_CLEANUP value '%v'", cleanupAfterTest)
			}

			if nvidiaNetworkConfig.OfedDriverVersion == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_OFED_DRIVER_VERSION" +
					" is not set, will use default ofed driver version from CSV alm-examples section")
				ofedDriverVersion = UndefinedValue
			} else {
				ofedDriverVersion = nvidiaNetworkConfig.OfedDriverVersion
				glog.V(networkparams.LogLevel).Infof("ofedDriverVersion is set to env variable "+
					"NVIDIANETWORK_OFED_DRIVER_VERSION value '%s'", ofedDriverVersion)
			}

			if nvidiaNetworkConfig.OfedDriverRepository == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_OFED_REPOSITORY" +
					" is not set, will use default ofed driver repository from CSV alm-examples section")
				ofedDriverRepository = UndefinedValue
			} else {
				ofedDriverRepository = nvidiaNetworkConfig.OfedDriverRepository
				glog.V(networkparams.LogLevel).Infof("ofedDriverRepository is set to env variable "+
					"NVIDIANETWORK_OFED_REPOSITORY value '%s'", ofedDriverRepository)
			}

			if nvidiaNetworkConfig.MacvlanNetworkName == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_MACVLANNETWORK_NAME"+
					" is not set, will use default name '%s'", nnoMacvlanNetworkNameDefault)
				macvlanNetworkName = nnoMacvlanNetworkNameDefault
			} else {
				macvlanNetworkName = nvidiaNetworkConfig.MacvlanNetworkName
				glog.V(networkparams.LogLevel).Infof("macvlanNetworkName is set to env variable "+
					"NVIDIANETWORK_MACVLANNETWORK_NAME value '%s'", macvlanNetworkName)
			}

			if nvidiaNetworkConfig.MacvlanNetworkIPAMRange == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE" +
					" is not set, skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE is not set")
				Skip("env variable NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE is not set")
			} else {
				macvlanNetworkIPAMRange = nvidiaNetworkConfig.MacvlanNetworkIPAMRange
				glog.V(networkparams.LogLevel).Infof("macvlanNetworkIPAMRange is set to env variable "+
					"NVIDIANETWORK_MACVLANNETWORK_IPAM_RANGE value '%s'", macvlanNetworkIPAMRange)
			}

			if nvidiaNetworkConfig.MacvlanNetworkIPAMGateway == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY" +
					" is not set, skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY is not set")
				Skip("env variable NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY is not set")
			} else {
				macvlanNetworkIPAMGateway = nvidiaNetworkConfig.MacvlanNetworkIPAMGateway
				glog.V(networkparams.LogLevel).Infof("macvlanNetworkIPAMGatway is set to env variable "+
					"NVIDIANETWORK_MACVLANNETWORK_IPAM_GATEWAY value '%s'", macvlanNetworkIPAMGateway)
			}

			if nvidiaNetworkConfig.IPoIBNetworkName == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_IPOIBNETWORK_NAME"+
					" is not set, will use default name '%s'", nnoIPoIBNetworkNameDefault)
				ipoibNetworkName = nnoIPoIBNetworkNameDefault
			} else {
				ipoibNetworkName = nvidiaNetworkConfig.IPoIBNetworkName
				glog.V(networkparams.LogLevel).Infof("ipoibNetworkName is set to env variable "+
					"NVIDIANETWORK_IPOIBNETWORK_NAME value '%s'", ipoibNetworkName)
			}

			if nvidiaNetworkConfig.IPoIBNetworkIPAMRange == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_IPOIBNETWORK_IPAM_RANGE" +
					" is not set, skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_IPOIBNETWORK_IPAM_RANGE is not set")
				Skip("env variable NVIDIANETWORK_IPOIBNETWORK_IPAM_RANGE is not set")
			} else {
				ipoibNetworkIPAMRange = nvidiaNetworkConfig.IPoIBNetworkIPAMRange
				glog.V(networkparams.LogLevel).Infof("ipoibNetworkIPAMRange is set to env variable "+
					"NVIDIANETWORK_IPOIBNETWORK_IPAM_RANGE value '%s'", ipoibNetworkIPAMRange)
			}

			if nvidiaNetworkConfig.IPoIBNetworkIPAMExcludeIP1 == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP1" +
					" is not set, skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP1 is not set")
				Skip("env variable NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP1 is not set")
			} else {
				ipoibNetworkIPAMExcludeIP1 = nvidiaNetworkConfig.IPoIBNetworkIPAMExcludeIP1
				glog.V(networkparams.LogLevel).Infof("ipoibNetworkIPAMExcludeIP1 is set to env variable "+
					"NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP1 value '%s'", ipoibNetworkIPAMExcludeIP1)
			}

			if nvidiaNetworkConfig.IPoIBNetworkIPAMExcludeIP2 == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP2" +
					" is not set, skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP2 is not set")
				Skip("env variable NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP2 is not set")
			} else {
				ipoibNetworkIPAMExcludeIP2 = nvidiaNetworkConfig.IPoIBNetworkIPAMExcludeIP2
				glog.V(networkparams.LogLevel).Infof("ipoibNetworkIPAMExcludeIP2 is set to env variable "+
					"NVIDIANETWORK_IPOIBNETWORK_IPAM_EXCLUDEIP2 value '%s'", ipoibNetworkIPAMExcludeIP2)
			}

			if nvidiaNetworkConfig.SriovNetworkName == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_SRIOV_NETWORK_NAME" +
					" is not set, skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_RDMA_SRIOV_NETWORK_NAME is not set")
				Skip("env variable NVIDIANETWORK_RDMA_SRIOV_NETWORK_NAME is not set")
			} else {
				sriovNetworkName = nvidiaNetworkConfig.SriovNetworkName
				glog.V(networkparams.LogLevel).Infof("SRIOV Network Name is set to env variable "+
					"NVIDIANETWORK_RDMA_SRIOV_NETWORK_NAME value '%s'", sriovNetworkName)
			}

			if nvidiaNetworkConfig.RdmaClientHostname == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_CLIENT_HOSTNAME" +
					" is not set skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_RDMA_CLIENT_HOSTNAME is not set")
				Skip("env variable NVIDIANETWORK_RDMA_CLIENT_HOSTNAME is not set")
			} else {
				rdmaClientHostname = nvidiaNetworkConfig.RdmaClientHostname
				glog.V(networkparams.LogLevel).Infof("rdmaClientHostname is set to env variable "+
					"NVIDIANETWORK_RDMA_CLIENT_HOSTNAME value '%v'", rdmaClientHostname)
			}

			if nvidiaNetworkConfig.RdmaServerHostname == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_SERVER_HOSTNAME" +
					" is not set skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_RDMA_SERVER_HOSTNAME is not set")
				Skip("env variable NVIDIANETWORK_RDMA_SERVER_HOSTNAME is not set")
			} else {
				rdmaServerHostname = nvidiaNetworkConfig.RdmaServerHostname
				glog.V(networkparams.LogLevel).Infof("rdmaServerHostname is set to env variable "+
					"NVIDIANETWORK_RDMA_SERVER_HOSTNAME value '%v'", rdmaServerHostname)
			}

			if nvidiaNetworkConfig.RdmaWorkloadNamespace == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE" +
					" is not set skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE is not set")
				Skip("env variable NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE is not set")
			} else {
				rdmaWorkloadNamespace = nvidiaNetworkConfig.RdmaWorkloadNamespace
				glog.V(networkparams.LogLevel).Infof("rdmaLinkType is set to env variable "+
					"NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE value '%v'", rdmaWorkloadNamespace)
			}

			if nvidiaNetworkConfig.RdmaLinkType == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_LINK_TYPE" +
					" is not set skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_RDMA_LINK_TYPE is not set")
				Skip("env variable NVIDIANETWORK_RDMA_LINK_TYPE is not set")
			} else {
				rdmaLinkType = nvidiaNetworkConfig.RdmaLinkType
				glog.V(networkparams.LogLevel).Infof("rdmaLinkType is set to env variable "+
					"NVIDIANETWORK_RDMA_LINK_TYPE value '%v'", rdmaLinkType)
			}

			if nvidiaNetworkConfig.RdmaMlxDevice == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_MLX_DEVICE" +
					" is not set skipping test case execution")
				glog.V(networkparams.LogLevel).Infof("Skipping testcase:  env variable " +
					"NVIDIANETWORK_RDMA_MLX_DEVICE is not set")
				Skip("env variable NVIDIANETWORK_RDMA_MLX_DEVICE is not set")
			} else {
				rdmaMlxDevice = nvidiaNetworkConfig.RdmaMlxDevice
				glog.V(networkparams.LogLevel).Infof("rdmaMlxDevice is set to env variable "+
					"NVIDIANETWORK_RDMA_MLX_DEVICE value '%v'", rdmaMlxDevice)
			}

			if nvidiaNetworkConfig.RdmaGPUDirect {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_GPUDIRECT" +
					" is set to 'True', will add the cuda switch in RDMA tests")
				rdmaGPUDirect = nvidiaNetworkConfig.RdmaGPUDirect
			} else {
				rdmaGPUDirect = false
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_GPUDIRECT is " +
					"not set or set to False, will execute RDMA tests without cuda switch")
			}

			switch nvidiaNetworkConfig.RdmaNetworkType {
			case "sriov":
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_NETWORK_TYPE" +
					" is set to 'sriov', will remove the RDMASaredDevicePlugin element form the NicClusterPolicy")
				rdmaNetworkType = nvidiaNetworkConfig.RdmaNetworkType
			case "shared-device":
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_NETWORK_TYPE" +
					" is set to 'shared-device', proceeding with setting up default NicCLusterPolicy for Shared Device")
				rdmaNetworkType = nvidiaNetworkConfig.RdmaNetworkType
			default:
				rdmaNetworkType = "shared-device"
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_NETWORK_TYPE is " +
					"not set, proceeding with setting up default NicCLusterPolicy for Shared Device")
			}

			if nvidiaNetworkConfig.MellanoxEthernetInterfaceName == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_MELLANOX_ETH_INTERFACE_NAME"+
					" is not set, will use default ethernet interface name '%s'", mellanoxEthernetInterfaceNameDefault)
				mellanoxEthernetInterfaceName = mellanoxEthernetInterfaceNameDefault
			} else {
				mellanoxEthernetInterfaceName = nvidiaNetworkConfig.MellanoxEthernetInterfaceName
				glog.V(networkparams.LogLevel).Infof("mellanoxEthernetInterfaceName is set to env variable "+
					"NVIDIANETWORK_MELLANOX_ETH_INTERFACE_NAME value '%v'", mellanoxEthernetInterfaceName)
			}

			if nvidiaNetworkConfig.MellanoxInfinibandInterfaceName == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_MELLANOX_IB_INTERFACE_NAME"+
					" is not set, will use default infiniband interface name '%s'", mellanoxInfinibandInterfaceNameDefault)
				mellanoxInfinibandInterfaceName = mellanoxEthernetInterfaceNameDefault
			} else {
				mellanoxInfinibandInterfaceName = nvidiaNetworkConfig.MellanoxInfinibandInterfaceName
				glog.V(networkparams.LogLevel).Infof("mellanoxInfinibandInterfaceName is set to env variable "+
					"NVIDIANETWORK_MELLANOX_IB_INTERFACE_NAME value '%v'", mellanoxInfinibandInterfaceName)
			}

			if nvidiaNetworkConfig.DeployFromBundle {
				deployFromBundle = nvidiaNetworkConfig.DeployFromBundle
				glog.V(networkparams.LogLevel).Infof("Flag deploy Network operator from bundle is set "+
					"to env variable NVIDIANETWORK_DEPLOY_FROM_BUNDLE value '%v'", deployFromBundle)
				if nvidiaNetworkConfig.BundleImage == "" {
					glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_BUNDLE_IMAGE"+
						" is not set, will use the default bundle image '%s'", networkOperatorDefaultMasterBundleImage)
					networkOperatorBundleImage = networkOperatorDefaultMasterBundleImage
				} else {
					networkOperatorBundleImage = nvidiaNetworkConfig.BundleImage
					glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_BUNDLE_IMAGE"+
						" is set, will use the specified bundle image '%s'", networkOperatorBundleImage)
				}
			} else {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_DEPLOY_FROM_BUNDLE" +
					" is set to false or is not set, will deploy Network Operator from catalogsource")
				deployFromBundle = false
			}

			if nvidiaNetworkConfig.OperatorUpgradeToChannel == "" {
				glog.V(networkparams.LogLevel).Infof("env variable " +
					"NVIDIANETWORK_SUBSCRIPTION_UPGRADE_TO_CHANNEL is not set, will not run the Upgrade Testcase")
				networkOperatorUpgradeToChannel = UndefinedValue
			} else {
				networkOperatorUpgradeToChannel = nvidiaNetworkConfig.OperatorUpgradeToChannel
				glog.V(networkparams.LogLevel).Infof("Network Operator Upgrade to channel now set to env"+
					" variable NVIDIANETWORK_SUBSCRIPTION_UPGRADE_TO_CHANNEL value '%s'", networkOperatorUpgradeToChannel)
			}

			if nvidiaNetworkConfig.NNOFallbackCatalogsourceIndexImage != "" {
				glog.V(networkparams.LogLevel).Infof("env variable "+
					"NVIDIANETWORK_NNO_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nvidiaNetworkConfig.NNOFallbackCatalogsourceIndexImage)

				CustomCatalogsourceIndexImage = nvidiaNetworkConfig.NNOFallbackCatalogsourceIndexImage

				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom Network Operator " +
					"catalogsource from fall back index image to True")

				createNNOCustomCatalogsource = true

				CustomCatalogSource = nnoCatalogSourceDefault + "-custom"
				glog.V(networkparams.LogLevel).Infof("Setting custom NNO catalogsource name to '%s'",
					CustomCatalogSource)

			} else {
				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom Network Operator " +
					"catalogsource from fall back index image to False")
				createNNOCustomCatalogsource = false
			}

			if nfdConfig.FallbackCatalogSourceIndexImage != "" {
				glog.V(networkparams.LogLevel).Infof("env variable "+
					"NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nfdConfig.FallbackCatalogSourceIndexImage)

				nfdInstance.CustomCatalogSourceIndexImage = nfdConfig.FallbackCatalogSourceIndexImage

				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom NFD operator " +
					"catalogsource from fall back index image to True")

				nfdInstance.CreateCustomCatalogsource = true

				nfdInstance.CustomCatalogSource = nfd.CatalogSourceDefault + "-custom"
				glog.V(networkparams.LogLevel).Infof("Setting custom NFD catalogsource name to '%s'",
					nfdInstance.CustomCatalogSource)

			} else {
				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom NFD operator " +
					"catalogsource from fall back index image to False")
				nfdInstance.CreateCustomCatalogsource = false
			}

			// Set the withCuda switch based on env var value
			if rdmaGPUDirect {
				withCuda = "yes"
			} else {
				withCuda = "no"
			}

			glog.V(networkparams.LogLevel).Infof("Setting the withCuda switch for RDMA workload to '%s'",
				withCuda)

			By("Report OpenShift version")
			ocpVersion, err := inittools.GetOpenShiftVersion()
			glog.V(networkparams.LogLevel).Infof("Current OpenShift cluster version is: '%s'", ocpVersion)

			if err != nil {
				glog.Error("Error getting OpenShift version: ", err)
			} else {
				if writeErr := inittools.GeneralConfig.WriteReport(OpenShiftVersionFile,
					[]byte(ocpVersion)); writeErr != nil {
					glog.Error("Error writing OpenShift version file: ", writeErr)
				}
			}

			nfd.EnsureNFDIsInstalled(inittools.APIClient, nfdInstance, ocpVersion, networkparams.LogLevel)

			// After NFD is installed, we can network operator enabled worker node architecture
			By("Get Cluster Architecture from first NVIDIA Network enabled worker node")
			glog.V(networkparams.LogLevel).Infof("Getting cluster architecture from nodes with "+
				"networkWorkerNodeSelector: %v", WorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			clusterArchitecture = clusterArch
			glog.V(networkparams.LogLevel).Infof("cluster architecture for NVIDIA Network enabled worker node "+
				"is: %s", clusterArchitecture)

			if nvidiaNetworkConfig.RdmaTestImage == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_RDMA_TEST_IMAGE"+
					" is not set, will use default container image '%s'", rdmaTestImageDefault[clusterArchitecture])
				rdmaTestImage = rdmaTestImageDefault[clusterArchitecture]
			} else {
				rdmaTestImage = nvidiaNetworkConfig.RdmaTestImage
				glog.V(networkparams.LogLevel).Infof("rdmaTestImage is set to env variable "+
					"NVIDIANETWORK_RDMA_TEST_IMAGE value '%v'", rdmaTestImage)
			}

		})

		BeforeEach(func() {

		})

		AfterEach(func() {

		})

		AfterAll(func() {

			if nfdInstance.CleanupAfterInstall && cleanupAfterTest {
				err := nfd.Cleanup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "Error cleaning up NFD resources: %v", err)
			}

		})

		It("Deploy NVIDIA Network Operator with DTK", Label("deploy"), func() {

			nfdcheck.CheckNfdInstallation(inittools.APIClient, nfd.OSLabel, nfd.GetAllowedOSLabels(),
				inittools.GeneralConfig.WorkerLabelMap, networkparams.LogLevel)

			By("Check if at least one worker node is has label for Mellanox cards enabled")
			networkNodeFound, _ := check.NodeWithLabel(inittools.APIClient, nvidiaNetworkLabel,
				inittools.GeneralConfig.WorkerLabelMap)

			glog.V(networkparams.LogLevel).Infof("The check for Nvidia Network label returned: %v",
				networkNodeFound)

			if !networkNodeFound {
				glog.V(networkparams.LogLevel).Infof("Skipping test:  No Nvidia Network Cards were " +
					"found on any node and flag")
				Skip("No Nvidia Network labeled worker nodes in this cluster")

			}

			By("Delete /opt/mofed-container/inventory RPMs directory on worker nodes")
			// Delete the "/opt/mofed-container/inventory" dir on each worker node
			// func DeleteMofedRpmDir(clientset *clients.Settings, podName, namespace, nodeName string) (string, error)
			rdmaWorkerNodes := []string{rdmaClientHostname, rdmaServerHostname}

			for i, workerNode := range rdmaWorkerNodes {
				glog.V(networkparams.LogLevel).Infof("Deleting MOFED RPMS dir on worker node '%d' named '%s'",
					i, workerNode)
				debugPodName := fmt.Sprintf("delete-rpm-pod-%d", i) // ensure unique name
				deleteMofedRPMDirOutput, err := rdmatest.DeleteMofedRpmDir(inittools.APIClient, debugPodName,
					"default", clusterArchitecture, workerNode)
				Expect(err).ToNot(HaveOccurred(), "Error deleting MOFED RPMs dir on worker node"+
					" '%s':   %v", workerNode, err)
				glog.V(networkparams.LogLevel).Infof("Output from deleting MOFED RPMS dir on worker node '%s'"+
					" : \n%s", workerNode, deleteMofedRPMDirOutput)
			}

			By("Check if Network Operator Deployment is from Bundle")
			if deployFromBundle {
				glog.V(networkparams.LogLevel).Infof("Deploying Network operator from bundle")
				// This returns the Deploy interface object initialized with the API client
				deployBundle = deploy.NewDeploy(inittools.APIClient)
				deployBundleConfig.BundleImage = networkOperatorBundleImage
				glog.V(networkparams.LogLevel).Infof("Deploying Network operator from bundle image '%s'",
					deployBundleConfig.BundleImage)

			} else {
				glog.V(networkparams.LogLevel).Infof("Deploying Network Operator from catalogsource")

				By("Check if 'nvidia-network-operator' packagemanifest exists in certified-operators catalog")
				glog.V(networkparams.LogLevel).Infof("Using NNO catalogsource '%s'", CatalogSource)

				nnoPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
					nnoPackage, nnoCatalogSourceNamespace, nnoCatalogSourceDefault)

				if err != nil {
					glog.V(networkparams.LogLevel).Infof("Error trying to pull NNO packagemanifest"+
						" '%s' from default catalog '%s': '%v'", nnoPackage, nnoCatalogSourceDefault, err.Error())
				}

				if nnoPkgManifestBuilderByCatalog == nil {
					glog.V(networkparams.LogLevel).Infof("The NNO packagemanifest '%s' was not "+
						"found in the default '%s' catalog", nnoPackage, nnoCatalogSourceDefault)

					if createNNOCustomCatalogsource {
						glog.V(networkparams.LogLevel).Infof("Creating custom catalogsource '%s' for Network "+
							"Operator, with index image '%s'", CustomCatalogSource, CustomCatalogsourceIndexImage)

						glog.V(networkparams.LogLevel).Infof("Deploying a custom NNO catalogsource '%s' with '%s' "+
							"index image", CustomCatalogSource, CustomCatalogsourceIndexImage)

						nnoCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
							CustomCatalogSource, nnoCatalogSourceNamespace, CustomCatalogsourceIndexImage,
							nnoCustomCatalogSourceDisplayName, nnoCustomCatalogSourcePublisherName)

						Expect(nnoCustomCatalogSourceBuilder).NotTo(BeNil(), "Failed to Initialize "+
							"CatalogSourceBuilder for custom NNO catalogsource '%s'", CustomCatalogSource)

						createdNNOCustomCatalogSourceBuilder, err := nnoCustomCatalogSourceBuilder.Create()
						glog.V(networkparams.LogLevel).Infof("Creating custom NNO Catalogsource builder object "+
							"'%s'", createdNNOCustomCatalogSourceBuilder.Definition.Name)
						Expect(err).ToNot(HaveOccurred(), "error creating custom NNO catalogsource "+
							"builder Object name %s:  %v", CustomCatalogSource, err)

						By("Sleep for 60 seconds to allow the NNO custom catalogsource to be created")
						time.Sleep(60 * time.Second)

						glog.V(networkparams.LogLevel).Infof("Wait up to 4 mins for custom NNO catalogsource " +
							"to be ready")

						Expect(createdNNOCustomCatalogSourceBuilder.IsReady(4 * time.Minute)).NotTo(BeFalse())

						CatalogSource = createdNNOCustomCatalogSourceBuilder.Definition.Name

						glog.V(networkparams.LogLevel).Infof("Custom NNO catalogsource '%s' is now ready",
							createdNNOCustomCatalogSourceBuilder.Definition.Name)

						nnoPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
							nnoPackage, nnoCatalogSourceNamespace, CustomCatalogSource)

						Expect(err).ToNot(HaveOccurred(), "error getting NNO packagemanifest '%s' "+
							"from custom catalog '%s':  %v", nnoPackage, CustomCatalogSource, err)

						By("Get the Network Operator Default Channel from Packagemanifest")
						DefaultSubscriptionChannel = nnoPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
						glog.V(networkparams.LogLevel).Infof("NNO channel '%s' retrieved from packagemanifest "+
							"of custom catalogsource '%s'", DefaultSubscriptionChannel, CustomCatalogSource)

					} else {
						Skip("nvidia-network-operator packagemanifest not found in default 'certified-operators'" +
							"catalogsource, and flag to deploy custom NNO catalogsource is false")
					}

				} else {
					glog.V(networkparams.LogLevel).Infof("NNO packagemanifest '%s' was found in the default"+
						" catalog '%s'", nnoPkgManifestBuilderByCatalog.Object.Name, nnoCatalogSourceDefault)

					CatalogSource = nnoCatalogSourceDefault

					By("Get the Network Operator Default Channel from Packagemanifest")
					DefaultSubscriptionChannel = nnoPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
					glog.V(networkparams.LogLevel).Infof("NNO channel '%s' was retrieved from NNO "+
						"packagemanifest", DefaultSubscriptionChannel)
				}

			}

			By("Check if NVIDIA Network Operator namespace exists, otherwise created it and label it")
			nsBuilder := namespace.NewBuilder(inittools.APIClient, nnoNamespace)
			if nsBuilder.Exists() {
				glog.V(networkparams.LogLevel).Infof("The namespace '%s' already exists",
					nsBuilder.Object.Name)
			} else {
				glog.V(networkparams.LogLevel).Infof("Creating the namespace:  %v", nnoNamespace)
				createdNsBuilder, err := nsBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating namespace '%s' :  %v ",
					nsBuilder.Definition.Name, err)

				glog.V(networkparams.LogLevel).Infof("Successfully created namespace '%s'",
					createdNsBuilder.Object.Name)

				glog.V(networkparams.LogLevel).Infof("Labeling the newly created namespace '%s'",
					nsBuilder.Object.Name)

				labeledNsBuilder := createdNsBuilder.WithMultipleLabels(map[string]string{
					"openshift.io/cluster-monitoring":    "true",
					"pod-security.kubernetes.io/enforce": "privileged",
				})

				newLabeledNsBuilder, err := labeledNsBuilder.Update()
				Expect(err).ToNot(HaveOccurred(), "error labeling namespace %v :  %v ",
					newLabeledNsBuilder.Definition.Name, err)

				glog.V(networkparams.LogLevel).Infof("The nvidia-network-operator labeled namespace has "+
					"labels:  %v", newLabeledNsBuilder.Object.Labels)
			}

			defer func() {
				if cleanupAfterTest {
					err := nsBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			// NNO Namespace should be created at this point
			if deployFromBundle {
				glog.V(networkparams.LogLevel).Infof("Initializing the kube API Client before deploying bundle")
				deployBundle = deploy.NewDeploy(inittools.APIClient)

				deployBundleConfig.BundleImage = networkOperatorBundleImage

				glog.V(networkparams.LogLevel).Infof("Deploy the Network Operator bundle image '%s'",
					deployBundleConfig.BundleImage)

				err := deployBundle.DeployBundle(networkparams.LogLevel, &deployBundleConfig, nnoNamespace,
					5*time.Minute)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.DeployBundle():  '%v' ", err)

				glog.V(networkparams.LogLevel).Infof("Network Operator bundle image '%s' deployed successfully "+
					"in namespace '%s", deployBundleConfig.BundleImage, nnoNamespace)

			} else {
				By("Create OperatorGroup in NVIDIA Network Operator Namespace")
				ogBuilder := olm.NewOperatorGroupBuilder(inittools.APIClient, nnoOperatorGroupName, nnoNamespace)

				if ogBuilder.Exists() {
					glog.V(networkparams.LogLevel).Infof("The ogBuilder that exists has name:  %v",
						ogBuilder.Object.Name)
				} else {
					glog.V(networkparams.LogLevel).Infof("Create a new operatorgroup with name:  %v",
						ogBuilder.Object.Name)

					ogBuilderCreated, err := ogBuilder.Create()
					Expect(err).ToNot(HaveOccurred(), "error creating operatorgroup %v :  %v ",
						ogBuilderCreated.Definition.Name, err)
				}

				defer func() {
					if cleanupAfterTest {
						err := ogBuilder.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
				}()

				By("Create Subscription in NVIDIA Network Operator Namespace")
				subBuilder := olm.NewSubscriptionBuilder(inittools.APIClient, nnoSubscriptionName,
					nnoSubscriptionNamespace, CatalogSource, nnoCatalogSourceNamespace, nnoPackage)

				if SubscriptionChannel != UndefinedValue {
					glog.V(networkparams.LogLevel).Infof("Setting the NNO subscription channel to: '%s'",
						SubscriptionChannel)
					subBuilder.WithChannel(SubscriptionChannel)
				} else {
					glog.V(networkparams.LogLevel).Infof("Setting the NNO subscription channel to "+
						"default channel: '%s'", DefaultSubscriptionChannel)
					subBuilder.WithChannel(DefaultSubscriptionChannel)
				}

				subBuilder.WithInstallPlanApproval(InstallPlanApproval)

				glog.V(networkparams.LogLevel).Infof("Creating the subscription, i.e Deploy the Network operator")
				createdSub, err := subBuilder.Create()

				Expect(err).ToNot(HaveOccurred(), "error creating subscription %v :  %v ",
					createdSub.Definition.Name, err)

				glog.V(networkparams.LogLevel).Infof("Newly created subscription: %s was successfully created",
					createdSub.Object.Name)

				if createdSub.Exists() {
					glog.V(networkparams.LogLevel).Infof("The newly created NNO subscription '%s' in "+
						"namespace '%v' has current CSV  '%v'", createdSub.Object.Name, createdSub.Object.Namespace,
						createdSub.Object.Status.CurrentCSV)
				}

				defer func() {
					if cleanupAfterTest {
						err := createdSub.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
				}()

			}

			By("Sleep for 2 minutes to allow the Network Operator deployment to be created")
			glog.V(networkparams.LogLevel).Infof("Sleep for 2 minutes to allow the Network Operator deployment" +
				" to be created")
			time.Sleep(2 * time.Minute)

			By("Wait for up to 4 minutes for Network Operator deployment to be created")
			nnoDeploymentCreated := wait.DeploymentCreated(inittools.APIClient, nnoDeployment, nnoNamespace,
				30*time.Second, 4*time.Minute)
			Expect(nnoDeploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy "+
				"Network operator")

			By("Check if the Network operator deployment is ready")
			nnoOperatorDeployment, err := deployment.Pull(inittools.APIClient, nnoDeployment, nnoNamespace)

			Expect(err).ToNot(HaveOccurred(), "Error trying to pull Network operator "+
				"deployment is: %v", err)

			glog.V(networkparams.LogLevel).Infof("Pulled Network operator deployment is:  %v ",
				nnoOperatorDeployment.Definition.Name)

			if nnoOperatorDeployment.IsReady(4 * time.Minute) {
				glog.V(networkparams.LogLevel).Infof("Pulled Network operator deployment '%s' is Ready",
					nnoOperatorDeployment.Definition.Name)
			}

			By("Get the CSV deployed in NVIDIA Network Operator namespace")
			csvBuilderList, err := olm.ListClusterServiceVersion(inittools.APIClient, nnoNamespace)

			Expect(err).ToNot(HaveOccurred(), "Error getting list of CSVs in Network operator "+
				"namespace: '%v'", err)

			// Need to handle case where there are more than one CSV in nvidia-network-operator namespace,
			// such as in RHOAI environment
			nnoCSVName := ""
			var nnoCSVBuilder *olm.ClusterServiceVersionBuilder = nil

			for _, csvBuilder := range csvBuilderList {
				if strings.HasPrefix(csvBuilder.Object.Name, "nvidia-network-operator") {
					// Found the matching CSV
					nnoCSVName = csvBuilder.Object.Name
					nnoCSVBuilder = csvBuilder
					glog.V(networkparams.LogLevel).Infof("Found nvidia-network-operator CSV '%s' in namespace '%s'",
						nnoCSVName, nnoNamespace)
					break
				}
			}

			Expect(nnoCSVName).To(Not(BeEmpty()), "nvidia-network-operator CSV not found in the list of CSVs in namespace '%s'", nnoNamespace)

			// 11-04-2025
			/*
				oc get csv -n nvidia-network-operator
				NAME                                      DISPLAY                            VERSION          REPLACES                              PHASE
				authorino-operator.v0.16.0                Authorino Operator                 0.16.0           authorino-operator.v0.15.1            Succeeded
				cert-manager.v1.16.5                      cert-manager                       1.16.5           cert-manager.v1.16.1                  Succeeded
				devworkspace-operator.v0.37.0             DevWorkspace Operator              0.37.0           devworkspace-operator.v0.36.0         Succeeded
				nvidia-network-operator.v25.10.0-beta.2   NVIDIA Network Operator            25.10.0-beta.2                                         Succeeded
				rhods-operator.2.25.0                     Red Hat OpenShift AI               2.25.0           rhods-operator.2.22.2                 Succeeded
				serverless-operator.v1.36.1               Red Hat OpenShift Serverless       1.36.1           serverless-operator.v1.36.0           Succeeded
				servicemeshoperator.v2.6.11               Red Hat OpenShift Service Mesh 2   2.6.11-0         servicemeshoperator.v2.6.10           Succeeded
				web-terminal.v1.13.0                      Web Terminal                       1.13.0           web-terminal.v1.12.1-0.1745393748.p   Succeeded

			*/

			nnoCurrentCSV := nnoCSVBuilder.Definition.Name

			glog.V(networkparams.LogLevel).Infof("Deployed ClusterServiceVersion is: '%s'", nnoCurrentCSV)

			nnoCurrentCSVVersion := nnoCSVBuilder.Definition.Spec.Version.String()
			csvVersionString := nnoCurrentCSVVersion

			glog.V(networkparams.LogLevel).Infof("ClusterServiceVersion version to be written in the operator "+
				"version file is: '%s'", csvVersionString)

			if err := inittools.GeneralConfig.WriteReport(OperatorVersionFile, []byte(csvVersionString)); err != nil {
				glog.Error("Error writing an operator version file: ", err)
			}

			By("Wait for deployed ClusterServiceVersion to be in Succeeded phase")
			glog.V(networkparams.LogLevel).Infof("Waiting for ClusterServiceVersion '%s' to be in Succeeded phase",
				nnoCurrentCSV)
			err = wait.CSVSucceeded(inittools.APIClient, nnoCurrentCSV, nnoNamespace, 60*time.Second,
				5*time.Minute)
			if err != nil {
				glog.V(networkparams.LogLevel).Infof("error waiting for ClusterServiceVersion '%s' to be "+
					"in Succeeded phase:  %v ", nnoCurrentCSV, err)
			}
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterServiceVersion to be "+
				"in Succeeded phase: ", err)

			By("Pull existing CSV in NVIDIA Network Operator Namespace")
			clusterCSV, err := olm.PullClusterServiceVersion(inittools.APIClient, nnoCurrentCSV, nnoNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling CSV from cluster:  %v", err)

			glog.V(networkparams.LogLevel).Infof("clusterCSV from cluster lastUpdatedTime is : %v ",
				clusterCSV.Definition.Status.LastUpdateTime)

			glog.V(networkparams.LogLevel).Infof("clusterCSV from cluster Phase is : \"%v\"",
				clusterCSV.Definition.Status.Phase)

			succeeded := v1alpha1.ClusterServiceVersionPhase("Succeeded")
			Expect(clusterCSV.Definition.Status.Phase).To(Equal(succeeded), "CSV Phase is not "+
				"succeeded")

			defer func() {
				if cleanupAfterTest {
					err := clusterCSV.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Get ALM examples block form CSV")
			almExamples, err := clusterCSV.GetAlmExamples()
			Expect(err).ToNot(HaveOccurred(), "Error from pulling almExamples from csv "+
				"from cluster:  %v ", err)
			glog.V(networkparams.LogLevel).Infof("almExamples block from clusterCSV  is : %v ", almExamples)

			By("Deploy NicClusterPolicy")
			glog.V(networkparams.LogLevel).Infof("Creating NicClusterPolicy from CSV almExamples")
			nicClusterPolicyBuilder := nvidianetwork.NewNicClusterPolicyBuilderFromObjectString(inittools.APIClient,
				almExamples)

			// 03-27-2025:  Need to only update if these values are provided to override versions from CSV alm-examples
			By("Check if NicClusterPolicy ofed driver version and repository need to be updated from values in env vars")
			glog.V(networkparams.LogLevel).Infof("Check if NicClusterPolicy ofed driver version and repository " +
				"need to be updated from values in env vars")
			if ofedDriverRepository != UndefinedValue {
				glog.V(networkparams.LogLevel).Infof("Updating NicClusterPolicyBuilder object driver "+
					"repository with value from env variables '%s'", ofedDriverRepository)
				nicClusterPolicyBuilder.Definition.Spec.OFEDDriver.Repository = ofedDriverRepository
			}
			if ofedDriverVersion != UndefinedValue {
				glog.V(networkparams.LogLevel).Infof("Updating NicClusterPolicyBuilder object driver "+
					"version with value from env variables '%s'", ofedDriverVersion)
				nicClusterPolicyBuilder.Definition.Spec.OFEDDriver.Version = ofedDriverVersion
			}

			By("Add extra env variables to the ofedDriver in NicClusterPolicy only for amd64 clusters")
			glog.V(networkparams.LogLevel).Infof("Add extra env variables to the ofedDriver in " +
				"NicClusterPolicy for amd64 clusters")

			glog.V(networkparams.LogLevel).Infof("Adding 4 extra env variables to the ofedDriver spec in " +
				"NicClusterPolicy")

			var updatedOfedDriverEnvVars []corev1.EnvVar

			newEnvVars := map[string]string{
				"UNLOAD_STORAGE_MODULES":            "true",
				"RESTORE_DRIVER_ON_POD_TERMINATION": "true",
				"CREATE_IFNAMES_UDEV":               "true",
				"ENTRYPOINT_DEBUG":                  "true",
			}

			for key, value := range newEnvVars {
				updatedOfedDriverEnvVars = append(updatedOfedDriverEnvVars, corev1.EnvVar{
					Name: key, Value: value})
			}

			nicClusterPolicyBuilder.Definition.Spec.OFEDDriver.Env = updatedOfedDriverEnvVars

			// } else {
			//	glog.V(networkparams.LogLevel).Infof("Cluster architecture is not 'amd64', skipping adding" +
			//		"extra env variables to the ofedDriver env spec in NicClusterPolicy ")
			//}
			By("Check if RDMA Test type is 'sriov'")
			if rdmaNetworkType == "sriov" {
				// Remove the RDMA Shared Device Plugin configuration
				if nicClusterPolicyBuilder.Definition.Spec.RdmaSharedDevicePlugin != nil {
					nicClusterPolicyBuilder.Definition.Spec.RdmaSharedDevicePlugin = nil
					glog.V(networkparams.LogLevel).Infof("Removed RdmaSharedDevicePlugin spec from " +
						"NicClusterPolicy to support RDMA Legacy SRIOV configuration")
				} else {
					glog.V(networkparams.LogLevel).Infof("RdmaSharedDevicePlugin spec in " +
						"NicClusterPolicy was not found")
				}

				// Will need an else if statement here for Host Device Network

				// default case is Shared Device
			} else {

				By("Updating default configuration for RdmaSharedDevicePlugin in NiCClusterPolicy")
				// Avoid a panic later if the RdmaSharedDevicePlugin is nil
				Expect(nicClusterPolicyBuilder.Definition.Spec.RdmaSharedDevicePlugin).ToNot(BeNil(),
					"NicClusterPolicy RdmaSharedDevicePlugin is nil")

				glog.V(networkparams.LogLevel).Infof("Building the new config data structure for NicClusterPolicy " +
					"rdmaSharedDevicePlugin to use Eth and IB interface names passed in env vars")

				// Need to update the rdmaSharedDevicePlugin config element to look like this:
				/*
					rdmaSharedDevicePlugin:
					      config: |
					        {
					          "configList": [
					            {
					              "resourceName": "rdma_shared_device_ib",
					              "rdmaHcaMax": 63,
					              "selectors": {
					                "ifNames": ["ibs2f0"]
					              }
					            },
					            {
					              "resourceName": "rdma_shared_device_eth",
					              "rdmaHcaMax": 63,
					              "selectors": {
					                "ifNames": ["ens8f0np0"]
					              }
					            }
					          ]
					        }



				*/

				// Define the JSON structure in Go structs
				type Selector struct {
					IfNames []string `json:"ifNames"`
				}

				type ConfigItem struct {
					ResourceName string   `json:"resourceName"`
					RdmaHcaMax   int      `json:"rdmaHcaMax"`
					Selectors    Selector `json:"selectors"`
				}

				type Config struct {
					ConfigList []ConfigItem `json:"configList"`
				}

				config := Config{
					ConfigList: []ConfigItem{
						{
							ResourceName: "rdma_shared_device_ib",
							RdmaHcaMax:   63,
							Selectors:    Selector{IfNames: []string{mellanoxInfinibandInterfaceName}},
						},
						{
							ResourceName: "rdma_shared_device_eth",
							RdmaHcaMax:   63,
							Selectors:    Selector{IfNames: []string{mellanoxEthernetInterfaceName}},
						},
					},
				}

				// Convert to JSON
				jsonData, err := json.MarshalIndent(config, "", "  ")
				if err != nil {
					fmt.Println("Error marshalling JSON:", err)
					return
				}

				// Assign the generated JSON string
				newRDMASharedDevicePluginConfig := string(jsonData)

				glog.V(networkparams.LogLevel).Infof("New config data structure for NicClusterPolicy "+
					"rdmaSharedDevicePlugin for Ethernet '%s' and IB '%s' interfaces from env vars:",
					mellanoxEthernetInterfaceName, mellanoxInfinibandInterfaceName)
				fmt.Println(newRDMASharedDevicePluginConfig)

				nicClusterPolicyBuilder.Definition.Spec.RdmaSharedDevicePlugin.Config = newRDMASharedDevicePluginConfig
			}

			By("Deploy NicClusterPolicy")
			createdNicClusterPolicyBuilder, err := nicClusterPolicyBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "Error Creating NicClusterPolicy from csv "+
				"almExamples  %v ", err)
			glog.V(networkparams.LogLevel).Infof("NicClusterPolicy '%s' is successfully created",
				createdNicClusterPolicyBuilder.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					_, err := createdNicClusterPolicyBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Pull the NicClusterPolicy just created from cluster, with updated fields")
			pulledNicClusterPolicy, err := nvidianetwork.PullNicClusterPolicy(inittools.APIClient,
				nnoNicClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling NicClusterPolicy %s from cluster: "+
				" %v ", nnoNicClusterPolicyName, err)

			cpJSON, err := json.MarshalIndent(pulledNicClusterPolicy, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The NicClusterPolicy just created has name:  %v",
					pulledNicClusterPolicy.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The NicClusterPolicy just created marshalled "+
					"in json: %v", string(cpJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling NicClusterPolicy into json:  %v",
					err)
			}

			By("Wait up to 24 minutes for NicClusterPolicy to be ready")
			glog.V(networkparams.LogLevel).Infof("Waiting for NicClusterPolicy to be ready")
			err = wait.NicClusterPolicyReady(inittools.APIClient, nnoNicClusterPolicyName, 60*time.Second,
				24*time.Minute)

			glog.V(networkparams.LogLevel).Infof("error waiting for NicClusterPolicy to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for NicClusterPolicy to be Ready: "+
				" %v ", err)

			By("Pull the ready NicClusterPolicy from cluster, with updated fields")
			pulledReadyNicClusterPolicy, err := nvidianetwork.PullNicClusterPolicy(inittools.APIClient,
				nnoNicClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling NicClusterPolicy %s from cluster: "+
				" %v ", nnoNicClusterPolicyName, err)

			ncpReadyJSON, err := json.MarshalIndent(pulledReadyNicClusterPolicy, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The ready NicClusterPolicy just has name:  %v",
					pulledReadyNicClusterPolicy.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The ready NicClusterPolicy just marshalled "+
					"in json: %v", string(ncpReadyJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling the ready NicClusterPolicy into "+
					"json:  %v", err)
			}

			By("Deploy MacvlanNetwork")
			glog.V(networkparams.LogLevel).Infof("Creating MacvlanNetwork from CSV almExamples")
			macvlanNetworkBuilder := nvidianetwork.NewMacvlanNetworkBuilderFromObjectString(inittools.APIClient,
				almExamples)

			By("Updating MacvlanNetworkBuilder object name from value in env vars")
			glog.V(networkparams.LogLevel).Infof("Updating MacvlanNetworkBuilder object value passed in "+
				"env variable '%s'", macvlanNetworkName)
			macvlanNetworkBuilder.Definition.Name = macvlanNetworkName

			By("Updating MacvlanNetworkBuilder object ipam and master from values in env vars")
			glog.V(networkparams.LogLevel).Infof("Updating MacvlanNetworkBuilder object ipam and " +
				"master with values passed in env variables")

			ipamConfig := fmt.Sprintf(
				`{"type": "whereabouts", "range": "%s", "gateway": "%s"}`,
				macvlanNetworkIPAMRange, macvlanNetworkIPAMGateway,
			)

			fmt.Println(ipamConfig)
			macvlanNetworkBuilder.Definition.Spec.IPAM = ipamConfig

			macvlanNetworkBuilder.Definition.Spec.Master = mellanoxEthernetInterfaceName

			By("Deploy MacvlanNetwork")
			createdMacvlanNetworkBuilder, err := macvlanNetworkBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "Error Creating MacvlanNetwork from csv "+
				"almExamples  %v ", err)
			glog.V(networkparams.LogLevel).Infof("MacvlanNetwork '%s' is successfully created",
				createdMacvlanNetworkBuilder.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					_, err := createdMacvlanNetworkBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Pull the MacvlanNetwork just created from cluster, with updated fields")
			pulledMacvlanNetwork, err := nvidianetwork.PullMacvlanNetwork(inittools.APIClient, macvlanNetworkName)
			Expect(err).ToNot(HaveOccurred(), "error pulling MacvlanNetwork %s from cluster: "+
				" %v ", macvlanNetworkName, err)

			mvnJSON, err := json.MarshalIndent(pulledMacvlanNetwork, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The MacvlanNetwork just created has name:  %v",
					pulledMacvlanNetwork.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The MacvlanNetwork just created marshalled "+
					"in json: %v", string(mvnJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling MacvlanNetwork into json:  %v",
					err)
			}

			By("Wait up to 5 minutes for MacvlanNetwork to be ready")
			glog.V(networkparams.LogLevel).Infof("Waiting for MacvlanNetwork to be ready")
			err = wait.MacvlanNetworkReady(inittools.APIClient, macvlanNetworkName, 60*time.Second,
				5*time.Minute)

			glog.V(networkparams.LogLevel).Infof("error waiting for MacvlanNetwork to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for MacvlanNetwork to be Ready: "+
				" %v ", err)

			By("Pull the ready MacvlanNetwork from cluster, with updated fields")
			pulledReadyMacvlanNetwork, err := nvidianetwork.PullMacvlanNetwork(inittools.APIClient, macvlanNetworkName)
			Expect(err).ToNot(HaveOccurred(), "error pulling MacvlanNetwork %s from cluster: "+
				" %v ", macvlanNetworkName, err)

			mvnReadyJSON, err := json.MarshalIndent(pulledReadyMacvlanNetwork, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The ready MacvlanNetwork just has name:  %v",
					pulledReadyMacvlanNetwork.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The ready MacvlanNetwork just marshalled "+
					"in json: %v", string(mvnReadyJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling the ready MacvlanNetwork into "+
					"json:  %v", err)
			}

			By("Deploy IPoIBNetwork")
			glog.V(networkparams.LogLevel).Infof("Creating IPoIBNetwork from CSV almExamples")
			ipoibNetworkBuilder := nvidianetwork.NewIPoIBNetworkBuilderFromObjectString(inittools.APIClient,
				almExamples)

			By("Updating IPoIBNetworkBuilder object name from value in env vars")
			glog.V(networkparams.LogLevel).Infof("Updating IPoIBNetworkBuilder object value passed in "+
				"env variable '%s'", ipoibNetworkName)
			ipoibNetworkBuilder.Definition.Name = ipoibNetworkName

			By("Updating IPoIBNetworkBuilder object ipam and master from values in env vars")
			glog.V(networkparams.LogLevel).Infof("Updating IPoIBNetworkBuilder object ipam and " +
				"master with values passed in env variables")
			/*
			   ipam: |
			     {
			       "type": "whereabouts",
			       "range": "192.168.6.225/28",
			       "exclude": [
			        "192.168.6.229/30",
			        "192.168.6.236/32"
			       ]
			     }
			   master: ibs1f1
			*/

			ipoibIpamConfig := fmt.Sprintf(
				`{"type": "whereabouts", "range": "%s", "exclude": ["%s", "%s"]}`,
				ipoibNetworkIPAMRange, ipoibNetworkIPAMExcludeIP1, ipoibNetworkIPAMExcludeIP2,
			)

			fmt.Println(ipoibIpamConfig)
			ipoibNetworkBuilder.Definition.Spec.IPAM = ipoibIpamConfig

			ipoibNetworkBuilder.Definition.Spec.Master = mellanoxInfinibandInterfaceName

			By("Deploy IPoIBNetwork")
			createdIPoIBNetworkBuilder, err := ipoibNetworkBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "Error Creating IPoIBNetwork from csv "+
				"almExamples  %v ", err)
			glog.V(networkparams.LogLevel).Infof("IPoIBNetwork '%s' is successfully created",
				createdIPoIBNetworkBuilder.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					_, err := createdIPoIBNetworkBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Pull the IPoIBNetwork just created from cluster, with updated fields")
			pulledIPoIBNetwork, err := nvidianetwork.PullIPoIBNetwork(inittools.APIClient, ipoibNetworkName)
			Expect(err).ToNot(HaveOccurred(), "error pulling IPoIBNetwork %s from cluster: "+
				" %v ", ipoibNetworkName, err)

			ipoibJSON, err := json.MarshalIndent(pulledIPoIBNetwork, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The IPoIBNetwork just created has name:  %v",
					pulledIPoIBNetwork.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The IPoIBNetwork just created marshalled "+
					"in json: %v", string(ipoibJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling IPoIBNetwork into json:  %v",
					err)
			}

			By("Wait up to 5 minutes for IPoIBNetwork to be ready")
			glog.V(networkparams.LogLevel).Infof("Waiting for IPoIBNetwork to be ready")
			err = wait.IPoIBNetworkReady(inittools.APIClient, ipoibNetworkName, 60*time.Second,
				5*time.Minute)

			glog.V(networkparams.LogLevel).Infof("error waiting for IPoIBNetwork to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for IPoIBNetwork to be Ready: "+
				" %v ", err)

			By("Pull the ready IPoIBNetwork from cluster, with updated fields")
			pulledReadyIPoIBNetwork, err := nvidianetwork.PullIPoIBNetwork(inittools.APIClient, ipoibNetworkName)
			Expect(err).ToNot(HaveOccurred(), "error pulling IPoIBNetwork %s from cluster: "+
				" %v ", ipoibNetworkName, err)

			ipoibReadyJSON, err := json.MarshalIndent(pulledReadyIPoIBNetwork, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The ready IPoIBNetwork just has name:  %v",
					pulledReadyIPoIBNetwork.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The ready IPoIBNetwork just marshalled "+
					"in json: %v", string(ipoibReadyJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling the ready IPoIBNetwork into "+
					"json:  %v", err)
			}

		})

		It("Run RDMA connectivity test with ib_write_bw", Label("rdma-shared-dev"), func() {

			var (
				rdmaServerPodNamePrefix = "rdma-shared-dev-server-ci"
				rdmaClientPodNamePrefix = "rdma-shared-dev-client-ci"
			)

			rdmaServerPodName := rdmaServerPodNamePrefix + "-" + rdmaLinkType
			rdmaClientPodName := rdmaClientPodNamePrefix + "-" + rdmaLinkType

			By("Starting RDMA connectivity test with ib_write_bw testcase")
			glog.V(networkparams.LogLevel).Infof("\nStarting RDMA connectivity test with ib_write_bw testcase")

			By("Create ib_write_bw server workload pod")
			glog.V(networkparams.LogLevel).Infof("Create ib_write_bw server workload pod '%s'", rdmaServerPodName)

			// need to set the network name ipoibNetworkName or  macvlanNetworkName
			workloadPodNetworkName := UndefinedValue

			switch rdmaLinkType {
			case "infiniband":
				glog.V(networkparams.LogLevel).Infof("rdmaLinkType is set to infiniband, setting workload "+
					"pod network name to ipoibnetwork cr name: '%s'", ipoibNetworkName)
				workloadPodNetworkName = ipoibNetworkName
			case "ethernet":
				glog.V(networkparams.LogLevel).Infof("rdmaLinkType is set to 'ethernet', setting workload "+
					"pod network name to macvlanNetworkName cr name: '%s'", macvlanNetworkName)
				workloadPodNetworkName = macvlanNetworkName
			}

			rdmaServerPod := rdmatest.CreateRdmaWorkloadPod(rdmaServerPodName,
				rdmaWorkloadNamespace, withCuda, "server", rdmaServerHostname, rdmaMlxDevice,
				workloadPodNetworkName, rdmaTestImage, rdmaLinkType, "none", rdmaNetworkType)

			createdRdmaServerPod, err := inittools.APIClient.Pods(rdmaServerPod.Namespace).Create(context.TODO(),
				rdmaServerPod, metav1.CreateOptions{})

			// DEBUG:
			glog.V(networkparams.LogLevel).Infof("####### Server side - Debug:  err '%v'", err)

			Expect(err).ToNot(HaveOccurred(), "error creating RDMA Server '%s' in cluster: %v",
				rdmaServerPodName, err)

			glog.V(networkparams.LogLevel).Infof("Successfully created RDMA ib_write_bw server workload pod '%s'",
				createdRdmaServerPod.Name)

			By("Wait 4 minutes for RDMA server pod to be running")
			glog.V(networkparams.LogLevel).Infof("Waiting for 4 minutes for the RDMA server to be running")
			time.Sleep(4 * time.Minute)

			By("Get the interface net1 IP address in the ib_write_bw server workload pod")
			glog.V(networkparams.LogLevel).Infof("Get the interface net1 interface Ip address in the "+
				"ib_write_bw server workload pod '%s'", rdmaServerPodName)

			net1IntIpAddrServer, err := rdmatest.GetMyServerIP(inittools.APIClient, rdmaServerPodName,
				rdmaWorkloadNamespace, "net1")

			Expect(err).ToNot(HaveOccurred(), "error getting RDMA Server '%s' net1 interface ip "+
				"address: %v", rdmaServerPodName, err)

			glog.V(networkparams.LogLevel).Infof("RDMA Server interface net1 IP address captured: '%s'",
				net1IntIpAddrServer)
			Expect(net1IntIpAddrServer).ToNot(BeNil(), fmt.Sprintf("error RDMA Server '%s' net1 interface "+
				"IP address: '%s' is null", rdmaServerPodName, net1IntIpAddrServer))

			By("Create ib_write_bw client workload pod")
			glog.V(networkparams.LogLevel).Infof("Create ib_write_bw Client workload pod '%s' and "+
				"passing server ip address '%s'", rdmaClientPodName, net1IntIpAddrServer)

			rdmaClientPod := rdmatest.CreateRdmaWorkloadPod(rdmaClientPodName, rdmaWorkloadNamespace, withCuda,
				"client", rdmaClientHostname, rdmaMlxDevice, workloadPodNetworkName, rdmaTestImage,
				rdmaLinkType, net1IntIpAddrServer, rdmaNetworkType)

			createdRdmaClientPod, err := inittools.APIClient.Pods(rdmaClientPod.Namespace).Create(context.TODO(),
				rdmaClientPod, metav1.CreateOptions{})

			// DEBUG:
			glog.V(networkparams.LogLevel).Infof("####### Client side - Debug:  err '%v'", err)

			Expect(err).ToNot(HaveOccurred(), "error creating RDMA Client '%s' in cluster: %v",
				rdmaClientPodName, err)

			glog.V(networkparams.LogLevel).Infof("RDMA Client workload pod '%s' was successfully created in "+
				"namespace '%s' and passed server IP Address '%s'", createdRdmaClientPod.Name,
				createdRdmaClientPod.Namespace, net1IntIpAddrServer)

			// Later remove sleep time and detect when RDMA test has completed
			By("Wait 7 minutes for RDMA ib_write_bw tests to complete")
			glog.V(networkparams.LogLevel).Infof("Waiting for 7 minutes for the RDMA ib_write_bw tests to " +
				"complete")
			time.Sleep(7 * time.Minute)

			By("Collect logs from RDMA ib_write_bw tests from server workload pod")
			glog.V(networkparams.LogLevel).Infof("Collect logs from RDMA ib_write_bw tests from server " +
				"workload pod")

			serverLogs, err := rdmatest.GetPodLogs(inittools.APIClient, rdmaWorkloadNamespace, rdmaServerPodName)

			Expect(err).ToNot(HaveOccurred(), "error collecting RDMA server '%s' pod logs: %v",
				rdmaServerPodName, err)

			glog.V(networkparams.LogLevel).Infof("RDMA server logs collected: \n'%s'", serverLogs)

			By("Parse logs from RDMA ib_write_bw tests from server workload pod")
			parseLogsMap, err := rdmatest.ParseRdmaOutput(serverLogs)
			Expect(err).ToNot(HaveOccurred(), "error parsing RDMA server '%s' pod logs: %v",
				rdmaServerPodName, err)

			// Pretty print JSON output
			jsonParseLogsMap, err := json.MarshalIndent(parseLogsMap, "", "  ")
			Expect(err).ToNot(HaveOccurred(), "error formatting parsed RDMA server pod logs: %v", err)

			glog.V(networkparams.LogLevel).Infof("Parsed and formatted RDMA server logs: \n'%s'",
				string(jsonParseLogsMap))

			By("Validate logs from RDMA ib_write_bw tests from server workload pod")
			rdmaTestPassFail, err := rdmatest.ValidateRDMAResults(parseLogsMap)

			Expect(rdmaTestPassFail).ToNot(BeFalse(), "RDMA test workload execution was FAILED, "+
				"errors encountered: %v", err)
			glog.V(networkparams.LogLevel).Infof("RDMA test validation has PASSED.  Successful test !")
		})

		// RDMA Legacy SRIOV testcase
		It("Run RDMA connectivity test with ib_write_bw", Label("rdma-legacy-sriov"), func() {

			By("Starting RDMA Legacy SRIOV connectivity test with ib_write_bw testcase")

			var (
				rdmaServerPodNamePrefix = "rdma-legacy-sriov-server-ci"
				rdmaClientPodNamePrefix = "rdma-legacy-sriov-client-ci"
			)

			rdmaServerPodName := rdmaServerPodNamePrefix + "-" + rdmaLinkType

			rdmaClientPodName := rdmaClientPodNamePrefix + "-" + rdmaLinkType

			By("Starting RDMA connectivity test with ib_write_bw testcase")
			glog.V(networkparams.LogLevel).Infof("\nStarting RDMA Legacy SRIOV connectivity test " +
				"with ib_write_bw testcase")

			By("Create ib_write_bw server workload pod")
			glog.V(networkparams.LogLevel).Infof("Create ib_write_bw server workload pod '%s'",
				rdmaServerPodName)

			// For SRIOV we need to find the RDMA device ids for both server and client
			// One way to do that is run `rdma link show` inside oc debug pod, but the workload
			// rdma-tools container will dynamically find it at runtime

			var rdmaCmd = []string{
				"sh",
				"-c",
				"rdma link show"}

			// For arm64 architecture image: quay.io/wabouham/ecosys-nvidia/ubi9-tools-arm64:0.0.1

			rdmaLinkShowCmdLogClientHostOutput, err := rdmatest.RunCommandsOnSpecificNode(inittools.APIClient,
				"oc-debug-busy-pod-client", rdmaWorkloadNamespace, clusterArchitecture, rdmaClientHostname,
				rdmaCmd)

			if err != nil {
				glog.V(networkparams.LogLevel).Infof("Failed to run RDMA cmd '%s' in debug node pod : %s",
					rdmaCmd, err.Error())
			}

			glog.V(networkparams.LogLevel).Infof("Log returned from RunCommandsOnSpecificNode(): \n'%s'",
				rdmaLinkShowCmdLogClientHostOutput)

			rdmaLinkShowCmdLogServerHostOutput, err := rdmatest.RunCommandsOnSpecificNode(inittools.APIClient,
				"oc-debug-busy-pod-server", rdmaWorkloadNamespace, clusterArchitecture, rdmaServerHostname,
				rdmaCmd)

			if err != nil {
				glog.V(networkparams.LogLevel).Infof("Failed to run RDMA cmd '%s' in debug node pod : %s",
					rdmaCmd, err.Error())
			}

			glog.V(networkparams.LogLevel).Infof("Log returned from RunCommandsOnSpecificNode: \n'%s'",
				rdmaLinkShowCmdLogServerHostOutput)

			// NO Need to parse the mlx5_x device id from the logs

			rdmaServerPod := rdmatest.CreateRdmaWorkloadPod(rdmaServerPodName, rdmaWorkloadNamespace, withCuda,
				"server", rdmaServerHostname, "sriov", sriovNetworkName, rdmaTestImage, rdmaLinkType,
				"none", rdmaNetworkType)

			createdRdmaServerPod, err := inittools.APIClient.Pods(rdmaServerPod.Namespace).Create(context.TODO(),
				rdmaServerPod, metav1.CreateOptions{})

			// DEBUG:
			glog.V(networkparams.LogLevel).Infof("####### Server side - Debug:  err '%v'", err)

			Expect(err).ToNot(HaveOccurred(), "error creating RDMA Server '%s' in cluster: %v",
				rdmaServerPodName, err)

			glog.V(networkparams.LogLevel).Infof("Successfully created RDMA ib_write_bw server workload pod '%s'",
				createdRdmaServerPod.Name)

			By("Wait 4 minutes for RDMA server pod to be running")
			glog.V(networkparams.LogLevel).Infof("Waiting for 4 minutes for the RDMA server to be running")
			time.Sleep(4 * time.Minute)

			By("Get the interface net1 IP address in the ib_write_bw server workload pod")
			glog.V(networkparams.LogLevel).Infof("Get the interface net1 interface Ip address in the "+
				"ib_write_bw server workload pod '%s'", rdmaServerPodName)

			net1IntIpAddrServer, err := rdmatest.GetMyServerIP(inittools.APIClient, rdmaServerPodName,
				rdmaWorkloadNamespace, "net1")

			Expect(err).ToNot(HaveOccurred(), "error getting RDMA Server '%s' net1 interface ip "+
				"address: %v", rdmaServerPodName, err)

			glog.V(networkparams.LogLevel).Infof("RDMA Server interface net1 IP address captured: '%s'",
				net1IntIpAddrServer)
			Expect(net1IntIpAddrServer).ToNot(BeNil(), fmt.Sprintf("error RDMA Server '%s' net1 interface "+
				"IP address: '%s' is null", rdmaServerPodName, net1IntIpAddrServer))

			By("Create ib_write_bw client workload pod")
			glog.V(networkparams.LogLevel).Infof("Create ib_write_bw Client workload pod '%s' and "+
				"passing server ip address '%s'", rdmaClientPodName, net1IntIpAddrServer)

			// Debug:
			glog.V(networkparams.LogLevel).Infof("withCuda switch for RDMA workload is set to '%s'",
				withCuda)

			rdmaClientPod := rdmatest.CreateRdmaWorkloadPod(rdmaClientPodName, rdmaWorkloadNamespace, withCuda,
				"client", rdmaClientHostname, "sriov", sriovNetworkName, rdmaTestImage,
				rdmaLinkType, net1IntIpAddrServer, rdmaNetworkType)

			createdRdmaClientPod, err := inittools.APIClient.Pods(rdmaClientPod.Namespace).Create(context.TODO(),
				rdmaClientPod, metav1.CreateOptions{})

			// DEBUG:
			glog.V(networkparams.LogLevel).Infof("####### Client side - Debug:  err '%v'", err)

			Expect(err).ToNot(HaveOccurred(), "error creating RDMA Client '%s' in cluster: %v",
				rdmaClientPodName, err)

			glog.V(networkparams.LogLevel).Infof("RDMA Client workload pod '%s' was successfully created in "+
				"namespace '%s' and passed server IP Address '%s'", createdRdmaClientPod.Name,
				createdRdmaClientPod.Namespace, net1IntIpAddrServer)

			// Later remove sleep time and detect when RDMA test has completed
			By("Wait 7 minutes for RDMA ib_write_bw tests to complete")
			glog.V(networkparams.LogLevel).Infof("Waiting for 7 minutes for the RDMA ib_write_bw tests to " +
				"complete")
			time.Sleep(7 * time.Minute)

			By("Collect logs from RDMA ib_write_bw tests from server workload pod")
			glog.V(networkparams.LogLevel).Infof("Collect logs from RDMA ib_write_bw tests from server " +
				"workload pod")

			serverLogs, err := rdmatest.GetPodLogs(inittools.APIClient, rdmaWorkloadNamespace, rdmaServerPodName)

			Expect(err).ToNot(HaveOccurred(), "error collecting RDMA server '%s' pod logs: %v",
				rdmaServerPodName, err)

			glog.V(networkparams.LogLevel).Infof("RDMA server logs collected: \n'%s'", serverLogs)

			By("Parse logs from RDMA ib_write_bw tests from server workload pod")
			parseLogsMap, err := rdmatest.ParseRdmaOutput(serverLogs)
			Expect(err).ToNot(HaveOccurred(), "error parsing RDMA server '%s' pod logs: %v",
				rdmaServerPodName, err)

			// Pretty print JSON output
			jsonParseLogsMap, err := json.MarshalIndent(parseLogsMap, "", "  ")
			Expect(err).ToNot(HaveOccurred(), "error formatting parsed RDMA server pod logs: %v", err)

			glog.V(networkparams.LogLevel).Infof("Parsed and formatted RDMA server logs: \n'%s'",
				string(jsonParseLogsMap))

			By("Validate logs from RDMA ib_write_bw tests from server workload pod")
			rdmaTestPassFail, err := rdmatest.ValidateRDMAResults(parseLogsMap)

			Expect(rdmaTestPassFail).ToNot(BeFalse(), "RDMA test workload execution was FAILED, "+
				"errors encountered: %v", err)
			glog.V(networkparams.LogLevel).Infof("RDMA test validation has PASSED.  Successful test !")

			////// debug end

		})

	})
})
