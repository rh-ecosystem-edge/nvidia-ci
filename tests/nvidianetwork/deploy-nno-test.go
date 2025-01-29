package nvidianetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidianetworkconfig"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidianetwork"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
)

var (
	Nfd = nfd.NewConfig()

	nnoWorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel: "",
		nvidiaNetworkLabel:                  "true",
	}

	// Temporary workarounds for arm64 servers
	// Need to do the following exports before running test case:
	// export OFED_REPOSITORY=quay.io/bschmaus
	// Note the default repo is:  nvcr.io/nvidia/mellanox
	// export OFED_DRIVER_VERSION ="24.10-0.5.5.0-0"
	ofedDriverVersion = os.Getenv("OFED_DRIVER_VERSION")
	ofedRepository    = os.Getenv("OFED_REPOSITORY")

	// NvidiaNetworkConfig provides access to general configuration parameters.
	nvidiaNetworkConfig    *nvidianetworkconfig.NvidiaNetworkConfig
	nnoCatalogSource                         = UndefinedValue
	nnoSubscriptionChannel                   = UndefinedValue
	nnoInstallPlanApproval v1alpha1.Approval = "Automatic"

	nnoDefaultSubscriptionChannel        = UndefinedValue
	networkOperatorUpgradeToChannel      = UndefinedValue
	cleanupAfterTest                bool = true
	deployFromBundle                bool = false
	networkOperatorBundleImage           = ""
	clusterArchitecture                  = UndefinedValue

	nnoCustomCatalogSource = UndefinedValue

	createNNOCustomCatalogsource bool = false

	nnoCustomCatalogsourceIndexImage = UndefinedValue
)

const (
	nvidiaNetworkLabel                      = "feature.node.kubernetes.io/pci-15b3.present"
	networkOperatorDefaultMasterBundleImage = "registry.gitlab.com/nvidia/kubernetes/network-operator/staging/network-operator-bundle:main-latest"

	nnoNamespace              = "nvidia-network-operator"
	nnoOperatorGroupName      = "nno-og"
	nnoDeployment             = "nvidia-network-operator-controller-manager"
	nnoSubscriptionName       = "nno-subscription"
	nnoSubscriptionNamespace  = "nvidia-network-operator"
	nnoCatalogSourceDefault   = "certified-operators"
	nnoCatalogSourceNamespace = nfd.CatalogSourceNamespace
	nnoPackage                = "nvidia-network-operator"
	nnoNicClusterPolicyName   = "nic-cluster-policy"

	nnoCustomCatalogSourcePublisherName = "Red Hat"

	nnoCustomCatalogSourceDisplayName = "Certified Operators Custom"
)

