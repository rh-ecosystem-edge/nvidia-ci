package nvidiagpu

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiagpuv1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"

	internalNFD "github.com/rh-ecosystem-edge/nvidia-ci/internal/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/machine"

	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfdcheck"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"
	gpuburn "github.com/rh-ecosystem-edge/nvidia-ci/internal/gpu-burn"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/mig"
	nodes "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	nfdInstance = operatorconfig.NewCustomConfig()
	burn        = nvidiagpu.NewDefaultGPUBurnConfig()

	InstallPlanApproval v1alpha1.Approval = "Automatic"

	WorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel: "",
		nvidiagpu.NvidiaGPULabel:            "true",
	}

	BurnImageName = map[string]string{
		"amd64": "quay.io/wabouham/gpu_burn_amd64:ubi9",
		"arm64": "quay.io/wabouham/gpu_burn_arm64:ubi9",
	}

	machineSetNamespace         = "openshift-machine-api"
	replicas              int32 = 1
	workerMachineSetLabel       = "machine.openshift.io/cluster-api-machine-role"

	// NvidiaGPUConfig provides access to general configuration parameters.
	nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig
	nfdConfig       *internalNFD.NFDConfig

	ScaleCluster  = false
	CatalogSource = UndefinedValue

	CustomCatalogSource = UndefinedValue

	createGPUCustomCatalogsource = false

	CustomCatalogsourceIndexImage = UndefinedValue

	SubscriptionChannel        = UndefinedValue
	DefaultSubscriptionChannel = UndefinedValue
	OperatorUpgradeToChannel   = UndefinedValue
	cleanupAfterTest           = true
	deployFromBundle           = false
	operatorBundleImage        = ""
	CurrentCSV                 = ""
	CurrentCSVVersion          = ""
	clusterArchitecture        = UndefinedValue
	labelsToCheck              = []string{}
)

