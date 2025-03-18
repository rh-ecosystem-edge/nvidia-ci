package nvidianetwork

import (
	"encoding/json"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidianetworkconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfdcheck"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
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
	Nfd = nfd.NewCustomConfig()

	WorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel: "",
		nvidiaNetworkLabel:                  "true",
	}

	ofedDriverVersion = os.Getenv("OFED_DRIVER_VERSION")
	ofedRepository    = os.Getenv("OFED_REPOSITORY")

	// NvidiaNetworkConfig provides access to general configuration parameters.
	nvidiaNetworkConfig *nvidianetworkconfig.NvidiaNetworkConfig
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
	nnoCustomCatalogSourceDisplayName   = "Certified Operators Custom"
)

var _ = Describe("NNO", Ordered, Label(tsparams.LabelSuite), func() {

	var (
		deployBundle       deploy.Deploy
		deployBundleConfig deploy.BundleConfig
	)

	nvidiaNetworkConfig = nvidianetworkconfig.NewNvidiaNetworkConfig()

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

			nfd.EnsureNFDIsInstalled(inittools.APIClient, Nfd, ocpVersion, networkparams.LogLevel)

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
				_ = nfd.NFDCRDeleteAndWait(inittools.APIClient, nfd.CRName, nfd.OperatorNamespace, 30*time.Second,
					5*time.Minute)

				By("Delete NFD CSV")
				_ = nfd.DeleteNFDCSV(inittools.APIClient)

				By("Delete NFD Subscription in NFD namespace")
				_ = nfd.DeleteNFDSubscription(inittools.APIClient)

				By("Delete NFD OperatorGroup in NFD namespace")
				_ = nfd.DeleteNFDOperatorGroup(inittools.APIClient)

				By("Delete NFD Namespace in NFD namespace")
				_ = nfd.DeleteNFDNamespace(inittools.APIClient)
			}

		})

		It("Deploy NVIDIA Network Operator with DTK", Label("nno"), func() {

			nfdcheck.CheckNfdInstallation(inittools.APIClient, nfd.RhcosLabel, nfd.RhcosLabelValue, inittools.GeneralConfig.WorkerLabelMap, networkparams.LogLevel)

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
				"networkWorkerNodeSelector: %v", WorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			clusterArchitecture = clusterArch
			glog.V(networkparams.LogLevel).Infof("cluster architecture for network enabled worker node "+
				"is: %s", clusterArchitecture)

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

				err = deployBundle.DeployBundle(networkparams.LogLevel, &deployBundleConfig, nnoNamespace,
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

			By("Wait up to 24 minutes for NicClusterPolicy to be ready")
			glog.V(networkparams.LogLevel).Infof("Waiting for NicClusterPolicy to be ready")
			err = wait.NicClusterPolicyReady(inittools.APIClient, nnoNicClusterPolicyName, 60*time.Second,
				24*time.Minute)

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
