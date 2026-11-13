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
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	internalNFD "github.com/rh-ecosystem-edge/nvidia-ci/internal/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/sriovnetworkconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/sriovparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork"
	sriovnetworkapi "github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	sriovNamespace              = "openshift-sriov-network-operator"
	sriovOperatorGroupName      = "sriov-network-operators"
	sriovSubscriptionName       = "sriov-network-operator-subscription"
	sriovDeploymentName         = "sriov-network-operator"
	sriovPackage                = "sriov-network-operator"
	sriovOperatorConfigName     = "default"
	sriovOperatorConfigLogLevel = 2
)

var (
	nfdInstance = operatorconfig.NewCustomConfig()

	sriovNetworkConfig *sriovnetworkconfig.SriovNetworkConfig
	nfdConfig          *internalNFD.NFDConfig

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
			Expect(sriovNetworkConfig).ToNot(BeNil(), "failed to load SRIOVNETWORK_* env config")
			Expect(nfdConfig).ToNot(BeNil(), "failed to load NFD env config")

			sriovNicPresentLabel = fmt.Sprintf("feature.node.kubernetes.io/pci-%s.present", sriovNetworkConfig.NicVendor)
			sriovNicCapableLabel = fmt.Sprintf("feature.node.kubernetes.io/pci-%s.sriov.capable", sriovNetworkConfig.NicVendor)
			// sriovNodeSelector targets the same nodes the SR-IOV Network Config daemon runs on
			// (see ConfigDaemonNodeSelector below), so nodes picked by the SriovNetworkNodePolicy
			// are guaranteed to produce a SriovNetworkNodeState for the sync wait.
			sriovNodeSelector = map[string]string{
				"network.nvidia.com/operator.mofed.wait": "false",
				"node-role.kubernetes.io/worker":         "",
				sriovNicCapableLabel:                     "true",
			}
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
			if nfdInstance.CleanupAfterInstall && sriovNetworkConfig.CleanupAfterTest {
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
				nfd.CatalogSourceNamespace, sriovNetworkConfig.CatalogSource)
			Expect(err).ToNot(HaveOccurred(), "error getting SRIOV Network Operator packagemanifest "+
				"'%s' from catalog '%s': %v", sriovPackage, sriovNetworkConfig.CatalogSource, err)

			channel := sriovNetworkConfig.SubscriptionChannel
			if channel == "" {
				channel = pkgManifestBuilder.Object.Status.DefaultChannel
				glog.V(sriovparams.LogLevel).Infof("SRIOVNETWORK_SUBSCRIPTION_CHANNEL is not set, using "+
					"the package's default channel '%s'", channel)
			}

			By("Deploy the SRIOV Network Operator via OLM")
			installResult, err := deploy.InstallOperatorFromCatalog(inittools.APIClient, sriovparams.LogLevel,
				deploy.OLMInstallConfig{
					Namespace:              sriovNamespace,
					OperatorGroupName:      sriovOperatorGroupName,
					SubscriptionName:       sriovSubscriptionName,
					PackageName:            sriovPackage,
					CatalogSource:          sriovNetworkConfig.CatalogSource,
					CatalogSourceNamespace: nfd.CatalogSourceNamespace,
					Channel:                channel,
					InstallPlanApproval:    v1alpha1.Approval("Automatic"),
					DeploymentName:         sriovDeploymentName,
				})

			defer func() {
				if !sriovNetworkConfig.CleanupAfterTest || installResult == nil {
					return
				}

				if installResult.SubscriptionBuilder != nil {
					Expect(installResult.SubscriptionBuilder.Delete()).ToNot(HaveOccurred())
				}

				if installResult.OperatorGroupBuilder != nil {
					Expect(installResult.OperatorGroupBuilder.Delete()).ToNot(HaveOccurred())
				}

				if installResult.NamespaceBuilder != nil {
					Expect(installResult.NamespaceBuilder.Delete()).ToNot(HaveOccurred())
				}
			}()

			Expect(err).ToNot(HaveOccurred(), "error installing SRIOV Network Operator via OLM: %v", err)

			By("Create or update SriovOperatorConfig")
			operatorConfigBuilder := sriovnetwork.NewSriovOperatorConfigBuilder(inittools.APIClient,
				sriovOperatorConfigName, sriovNamespace)

			// The SR-IOV Network Operator auto-creates SriovOperatorConfig/default on install, so
			// pull and patch it when present instead of blindly Create()-ing (which would silently
			// no-op and never apply our spec, and would delete the operator's own object on cleanup).
			// A snapshot of the pre-existing object is kept so the update can be sent as a merge
			// patch: our local SriovOperatorConfigSpec type is trimmed down from the real upstream
			// type, so a full Update() would wipe out any upstream spec fields it doesn't know about.
			operatorConfigPreexisting := operatorConfigBuilder.Exists()

			var operatorConfigOriginal *sriovnetworkapi.SriovOperatorConfig
			if operatorConfigPreexisting {
				operatorConfigOriginal = operatorConfigBuilder.Object.DeepCopy()
				operatorConfigBuilder.Definition = operatorConfigBuilder.Object
			}

			operatorConfigBuilder.Definition.Spec.EnableInjector = true
			operatorConfigBuilder.Definition.Spec.EnableOperatorWebhook = true
			operatorConfigBuilder.Definition.Spec.LogLevel = sriovOperatorConfigLogLevel
			operatorConfigBuilder.Definition.Spec.ConfigDaemonNodeSelector = sriovNodeSelector

			var createdOperatorConfig *sriovnetwork.SriovOperatorConfigBuilder
			if operatorConfigPreexisting {
				createdOperatorConfig, err = operatorConfigBuilder.Patch(operatorConfigOriginal)
				Expect(err).ToNot(HaveOccurred(), "error updating SriovOperatorConfig '%s': %v",
					sriovOperatorConfigName, err)
			} else {
				createdOperatorConfig, err = operatorConfigBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating SriovOperatorConfig '%s': %v",
					sriovOperatorConfigName, err)
			}
			glog.V(sriovparams.LogLevel).Infof("Successfully configured SriovOperatorConfig '%s'",
				createdOperatorConfig.Definition.Name)

			defer func() {
				if sriovNetworkConfig.CleanupAfterTest && !operatorConfigPreexisting {
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
				if sriovNetworkConfig.CleanupAfterTest {
					_, err := createdNodePolicy.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Resolve the worker nodes targeted by the SriovNetworkNodePolicy")
			targetNodeBuilders, err := nodes.List(inittools.APIClient,
				metav1.ListOptions{LabelSelector: labels.Set(sriovNodeSelector).String()})
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
				if sriovNetworkConfig.CleanupAfterTest {
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