var _ = Describe("GPU", Ordered, Label(tsparams.LabelSuite), func() {

	var (
		deployBundle       deploy.Deploy
		deployBundleConfig deploy.BundleConfig
	)

	nvidiaGPUConfig = nvidiagpuconfig.NewNvidiaGPUConfig()

	nfdConfig, _ = internalNFD.NewNFDConfig()

	Context("DeployGpu", Label("deploy-gpu-with-dtk"), func() {

		BeforeAll(func() {
			glog.V(0).Infof("Start of the test case, BeforeAll")
			if nvidiaGPUConfig.InstanceType == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE" +
					" is not set, skipping scaling cluster")
				ScaleCluster = false

			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE"+
					" is set to '%s', scaling cluster to add a GPU enabled machineset", nvidiaGPUConfig.InstanceType)
				ScaleCluster = true
			}

			if nvidiaGPUConfig.CatalogSource == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_CATALOGSOURCE"+
					" is not set, using default GPU catalogsource '%s'", nvidiagpu.CatalogSourceDefault)
				CatalogSource = nvidiagpu.CatalogSourceDefault
			} else {
				CatalogSource = nvidiaGPUConfig.CatalogSource
				glog.V(gpuparams.GpuLogLevel).Infof("GPU catalogsource now set to env variable "+
					"NVIDIAGPU_CATALOGSOURCE value '%s'", CatalogSource)
			}

			if nvidiaGPUConfig.SubscriptionChannel == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_SUBSCRIPTION_CHANNEL" +
					" is not set, will deploy latest channel")
				SubscriptionChannel = UndefinedValue
			} else {
				SubscriptionChannel = nvidiaGPUConfig.SubscriptionChannel
				glog.V(gpuparams.GpuLogLevel).Infof("GPU Subscription Channel now set to env variable "+
					"NVIDIAGPU_SUBSCRIPTION_CHANNEL value '%s'", SubscriptionChannel)
			}

			if nvidiaGPUConfig.ClusterPolicyPatch == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH" +
					" is not set, will deploy default ClusterPolicy")
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy patch env variable NVIDIAGPU_GPU_CLUSTER_POLICY_PATCH"+
					" is set to '%s'", nvidiaGPUConfig.ClusterPolicyPatch)
			}

			cleanupAfterTest = nvidiaGPUConfig.CleanupAfterTest
			glog.V(0).Infof("CleanupAfterTest: %v", cleanupAfterTest)

			// if any of the following labels are present, the operator should be kept
			labelsToCheck = []string{"operator-upgrade", "gpu-burn-mig", "single-mig"}
			glog.V(0).Infof("LabelsToCheck: %v", labelsToCheck)

			if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
				glog.V(gpuparams.GpuLogLevel).Info("NVIDIAGPU_CLEANUP is not set or is set to true; cleaning up resources after test case execution.")
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("NVIDIAGPU_CLEANUP is set to '%v'; skipping cleanup after test case execution.", cleanupAfterTest)
				glog.V(gpuparams.GpuLogLevel).Infof("... or labels prevent immediate cleanup")
			}

			if nvidiaGPUConfig.DeployFromBundle {
				deployFromBundle = nvidiaGPUConfig.DeployFromBundle
				glog.V(gpuparams.GpuLogLevel).Infof("Flag deploy GPU operator from bundle is set to env "+
					"variable NVIDIAGPU_DEPLOY_FROM_BUNDLE value '%v'", deployFromBundle)
				if nvidiaGPUConfig.BundleImage == "" {
					glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_BUNDLE_IMAGE"+
						" is not set, will use the default bundle image '%s'",
						nvidiagpu.OperatorDefaultMasterBundleImage)
					operatorBundleImage = nvidiagpu.OperatorDefaultMasterBundleImage
				} else {
					operatorBundleImage = nvidiaGPUConfig.BundleImage
					glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_BUNDLE_IMAGE"+
						" is set, will use the specified bundle image '%s'", operatorBundleImage)
				}
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_DEPLOY_FROM_BUNDLE" +
					" is set to false or is not set, will deploy GPU Operator from catalogsource")
				deployFromBundle = false
			}

			if nvidiaGPUConfig.OperatorUpgradeToChannel == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL" +
					" is not set, will not run the Upgrade Testcase")
				OperatorUpgradeToChannel = UndefinedValue
			} else {
				OperatorUpgradeToChannel = nvidiaGPUConfig.OperatorUpgradeToChannel
				glog.V(gpuparams.GpuLogLevel).Infof("GPU Operator Upgrade to channel now set to env variable "+
					"NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL value '%s'", OperatorUpgradeToChannel)
			}

			if nvidiaGPUConfig.GPUFallbackCatalogsourceIndexImage != "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable "+
					"NVIDIAGPU_GPU_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nvidiaGPUConfig.GPUFallbackCatalogsourceIndexImage)

				CustomCatalogsourceIndexImage = nvidiaGPUConfig.GPUFallbackCatalogsourceIndexImage

				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom GPU operator catalogsource" +
					" from fall back index image to True")

				createGPUCustomCatalogsource = true

				CustomCatalogSource = nvidiagpu.CatalogSourceDefault + "-custom"
				glog.V(gpuparams.GpuLogLevel).Infof("Setting custom GPU catalogsource name to '%s'",
					CustomCatalogSource)

			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom GPU operator catalogsource" +
					" from fall back index image to False")
				createGPUCustomCatalogsource = false
			}

			if nfdConfig.FallbackCatalogSourceIndexImage != "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable "+
					"NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nfdConfig.FallbackCatalogSourceIndexImage)

				nfdInstance.CustomCatalogSourceIndexImage = nfdConfig.FallbackCatalogSourceIndexImage

				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom NFD operator catalogsource" +
					" from fall back index image to True")

				nfdInstance.CreateCustomCatalogsource = true

				nfdInstance.CustomCatalogSource = nfd.CatalogSourceDefault + "-custom"
				glog.V(gpuparams.GpuLogLevel).Infof("Setting custom NFD catalogsource name to '%s'",
					nfdInstance.CustomCatalogSource)

			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom NFD operator catalogsource" +
					" from fall back index image to False")
				nfdInstance.CreateCustomCatalogsource = false
			}

			By("Report OpenShift version")
			ocpVersion, err := inittools.GetOpenShiftVersion()
			glog.V(gpuparams.GpuLogLevel).Infof("Current OpenShift cluster version is: '%s'", ocpVersion)

			if err != nil {
				glog.Error("Error getting OpenShift version: ", err)
			} else if err := inittools.GeneralConfig.WriteReport(OpenShiftVersionFile, []byte(ocpVersion)); err != nil {
				glog.Error("Error writing an OpenShift version file: ", err)
			}

			nfd.EnsureNFDIsInstalled(inittools.APIClient, nfdInstance, ocpVersion, gpuparams.GpuLogLevel)

		})

		BeforeEach(func() {
			glog.V(0).Infof(
				"Verboselevel: %s GPUloglevel: %d",
				inittools.GeneralConfig.VerboseLevel, gpuparams.GpuLogLevel)
		})

		AfterEach(func() {

		})

		AfterAll(func() {
			glog.V(gpuparams.Gpu10LogLevel).Infof("cleanup in AfterAll")
			if nfdInstance.CleanupAfterInstall && cleanupAfterTest {
				err := nfd.Cleanup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "Error cleaning up NFD resources: %v", err)
			}
			// Cleanup GPU Operator Resources, if requested
			if cleanupAfterTest {
				cleanupGPUOperatorResources()
			}
		})

		It("Deploy NVIDIA GPU Operator with DTK", Label("nvidia-ci:gpu"), func() {

			nfdcheck.CheckNfdInstallation(inittools.APIClient, nfd.OSLabel, nfd.GetAllowedOSLabels(), inittools.GeneralConfig.WorkerLabelMap, networkparams.LogLevel)

			By("Check if at least one worker node is GPU enabled")
			gpuNodeFound, _ := check.NodeWithLabel(inittools.APIClient, nvidiagpu.NvidiaGPULabel, inittools.GeneralConfig.WorkerLabelMap)

			glog.V(gpuparams.GpuLogLevel).Infof("The check for Nvidia GPU label returned: %v", gpuNodeFound)

			if !gpuNodeFound && !ScaleCluster {
				glog.V(gpuparams.GpuLogLevel).Infof("Skipping test:  No GPUs were found on any node and flag " +
					"to scale cluster and add a GPU machineset is set to false")
				Skip("No GPU labeled worker nodes were found and not scaling current cluster")

			} else if !gpuNodeFound && ScaleCluster {
				By("Expand the OCP cluster using machineset instanceType from the env variable " +
					"NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE")

				var instanceType = nvidiaGPUConfig.InstanceType

				glog.V(gpuparams.GpuLogLevel).Infof(
					"Initializing new MachineSetBuilder structure with the following params: %s, %s, %v",
					machineSetNamespace, instanceType, replicas)

				gpuMsBuilder := machine.NewSetBuilderFromCopy(inittools.APIClient, machineSetNamespace, instanceType,
					workerMachineSetLabel, replicas)
				Expect(gpuMsBuilder).NotTo(BeNil(), "Failed to Initialize MachineSetBuilder"+
					" from copy")

				glog.V(gpuparams.GpuLogLevel).Infof(
					"Successfully Initialized new MachineSetBuilder from copy with name: %s",
					gpuMsBuilder.Definition.Name)

				glog.V(gpuparams.GpuLogLevel).Infof(
					"Creating MachineSet named: %s", gpuMsBuilder.Definition.Name)

				By("Create the new GPU enabled MachineSet")
				createdMsBuilder, err := gpuMsBuilder.Create()

				Expect(err).ToNot(HaveOccurred(), "error creating a GPU enabled machineset: %v",
					err)

				pulledMachineSetBuilder, err := machine.PullSet(inittools.APIClient,
					createdMsBuilder.Definition.ObjectMeta.Name,
					machineSetNamespace)

				Expect(err).ToNot(HaveOccurred(), "error pulling GPU enabled machineset:"+
					"  %v", err)

				glog.V(gpuparams.GpuLogLevel).Infof("Successfully pulled GPU enabled machineset %s",
					pulledMachineSetBuilder.Object.Name)

				By("Wait on machineset to be ready")
				glog.V(gpuparams.GpuLogLevel).Infof("Just before waiting for GPU enabled machineset %s "+
					"to be in Ready state", createdMsBuilder.Definition.ObjectMeta.Name)

				err = machine.WaitForMachineSetReady(inittools.APIClient, createdMsBuilder.Definition.ObjectMeta.Name,
					machineSetNamespace, nvidiagpu.MachineReadyWaitDuration)

				Expect(err).ToNot(HaveOccurred(), "Failed to detect at least one replica"+
					" of MachineSet %s in Ready state during 15 min polling interval: %v",
					pulledMachineSetBuilder.Definition.ObjectMeta.Name, err)

				defer func() {
					defer GinkgoRecover()
					if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
						err := pulledMachineSetBuilder.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
					// later add wait for machineset to be deleted
				}()
			}

			// Here we don't need this step is we already have a GPU worker node on cluster
			if ScaleCluster {
				glog.V(gpuparams.GpuLogLevel).Infof("Sleeping for %s to allow the newly created GPU worker node to be labeled by NFD", nvidiagpu.NodeLabelingDelay.String())
				time.Sleep(nvidiagpu.NodeLabelingDelay)
			}

			By("Get Cluster Architecture from first GPU enabled worker node")
			glog.V(gpuparams.GpuLogLevel).Infof("Getting cluster architecture from nodes with "+
				"WorkerNodeSelector: %v", WorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			clusterArchitecture = clusterArch
			glog.V(gpuparams.GpuLogLevel).Infof("cluster architecture for GPU enabled worker node is: %s",
				clusterArchitecture)

			By("Check if GPU Operator Deployment is from Bundle")
			if deployFromBundle {
				// This returns the Deploy interface object initialized with the API client
				deployBundle = deploy.NewDeploy(inittools.APIClient)
				deployBundleConfig.BundleImage = operatorBundleImage
				glog.V(gpuparams.GpuLogLevel).Infof("Deploying GPU operator from bundle image '%s'",
					deployBundleConfig.BundleImage)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Deploying GPU operator from catalogsource")

				By("Check if GPU packagemanifest exists in default GPU catalog")
				glog.V(gpuparams.GpuLogLevel).Infof("Using default GPU catalogsource '%s'",
					nvidiagpu.CatalogSourceDefault)

				gpuPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
					nvidiagpu.Package, nvidiagpu.CatalogSourceNamespace, nvidiagpu.CatalogSourceDefault)

				if err != nil {
					glog.V(gpuparams.GpuLogLevel).Infof("Error trying to pull GPU packagemanifest '%s' from"+
						" default catalog '%s': '%v'", nvidiagpu.Package, nvidiagpu.CatalogSourceDefault, err.Error())
				}

				if gpuPkgManifestBuilderByCatalog == nil {
					glog.V(gpuparams.GpuLogLevel).Infof("The GPU packagemanifest '%s' was not "+
						"found in the default '%s' catalog", nvidiagpu.Package, nvidiagpu.CatalogSourceDefault)

					if createGPUCustomCatalogsource {
						glog.V(gpuparams.GpuLogLevel).Infof("Creating custom catalogsource '%s' for GPU Operator, "+
							"with index image '%s'", CustomCatalogSource, CustomCatalogsourceIndexImage)

						glog.V(gpuparams.GpuLogLevel).Infof("Deploying a custom GPU catalogsource '%s' with '%s' "+
							"index image", CustomCatalogSource, CustomCatalogsourceIndexImage)

						gpuCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
							CustomCatalogSource, nvidiagpu.CatalogSourceNamespace, CustomCatalogsourceIndexImage,
							nvidiagpu.CustomCatalogSourceDisplayName, nvidiagpu.CustomCatalogSourcePublisherName)

						Expect(gpuCustomCatalogSourceBuilder).NotTo(BeNil(), "Failed to Initialize "+
							"CatalogSourceBuilder for custom GPU catalogsource '%s'", CustomCatalogSource)

						createdGPUCustomCatalogSourceBuilder, err := gpuCustomCatalogSourceBuilder.Create()
						glog.V(gpuparams.GpuLogLevel).Infof("Creating custom GPU Catalogsource builder object "+
							"'%s'", createdGPUCustomCatalogSourceBuilder.Definition.Name)
						Expect(err).ToNot(HaveOccurred(), "error creating custom GPU catalogsource "+
							"builder Object name %s:  %v", CustomCatalogSource, err)

						By(fmt.Sprintf("Sleep for %s to allow the GPU custom catalogsource to be created", nvidiagpu.CatalogSourceCreationDelay))
						time.Sleep(nvidiagpu.CatalogSourceCreationDelay)

						glog.V(gpuparams.GpuLogLevel).Infof("Wait up to %s for custom GPU catalogsource to be ready", nvidiagpu.CatalogSourceReadyTimeout)

						Expect(createdGPUCustomCatalogSourceBuilder.IsReady(nvidiagpu.CatalogSourceReadyTimeout)).NotTo(BeFalse())

						CatalogSource = createdGPUCustomCatalogSourceBuilder.Definition.Name

						glog.V(gpuparams.GpuLogLevel).Infof("Custom GPU catalogsource '%s' is now ready",
							createdGPUCustomCatalogSourceBuilder.Definition.Name)

						gpuPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalogWithTimeout(inittools.APIClient,
							nvidiagpu.Package, nvidiagpu.CatalogSourceNamespace, CustomCatalogSource,
							nvidiagpu.PackageManifestCheckInterval, nvidiagpu.PackageManifestTimeout)

						Expect(err).ToNot(HaveOccurred(), "error getting GPU packagemanifest '%s' "+
							"from custom catalog '%s':  %v", nvidiagpu.Package, CustomCatalogSource, err)

						By("Get the GPU Default Channel from Packagemanifest")
						DefaultSubscriptionChannel = gpuPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
						glog.V(gpuparams.GpuLogLevel).Infof("GPU channel '%s' retrieved from packagemanifest "+
							"of custom catalogsource '%s'", DefaultSubscriptionChannel, CustomCatalogSource)

					} else {
						Skip("gpu-operator-certified packagemanifest not found in default 'certified-operators'" +
							"catalogsource, and flag to deploy custom GPU catalogsource is false")
					}

				} else {
					glog.V(gpuparams.GpuLogLevel).Infof("GPU packagemanifest '%s' was found in the default"+
						" catalog '%s'", gpuPkgManifestBuilderByCatalog.Object.Name, nvidiagpu.CatalogSourceDefault)

					CatalogSource = nvidiagpu.CatalogSourceDefault

					By("Get the GPU Default Channel from Packagemanifest")
					DefaultSubscriptionChannel = gpuPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
					glog.V(gpuparams.GpuLogLevel).Infof("GPU channel '%s' was retrieved from GPU packagemanifest",
						DefaultSubscriptionChannel)
				}

			}

			By("Check if NVIDIA GPU Operator namespace exists, otherwise created it and label it")
			nsBuilder := namespace.NewBuilder(inittools.APIClient, nvidiagpu.NvidiaGPUNamespace)
			if nsBuilder.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("The namespace '%s' already exists",
					nsBuilder.Object.Name)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating the namespace:  %v", nvidiagpu.NvidiaGPUNamespace)
				createdNsBuilder, err := nsBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating namespace '%s' :  %v ",
					nsBuilder.Definition.Name, err)

				glog.V(gpuparams.GpuLogLevel).Infof("Successfully created namespace '%s'",
					createdNsBuilder.Object.Name)

				glog.V(gpuparams.GpuLogLevel).Infof("Labeling the newly created namespace '%s'",
					nsBuilder.Object.Name)

				labeledNsBuilder := createdNsBuilder.WithMultipleLabels(map[string]string{
					"openshift.io/cluster-monitoring":    "true",
					"pod-security.kubernetes.io/enforce": "privileged",
				})

				newLabeledNsBuilder, err := labeledNsBuilder.Update()
				Expect(err).ToNot(HaveOccurred(), "error labeling namespace %v :  %v ",
					newLabeledNsBuilder.Definition.Name, err)

				glog.V(gpuparams.GpuLogLevel).Infof("The nvidia-gpu-operator labeled namespace has "+
					"labels:  %v", newLabeledNsBuilder.Object.Labels)
			}

			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
					err := nsBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			// Namespace needed to be created by this point or checked if created
			if deployFromBundle {
				deployBundleConfig.BundleImage = operatorBundleImage

				glog.V(gpuparams.GpuLogLevel).Infof("Deploy the GPU Operator bundle image '%s'",
					deployBundleConfig.BundleImage)

				err = deployBundle.DeployBundle(gpuparams.GpuLogLevel, &deployBundleConfig, nvidiagpu.NvidiaGPUNamespace,
					nvidiagpu.GpuBundleDeploymentTimeout)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.DeployBundle():  '%v' ", err)

				glog.V(gpuparams.GpuLogLevel).Infof("GPU Operator bundle image '%s' deployed successfully "+
					"in namespace '%s", deployBundleConfig.BundleImage, nvidiagpu.NvidiaGPUNamespace)
			} else {
				By("Create OperatorGroup in NVIDIA GPU Operator Namespace")
				ogBuilder := olm.NewOperatorGroupBuilder(inittools.APIClient, nvidiagpu.OperatorGroupName, nvidiagpu.NvidiaGPUNamespace)
				if ogBuilder.Exists() {
					glog.V(gpuparams.GpuLogLevel).Infof("The ogBuilder that exists has name:  %v",
						ogBuilder.Object.Name)
				} else {
					glog.V(gpuparams.GpuLogLevel).Infof("Create a new operatorgroup with name:  %v",
						ogBuilder.Object.Name)

					ogBuilderCreated, err := ogBuilder.Create()
					Expect(err).ToNot(HaveOccurred(), "error creating operatorgroup %v :  %v ",
						ogBuilderCreated.Definition.Name, err)
				}

				defer func() {
					defer GinkgoRecover()
					if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
						err := ogBuilder.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
				}()

				By("Create Subscription in NVIDIA GPU Operator Namespace")
				subBuilder := olm.NewSubscriptionBuilder(inittools.APIClient, nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace,
					CatalogSource, nvidiagpu.CatalogSourceNamespace, nvidiagpu.Package)

				if SubscriptionChannel != UndefinedValue {
					glog.V(gpuparams.GpuLogLevel).Infof("Setting the subscription channel to: '%s'",
						SubscriptionChannel)
					subBuilder.WithChannel(SubscriptionChannel)
				} else {
					glog.V(gpuparams.GpuLogLevel).Infof("Setting the subscription channel to default channel: '%s'",
						DefaultSubscriptionChannel)
					subBuilder.WithChannel(DefaultSubscriptionChannel)
				}

				subBuilder.WithInstallPlanApproval(InstallPlanApproval)

				glog.V(gpuparams.GpuLogLevel).Infof("Creating the subscription, i.e Deploy the GPU operator")
				createdSub, err := subBuilder.Create()

				Expect(err).ToNot(HaveOccurred(), "error creating subscription %v :  %v ",
					createdSub.Definition.Name, err)

				glog.V(gpuparams.GpuLogLevel).Infof("Newly created subscription: %s was successfully created",
					createdSub.Object.Name)

				if createdSub.Exists() {
					glog.V(gpuparams.GpuLogLevel).Infof("The newly created subscription '%s' in namespace '%v' "+
						"has current CSV  '%v'", createdSub.Object.Name, createdSub.Object.Namespace,
						createdSub.Object.Status.CurrentCSV)
				}

				defer func() {
					defer GinkgoRecover()
					if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
						err := createdSub.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
				}()

			}

			By(fmt.Sprintf("Sleep for %s to allow the GPU Operator deployment to be created", nvidiagpu.OperatorDeploymentCreationDelay))
			glog.V(gpuparams.GpuLogLevel).Infof("Sleep for %s to allow the GPU Operator deployment to be created", nvidiagpu.OperatorDeploymentCreationDelay)
			time.Sleep(nvidiagpu.OperatorDeploymentCreationDelay)

			By(fmt.Sprintf("Wait for up to %s for GPU Operator deployment to be created", nvidiagpu.DeploymentCreationTimeout))
			gpuDeploymentCreated := wait.DeploymentCreated(
				inittools.APIClient,
				nvidiagpu.OperatorDeployment,
				nvidiagpu.NvidiaGPUNamespace,
				nvidiagpu.DeploymentCreationCheckInterval,
				nvidiagpu.DeploymentCreationTimeout)

			Expect(gpuDeploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy GPU operator")

			By("Check if the GPU operator deployment is ready")
			gpuOperatorDeployment, err := deployment.Pull(inittools.APIClient, nvidiagpu.OperatorDeployment, nvidiagpu.NvidiaGPUNamespace)

			Expect(err).ToNot(HaveOccurred(), "Error trying to pull GPU operator "+
				"deployment is: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Pulled GPU operator deployment is:  %v ",
				gpuOperatorDeployment.Definition.Name)

			if gpuOperatorDeployment.IsReady(nvidiagpu.OperatorDeploymentReadyTimeout) {
				glog.V(gpuparams.GpuLogLevel).Infof("Pulled GPU operator deployment '%s' is Ready",
					gpuOperatorDeployment.Definition.Name)
			}

			By("Get the CSV deployed in NVIDIA GPU Operator namespace")
			csvBuilderList, err := olm.ListClusterServiceVersion(inittools.APIClient, nvidiagpu.NvidiaGPUNamespace)

			Expect(err).ToNot(HaveOccurred(), "Error getting list of CSVs in GPU operator "+
				"namespace: '%v'", err)
			Expect(csvBuilderList).To(HaveLen(1), "Exactly one GPU operator CSV is expected")

			csvBuilder := csvBuilderList[0]

			CurrentCSV = csvBuilder.Definition.Name
			glog.V(gpuparams.GpuLogLevel).Infof("Deployed ClusterServiceVersion is: '%s", CurrentCSV)

			CurrentCSVVersion = csvBuilder.Definition.Spec.Version.String()
			csvVersionString := CurrentCSVVersion

			if deployFromBundle {
				csvVersionString = fmt.Sprintf("%s(bundle)", csvBuilder.Definition.Spec.Version.String())
			}

			glog.V(gpuparams.GpuLogLevel).Infof("ClusterServiceVersion version to be written in the operator "+
				"version file is: '%s'", csvVersionString)

			if err := inittools.GeneralConfig.WriteReport(OperatorVersionFile, []byte(csvVersionString)); err != nil {
				glog.Error("Error writing an operator version file: ", err)
			}

			By("Wait for deployed ClusterServiceVersion to be in Succeeded phase")
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterServiceVersion '%s' to be in Succeeded phase",
				CurrentCSV)
			err = wait.CSVSucceeded(inittools.APIClient, CurrentCSV, nvidiagpu.NvidiaGPUNamespace,
				nvidiagpu.CsvSucceededCheckInterval, nvidiagpu.CsvSucceededTimeout)
			glog.V(gpuparams.GpuLogLevel).Info("error waiting for ClusterServiceVersion '%s' to be "+
				"in Succeeded phase:  %v ", CurrentCSV, err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterServiceVersion to be "+
				"in Succeeded phase: ", err)

			By("Pull existing CSV in NVIDIA GPU Operator Namespace")
			clusterCSV, err := olm.PullClusterServiceVersion(inittools.APIClient, CurrentCSV, nvidiagpu.NvidiaGPUNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling CSV from cluster:  %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster lastUpdatedTime is : %v ",
				clusterCSV.Definition.Status.LastUpdateTime)

			glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster Phase is : \"%v\"",
				clusterCSV.Definition.Status.Phase)

			succeeded := v1alpha1.ClusterServiceVersionPhase("Succeeded")
			Expect(clusterCSV.Definition.Status.Phase).To(Equal(succeeded), "CSV Phase is not "+
				"succeeded")

			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
					err := clusterCSV.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Get ALM examples block form CSV")
			almExamples, err := clusterCSV.GetAlmExamples()
			Expect(err).ToNot(HaveOccurred(), "Error from pulling almExamples from csv "+
				"from cluster:  %v ", err)
			glog.V(gpuparams.GpuLogLevel).Infof("almExamples block from clusterCSV  is : %v ", almExamples)

			By("Deploy ClusterPolicy")

			var clusterPolicyBuilder *nvidiagpu.Builder
			if nvidiaGPUConfig.ClusterPolicyPatch == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating default ClusterPolicy from CSV almExamples without modifications")
				clusterPolicyBuilder = nvidiagpu.NewBuilderFromObjectString(inittools.APIClient, almExamples)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating default ClusterPolicy from CSV almExamples and aplying a patch")
				clusterPolicyBuilder = nvidiagpu.NewBuilderFromObjectStringAndPatch(inittools.APIClient, almExamples, nvidiaGPUConfig.ClusterPolicyPatch)
			}

			createdClusterPolicyBuilder, err := clusterPolicyBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "Error Creating ClusterPolicy from csv "+
				"almExamples  %v ", err)
			glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy '%s' is successfully created",
				createdClusterPolicyBuilder.Definition.Name)

			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
					_, err := createdClusterPolicyBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Pull the ClusterPolicy just created from cluster, with updated fields")
			pulledClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: "+
				" %v ", nvidiagpu.ClusterPolicyName, err)

			cpJSON, err := json.MarshalIndent(pulledClusterPolicy, "", " ")

			if err == nil {
				glog.V(gpuparams.GpuLogLevel).Infof("The ClusterPolicy just created has name:  %v",
					pulledClusterPolicy.Definition.Name)
				glog.V(gpuparams.GpuLogLevel).Infof("The ClusterPolicy just created marshalled "+
					"in json: %v", string(cpJSON))
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Error Marshalling ClusterPolicy into json:  %v",
					err)
			}

			By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout))
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting up to %s for ClusterPolicy to be ready", nvidiagpu.ClusterPolicyReadyTimeout)
			err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
				nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)

			glog.V(gpuparams.GpuLogLevel).Infof("error waiting for ClusterPolicy to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterPolicy to be Ready:  %v ",
				err)

			By("Pull the ready ClusterPolicy from cluster, with updated fields")
			pulledReadyClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: "+
				" %v ", nvidiagpu.ClusterPolicyName, err)

			cpReadyJSON, err := json.MarshalIndent(pulledReadyClusterPolicy, "", " ")

			if err == nil {
				glog.V(gpuparams.GpuLogLevel).Infof("The ready ClusterPolicy just has name:  %v",
					pulledReadyClusterPolicy.Definition.Name)
				glog.V(gpuparams.GpuLogLevel).Infof("The ready ClusterPolicy just marshalled "+
					"in json: %v", string(cpReadyJSON))
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Error Marshalling the ready ClusterPolicy into json:  %v",
					err)
			}

			By("Create GPU Burn namespace 'test-gpu-burn'")
			gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, burn.Namespace)
			if gpuBurnNsBuilder.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("The namespace '%s' already exists",
					gpuBurnNsBuilder.Object.Name)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating the gpu burn namespace '%s'",
					burn.Namespace)
				createdGPUBurnNsBuilder, err := gpuBurnNsBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
					"namespace '%s' :  %v ", burn.Namespace, err)

				glog.V(gpuparams.GpuLogLevel).Infof("Successfully created namespace '%s'",
					createdGPUBurnNsBuilder.Object.Name)

				glog.V(gpuparams.GpuLogLevel).Infof("Labeling the newly created namespace '%s'",
					createdGPUBurnNsBuilder.Object.Name)

				labeledGPUBurnNsBuilder := createdGPUBurnNsBuilder.WithMultipleLabels(map[string]string{
					"openshift.io/cluster-monitoring":    "true",
					"pod-security.kubernetes.io/enforce": "privileged",
				})

				newGPUBurnLabeledNsBuilder, err := labeledGPUBurnNsBuilder.Update()
				Expect(err).ToNot(HaveOccurred(), "error labeling namespace %v :  %v ",
					newGPUBurnLabeledNsBuilder.Definition.Name, err)

				glog.V(gpuparams.GpuLogLevel).Infof("The nvidia-gpu-operator labeled namespace has "+
					"labels:  %v", newGPUBurnLabeledNsBuilder.Object.Labels)
			}

			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
					err := gpuBurnNsBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			// If there is a previously deployed gpu-burn pod, delete it.
			// Any error is ignored, as the pod is expected to not be found.

			By("Pull the possibly existing gpu-burn pod object from the cluster")
			currentGpuBurnPodPulled, _ := pod.Pull(inittools.APIClient, burn.PodName, burn.Namespace)

			currentGpuBurnPodName, _ := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace,
				burn.PodLabel)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", currentGpuBurnPodName)

			// If pod name is not nil, delete it.
			if currentGpuBurnPodPulled != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("Deleting gpu-burn pod")
				_, deleteErr := currentGpuBurnPodPulled.Delete()
				Expect(deleteErr).ToNot(HaveOccurred(), "Error deleting gpu-burn pod: %v", deleteErr)
			}

			By("Deploy GPU Burn configmap in test-gpu-burn namespace")
			gpuBurnConfigMap, err := gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, burn.ConfigMapName,
				burn.Namespace)
			Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("The created gpuBurnConfigMap has name: %s",
				gpuBurnConfigMap.Name)

			configmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
			Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
				"namespace '%s': %v", burn.ConfigMapName, burn.Namespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The pulled gpuBurnConfigMap has name: %s",
				configmapBuilder.Definition.Name)

			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
					err := configmapBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Deploy gpu-burn pod in test-gpu-burn namespace")
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod image name is: '%s', in namespace '%s'",
				BurnImageName[clusterArchitecture], burn.Namespace)

			gpuBurnPod, err := gpuburn.CreateGPUBurnPod(inittools.APIClient, burn.PodName, burn.Namespace,
				BurnImageName[(clusterArchitecture)], nvidiagpu.BurnPodCreationTimeout)
			Expect(err).ToNot(HaveOccurred(), "Error creating gpu burn pod: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Creating gpu-burn pod '%s' in namespace '%s'",
				burn.Namespace, burn.Namespace)

			_, err = inittools.APIClient.Pods(gpuBurnPod.Namespace).Create(context.TODO(), gpuBurnPod,
				metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred(), "Error creating gpu-burn '%s' in "+
				"namespace '%s': %v", burn.Namespace, burn.Namespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The created gpuBurnPod has name: %s has status: %v ",
				gpuBurnPod.Name, gpuBurnPod.Status)

			By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
			gpuPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod with label "+
				"'app=gpu-burn-app' from namespace '%s' :  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", gpuPodName)

			By("Pull the gpu-burn pod object from the cluster")
			gpuPodPulled, err := pod.Pull(inittools.APIClient, gpuPodName, burn.Namespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling gpu-burn pod from "+
				"namespace '%s' :  %v ", burn.Namespace, err)

			By("Cleanup gpu-burn pod only if cleanupAfterTest is true and OperatorUpgradeToChannel is undefined")
			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) && OperatorUpgradeToChannel == UndefinedValue {
					_, err := gpuPodPulled.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By(fmt.Sprintf("Wait for up to %s for gpu-burn pod to be in Running phase", nvidiagpu.BurnPodRunningTimeout))
			err = gpuPodPulled.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.BurnPodRunningTimeout)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod in "+
				"namespace '%s' to go to Running phase:  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Running phase")

			By(fmt.Sprintf("Wait for up to %s for gpu-burn pod to run to completion and be in Succeeded phase/Completed status", nvidiagpu.BurnPodSuccessTimeout))
			err = gpuPodPulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.BurnPodSuccessTimeout)

			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' in "+
				"namespace '%s'to go Succeeded phase/Completed status:  %v ", burn.Namespace, burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Succeeded Phase/Completed status")

			By("Get the gpu-burn pod logs")
			glog.V(gpuparams.GpuLogLevel).Infof("Get the gpu-burn pod logs")

			gpuBurnLogs, err := gpuPodPulled.GetLog(nvidiagpu.BurnLogCollectionPeriod, "gpu-burn-ctr")

			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
				"from gpu burn namespace '%s' :  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod '%s' logs:\n%s",
				gpuPodPulled.Definition.Name, gpuBurnLogs)

			By("Parse the gpu-burn pod logs and check for successful execution")
			match1 := strings.Contains(gpuBurnLogs, "GPU 0: OK")
			match2 := strings.Contains(gpuBurnLogs, "100.0%  proc'd:")

			Expect(match1 && match2).ToNot(BeFalse(), "gpu-burn pod execution was FAILED")
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod execution was successful")

		})

		It("Upgrade NVIDIA GPU Operator", Label("operator-upgrade"), func() {

			if OperatorUpgradeToChannel == UndefinedValue {
				glog.V(gpuparams.GpuLogLevel).Infof("Operator Upgrade To Channel not set, skipping " +
					"Operator Upgrade Testcase")
				Skip("Operator Upgrade To Channel not set, skipping Operator Upgrade Testcase")
			}

			By("Starting GPU Operator Upgrade testcase")
			glog.V(gpuparams.GpuLogLevel).Infof("\"Starting GPU Operator Upgrade testcase")

			glog.V(100).Infof(
				"Pulling ClusterPolicy builder structure named '%s'", nvidiagpu.ClusterPolicyName)
			pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)

			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy builder object name '%s' "+
				"from cluster: %v", nvidiagpu.ClusterPolicyName, err)

			glog.V(100).Infof(
				"Pulled ClusterPolicy builder structure named '%s'", pulledClusterPolicyBuilder.Object.Name)

			By("Capturing current clusterPolicy ResourceVersion")
			initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
			glog.V(100).Infof(
				"Pulled ClusterPolicy resourceVersion is '%s'", initialClusterPolicyResourceVersion)

			By("Updating ClusterPolicy rollingUpdate.MaxUnavailable and Driver.UpgradePolicy fields")
			var maxUnavailable = "1"
			glog.V(100).Infof(
				"Setting pulled ClusterPolicy builder daemonset rollingUpdate.MaxUnavailable value to '%s'",
				maxUnavailable)

			myRollingUpdate := nvidiagpuv1.RollingUpdateSpec{
				MaxUnavailable: maxUnavailable,
			}

			if pulledClusterPolicyBuilder.Definition.Spec.Daemonsets.RollingUpdate == nil {
				pulledClusterPolicyBuilder.Definition.Spec.Daemonsets.RollingUpdate = &myRollingUpdate
			}

			myDriverAutoUpgradeTrue := nvidiagpuv1alpha1.DriverUpgradePolicySpec{
				AutoUpgrade: true}

			if pulledClusterPolicyBuilder.Definition.Spec.Driver.UpgradePolicy == nil {
				pulledClusterPolicyBuilder.Definition.Spec.Driver.UpgradePolicy = &myDriverAutoUpgradeTrue
			}

			pulledClusterPolicyBuilder.Definition.Spec.Daemonsets.RollingUpdate.MaxUnavailable = maxUnavailable
			updatedPulledClusterPolicyBuilder, err := pulledClusterPolicyBuilder.Update(true)

			Expect(err).ToNot(HaveOccurred(), "error updating pulled ClusterPolicy builder"+
				" daemonset rollingUpdate.MaxUnavailable and Driver.UpgradePolicy fields:  %v", err)

			By("Capturing updated clusterPolicy ResourceVersion")
			updatedClusterPolicyResourceVersion := updatedPulledClusterPolicyBuilder.Object.ResourceVersion
			glog.V(100).Infof(
				"Pulled ClusterPolicy resourceVersion is '%s'", updatedClusterPolicyResourceVersion)

			glog.V(100).Infof(
				"After updating pulled ClusterPolicy builder, value of daemonset rollingUpdate.MaxUnavailable "+
					"value is now '%v'",
				updatedPulledClusterPolicyBuilder.Definition.Spec.Daemonsets.RollingUpdate.MaxUnavailable)

			glog.V(100).Infof(
				"Pulling SubscriptionBuilder structure with the following params: %s, %s", nvidiagpu.SubscriptionName,
				nvidiagpu.SubscriptionNamespace)

			pulledSubBuilder, err := olm.PullSubscription(inittools.APIClient, nvidiagpu.SubscriptionName,
				nvidiagpu.SubscriptionNamespace)

			Expect(err).ToNot(HaveOccurred(), "Error pulling subscription '%s' in "+
				"namespace '%s': %v", nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace, err)

			glog.V(100).Infof(
				"Successfully Initialized pulledNodeBuilder with name: %s", pulledSubBuilder.Definition.Name)

			glog.V(100).Infof("Current Subscription Channel : %s", pulledSubBuilder.Definition.Spec.Channel)

			pulledSubBuilder.Definition.Spec.Channel = OperatorUpgradeToChannel
			glog.V(100).Infof("Updating Subscription Channel to upgrade to : %s",
				pulledSubBuilder.Definition.Spec.Channel)

			glog.V(100).Infof(
				"Before Subcsription Channel upgrade the StartingCSV is now '%s'",
				pulledSubBuilder.Object.Spec.StartingCSV)

			By("Update the Subscription builder object with new channel value")
			updatedPulledSubBuilder, err := pulledSubBuilder.Update()

			Expect(err).ToNot(HaveOccurred(), "Error updating pulled subscription '%s' in "+
				"namespace '%s': %v", nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace, err)

			glog.V(100).Infof("Successfully updated Subscription Channel to upgrade to '%s'",
				updatedPulledSubBuilder.Definition.Spec.Channel)

			glog.V(100).Infof("Sleeping for %s to allow new CSV to be deployed", nvidiagpu.CsvDeploymentSleepInterval)
			time.Sleep(nvidiagpu.CsvDeploymentSleepInterval)

			glog.V(100).Infof("After Subscription Channel upgrade, the StartingCSV is now '%s'",
				updatedPulledSubBuilder.Object.Spec.StartingCSV)

			By("Wait for daemonsets to be redeployed up to 15 minutes and for ClusterPolicy to be ready again")
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting up to 15 mins for ClusterPolicy to be ready again " +
				"after upgrade")
			err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName, 60*time.Second, 15*time.Minute)

			glog.V(gpuparams.GpuLogLevel).Infof("error waiting for ClusterPolicy to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterPolicy to be Ready:  %v ",
				err)

			By("Pull the post-upgrade Ready ClusterPolicy from cluster, with updated fields")
			pulledUpdatedReadyClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: "+
				" %v ", nvidiagpu.ClusterPolicyName, err)

			By("Capturing Post-Upgrade clusterPolicy ResourceVersion")
			updatedReadyClusterPolicyResourceVersion := pulledUpdatedReadyClusterPolicy.Object.ResourceVersion
			glog.V(100).Infof("Pulled Post-Upgrade Ready ClusterPolicy resourceVersion is '%s'",
				updatedReadyClusterPolicyResourceVersion)

			By("Comparing previous and updated and ready clusterPolicy ResourceVersions")
			glog.V(100).Infof(
				"Previous ClusterPolicy resourceVersion is '%s', updated and Ready clusterPolicy resource "+
					"version is '%s'", updatedClusterPolicyResourceVersion, updatedReadyClusterPolicyResourceVersion)
			Expect(updatedClusterPolicyResourceVersion).To(Not(Equal(updatedReadyClusterPolicyResourceVersion)),
				"ClusterPolicy resourceVersion strings are equal")

			cpReadyAgainJSON, err := json.MarshalIndent(pulledUpdatedReadyClusterPolicy, "", " ")

			Expect(err).ToNot(HaveOccurred(), "Error marshalling the ready ClusterPolicy into json: "+
				" %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("The ready ClusterPolicy after upgrade has name:  %v",
				pulledUpdatedReadyClusterPolicy.Definition.Name)
			glog.V(gpuparams.GpuLogLevel).Infof("The ready ClusterPolicy just marshalled "+
				"in json: %v", string(cpReadyAgainJSON))

			By("Pull the previously deployed gpu-burn pod object from the cluster")
			currentGpuBurnPodPulled, err := pod.Pull(inittools.APIClient, burn.PodName, burn.Namespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling previously deployed and completed "+
				"gpu-burn pod from namespace '%s' :  %v ", burn.Namespace, err)

			By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
			currentGpuBurnPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace,
				burn.PodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting previously deployed gpu-burn pod "+
				"with label 'app=gpu-burn-app' from namespace '%s' :  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", currentGpuBurnPodName)

			By("Delete the previously deployed gpu-burn-pod")
			glog.V(gpuparams.GpuLogLevel).Infof("Deleting previously deployed and completed gpu-burn pod")

			_, err = currentGpuBurnPodPulled.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pod")

			By("Re-deploy gpu-burn pod in test-gpu-burn namespace")
			glog.V(gpuparams.GpuLogLevel).Infof("Re-deployed gpu-burn pod image name is: '%s', in "+
				"namespace '%s'", BurnImageName[clusterArchitecture], burn.Namespace)

			By("Get Cluster Architecture from first GPU enabled worker node")
			glog.V(gpuparams.GpuLogLevel).Infof("Getting cluster architecture from nodes with "+
				"WorkerNodeSelector: %v", WorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			glog.V(gpuparams.GpuLogLevel).Infof("cluster architecture for GPU enabled worker node is: %s",
				clusterArch)

			gpuBurnPod2, err := gpuburn.CreateGPUBurnPod(inittools.APIClient, burn.PodName, burn.Namespace,
				BurnImageName[(clusterArch)], nvidiagpu.BurnPodPostUpgradeCreationTimeout)
			Expect(err).ToNot(HaveOccurred(), "Error re-building gpu burn pod object after "+
				"upgrade: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Re-deploying gpu-burn pod '%s' in namespace '%s'",
				burn.Namespace, burn.Namespace)

			_, err = inittools.APIClient.Pods(burn.Namespace).Create(context.TODO(), gpuBurnPod2,
				metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred(), "Error re-deploying gpu-burn '%s' after operator"+
				" upgrade in namespace '%s': %v", burn.Namespace, burn.Namespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The re-deployed post upgrade gpuBurnPod has name: %s has "+
				"status: %v ", gpuBurnPod2.Name, gpuBurnPod2.Status)

			By("Get the re-deployed gpu-burn pod with label \"app=gpu-burn-app\"")
			gpuBurnPod2Name, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting re-deployed gpu-burn pod with label "+
				"'app=gpu-burn-app' from namespace '%s' :  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", gpuBurnPod2Name)

			By("Pull the re-created gpu-burn pod object from the cluster")
			gpuBurnPod2Pulled, err := pod.Pull(inittools.APIClient, gpuBurnPod2.Name, burn.Namespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling re-deployed gpu-burn pod from "+
				"namespace '%s' :  %v ", burn.Namespace, err)

			defer func() {
				defer GinkgoRecover()
				if cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck) {
					_, err := gpuBurnPod2Pulled.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By(fmt.Sprintf("Wait for up to %s for re-deployed burn pod to be in Running phase", nvidiagpu.RedeployedBurnPodRunningTimeout))
			err = gpuBurnPod2Pulled.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.RedeployedBurnPodRunningTimeout)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for re-deployed gpu-burn pod in "+
				"namespace '%s' to go to Running phase:  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Running phase")

			By(fmt.Sprintf("Wait for up to %s for re-deployed burn pod to run to completion and be in Succeeded phase/Completed status", nvidiagpu.RedeployedBurnPodSuccessTimeout))
			err = gpuBurnPod2Pulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.RedeployedBurnPodSuccessTimeout)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' in "+
				"namespace '%s'to go Succeeded phase/Completed status:  %v ", burn.Namespace, burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Succeeded Phase/Completed status")

			By("Get the gpu-burn pod logs")
			glog.V(gpuparams.GpuLogLevel).Infof("Get the re-created gpu-burn pod logs")

			gpuBurnPod2Logs, err := gpuBurnPod2Pulled.GetLog(nvidiagpu.RedeployedBurnLogCollectionPeriod, "gpu-burn-ctr")

			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
				"from gpu burn namespace '%s' :  %v ", burn.Namespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod '%s' logs:\n%s",
				gpuBurnPod2Pulled.Definition.Name, gpuBurnPod2Logs)

			By("Parse the re-created gpu-burn pod logs and check for successful execution")
			match1a := strings.Contains(gpuBurnPod2Logs, "GPU 0: OK")
			match2a := strings.Contains(gpuBurnPod2Logs, "100.0%  proc'd:")

			Expect(match1a && match2a).ToNot(BeFalse(), "Re-deployed gpu-burn pod execution was FAILED")
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod execution was successful")

		})

		It("Test GPU Workload with single strategy MIG Configuration", Label("gpu-burn-mig"), func() {
			// Skip if gpu-burn-mig label is not in the ginkgo label filter
			if !mig.IsLabelInFilter("gpu-burn-mig") {
				glog.V(gpuparams.GpuLogLevel).Infof("Skipping test: 'gpu-burn-mig' label not present in ginkgo label filter")
				Skip("Test skipped: 'gpu-burn-mig' label not present in ginkgo label filter")
			}
			testWorkloadWithSingleMig()
		})

		It("Test GPU Workload with single strategy MIG Configuration in mig package", Label("single-mig"), func() {
			// Skip if single-mig label is not in the ginkgo label filter
			if !mig.IsLabelInFilter("single-mig") {
				glog.V(gpuparams.GpuLogLevel).Infof("Skipping test: 'single-mig' label not present in ginkgo label filter")
				Skip("Test skipped: 'single-mig' label not present in ginkgo label filter")
			}
			cleanup := cleanupAfterTest && !mig.ShouldKeepOperator(labelsToCheck)
			mig.TestSingleMIGGPUWorkload(nvidiaGPUConfig, burn, BurnImageName, WorkerNodeSelector, cleanup)
		})
	})
})

