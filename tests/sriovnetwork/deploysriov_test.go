package sriovnetwork

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	internalNFD "github.com/rh-ecosystem-edge/nvidia-ci/internal/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/sriovnetworkconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/sriovparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sriovNamespace              = "openshift-sriov-network-operator"
	sriovOperatorGroupName      = "sriov-network-operators"
	sriovSubscriptionName       = "sriov-network-operator-subscription"
	sriovDeploymentName         = "sriov-network-operator"
	sriovPackage                = "sriov-network-operator"
	sriovOperatorConfigName     = "default"
	sriovCatalogSourceDefault   = "redhat-operators"
	sriovOperatorConfigLogLevel = 2
)

var (
	nfdInstance = operatorconfig.NewCustomConfig()

	sriovNetworkConfig *sriovnetworkconfig.SriovNetworkConfig
	nfdConfig          *internalNFD.NFDConfig

	catalogSource       = UndefinedValue
	subscriptionChannel = UndefinedValue
	cleanupAfterTest    = true

	sriovNicPresentLabel = UndefinedValue
	sriovNicCapableLabel = UndefinedValue
	sriovNodeSelector    map[string]string
	sriovPfNames         []string
)

var _ = Describe("SRIOV", Ordered, Label(tsparams.SriovLabelSuite), func() {
	sriovNetworkConfig = sriovnetworkconfig.NewSriovNetworkConfig()
	nfdConfig, _ = internalNFD.NewNFDConfig()

	Context("DeploySRIOV", Label("deploy-sriov-network-operator"), func() {

		BeforeAll(func() {
			if sriovNetworkConfig.CatalogSource == "" {
				glog.V(sriovparams.LogLevel).Infof("env variable SRIOVNETWORK_CATALOGSOURCE"+
					" is not set, using default catalogsource '%s'", sriovCatalogSourceDefault)
				catalogSource = sriovCatalogSourceDefault
			} else {
				catalogSource = sriovNetworkConfig.CatalogSource
			}

			if sriovNetworkConfig.SubscriptionChannel == "" {
				glog.V(sriovparams.LogLevel).Infof("env variable SRIOVNETWORK_SUBSCRIPTION_CHANNEL" +
					" is not set, will deploy the package's default channel")
				subscriptionChannel = UndefinedValue
			} else {
				subscriptionChannel = sriovNetworkConfig.SubscriptionChannel
			}

			cleanupAfterTest = sriovNetworkConfig.CleanupAfterTest

			sriovNicPresentLabel = fmt.Sprintf("feature.node.kubernetes.io/pci-%s.present", sriovNetworkConfig.NicVendor)
			sriovNicCapableLabel = fmt.Sprintf("feature.node.kubernetes.io/pci-%s.sriov.capable", sriovNetworkConfig.NicVendor)
			sriovNodeSelector = map[string]string{sriovNicPresentLabel: "true"}
			sriovPfNames = strings.Split(sriovNetworkConfig.PfNames, ",")

			if nfdConfig.FallbackCatalogSourceIndexImage != "" {
				nfdInstance.CustomCatalogSourceIndexImage = nfdConfig.FallbackCatalogSourceIndexImage
				nfdInstance.CreateCustomCatalogsource = true
				nfdInstance.CustomCatalogSource = nfd.CatalogSourceDefault + "-custom"
			}

			By("Report OpenShift version")
			ocpVersion, err := inittools.GetOpenShiftVersion()
			Expect(err).ToNot(HaveOccurred(), "error getting OpenShift version: %v", err)

			nfd.EnsureNFDIsInstalled(inittools.APIClient, nfdInstance, ocpVersion, sriovparams.LogLevel)
		})

		AfterAll(func() {
			if nfdInstance.CleanupAfterInstall && cleanupAfterTest {
				err := nfd.Cleanup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "Error cleaning up NFD resources: %v", err)
			}
		})

		It("Deploy SRIOV Network Operator", Label("deploy"), func() {
			By("Check if at least one worker node has the Mellanox NIC present label")
			nicNodeFound, _ := check.NodeWithLabel(inittools.APIClient, sriovNicPresentLabel,
				inittools.GeneralConfig.WorkerLabelMap)

			if !nicNodeFound {
				Skip("No worker nodes with label " + sriovNicPresentLabel + " found in this cluster")
			}

			By("Check if 'sriov-network-operator' packagemanifest exists in the configured catalog")
			pkgManifestBuilder, err := olm.PullPackageManifestByCatalog(inittools.APIClient, sriovPackage,
				nfd.CatalogSourceNamespace, catalogSource)
			Expect(err).ToNot(HaveOccurred(), "error getting SRIOV Network Operator packagemanifest "+
				"'%s' from catalog '%s': %v", sriovPackage, catalogSource, err)

			defaultSubscriptionChannel := pkgManifestBuilder.Object.Status.DefaultChannel
			glog.V(sriovparams.LogLevel).Infof("SRIOV Network Operator default channel from packagemanifest "+
				"is '%s'", defaultSubscriptionChannel)

			By("Create the SRIOV Network Operator namespace")
			nsBuilder := namespace.NewBuilder(inittools.APIClient, sriovNamespace)
			if !nsBuilder.Exists() {
				createdNsBuilder, err := nsBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating namespace '%s': %v",
					nsBuilder.Definition.Name, err)
				glog.V(sriovparams.LogLevel).Infof("Successfully created namespace '%s'",
					createdNsBuilder.Object.Name)
			}

			defer func() {
				if cleanupAfterTest {
					err := nsBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Create OperatorGroup in the SRIOV Network Operator namespace")
			ogBuilder := olm.NewOperatorGroupBuilder(inittools.APIClient, sriovOperatorGroupName, sriovNamespace)
			if !ogBuilder.Exists() {
				createdOgBuilder, err := ogBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating operatorgroup '%s': %v",
					sriovOperatorGroupName, err)
				glog.V(sriovparams.LogLevel).Infof("Successfully created operatorgroup '%s'",
					createdOgBuilder.Definition.Name)
			}

			defer func() {
				if cleanupAfterTest {
					err := ogBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Create Subscription in the SRIOV Network Operator namespace")
			subBuilder := olm.NewSubscriptionBuilder(inittools.APIClient, sriovSubscriptionName, sriovNamespace,
				catalogSource, nfd.CatalogSourceNamespace, sriovPackage)

			if subscriptionChannel != UndefinedValue {
				subBuilder.WithChannel(subscriptionChannel)
			} else {
				subBuilder.WithChannel(defaultSubscriptionChannel)
			}
			subBuilder.WithInstallPlanApproval(v1alpha1.Approval("Automatic"))

			createdSub, err := subBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating subscription '%s': %v",
				sriovSubscriptionName, err)
			glog.V(sriovparams.LogLevel).Infof("Successfully created subscription '%s'",
				createdSub.Object.Name)

			defer func() {
				if cleanupAfterTest {
					err := createdSub.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Wait for up to 4 minutes for SRIOV Network Operator deployment to be created")
			deploymentCreated := wait.DeploymentCreated(inittools.APIClient, sriovDeploymentName, sriovNamespace,
				30*time.Second, 4*time.Minute)
			Expect(deploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy SRIOV Network Operator")

			By("Check if the SRIOV Network Operator deployment is ready")
			sriovOperatorDeployment, err := deployment.Pull(inittools.APIClient, sriovDeploymentName, sriovNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling SRIOV Network Operator deployment: %v", err)
			Expect(sriovOperatorDeployment.IsReady(4*time.Minute)).To(BeTrue(),
				"SRIOV Network Operator deployment '%s' did not become ready", sriovOperatorDeployment.Definition.Name)

			By("Get the CSV deployed in the SRIOV Network Operator namespace")
			csvBuilderList, err := olm.ListClusterServiceVersion(inittools.APIClient, sriovNamespace)
			Expect(err).ToNot(HaveOccurred(), "error listing CSVs in SRIOV Network Operator namespace: %v", err)

			sriovCSVName := ""
			for _, csvBuilder := range csvBuilderList {
				if strings.HasPrefix(csvBuilder.Object.Name, sriovPackage) {
					sriovCSVName = csvBuilder.Object.Name

					break
				}
			}
			Expect(sriovCSVName).ToNot(BeEmpty(), "sriov-network-operator CSV not found in namespace '%s'",
				sriovNamespace)

			By("Wait for the deployed ClusterServiceVersion to be in Succeeded phase")
			err = wait.CSVSucceeded(inittools.APIClient, sriovCSVName, sriovNamespace, 60*time.Second, 5*time.Minute)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterServiceVersion '%s' to succeed: %v",
				sriovCSVName, err)

			By("Create SriovOperatorConfig")
			operatorConfigBuilder := sriovnetwork.NewSriovOperatorConfigBuilder(inittools.APIClient,
				sriovOperatorConfigName, sriovNamespace)
			operatorConfigBuilder.Definition.Spec.EnableInjector = true
			operatorConfigBuilder.Definition.Spec.EnableOperatorWebhook = true
			operatorConfigBuilder.Definition.Spec.LogLevel = sriovOperatorConfigLogLevel
			operatorConfigBuilder.Definition.Spec.ConfigDaemonNodeSelector = map[string]string{
				"network.nvidia.com/operator.mofed.wait": "false",
				"node-role.kubernetes.io/worker":         "",
				sriovNicCapableLabel:                     "true",
			}

			createdOperatorConfig, err := operatorConfigBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating SriovOperatorConfig '%s': %v",
				sriovOperatorConfigName, err)
			glog.V(sriovparams.LogLevel).Infof("Successfully created SriovOperatorConfig '%s'",
				createdOperatorConfig.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					_, err := createdOperatorConfig.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Create SriovNetworkNodePolicy")
			nodePolicyBuilder := sriovnetwork.NewSriovNetworkNodePolicyBuilder(inittools.APIClient,
				sriovNetworkConfig.NodePolicyName, sriovNamespace)
			nodePolicyBuilder.Definition.Spec.DeviceType = "netdevice"
			nodePolicyBuilder.Definition.Spec.Mtu = sriovNetworkConfig.Mtu
			nodePolicyBuilder.Definition.Spec.NicSelector.Vendor = sriovNetworkConfig.NicVendor
			nodePolicyBuilder.Definition.Spec.NicSelector.PfNames = sriovPfNames
			nodePolicyBuilder.Definition.Spec.NodeSelector = sriovNodeSelector
			nodePolicyBuilder.Definition.Spec.NumVfs = sriovNetworkConfig.NumVfs
			nodePolicyBuilder.Definition.Spec.Priority = sriovNetworkConfig.Priority
			nodePolicyBuilder.Definition.Spec.IsRdma = true
			nodePolicyBuilder.Definition.Spec.ResourceName = sriovNetworkConfig.ResourceName

			createdNodePolicy, err := nodePolicyBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating SriovNetworkNodePolicy '%s': %v",
				sriovNetworkConfig.NodePolicyName, err)
			glog.V(sriovparams.LogLevel).Infof("Successfully created SriovNetworkNodePolicy '%s'",
				createdNodePolicy.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					_, err := createdNodePolicy.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Resolve the worker nodes targeted by the SriovNetworkNodePolicy")
			targetNodeBuilders, err := nodes.List(inittools.APIClient,
				metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=true", sriovNicPresentLabel)})
			Expect(err).ToNot(HaveOccurred(), "error listing nodes matching the SriovNetworkNodePolicy "+
				"nodeSelector: %v", err)
			Expect(targetNodeBuilders).ToNot(BeEmpty(), "no nodes matched the SriovNetworkNodePolicy nodeSelector")

			targetNodeNames := make([]string, 0, len(targetNodeBuilders))
			for _, nodeBuilder := range targetNodeBuilders {
				targetNodeNames = append(targetNodeNames, nodeBuilder.Object.Name)
			}

			By("Wait up to 30 minutes for the targeted nodes to drain, reboot and sync the SR-IOV configuration")
			err = wait.SriovNetworkNodeStateSynced(inittools.APIClient, targetNodeNames, sriovNamespace,
				30*time.Second, 30*time.Minute)
			Expect(err).ToNot(HaveOccurred(), "error waiting for SriovNetworkNodeState to sync on nodes %v: %v",
				targetNodeNames, err)

			By("Create SriovNetwork")
			ipamJSON, err := buildWhereaboutsIPAM(sriovNetworkConfig.IPAMRange,
				sriovNetworkConfig.IPAMExcludeIP1, sriovNetworkConfig.IPAMExcludeIP2)
			Expect(err).ToNot(HaveOccurred(), "error building whereabouts IPAM config: %v", err)

			networkBuilder := sriovnetwork.NewSriovNetworkBuilder(inittools.APIClient,
				sriovNetworkConfig.NetworkName, sriovNamespace)
			networkBuilder.Definition.Spec.ResourceName = sriovNetworkConfig.ResourceName
			networkBuilder.Definition.Spec.NetworkNamespace = sriovNetworkConfig.NetworkNamespace
			networkBuilder.Definition.Spec.Vlan = sriovNetworkConfig.Vlan
			networkBuilder.Definition.Spec.IPAM = ipamJSON

			createdNetwork, err := networkBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating SriovNetwork '%s': %v",
				sriovNetworkConfig.NetworkName, err)
			glog.V(sriovparams.LogLevel).Infof("Successfully created SriovNetwork '%s'",
				createdNetwork.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					_, err := createdNetwork.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Pull the SriovNetwork just created from cluster")
			pulledNetwork, err := sriovnetwork.PullSriovNetwork(inittools.APIClient, sriovNetworkConfig.NetworkName,
				sriovNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling SriovNetwork '%s' from cluster: %v",
				sriovNetworkConfig.NetworkName, err)

			networkJSON, err := json.MarshalIndent(pulledNetwork, "", " ")
			if err == nil {
				glog.V(sriovparams.LogLevel).Infof("The SriovNetwork just created: %v", string(networkJSON))
			}
		})
	})
})

// whereaboutsIPAM mirrors the whereabouts CNI IPAM JSON config shape used by SriovNetwork.Spec.IPAM.
type whereaboutsIPAM struct {
	Type    string   `json:"type"`
	Range   string   `json:"range"`
	Exclude []string `json:"exclude,omitempty"`
}

// buildWhereaboutsIPAM builds the whereabouts IPAM JSON string for a SriovNetwork, given a CIDR range and up
// to two CIDRs to exclude (either may be empty to omit it).
func buildWhereaboutsIPAM(ipamRange, ipamExcludeIP1, ipamExcludeIP2 string) (string, error) {
	var exclude []string
	for _, excludeIP := range []string{ipamExcludeIP1, ipamExcludeIP2} {
		if excludeIP != "" {
			exclude = append(exclude, excludeIP)
		}
	}

	ipamBytes, err := json.Marshal(whereaboutsIPAM{
		Type:    "whereabouts",
		Range:   ipamRange,
		Exclude: exclude,
	})
	if err != nil {
		return "", fmt.Errorf("error marshalling whereabouts IPAM config: %w", err)
	}

	return string(ipamBytes), nil
}