var _ = Describe("NNO", Ordered, Label(tsparams.LabelSuite), func() {

	var (
		deployBundle deploy.Deploy
	)

	nvidiaNetworkConfig = nvidianetworkconfig.NewNvidiaNetworkConfig()

	Context("DeployNNO", Label("deploy-nno-with-dtk"), func() {

		BeforeAll(func() {

			if nvidiaNetworkConfig.CatalogSource == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_CATALOGSOURCE"+
					" is not set, using default NNO catalogsource '%s'", nnoCatalogSourceDefault)
				nnoCatalogSource = nnoCatalogSourceDefault
			} else {
				nnoCatalogSource = nvidiaNetworkConfig.CatalogSource
				glog.V(networkparams.LogLevel).Infof("NNO catalogsource now set to env variable "+
					"NVIDIANETWORK_CATALOGSOURCE value '%s'", nnoCatalogSource)
			}

			if nvidiaNetworkConfig.SubscriptionChannel == "" {
				glog.V(networkparams.LogLevel).Infof("env variable NVIDIANETWORK_SUBSCRIPTION_CHANNEL" +
					" is not set, will deploy latest channel")
				nnoSubscriptionChannel = UndefinedValue
			} else {
				nnoSubscriptionChannel = nvidiaNetworkConfig.SubscriptionChannel
				glog.V(networkparams.LogLevel).Infof("NNO Subscription Channel now set to env variable "+
					"NVIDIANETWORK_SUBSCRIPTION_CHANNEL value '%s'", nnoSubscriptionChannel)
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

				nnoCustomCatalogsourceIndexImage = nvidiaNetworkConfig.NNOFallbackCatalogsourceIndexImage

				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom Network Operator " +
					"catalogsource from fall back index image to True")

				createNNOCustomCatalogsource = true

				nnoCustomCatalogSource = nnoCatalogSourceDefault + "-custom"
				glog.V(networkparams.LogLevel).Infof("Setting custom NNO catalogsource name to '%s'",
					nnoCustomCatalogSource)

			} else {
				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom Network Operator " +
					"catalogsource from fall back index image to False")
				createNNOCustomCatalogsource = false
			}

			if nvidiaNetworkConfig.NFDFallbackCatalogsourceIndexImage != "" {
				glog.V(networkparams.LogLevel).Infof("env variable "+
					"NVIDIANETWORK_NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nvidiaNetworkConfig.NFDFallbackCatalogsourceIndexImage)

				Nfd.CustomCatalogSourceIndexImage = nvidiaNetworkConfig.NFDFallbackCatalogsourceIndexImage

				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom NFD operator " +
					"catalogsource from fall back index image to True")

				Nfd.CreateCustomCatalogsource = true

				Nfd.CustomCatalogSource = nfd.CatalogSourceDefault + "-custom"
				glog.V(networkparams.LogLevel).Infof("Setting custom NFD catalogsource name to '%s'",
					Nfd.CustomCatalogSource)

			} else {
				glog.V(networkparams.LogLevel).Infof("Setting flag to create custom NFD operator " +
					"catalogsource from fall back index image to False")
				Nfd.CreateCustomCatalogsource = false
			}

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

			By("Check if NFD is installed")
			nfdInstalled, err := check.NFDDeploymentsReady(inittools.APIClient)

			if nfdInstalled && err == nil {
				glog.V(networkparams.LogLevel).Infof("The check for ready NFD deployments is: %v",
					nfdInstalled)
				glog.V(networkparams.LogLevel).Infof("NFD operators and operands are already " +
					"installed on this cluster")
			} else {
				glog.V(networkparams.LogLevel).Infof("NFD is not currently installed on this cluster")
				glog.V(networkparams.LogLevel).Infof("Deploying NFD Operator and CR instance on this cluster")

				Nfd.CleanupAfterInstall = true

				By("Check if 'nfd' packagemanifest exists in 'redhat-operators' default catalog")
				nfdPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
					nfd.Package, nfd.CatalogSourceNamespace, nfd.CatalogSourceDefault)

				if nfdPkgManifestBuilderByCatalog == nil {
					glog.V(networkparams.LogLevel).Infof("NFD packagemanifest was not found in the "+
						"default '%s' catalog.", nfd.CatalogSourceNamespace)

					if Nfd.CreateCustomCatalogsource {
						glog.V(networkparams.LogLevel).Infof("Creating custom catalogsource '%s' for NFD "+
							"catalog.", Nfd.CustomCatalogSource)
						glog.V(networkparams.LogLevel).Infof("Creating custom catalogsource '%s' for NFD "+
							"Operator with index image '%s'", Nfd.CustomCatalogSource, Nfd.CustomCatalogSourceIndexImage)

						nfdCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
							Nfd.CustomCatalogSource, nfd.CatalogSourceNamespace, Nfd.CustomCatalogSourceIndexImage,
							nfd.CustomCatalogSourceDisplayName, nfd.CustomNFDCatalogSourcePublisherName)

						Expect(nfdCustomCatalogSourceBuilder).ToNot(BeNil(), "error creating custom "+
							"NFD catalogsource %s:  %v", nfd.Package, Nfd.CustomCatalogSource, err)

						createdNFDCustomCatalogSourceBuilder, err := nfdCustomCatalogSourceBuilder.Create()
						Expect(err).ToNot(HaveOccurred(), "error creating custom NFD "+
							"catalogsource '%s':  %v", nfd.Package, Nfd.CustomCatalogSource, err)

						Expect(createdNFDCustomCatalogSourceBuilder).ToNot(BeNil(), "Failed to "+
							" create custom NFD catalogsource '%s'", Nfd.CustomCatalogSource)

						By("Sleep for 60 seconds to allow the NFD custom catalogsource to be created")
						time.Sleep(60 * time.Second)

						glog.V(networkparams.LogLevel).Infof("Wait up to 4 mins for custom NFD "+
							"catalogsource '%s' to be ready", createdNFDCustomCatalogSourceBuilder.Definition.Name)

						Expect(createdNFDCustomCatalogSourceBuilder.IsReady(4 * time.Minute)).NotTo(BeFalse())

						nfdPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalogWithTimeout(
							inittools.APIClient, nfd.Package, nfd.CatalogSourceNamespace, Nfd.CustomCatalogSource,
							30*time.Second, 5*time.Minute)

						Expect(err).ToNot(HaveOccurred(), "error getting NFD packagemanifest '%s' "+
							"from custom catalog '%s':  %v", nfd.Package, Nfd.CustomCatalogSource, err)

						Nfd.CatalogSource = Nfd.CustomCatalogSource
						nfdChannel := nfdPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
						glog.V(networkparams.LogLevel).Infof("NFD channel '%s' retrieved from "+
							"packagemanifest of custom catalogsource '%s'", nfdChannel, Nfd.CustomCatalogSource)

					} else {
						glog.V(networkparams.LogLevel).Info("Skipping test due to missing NFD Packagemanifest " +
							"in default 'redhat-operators' catalogsource, and flag to deploy custom catalogsource " +
							"is false")
						Skip("NFD packagemanifest not found in default 'redhat-operators' catalogsource, " +
							"and flag to deploy custom catalogsource is false")
					}

				} else {
					glog.V(networkparams.LogLevel).Infof("The nfd packagemanifest '%s' was found in the "+
						"default catalog '%s'", nfdPkgManifestBuilderByCatalog.Object.Name, nfd.CatalogSourceNamespace)

					Nfd.CatalogSource = nfd.CatalogSourceNamespace
					nfdChannel := nfdPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
					glog.V(networkparams.LogLevel).Infof("The NFD channel retrieved from "+
						"packagemanifest is:  %v", nfdChannel)

				}

				By("Deploy NFD Operator in NFD namespace")
				err = deploy.CreateNFDNamespace(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating  NFD Namespace: %v", err)

				By("Deploy NFD OperatorGroup in NFD namespace")
				err = deploy.CreateNFDOperatorGroup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating NFD OperatorGroup:  %v", err)

				nfdDeployed := createNFDDeployment()

				if !nfdDeployed {
					By(fmt.Sprintf("Applying workaround for NFD failing to deploy on OCP %s", ocpVersion))
					err = deploy.DeleteNFDSubscription(inittools.APIClient)
					Expect(err).ToNot(HaveOccurred(), "error deleting NFD subscription: %v", err)

					err = deploy.DeleteAnyNFDCSV(inittools.APIClient)
					Expect(err).ToNot(HaveOccurred(), "error deleting NFD CSV: %v", err)

					err = deleteOLMPods(inittools.APIClient)
					Expect(err).ToNot(HaveOccurred(), "error deleting OLM pods for operator cache "+
						"workaround: %v", err)

					glog.V(networkparams.LogLevel).Info("Re-trying NFD deployment")
					nfdDeployed = createNFDDeployment()
				}

				Expect(nfdDeployed).ToNot(BeFalse(), "failed to deploy NFD operator")

				By("Deploy NFD CR instance in NFD namespace")
				err = deploy.DeployCRInstance(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error deploying NFD CR instance in"+
					" NFD namespace:  %v", err)

			}
		})

		BeforeEach(func() {

		})

		AfterEach(func() {

		})

		AfterAll(func() {

			if Nfd.CleanupAfterInstall && cleanupAfterTest {
				// Here need to check if NFD CR is deployed, otherwise Deleting a non-existing CR will throw an error
				// skipping error check for now cause any failure before entire NFD stack
				By("Delete NFD CR instance in NFD namespace")
				_ = deploy.NFDCRDeleteAndWait(inittools.APIClient, nfd.CRName, nfd.OperatorNamespace, 30*time.Second,
					5*time.Minute)

				By("Delete NFD CSV")
				_ = deploy.DeleteNFDCSV(inittools.APIClient)

				By("Delete NFD Subscription in NFD namespace")
				_ = deploy.DeleteNFDSubscription(inittools.APIClient)

				By("Delete NFD OperatorGroup in NFD namespace")
				_ = deploy.DeleteNFDOperatorGroup(inittools.APIClient)

				By("Delete NFD Namespace in NFD namespace")
				_ = deploy.DeleteNFDNamespace(inittools.APIClient)
			}

		})

		It("Deploy NVIDIA Network Operator with DTK", Label("nno"), func() {

			By("Check if NFD is installed %s")
			nfdLabelDetected, err := check.AllNodeLabel(inittools.APIClient, nfd.RhcosLabel, nfd.RhcosLabelValue,
				inittools.GeneralConfig.WorkerLabelMap)

			Expect(err).ToNot(HaveOccurred(), "error calling check.NodeLabel:  %v ", err)
			Expect(nfdLabelDetected).NotTo(BeFalse(), "NFD node label check failed to match "+
				"label %s and label value %s on all nodes", nfd.RhcosLabel, nfd.RhcosLabelValue)
			glog.V(networkparams.LogLevel).Infof("The check for NFD label returned: %v", nfdLabelDetected)

			isNfdInstalled, err := check.NFDDeploymentsReady(inittools.APIClient)
			Expect(err).ToNot(HaveOccurred(), "error checking if NFD deployments are ready:  "+
				"%v ", err)
			glog.V(networkparams.LogLevel).Infof("The check for NFD deployments ready returned: %v",
				isNfdInstalled)

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

			By("Get Cluster Architecture from first Nvidia Network enabled worker node")
			glog.V(networkparams.LogLevel).Infof("Getting cluster architecture from nodes with "+
				"networkWorkerNodeSelector: %v", nnoWorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, nnoWorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			clusterArchitecture = clusterArch
			glog.V(networkparams.LogLevel).Infof("cluster architecture for network enabled worker node "+
				"is: %s", clusterArchitecture)

			By("Check if Network Operator Deployment is from Bundle")
			if deployFromBundle {
				glog.V(networkparams.LogLevel).Infof("Deploying Network operator from bundle")
				// This returns the Deploy interface object initialized with the API client
				deployBundle = deploy.NewDeploy(inittools.APIClient)
				nnoBundleConfig, err := deployBundle.GetBundleConfig(networkparams.LogLevel)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.GetBundleConfig %s ", err)
				glog.V(networkparams.LogLevel).Infof("Extracted env var NETWORK_BUNDLE_IMAGE"+
					" is '%s'", nnoBundleConfig.BundleImage)

			} else {
				glog.V(networkparams.LogLevel).Infof("Deploying Network Operator from catalogsource")

				By("Check if 'nvidia-network-operator' packagemanifest exists in certified-operators catalog")
				glog.V(networkparams.LogLevel).Infof("Using NNO catalogsource '%s'", nnoCatalogSource)

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
							"Operator, with index image '%s'", nnoCustomCatalogSource, nnoCustomCatalogsourceIndexImage)

						glog.V(networkparams.LogLevel).Infof("Deploying a custom NNO catalogsource '%s' with '%s' "+
							"index image", nnoCustomCatalogSource, nnoCustomCatalogsourceIndexImage)

						nnoCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
							nnoCustomCatalogSource, nnoCatalogSourceNamespace, nnoCustomCatalogsourceIndexImage,
							nnoCustomCatalogSourceDisplayName, nnoCustomCatalogSourcePublisherName)

						Expect(nnoCustomCatalogSourceBuilder).NotTo(BeNil(), "Failed to Initialize "+
							"CatalogSourceBuilder for custom NNO catalogsource '%s'", nnoCustomCatalogSource)

						createdNNOCustomCatalogSourceBuilder, err := nnoCustomCatalogSourceBuilder.Create()
						glog.V(networkparams.LogLevel).Infof("Creating custom NNO Catalogsource builder object "+
							"'%s'", createdNNOCustomCatalogSourceBuilder.Definition.Name)
						Expect(err).ToNot(HaveOccurred(), "error creating custom NNO catalogsource "+
							"builder Object name %s:  %v", nnoCustomCatalogSource, err)

						By("Sleep for 60 seconds to allow the NNO custom catalogsource to be created")
						time.Sleep(60 * time.Second)

						glog.V(networkparams.LogLevel).Infof("Wait up to 4 mins for custom NNO catalogsource " +
							"to be ready")

						Expect(createdNNOCustomCatalogSourceBuilder.IsReady(4 * time.Minute)).NotTo(BeFalse())

						nnoCatalogSource = createdNNOCustomCatalogSourceBuilder.Definition.Name

						glog.V(networkparams.LogLevel).Infof("Custom NNO catalogsource '%s' is now ready",
							createdNNOCustomCatalogSourceBuilder.Definition.Name)

						nnoPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
							nnoPackage, nnoCatalogSourceNamespace, nnoCustomCatalogSource)

						Expect(err).ToNot(HaveOccurred(), "error getting NNO packagemanifest '%s' "+
							"from custom catalog '%s':  %v", nnoPackage, nnoCustomCatalogSource, err)

						By("Get the Network Operator Default Channel from Packagemanifest")
						nnoDefaultSubscriptionChannel = nnoPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
						glog.V(networkparams.LogLevel).Infof("NNO channel '%s' retrieved from packagemanifest "+
							"of custom catalogsource '%s'", nnoDefaultSubscriptionChannel, nnoCustomCatalogSource)

					} else {
						Skip("nvidia-network-operator packagemanifest not found in default 'certified-operators'" +
							"catalogsource, and flag to deploy custom NNO catalogsource is false")
					}

				} else {
					glog.V(networkparams.LogLevel).Infof("NNO packagemanifest '%s' was found in the default"+
						" catalog '%s'", nnoPkgManifestBuilderByCatalog.Object.Name, nnoCatalogSourceDefault)

					nnoCatalogSource = nnoCatalogSourceDefault

					By("Get the Network Operator Default Channel from Packagemanifest")
					nnoDefaultSubscriptionChannel = nnoPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
					glog.V(networkparams.LogLevel).Infof("NNO channel '%s' was retrieved from NNO "+
						"packagemanifest", nnoDefaultSubscriptionChannel)
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
				nnoBundleConfig, err := deployBundle.GetBundleConfig(networkparams.LogLevel)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.GetBundleConfig %s ", err)

				glog.V(networkparams.LogLevel).Infof("Extracted NetworkOperator bundle image from env var "+
					"NVIDIANETWORK_BUNDLE_IMAGE '%s'", nnoBundleConfig.BundleImage)

				glog.V(networkparams.LogLevel).Infof("Deploy the Network Operator bundle '%s'",
					nnoBundleConfig.BundleImage)
				err = deployBundle.DeployBundle(networkparams.LogLevel, nnoBundleConfig, nnoNamespace, 5*time.Minute)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.DeployBundle():  '%v' ", err)

				glog.V(networkparams.LogLevel).Infof("Network Operator bundle image '%s' deployed successfully "+
					"in namespace '%s", nnoBundleConfig.BundleImage, nnoNamespace)

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
					nnoSubscriptionNamespace, nnoCatalogSource, nnoCatalogSourceNamespace, nnoPackage)

				if nnoSubscriptionChannel != UndefinedValue {
					glog.V(networkparams.LogLevel).Infof("Setting the NNO subscription channel to: '%s'",
						nnoSubscriptionChannel)
					subBuilder.WithChannel(nnoSubscriptionChannel)
				} else {
					glog.V(networkparams.LogLevel).Infof("Setting the NNO subscription channel to "+
						"default channel: '%s'", nnoDefaultSubscriptionChannel)
					subBuilder.WithChannel(nnoDefaultSubscriptionChannel)
				}

				subBuilder.WithInstallPlanApproval(nnoInstallPlanApproval)

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
			Expect(csvBuilderList).To(HaveLen(1), "Exactly one Network operator CSV is expected")

			csvBuilder := csvBuilderList[0]

			nnoCurrentCSV := csvBuilder.Definition.Name
			glog.V(networkparams.LogLevel).Infof("Deployed ClusterServiceVersion is: '%s", nnoCurrentCSV)

			nnoCurrentCSVVersion := csvBuilder.Definition.Spec.Version.String()
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
			glog.V(networkparams.LogLevel).Info("error waiting for ClusterServiceVersion '%s' to be "+
				"in Succeeded phase:  %v ", nnoCurrentCSV, err)
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
			nicClusterPolicyBuilder := nvidianetwork.NewBuilderFromObjectString(inittools.APIClient, almExamples)

			By("Updating NicClusterPolicyBuilder object driver version and driver repository from values in env vars")
			glog.V(networkparams.LogLevel).Infof("Updating NicClusterPolicyBuilder object driver version and " +
				"driver repository with values passed in env variables")
			nicClusterPolicyBuilder.Definition.Spec.OFEDDriver.Repository = ofedRepository
			nicClusterPolicyBuilder.Definition.Spec.OFEDDriver.Version = ofedDriverVersion

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
			pulledNicClusterPolicy, err := nvidianetwork.Pull(inittools.APIClient, nnoNicClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: "+
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

			By("Wait up to 12 minutes for NicClusterPolicy to be ready")
			glog.V(networkparams.LogLevel).Infof("Waiting for NicClusterPolicy to be ready")
			err = wait.NicClusterPolicyReady(inittools.APIClient, nnoNicClusterPolicyName, 60*time.Second,
				12*time.Minute)

			glog.V(networkparams.LogLevel).Infof("error waiting for NicClusterPolicy to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for NicClusterPolicy to be Ready: "+
				" %v ", err)

			By("Pull the ready NicClusterPolicy from cluster, with updated fields")
			pulledReadyNicClusterPolicy, err := nvidianetwork.Pull(inittools.APIClient, nnoNicClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling NicClusterPolicy %s from cluster: "+
				" %v ", nnoNicClusterPolicyName, err)

			cpReadyJSON, err := json.MarshalIndent(pulledReadyNicClusterPolicy, "", " ")

			if err == nil {
				glog.V(networkparams.LogLevel).Infof("The ready NicClusterPolicy just has name:  %v",
					pulledReadyNicClusterPolicy.Definition.Name)
				glog.V(networkparams.LogLevel).Infof("The ready NicClusterPolicy just marshalled "+
					"in json: %v", string(cpReadyJSON))
			} else {
				glog.V(networkparams.LogLevel).Infof("Error Marshalling the ready NicClusterPolicy into "+
					"json:  %v", err)
			}
		})

	})
})