// cleanupGPUOperatorResources performs cleanup of GPU Operator resources
// It checks if cleanup should run based on cleanupAfterTest and cleanup label
func cleanupGPUOperatorResources() {
	cleanupClusterPolicy()
	cleanupCSV()
	cleanupSubscription()
	cleanupOperatorGroup()
	cleanupGPUOperatorNamespace()
	cleanupGPUBurnPod()
	cleanupGPUBurnConfigmap()
	cleanupGPUBurnNamespace()

	glog.V(gpuparams.GpuLogLevel).Infof("Completed cleanup of GPU Operator Resources")
}

// cleanupClusterPolicy deletes the ClusterPolicy resource
func cleanupClusterPolicy() {
	By("Deleting ClusterPolicy")
	clusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	if err == nil && clusterPolicyBuilder.Exists() {
		_, err = clusterPolicyBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting ClusterPolicy: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy deleted successfully")
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy not found or already deleted")
	}
}

// cleanupCSV deletes the ClusterServiceVersion resources
func cleanupCSV() {
	By("Deleting CSV")
	// Since this is out of defer functions, the CSV need to be listed before deleting gpu-operator CSV.
	csvList, err := olm.ListClusterServiceVersion(inittools.APIClient, nvidiagpu.SubscriptionNamespace)
	Expect(err).ToNot(HaveOccurred(), "Error listing CSV: %v", err)
	for _, csv := range csvList {
		if strings.Contains(csv.Definition.Name, "gpu-operator") {
			err := csv.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting CSV: %v", err)
			glog.V(gpuparams.GpuLogLevel).Infof("CSV %s deleted successfully", csv.Definition.Name)
		}
	}
}

// cleanupSubscription deletes the Subscription resource
func cleanupSubscription() {
	By("Deleting Subscription")
	subBuilder, err := olm.PullSubscription(inittools.APIClient, nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace)
	if err == nil && subBuilder.Exists() {
		err = subBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting Subscription: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Subscription deleted successfully")
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("Subscription not found or already deleted")
	}
}

// cleanupOperatorGroup deletes the OperatorGroup resource
func cleanupOperatorGroup() {
	By("Deleting OperatorGroup")
	ogBuilder, err := olm.PullOperatorGroup(inittools.APIClient, nvidiagpu.OperatorGroupName, nvidiagpu.SubscriptionNamespace)
	if err == nil && ogBuilder.Exists() {
		err = ogBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting OperatorGroup: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("OperatorGroup deleted successfully")
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("OperatorGroup not found or already deleted")
	}
}

// cleanupGPUOperatorNamespace deletes the GPU Operator namespace
func cleanupGPUOperatorNamespace() {
	By("Deleting GPU Operator Namespace")
	nsBuilder, err := namespace.Pull(inittools.APIClient, nvidiagpu.SubscriptionNamespace)
	if err == nil && nsBuilder.Exists() {
		err = nsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", nvidiagpu.SubscriptionNamespace)
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s not found or already deleted", nvidiagpu.SubscriptionNamespace)
	}
}