func createNFDDeployment() bool {

	By("Deploy NFD Subscription in NFD namespace")
	err := deploy.CreateNFDSubscription(inittools.APIClient, Nfd.CatalogSource)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD Subscription:  %v", err)

	By("Sleep for 2 minutes to allow the NFD Operator deployment to be created")
	glog.V(networkparams.LogLevel).Infof("Sleep for 2 minutes to allow the NFD Operator deployment" +
		" to be created")
	time.Sleep(2 * time.Minute)

	By("Wait up to 5 mins for NFD Operator deployment to be created")
	nfdDeploymentCreated := wait.DeploymentCreated(inittools.APIClient, nfd.OperatorDeploymentName, nfd.OperatorNamespace,
		30*time.Second, 5*time.Minute)
	Expect(nfdDeploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy "+
		"NFD operator")

	By("Check if NFD Operator has been deployed")
	nfdDeployed, err := deploy.CheckNFDOperatorDeployed(inittools.APIClient, 240*time.Second)
	Expect(err).ToNot(HaveOccurred(), "error deploying NFD Operator in"+
		" NFD namespace:  %v", err)
	return nfdDeployed
}

func deleteOLMPods(apiClient *clients.Settings) error {

	olmNamespace := "openshift-operator-lifecycle-manager"
	glog.V(networkparams.LogLevel).Info("Deleting catalog operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=catalog-operator"}); err != nil {
		glog.Error("Error deleting catalog operator pods: ", err)
		return err
	}

	glog.V(networkparams.LogLevel).Info("Deleting OLM operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=olm-operator"}); err != nil {
		glog.Error("Error deleting OLM operator pods: ", err)
		return err
	}

	return nil
}