// cleanupGPUBurnPod deletes the GPU Burn pod
func cleanupGPUBurnPod() {
	By("Deleting GPU Burn Pod")
	gpuBurnPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found existing gpu-burn pod '%s', deleting it", gpuBurnPodName)
		existingPodBuilder, err := pod.Pull(inittools.APIClient, gpuBurnPodName, burn.Namespace)
		Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn pod: %v", err)
		_, err = existingPodBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pod: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully deleted gpu-burn pod '%s'", gpuBurnPodName)
	}
}

// cleanupGPUBurnConfigmap deletes the GPU Burn configmap
func cleanupGPUBurnConfigmap() {
	By("Deleting GPU Burn Configmap")
	existingConfigmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found existing gpu-burn configmap '%s', deleting it", burn.ConfigMapName)
		err = existingConfigmapBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn configmap: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully deleted gpu-burn configmap '%s'", burn.ConfigMapName)
	}
}

// cleanupGPUBurnNamespace deletes the GPU Burn namespace
func cleanupGPUBurnNamespace() {
	By("Deleting GPU Burn Namespace")
	burnNsBuilder, err := namespace.Pull(inittools.APIClient, burn.Namespace)
	if err == nil {
		err = burnNsBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting burn namespace: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("Namespace %s deleted successfully", burn.Namespace)
	}
}

// testWorkloadWithSingleMig performs the GPU Burn test with single strategy MIG Configuration
// Check mig.capable label
// Clean up existing GPU workload resources, if any
// Query MIG profiles from hardware and select one of them as a strategy label for the GPU node
// Set the strategy and config labels on the GPU node
// Waiting for ClusterPolicy state transition first to notReady with quick timeout and interval, then to ready
// Waiting for mig.strategy=single label to be present on GPU nodes
// Pulling and updating ClusterPolicy, and waiting for the label to be present on GPU nodes
// Prepare the workload and deploy it (namespace, configmap, pod)
// After it has been running and finished, get the logs and analyze
func testWorkloadWithSingleMig() {
	By("Check if mig.capable label is present on GPU nodes")
	migCapableLabel := "nvidia.com/mig.capable"
	nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "No GPU nodes with mig.capable label: %v", err)

	var migCapableValue string
	migCapableAvailable := false
	for _, node := range nodeBuilders {
		labelValue, status := node.Object.Labels[migCapableLabel]
		Expect(status).To(BeTrue(), "Error getting label value: %v", status)
		migCapableValue = labelValue
		migCapableAvailable = true
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found %t label '%s' with value '%s' on node '%s'", migCapableAvailable, migCapableLabel, labelValue, node.Object.Name)
		break
	}
	Expect(migCapableAvailable).To(BeTrue(), "mig.capable label not found on at least 1 GPU nodes")
	Expect(migCapableValue).To(Equal("true"), "mig.capable label value is not true")
	glog.V(gpuparams.Gpu100LogLevel).Infof("mig.capable label found on at least 1 GPU node with value '%s', proceeding with test", migCapableValue)

	By("Starting GPU Burn with single strategy MIG Configuration testcase")
	// select one mig profile from the list of available mig profiles
	var useMigProfile string // example: "mig-1g.5gb"  // mig profiles are queried from the hardware
	var useMigIndex int = -1 // will be set to random value after migCapabilities is populated

	// Delete any existing gpu-burn pods with the label, if it exists.
	gpuBurnPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found gpu-burn pod '%s' with: %v", gpuBurnPodName, err)
		existingPodBuilder, err := pod.Pull(inittools.APIClient, gpuBurnPodName, burn.Namespace)
		Expect(err).ToNot(HaveOccurred(), "Error pulling workload pod: %v", err)
		_, err = existingPodBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting workload pod: %v", err)
		err = existingPodBuilder.WaitUntilDeleted(30 * time.Second)
		Expect(err).ToNot(HaveOccurred(), "Error waiting for workload pod to be deleted: %v", err)
	}
	// Delete the configmap if it exists
	existingConfigmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found gpu-burn configmap '%s' with: %v", burn.ConfigMapName, err)
		err = existingConfigmapBuilder.Delete()
		Expect(err).ToNot(HaveOccurred(), "Error deleting workload configmap: %v", err)
		err = existingConfigmapBuilder.WaitUntilDeleted(30 * time.Second)
		Expect(err).ToNot(HaveOccurred(), "Error waiting for workload configmap to be deleted: %v", err)
	}
	// Delete the namespace if it exists
	existingNamespaceBuilder, err := namespace.Pull(inittools.APIClient, burn.Namespace)
	if err == nil {
		glog.V(gpuparams.Gpu10LogLevel).Infof("Found gpu-burn namespace '%s' with: %v", burn.Namespace, err)
		err = existingNamespaceBuilder.DeleteAndWait(30 * time.Second)
		Expect(err).ToNot(HaveOccurred(), "Error deleting workload namespace: %v", err)
	}

	By("Check if mig.strategy=single label is available on GPU nodes")
	_, migCapabilities, err := mig.MIGProfiles(inittools.APIClient, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "Error getting MIG capabilities: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("Found %d MIG configuration profiles", len(migCapabilities))
	for i, info := range migCapabilities {
		glog.V(gpuparams.GpuLogLevel).Infof("  [%d] Profile name: %s, slices %d/%d", i, info.MigName, info.Available, info.Total)
	}
	Expect(len(migCapabilities)).ToNot(BeZero(), "No MIG configurations available")

	// Select random index if not already set
	if useMigIndex < 0 {
		useMigIndex = rand.Intn(len(migCapabilities))
		glog.V(gpuparams.Gpu10LogLevel).Infof("Selected random MIG index: %d (available: 0-%d)", useMigIndex, len(migCapabilities)-1)
	} else if useMigIndex >= len(migCapabilities) {
		glog.V(gpuparams.Gpu10LogLevel).Infof("MIG index %d is out of range (available: 0-%d), using last available index", useMigIndex, len(migCapabilities)-1)
		useMigIndex = len(migCapabilities) - 1
	} else {
		glog.V(gpuparams.Gpu10LogLevel).Infof("MIG index %d is within range (available: 0-%d), using it", useMigIndex, len(migCapabilities)-1)
	}

	By("Set the MIG strategy label on GPU worker nodes")
	glog.V(gpuparams.Gpu10LogLevel).Infof("Setting MIG strategy label on GPU worker nodes from entry %d of the list (profile: %s with %d/%d slices)",
		useMigIndex, migCapabilities[useMigIndex].MigName, migCapabilities[useMigIndex].Available, migCapabilities[useMigIndex].Total)
	MigProfile := "all-" + migCapabilities[useMigIndex].MigName
	strategy := "single"
	useMigProfile = migCapabilities[useMigIndex].Flavor

	// use first mig profile from the list, unless specified otherwise
	nodeBuilders, err = nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
	Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
	for _, nodeBuilder := range nodeBuilders {
		migStrategyLabel := "nvidia.com/mig.strategy"
		currentLabelValue, labelExists := nodeBuilder.Object.Labels[migStrategyLabel]

		// Check if label exists and has the correct value
		if labelExists && currentLabelValue == strategy {
			glog.V(gpuparams.GpuLogLevel).Infof("MIG %s strategy label already exists with correct value on node '%s', skipping update", strategy, nodeBuilder.Definition.Name)
		} else {
			// Need to check node by node if labels can be set or not.
			if labelExists {
				// Label exists with different value, use WithLabel to overwrite
				glog.V(gpuparams.GpuLogLevel).Infof("MIG strategy label exists with value '%s' on node '%s', updating to '%s' (overwrite=true)", currentLabelValue, nodeBuilder.Definition.Name, strategy)
				nodeBuilder = nodeBuilder.WithLabel(migStrategyLabel, strategy)
			} else {
				// Label doesn't exist, use WithNewLabel to add it
				glog.V(gpuparams.GpuLogLevel).Infof("MIG strategy label does not exist on node '%s', adding with value '%s'", nodeBuilder.Definition.Name, strategy)
				nodeBuilder = nodeBuilder.WithNewLabel(migStrategyLabel, strategy)
			}
			_, err = nodeBuilder.Update()
			Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v, error: %v", nodeBuilder.Definition.Name, strategy, err)
			glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG %s strategy label on node '%s'", strategy, nodeBuilder.Definition.Name)
		}

		glog.V(gpuparams.GpuLogLevel).Infof("Setting MIG configuration label %s on node '%s' (overwrite=true)", MigProfile, nodeBuilder.Definition.Name)
		nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", MigProfile)
		_, err = nodeBuilder.Update()
		Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
		glog.V(gpuparams.GpuLogLevel).Infof("Successfully set MIG configuration label on node '%s' with %s", nodeBuilder.Definition.Name, MigProfile)
	}

	glog.V(gpuparams.Gpu100LogLevel).Infof("Waiting for ClusterPolicy to be notReady after setting MIG node labels")
	_ = wait.ClusterPolicyNotReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyNotReadyCheckInterval, nvidiagpu.ClusterPolicyNotReadyTimeout)

	By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready after node label changes",
		nvidiagpu.ClusterPolicyReadyTimeout))
	glog.V(gpuparams.Gpu100LogLevel).Infof("Waiting for ClusterPolicy to be ready after setting MIG node labels")
	err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
		nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready: %v", err)
	glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy is ready after node label changes")

	By("Check for MIG single strategy capability labels on GPU nodes")
	migSingleLabel := "nvidia.com/mig.strategy"
	expectedLabelValue := "single"
	err = wait.NodeLabelExists(inittools.APIClient, migSingleLabel, expectedLabelValue,
		labels.Set(WorkerNodeSelector), nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Could not find at least one node with label '%s' set to '%s'", migSingleLabel, expectedLabelValue)
	glog.V(gpuparams.Gpu10LogLevel).Infof("MIG single strategy label found, proceeding with test")

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("Setting MIG strategy label on GPU worker nodes")
		nodeBuilders, err := nodes.List(inittools.APIClient, metav1.ListOptions{LabelSelector: labels.Set(WorkerNodeSelector).String()})
		Expect(err).ToNot(HaveOccurred(), "Error listing worker nodes: %v", err)
		for _, nodeBuilder := range nodeBuilders {
			glog.V(gpuparams.Gpu100LogLevel).Infof("NOT setting or removing MIG strategy label on node '%s')", nodeBuilder.Definition.Name)

			glog.V(gpuparams.Gpu100LogLevel).Infof("Setting MIG configuration label on node '%s' (overwrite=true)", nodeBuilder.Definition.Name)
			nodeBuilder = nodeBuilder.WithLabel("nvidia.com/mig.config", "all-disabled")
			_, err = nodeBuilder.Update()
			Expect(err).ToNot(HaveOccurred(), "Error updating node '%s' with MIG label: %v", nodeBuilder.Definition.Name, err)
			glog.V(gpuparams.Gpu10LogLevel).Infof("Successfully set MIG configuration label on node '%s'", nodeBuilder.Definition.Name)
		}

		By("Wait for GPU operator to reconcile after setting MIG node labels")
		glog.V(gpuparams.GpuLogLevel).Infof("Sleeping for 30 seconds to allow GPU operator to process node label changes")
		time.Sleep(30 * time.Second)
		// Sleeping to ensure ClusterPolicy gets notReady before entering the next step

		By(fmt.Sprintf("Wait up to %s for ClusterPolicy to be ready after node label changes", nvidiagpu.ClusterPolicyReadyTimeout))
		glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterPolicy to be ready after setting MIG node labels")
		err = wait.ClusterPolicyReady(inittools.APIClient, nvidiagpu.ClusterPolicyName,
			nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
		Expect(err).ToNot(HaveOccurred(), "Error waiting for ClusterPolicy to be ready: %v", err)
		glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy is ready after node label changes")
	}()

	By("Pull existing ClusterPolicy")
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Pulling ClusterPolicy builder structure named '%s'", nvidiagpu.ClusterPolicyName)
	pulledClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy builder object name '%s' "+
		"from cluster: %v", nvidiagpu.ClusterPolicyName, err)
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Pulled ClusterPolicy builder structure named '%s'", pulledClusterPolicyBuilder.Object.Name)

	By("Capturing current clusterPolicy ResourceVersion")
	initialClusterPolicyResourceVersion := pulledClusterPolicyBuilder.Object.ResourceVersion
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Pulled ClusterPolicy resourceVersion is '%s'", initialClusterPolicyResourceVersion)

	By("Configuring MIG strategy in ClusterPolicy")
	migStrategy := nvidiagpuv1.MIGStrategySingle
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Setting ClusterPolicy MIG strategy to '%s'", migStrategy)

	currentMigStrategy := pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy
	Expect(currentMigStrategy).ToNot(BeEmpty(), "Current MIG strategy is empty")
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Current MIG strategy is '%s', updating to '%s'",
		currentMigStrategy, migStrategy)
	pulledClusterPolicyBuilder.Definition.Spec.MIG.Strategy = migStrategy

	By("Update ClusterPolicy with MIG configuration")
	updatedClusterPolicyBuilder, err := pulledClusterPolicyBuilder.Update(true)
	Expect(err).ToNot(HaveOccurred(), "error updating ClusterPolicy with MIG configuration: %v", err)

	By("Capturing updated clusterPolicy ResourceVersion")
	updatedClusterPolicyResourceVersion := updatedClusterPolicyBuilder.Object.ResourceVersion
	glog.V(gpuparams.GpuLogLevel).Infof(
		"Updated ClusterPolicy resourceVersion is '%s'", updatedClusterPolicyResourceVersion)
	glog.V(gpuparams.Gpu10LogLevel).Infof(
		"After updating ClusterPolicy, MIG strategy is now '%v'",
		updatedClusterPolicyBuilder.Definition.Spec.MIG.Strategy)
	err = wait.NodeLabelExists(inittools.APIClient, "nvidia.com/mig.strategy", "single", labels.Set(WorkerNodeSelector),
		nvidiagpu.LabelCheckInterval, nvidiagpu.LabelCheckTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error checking MIG strategy on nodes: %v", err)

	By("Pull the ready ClusterPolicy with MIG configuration from cluster")
	pulledMIGReadyClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName)
	Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: %v",
		nvidiagpu.ClusterPolicyName, err)

	migReadyJSON, err := json.MarshalIndent(pulledMIGReadyClusterPolicy, "", " ")
	Expect(err).ToNot(HaveOccurred(), "error marshalling ClusterPolicy with MIG into json: %v", err)
	glog.V(gpuparams.Gpu100LogLevel).Infof("The ClusterPolicy with MIG configuration has name: %v",
		pulledMIGReadyClusterPolicy.Definition.Name)
	glog.V(gpuparams.GpuLogLevel).Infof("The ClusterPolicy with MIG configuration marshalled "+
		"in json: %v", string(migReadyJSON))

	By("Get Cluster Architecture from first GPU enabled worker node")
	glog.V(gpuparams.Gpu100LogLevel).Infof("Getting cluster architecture from nodes with "+
		"WorkerNodeSelector: %v", WorkerNodeSelector)
	clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, WorkerNodeSelector)
	Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture: %v", err)

	By("Add test-gpu-burn namespace")
	gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, burn.Namespace)
	var createdGPUBurnNsBuilder *namespace.Builder
	if gpuBurnNsBuilder.Exists() {
		glog.V(gpuparams.Gpu10LogLevel).Infof("The namespace '%s' already exists",
			gpuBurnNsBuilder.Object.Name)
		createdGPUBurnNsBuilder = gpuBurnNsBuilder
	} else {
		glog.V(gpuparams.Gpu100LogLevel).Infof("Creating the gpu burn namespace '%s'",
			burn.Namespace)
		var err error
		createdGPUBurnNsBuilder, err = gpuBurnNsBuilder.Create()
		Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
			"namespace '%s' : %v ", burn.Namespace, err)

		glog.V(gpuparams.Gpu10LogLevel).Infof("Successfully created namespace '%s'",
			createdGPUBurnNsBuilder.Object.Name)
	}

	labeledGPUBurnNsBuilder := createdGPUBurnNsBuilder.WithMultipleLabels(map[string]string{
		"openshift.io/cluster-monitoring":    "true",
		"pod-security.kubernetes.io/enforce": "privileged",
	})

	newGPUBurnLabeledNsBuilder, err := labeledGPUBurnNsBuilder.Update()
	Expect(err).ToNot(HaveOccurred(), "error labeling namespace %v : %v ",
		newGPUBurnLabeledNsBuilder.Definition.Name, err)
	glog.V(gpuparams.Gpu100LogLevel).Infof("The gpu-burn labeled namespace has "+
		"labels: %v", newGPUBurnLabeledNsBuilder.Object.Labels)

	By("Deploy GPU Burn configmap in test-gpu-burn namespace")
	gpuBurnConfigMap, err := gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, burn.ConfigMapName,
		burn.Namespace)
	Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)

	glog.V(gpuparams.Gpu100LogLevel).Infof("The created gpuBurnConfigMap has name: %s",
		gpuBurnConfigMap.Name)

	configmapBuilder, err := configmap.Pull(inittools.APIClient, burn.ConfigMapName, burn.Namespace)
	Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
		"namespace '%s': %v", burn.ConfigMapName, burn.Namespace, err)

	glog.V(gpuparams.Gpu10LogLevel).Infof("The pulled gpuBurnConfigMap has name: %s",
		configmapBuilder.Definition.Name)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer11 (configmapBuilder) sleeping for 15 seconds")
		if cleanupAfterTest {
			err := configmapBuilder.Delete()
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(time.Second * 15)
		}
	}()

	By("Deploy gpu-burn pod with MIG configuration in test-gpu-burn namespace")
	glog.V(gpuparams.Gpu100LogLevel).Infof("gpu-burn pod image name is: '%s', in namespace '%s'",
		BurnImageName[clusterArch], burn.Namespace)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Creating pod with MIG profile '%s' requesting %d instances",
		useMigProfile, migCapabilities[useMigIndex].Total)

	// Sometimes available is zero, so using total instead
	gpuBurnMigPod, err := gpuburn.CreateGPUBurnPodWithMIG(inittools.APIClient, burn.PodName, burn.Namespace,
		BurnImageName[clusterArch], useMigProfile, migCapabilities[useMigIndex].Total, nvidiagpu.BurnPodCreationTimeout)
	Expect(err).ToNot(HaveOccurred(), "Error creating gpu burn pod with MIG: %v", err)

	_, err = inittools.APIClient.Pods(gpuBurnMigPod.Namespace).Create(context.TODO(), gpuBurnMigPod,
		metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred(), "Error creating gpu-burn '%s' with MIG in "+
		"namespace '%s': %v", burn.Namespace, burn.Namespace, err)
	glog.V(gpuparams.Gpu100LogLevel).Infof("The created gpuBurnMigPod has name: %s has status: %v",
		gpuBurnMigPod.Name, gpuBurnMigPod.Status)

	By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
	gpuMigPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, burn.Namespace, burn.PodLabel)
	Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod with label "+
		"'app=gpu-burn-app' from namespace '%s': %v", burn.Namespace, err)
	glog.V(gpuparams.Gpu100LogLevel).Infof("gpuMigPodName is %s", gpuMigPodName)

	By("Pull the gpu-burn pod object from the cluster")
	gpuMigPodPulled, err := pod.Pull(inittools.APIClient, gpuMigPodName, burn.Namespace)
	Expect(err).ToNot(HaveOccurred(), "error pulling gpu-burn pod from "+
		"namespace '%s': %v", burn.Namespace, err)

	defer func() {
		defer GinkgoRecover()
		glog.V(gpuparams.Gpu100LogLevel).Infof("defer12 (gpuMigPodPulled) sleeping for 5 seconds	")
		if cleanupAfterTest {
			time.Sleep(time.Second * 5)
			_, err := gpuMigPodPulled.Delete()
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	By(fmt.Sprintf("Wait for up to %s for gpu-burn pod with MIG to be in Running phase", nvidiagpu.BurnPodRunningTimeout))
	err = gpuMigPodPulled.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.BurnPodRunningTimeout)
	Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod with MIG in "+
		"namespace '%s' to go to Running phase: %v", burn.Namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG now in Running phase")

	By(fmt.Sprintf("Wait for up to %s for gpu-burn pod with MIG to run to completion and be in Succeeded phase/Completed status", nvidiagpu.BurnPodSuccessTimeout))
	err = gpuMigPodPulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.BurnPodSuccessTimeout)
	Expect(err).ToNot(HaveOccurred(), "error waiting for gpu-burn pod '%s' with MIG in "+
		"namespace '%s' to go Succeeded phase/Completed status: %v", burn.Namespace, burn.Namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-burn pod with MIG now in Succeeded Phase/Completed status")

	By("Get the gpu-burn pod logs")
	glog.V(gpuparams.Gpu100LogLevel).Infof("Get the gpu-burn pod logs with MIG configuration")
	gpuBurnMigLogs, err := gpuMigPodPulled.GetLog(nvidiagpu.BurnLogCollectionPeriod, "gpu-burn-ctr")
	Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
		"from gpu burn namespace '%s': %v", burn.Namespace, err)
	glog.V(gpuparams.Gpu10LogLevel).Infof("Gpu-burn pod '%s' with MIG logs:\n%s",
		gpuMigPodPulled.Definition.Name, gpuBurnMigLogs)

	// Need to add checking for other possible GPU's
	By("Parse the gpu-burn pod logs and check for successful execution with MIG")
	for i := 0; i < migCapabilities[useMigIndex].Total; i++ {
		match1Mig := strings.Contains(gpuBurnMigLogs, fmt.Sprintf("GPU %d: OK", i))
		glog.V(gpuparams.Gpu10LogLevel).Infof("Checking if GPU %d: OK is present in logs: %v", i, match1Mig)
		Expect(match1Mig).ToNot(BeFalse(), "gpu-burn pod execution with MIG was FAILED")
	}
	match2Mig := strings.Contains(gpuBurnMigLogs, "100.0%  proc'd:")

	Expect(match2Mig).ToNot(BeFalse(), "gpu-burn pod execution with MIG was FAILED")
	glog.V(gpuparams.Gpu10LogLevel).Infof("Gpu-burn pod execution with MIG configuration was successful")
}
