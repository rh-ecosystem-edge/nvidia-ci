package nvidiagpu

import (
	"context"
	"encoding/json"
	"fmt"
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	nvidiagpuv1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/machine"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"strings"
	"time"

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	Nfd                                      = nfd.NewCustomConfig()
	gpuInstallPlanApproval v1alpha1.Approval = "Automatic"

	gpuWorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel: "",
		nvidiagpu.NvidiaGPULabel:            "true",
	}

	gpuBurnImageName = map[string]string{
		"amd64": "quay.io/wabouham/gpu_burn_amd64:ubi9",
		"arm64": "quay.io/wabouham/gpu_burn_arm64:ubi9",
	}

	machineSetNamespace         = "openshift-machine-api"
	replicas              int32 = 1
	workerMachineSetLabel       = "machine.openshift.io/cluster-api-machine-role"

	// NvidiaGPUConfig provides access to general configuration parameters.
	nvidiaGPUConfig  *nvidiagpuconfig.NvidiaGPUConfig
	gpuScaleCluster  = false
	gpuCatalogSource = UndefinedValue

	gpuCustomCatalogSource = UndefinedValue

	createGPUCustomCatalogsource = false

	gpuCustomCatalogsourceIndexImage = UndefinedValue

	gpuSubscriptionChannel        = UndefinedValue
	gpuDefaultSubscriptionChannel = UndefinedValue
	gpuOperatorUpgradeToChannel   = UndefinedValue
	cleanupAfterTest              = true
	deployFromBundle              = false
	gpuOperatorBundleImage        = ""
	gpuCurrentCSV                 = ""
	gpuCurrentCSVVersion          = ""
	clusterArchitecture           = UndefinedValue
)

var _ = Describe("GPU", Ordered, Label(tsparams.LabelSuite), func() {

	var (
		deployBundle deploy.Deploy
	)

	nvidiaGPUConfig = nvidiagpuconfig.NewNvidiaGPUConfig()

	Context("DeployGpu", Label("deploy-gpu-with-dtk"), func() {

		BeforeAll(func() {
			if nvidiaGPUConfig.InstanceType == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE" +
					" is not set, skipping scaling cluster")
				gpuScaleCluster = false

			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_GPU_MACHINESET_INSTANCE_TYPE"+
					" is set to '%s', scaling cluster to add a GPU enabled machineset", nvidiaGPUConfig.InstanceType)
				gpuScaleCluster = true
			}

			if nvidiaGPUConfig.CatalogSource == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_CATALOGSOURCE"+
					" is not set, using default GPU catalogsource '%s'", nvidiagpu.CatalogSourceDefault)
				gpuCatalogSource = nvidiagpu.CatalogSourceDefault
			} else {
				gpuCatalogSource = nvidiaGPUConfig.CatalogSource
				glog.V(gpuparams.GpuLogLevel).Infof("GPU catalogsource now set to env variable "+
					"NVIDIAGPU_CATALOGSOURCE value '%s'", gpuCatalogSource)
			}

			if nvidiaGPUConfig.SubscriptionChannel == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_SUBSCRIPTION_CHANNEL" +
					" is not set, will deploy latest channel")
				gpuSubscriptionChannel = UndefinedValue
			} else {
				gpuSubscriptionChannel = nvidiaGPUConfig.SubscriptionChannel
				glog.V(gpuparams.GpuLogLevel).Infof("GPU Subscription Channel now set to env variable "+
					"NVIDIAGPU_SUBSCRIPTION_CHANNEL value '%s'", gpuSubscriptionChannel)
			}

			if nvidiaGPUConfig.CleanupAfterTest {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_CLEANUP" +
					" is not set or is set to True, will cleanup resources after test case execution")
				cleanupAfterTest = true
			} else {
				cleanupAfterTest = nvidiaGPUConfig.CleanupAfterTest
				glog.V(gpuparams.GpuLogLevel).Infof("Flag to cleanup after test is set to env variable "+
					"NVIDIAGPU_CLEANUP value '%v'", cleanupAfterTest)
			}

			if nvidiaGPUConfig.DeployFromBundle {
				deployFromBundle = nvidiaGPUConfig.DeployFromBundle
				glog.V(gpuparams.GpuLogLevel).Infof("Flag deploy GPU operator from bundle is set to env variable "+
					"NVIDIAGPU_DEPLOY_FROM_BUNDLE value '%v'", deployFromBundle)
				if nvidiaGPUConfig.BundleImage == "" {
					glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_BUNDLE_IMAGE"+
						" is not set, will use the default bundle image '%s'", nvidiagpu.OperatorDefaultMasterBundleImage)
					gpuOperatorBundleImage = nvidiagpu.OperatorDefaultMasterBundleImage
				} else {
					gpuOperatorBundleImage = nvidiaGPUConfig.BundleImage
					glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_BUNDLE_IMAGE"+
						" is set, will use the specified bundle image '%s'", gpuOperatorBundleImage)
				}
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_DEPLOY_FROM_BUNDLE" +
					" is set to false or is not set, will deploy GPU Operator from catalogsource")
				deployFromBundle = false
			}

			if nvidiaGPUConfig.OperatorUpgradeToChannel == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL" +
					" is not set, will not run the Upgrade Testcase")
				gpuOperatorUpgradeToChannel = UndefinedValue
			} else {
				gpuOperatorUpgradeToChannel = nvidiaGPUConfig.OperatorUpgradeToChannel
				glog.V(gpuparams.GpuLogLevel).Infof("GPU Operator Upgrade to channel now set to env variable "+
					"NVIDIAGPU_SUBSCRIPTION_UPGRADE_TO_CHANNEL value '%s'", gpuOperatorUpgradeToChannel)
			}

			if nvidiaGPUConfig.GPUFallbackCatalogsourceIndexImage != "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable "+
					"NVIDIAGPU_GPU_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nvidiaGPUConfig.GPUFallbackCatalogsourceIndexImage)

				gpuCustomCatalogsourceIndexImage = nvidiaGPUConfig.GPUFallbackCatalogsourceIndexImage

				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom GPU operator catalogsource" +
					" from fall back index image to True")

				createGPUCustomCatalogsource = true

				gpuCustomCatalogSource = nvidiagpu.CatalogSourceDefault + "-custom"
				glog.V(gpuparams.GpuLogLevel).Infof("Setting custom GPU catalogsource name to '%s'",
					gpuCustomCatalogSource)

			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom GPU operator catalogsource" +
					" from fall back index image to False")
				createGPUCustomCatalogsource = false
			}

			if nvidiaGPUConfig.NFDFallbackCatalogsourceIndexImage != "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable "+
					"NVIDIAGPU_NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE is set, and has value: '%s'",
					nvidiaGPUConfig.NFDFallbackCatalogsourceIndexImage)

				Nfd.CustomCatalogSourceIndexImage = nvidiaGPUConfig.NFDFallbackCatalogsourceIndexImage

				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom NFD operator catalogsource" +
					" from fall back index image to True")

				Nfd.CreateCustomCatalogsource = true

				Nfd.CustomCatalogSource = nfd.CatalogSourceDefault + "-custom"
				glog.V(gpuparams.GpuLogLevel).Infof("Setting custom NFD catalogsource name to '%s'",
					Nfd.CustomCatalogSource)

			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Setting flag to create custom NFD operator catalogsource" +
					" from fall back index image to False")
				Nfd.CreateCustomCatalogsource = false
			}

			By("Report OpenShift version")
			ocpVersion, err := inittools.GetOpenShiftVersion()
			glog.V(gpuparams.GpuLogLevel).Infof("Current OpenShift cluster version is: '%s'", ocpVersion)

			if err != nil {
				glog.Error("Error getting OpenShift version: ", err)
			} else if err := inittools.GeneralConfig.WriteReport(OpenShiftVersionFile, []byte(ocpVersion)); err != nil {
				glog.Error("Error writing an OpenShift version file: ", err)
			}

			By("Check if NFD is installed")
			nfdInstalled, err := check.NFDDeploymentsReady(inittools.APIClient)

			if nfdInstalled && err == nil {
				glog.V(gpuparams.GpuLogLevel).Infof("The check for ready NFD deployments is: %v", nfdInstalled)
				glog.V(gpuparams.GpuLogLevel).Infof("NFD operators and operands are already installed on " +
					"this cluster")
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("NFD is not currently installed on this cluster")
				glog.V(gpuparams.GpuLogLevel).Infof("Deploying NFD Operator and CR instance on this cluster")

				Nfd.CleanupAfterInstall = true

				By("Check if 'nfd' packagemanifest exists in 'redhat-operators' default catalog")
				nfdPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
					nfd.Package, nfd.CatalogSourceNamespace, nfd.CatalogSourceDefault)

				if nfdPkgManifestBuilderByCatalog == nil {
					glog.V(gpuparams.GpuLogLevel).Infof("NFD packagemanifest was not found in the default '%s'"+
						" catalog.", nfd.CatalogSourceDefault)

					if Nfd.CreateCustomCatalogsource {
						glog.V(gpuparams.GpuLogLevel).Infof("Creating custom catalogsource '%s' for NFD "+
							"catalog.", Nfd.CustomCatalogSource)
						glog.V(gpuparams.GpuLogLevel).Infof("Creating custom catalogsource '%s' for NFD "+
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

						By(fmt.Sprintf("Sleep for %s to allow the NFD custom catalogsource to be created", nvidiagpu.SleepDuration.String()))
						time.Sleep(nvidiagpu.SleepDuration)

						glog.V(gpuparams.GpuLogLevel).Infof("Wait up to %s for custom NFD catalogsource '%s' to be ready", nvidiagpu.WaitDuration.String(), createdNFDCustomCatalogSourceBuilder.Definition.Name)

						Expect(createdNFDCustomCatalogSourceBuilder.IsReady(nvidiagpu.WaitDuration)).NotTo(BeFalse())

						nfdPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalogWithTimeout(inittools.APIClient,
							nfd.Package, nfd.CatalogSourceNamespace, Nfd.CustomCatalogSource, 30*time.Second, 5*time.Minute)

						Expect(err).ToNot(HaveOccurred(), "error getting NFD packagemanifest '%s' "+
							"from custom catalog '%s':  %v", nfd.Package, Nfd.CustomCatalogSource, err)

						Nfd.CatalogSource = Nfd.CustomCatalogSource
						nfdChannel := nfdPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
						glog.V(gpuparams.GpuLogLevel).Infof("NFD channel '%s' retrieved from packagemanifest "+
							"of custom catalogsource '%s'", nfdChannel, Nfd.CustomCatalogSource)

					} else {
						Skip("NFD packagemanifest not found in default 'redhat-operators' catalogsource, " +
							"and flag to deploy custom catalogsource is false")
					}

				} else {
					glog.V(gpuparams.GpuLogLevel).Infof("The nfd packagemanifest '%s' was found in the default"+
						" catalog '%s'", nfdPkgManifestBuilderByCatalog.Object.Name, nfd.CatalogSourceDefault)

					Nfd.CatalogSource = nfd.CatalogSourceDefault
					nfdChannel := nfdPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
					glog.V(gpuparams.GpuLogLevel).Infof("The NFD channel retrieved from packagemanifest is:  %v",
						nfdChannel)

				}

				By("Deploy NFD Operator in NFD namespace")
				err = deploy.CreateNFDNamespace(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating  NFD Namespace: %v", err)

				By("Deploy NFD OperatorGroup in NFD namespace")
				err = deploy.CreateNFDOperatorGroup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating NFD OperatorGroup:  %v", err)

				nfdDeployed := deploy.CreateNFDDeployment(inittools.APIClient, Nfd.CatalogSource,
					nfd.OperatorDeploymentName, nfd.OperatorNamespace, nfd.NFDOperatorCheckInterval,
					nfd.NFDOperatorTimeout, gpuparams.GpuLogLevel)

				if !nfdDeployed {
					By(fmt.Sprintf("Applying workaround for NFD failing to deploy on OCP %s", ocpVersion))
					err = deploy.DeleteNFDSubscription(inittools.APIClient)
					Expect(err).ToNot(HaveOccurred(), "error deleting NFD subscription: %v", err)

					err = deploy.DeleteAnyNFDCSV(inittools.APIClient)
					Expect(err).ToNot(HaveOccurred(), "error deleting NFD CSV: %v", err)

					err = deleteOLMPods(inittools.APIClient)
					Expect(err).ToNot(HaveOccurred(), "error deleting OLM pods for operator cache "+
						"workaround: %v", err)

					glog.V(gpuparams.GpuLogLevel).Info("Re-trying NFD deployment")
					nfdDeployed = deploy.CreateNFDDeployment(inittools.APIClient, Nfd.CatalogSource, nfd.OperatorDeploymentName,
						nfd.OperatorNamespace, nfd.NFDOperatorCheckInterval,
						nfd.NFDOperatorTimeout, gpuparams.GpuLogLevel)
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
				_ = deploy.NFDCRDeleteAndWait(inittools.APIClient, nfd.CRName, nfd.OperatorNamespace, nvidiagpu.DeletionPollInterval, nvidiagpu.DeletionTimeoutDuration)

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

		It("Deploy NVIDIA GPU Operator with DTK", Label("nvidia-ci:gpu"), func() {

			nfd.CheckNfdInstallation(inittools.APIClient, nfd.RhcosLabel, nfd.RhcosLabelValue, inittools.GeneralConfig.WorkerLabelMap, networkparams.LogLevel)

			By("Check if at least one worker node is GPU enabled")
			gpuNodeFound, _ := check.NodeWithLabel(inittools.APIClient, nvidiagpu.NvidiaGPULabel, inittools.GeneralConfig.WorkerLabelMap)

			glog.V(gpuparams.GpuLogLevel).Infof("The check for Nvidia GPU label returned: %v", gpuNodeFound)

			if !gpuNodeFound && !gpuScaleCluster {
				glog.V(gpuparams.GpuLogLevel).Infof("Skipping test:  No GPUs were found on any node and flag " +
					"to scale cluster and add a GPU machineset is set to false")
				Skip("No GPU labeled worker nodes were found and not scaling current cluster")

			} else if !gpuNodeFound && gpuScaleCluster {
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
					if cleanupAfterTest {
						err := pulledMachineSetBuilder.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
					// later add wait for machineset to be deleted
				}()
			}

			// Here we don't need this step is we already have a GPU worker node on cluster
			if gpuScaleCluster {
				glog.V(gpuparams.GpuLogLevel).Infof("Sleeping for %s to allow the newly created GPU worker node to be labeled by NFD", nvidiagpu.NodeLabelingDelay.String())
				time.Sleep(nvidiagpu.NodeLabelingDelay)
			}

			By("Get Cluster Architecture from first GPU enabled worker node")
			glog.V(gpuparams.GpuLogLevel).Infof("Getting cluster architecture from nodes with "+
				"gpuWorkerNodeSelector: %v", gpuWorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, gpuWorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			clusterArchitecture = clusterArch
			glog.V(gpuparams.GpuLogLevel).Infof("cluster architecture for GPU enabled worker node is: %s",
				clusterArchitecture)

			By("Check if GPU Operator Deployment is from Bundle")
			if deployFromBundle {
				glog.V(gpuparams.GpuLogLevel).Infof("Deploying GPU operator from bundle")
				// This returns the Deploy interface object initialized with the API client
				deployBundle = deploy.NewDeploy(inittools.APIClient)
				gpuBundleConfig, err := deployBundle.GetBundleConfig(gpuparams.GpuLogLevel)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.GetBundleConfig %s ", err)
				glog.V(gpuparams.GpuLogLevel).Infof("Extracted env var GPU_BUNDLE_IMAGE"+
					" is '%s'", gpuBundleConfig.BundleImage)

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
							"with index image '%s'", gpuCustomCatalogSource, gpuCustomCatalogsourceIndexImage)

						glog.V(gpuparams.GpuLogLevel).Infof("Deploying a custom GPU catalogsource '%s' with '%s' "+
							"index image", gpuCustomCatalogSource, gpuCustomCatalogsourceIndexImage)

						gpuCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
							gpuCustomCatalogSource, nvidiagpu.CatalogSourceNamespace, gpuCustomCatalogsourceIndexImage,
							nvidiagpu.CustomCatalogSourceDisplayName, nvidiagpu.CustomCatalogSourcePublisherName)

						Expect(gpuCustomCatalogSourceBuilder).NotTo(BeNil(), "Failed to Initialize "+
							"CatalogSourceBuilder for custom GPU catalogsource '%s'", gpuCustomCatalogSource)

						createdGPUCustomCatalogSourceBuilder, err := gpuCustomCatalogSourceBuilder.Create()
						glog.V(gpuparams.GpuLogLevel).Infof("Creating custom GPU Catalogsource builder object "+
							"'%s'", createdGPUCustomCatalogSourceBuilder.Definition.Name)
						Expect(err).ToNot(HaveOccurred(), "error creating custom GPU catalogsource "+
							"builder Object name %s:  %v", gpuCustomCatalogSource, err)

						By(fmt.Sprintf("Sleep for %s to allow the GPU custom catalogsource to be created", nvidiagpu.CatalogSourceCreationDelay))
						time.Sleep(nvidiagpu.CatalogSourceCreationDelay)

						glog.V(gpuparams.GpuLogLevel).Infof("Wait up to %s for custom GPU catalogsource to be ready", nvidiagpu.CatalogSourceReadyTimeout)

						Expect(createdGPUCustomCatalogSourceBuilder.IsReady(nvidiagpu.CatalogSourceReadyTimeout)).NotTo(BeFalse())

						gpuCatalogSource = createdGPUCustomCatalogSourceBuilder.Definition.Name

						glog.V(gpuparams.GpuLogLevel).Infof("Custom GPU catalogsource '%s' is now ready",
							createdGPUCustomCatalogSourceBuilder.Definition.Name)

						gpuPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalogWithTimeout(inittools.APIClient,
							nvidiagpu.Package, nvidiagpu.CatalogSourceNamespace, gpuCustomCatalogSource,
							nvidiagpu.PackageManifestCheckInterval, nvidiagpu.PackageManifestTimeout)

						Expect(err).ToNot(HaveOccurred(), "error getting GPU packagemanifest '%s' "+
							"from custom catalog '%s':  %v", nvidiagpu.Package, gpuCustomCatalogSource, err)

						By("Get the GPU Default Channel from Packagemanifest")
						gpuDefaultSubscriptionChannel = gpuPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
						glog.V(gpuparams.GpuLogLevel).Infof("GPU channel '%s' retrieved from packagemanifest "+
							"of custom catalogsource '%s'", gpuDefaultSubscriptionChannel, gpuCustomCatalogSource)

					} else {
						Skip("gpu-operator-certified packagemanifest not found in default 'certified-operators'" +
							"catalogsource, and flag to deploy custom GPU catalogsource is false")
					}

				} else {
					glog.V(gpuparams.GpuLogLevel).Infof("GPU packagemanifest '%s' was found in the default"+
						" catalog '%s'", gpuPkgManifestBuilderByCatalog.Object.Name, nvidiagpu.CatalogSourceDefault)

					gpuCatalogSource = nvidiagpu.CatalogSourceDefault

					By("Get the GPU Default Channel from Packagemanifest")
					gpuDefaultSubscriptionChannel = gpuPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
					glog.V(gpuparams.GpuLogLevel).Infof("GPU channel '%s' was retrieved from GPU packagemanifest",
						gpuDefaultSubscriptionChannel)
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
				if cleanupAfterTest {
					err := nsBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			// Namespace needed to be created by this point or checked if created
			if deployFromBundle {
				glog.V(gpuparams.GpuLogLevel).Infof("Initializing the kube API Client before deploying bundle")
				deployBundle = deploy.NewDeploy(inittools.APIClient)
				gpuBundleConfig, err := deployBundle.GetBundleConfig(gpuparams.GpuLogLevel)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.GetBundleConfig %s ", err)

				glog.V(gpuparams.GpuLogLevel).Infof("Extracted GPU Operator bundle image from env var "+
					"NVIDIAGPU_BUNDLE_IMAGE '%s'", gpuBundleConfig.BundleImage)

				glog.V(gpuparams.GpuLogLevel).Infof("Deploy the GPU Operator bundle '%s'",
					gpuBundleConfig.BundleImage)
				err = deployBundle.DeployBundle(gpuparams.GpuLogLevel, gpuBundleConfig, nvidiagpu.NvidiaGPUNamespace,
					nvidiagpu.GpuBundleDeploymentTimeout)
				Expect(err).ToNot(HaveOccurred(), "error from deploy.DeployBundle():  '%v' ", err)

				glog.V(gpuparams.GpuLogLevel).Infof("GPU Operator bundle image '%s' deployed successfully "+
					"in namespace '%s", gpuBundleConfig.BundleImage, nvidiagpu.NvidiaGPUNamespace)

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
					if cleanupAfterTest {
						err := ogBuilder.Delete()
						Expect(err).ToNot(HaveOccurred())
					}
				}()

				By("Create Subscription in NVIDIA GPU Operator Namespace")
				subBuilder := olm.NewSubscriptionBuilder(inittools.APIClient, nvidiagpu.SubscriptionName, nvidiagpu.SubscriptionNamespace,
					gpuCatalogSource, nvidiagpu.CatalogSourceNamespace, nvidiagpu.Package)

				if gpuSubscriptionChannel != UndefinedValue {
					glog.V(gpuparams.GpuLogLevel).Infof("Setting the subscription channel to: '%s'",
						gpuSubscriptionChannel)
					subBuilder.WithChannel(gpuSubscriptionChannel)
				} else {
					glog.V(gpuparams.GpuLogLevel).Infof("Setting the subscription channel to default channel: '%s'",
						gpuDefaultSubscriptionChannel)
					subBuilder.WithChannel(gpuDefaultSubscriptionChannel)
				}

				subBuilder.WithInstallPlanApproval(gpuInstallPlanApproval)

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
					if cleanupAfterTest {
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

			gpuCurrentCSV = csvBuilder.Definition.Name
			glog.V(gpuparams.GpuLogLevel).Infof("Deployed ClusterServiceVersion is: '%s", gpuCurrentCSV)

			gpuCurrentCSVVersion = csvBuilder.Definition.Spec.Version.String()
			csvVersionString := gpuCurrentCSVVersion

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
				gpuCurrentCSV)
			err = wait.CSVSucceeded(inittools.APIClient, gpuCurrentCSV, nvidiagpu.NvidiaGPUNamespace,
				nvidiagpu.CsvSucceededCheckInterval, nvidiagpu.CsvSucceededTimeout)
			glog.V(gpuparams.GpuLogLevel).Info("error waiting for ClusterServiceVersion '%s' to be "+
				"in Succeeded phase:  %v ", gpuCurrentCSV, err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterServiceVersion to be "+
				"in Succeeded phase: ", err)

			By("Pull existing CSV in NVIDIA GPU Operator Namespace")
			clusterCSV, err := olm.PullClusterServiceVersion(inittools.APIClient, gpuCurrentCSV, nvidiagpu.NvidiaGPUNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling CSV from cluster:  %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster lastUpdatedTime is : %v ",
				clusterCSV.Definition.Status.LastUpdateTime)

			glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster Phase is : \"%v\"",
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
			glog.V(gpuparams.GpuLogLevel).Infof("almExamples block from clusterCSV  is : %v ", almExamples)

			By("Deploy ClusterPolicy")
			glog.V(gpuparams.GpuLogLevel).Infof("Creating ClusterPolicy from CSV almExamples")
			clusterPolicyBuilder := nvidiagpu.NewBuilderFromObjectString(inittools.APIClient, almExamples)
			createdClusterPolicyBuilder, err := clusterPolicyBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "Error Creating ClusterPolicy from csv "+
				"almExamples  %v ", err)
			glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy '%s' is successfully created",
				createdClusterPolicyBuilder.Definition.Name)

			defer func() {
				if cleanupAfterTest {
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
			gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, nvidiagpu.BurnNamespace)
			if gpuBurnNsBuilder.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("The namespace '%s' already exists",
					gpuBurnNsBuilder.Object.Name)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating the gpu burn namespace '%s'",
					nvidiagpu.BurnNamespace)
				createdGPUBurnNsBuilder, err := gpuBurnNsBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
					"namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)

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
				if cleanupAfterTest {
					err := gpuBurnNsBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Deploy GPU Burn configmap in test-gpu-burn namespace")
			gpuBurnConfigMap, err := gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, nvidiagpu.BurnConfigmapName,
				nvidiagpu.BurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("The created gpuBurnConfigMap has name: %s",
				gpuBurnConfigMap.Name)

			configmapBuilder, err := configmap.Pull(inittools.APIClient, nvidiagpu.BurnConfigmapName, nvidiagpu.BurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
				"namespace '%s': %v", nvidiagpu.BurnConfigmapName, nvidiagpu.BurnNamespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The pulled gpuBurnConfigMap has name: %s",
				configmapBuilder.Definition.Name)

			defer func() {
				if cleanupAfterTest {
					err := configmapBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By("Deploy gpu-burn pod in test-gpu-burn namespace")
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod image name is: '%s', in namespace '%s'",
				gpuBurnImageName[clusterArchitecture], nvidiagpu.BurnNamespace)

			gpuBurnPod, err := gpuburn.CreateGPUBurnPod(inittools.APIClient, nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace,
				gpuBurnImageName[(clusterArchitecture)], nvidiagpu.BurnPodCreationTimeout)
			Expect(err).ToNot(HaveOccurred(), "Error creating gpu burn pod: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Creating gpu-burn pod '%s' in namespace '%s'",
				nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace)

			_, err = inittools.APIClient.Pods(gpuBurnPod.Namespace).Create(context.TODO(), gpuBurnPod,
				metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred(), "Error creating gpu-burn '%s' in "+
				"namespace '%s': %v", nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The created gpuBurnPod has name: %s has status: %v ",
				gpuBurnPod.Name, gpuBurnPod.Status)

			By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
			gpuPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, nvidiagpu.BurnNamespace, nvidiagpu.BurnPodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod with label "+
				"'app=gpu-burn-app' from namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", gpuPodName)

			By("Pull the gpu-burn pod object from the cluster")
			gpuPodPulled, err := pod.Pull(inittools.APIClient, gpuPodName, nvidiagpu.BurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling gpu-burn pod from "+
				"namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)

			By("Cleanup gpu-burn pod only if cleanupAfterTest is true and gpuOperatorUpgradeToChannel is undefined")
			defer func() {
				if cleanupAfterTest && gpuOperatorUpgradeToChannel == UndefinedValue {
					_, err := gpuPodPulled.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By(fmt.Sprintf("Wait for up to %s for gpu-burn pod to be in Running phase", nvidiagpu.BurnPodRunningTimeout))
			err = gpuPodPulled.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.BurnPodRunningTimeout)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod in "+
				"namespace '%s' to go to Running phase:  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Running phase")

			By(fmt.Sprintf("Wait for up to %s for gpu-burn pod to run to completion and be in Succeeded phase/Completed status", nvidiagpu.BurnPodSuccessTimeout))
			err = gpuPodPulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.BurnPodSuccessTimeout)

			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' in "+
				"namespace '%s'to go Succeeded phase/Completed status:  %v ", nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Succeeded Phase/Completed status")

			By("Get the gpu-burn pod logs")
			glog.V(gpuparams.GpuLogLevel).Infof("Get the gpu-burn pod logs")

			gpuBurnLogs, err := gpuPodPulled.GetLog(nvidiagpu.BurnLogCollectionPeriod, "gpu-burn-ctr")

			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
				"from gpu burn namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod '%s' logs:\n%s",
				gpuPodPulled.Definition.Name, gpuBurnLogs)

			By("Parse the gpu-burn pod logs and check for successful execution")
			match1 := strings.Contains(gpuBurnLogs, "GPU 0: OK")
			match2 := strings.Contains(gpuBurnLogs, "100.0%  proc'd:")

			Expect(match1 && match2).ToNot(BeFalse(), "gpu-burn pod execution was FAILED")
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod execution was successful")

		})

		It("Upgrade NVIDIA GPU Operator", Label("operator-upgrade"), func() {

			if gpuOperatorUpgradeToChannel == UndefinedValue {
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

			pulledSubBuilder.Definition.Spec.Channel = gpuOperatorUpgradeToChannel
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
			currentGpuBurnPodPulled, err := pod.Pull(inittools.APIClient, nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling previously deployed and completed "+
				"gpu-burn pod from namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)

			By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
			currentGpuBurnPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, nvidiagpu.BurnNamespace,
				nvidiagpu.BurnPodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting previously deployed gpu-burn pod "+
				"with label 'app=gpu-burn-app' from namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", currentGpuBurnPodName)

			By("Delete the previously deployed gpu-burn-pod")
			glog.V(gpuparams.GpuLogLevel).Infof("Deleting previously deployed and completed gpu-burn pod")

			_, err = currentGpuBurnPodPulled.Delete()
			Expect(err).ToNot(HaveOccurred(), "Error deleting gpu-burn pod")

			By("Re-deploy gpu-burn pod in test-gpu-burn namespace")
			glog.V(gpuparams.GpuLogLevel).Infof("Re-deployed gpu-burn pod image name is: '%s', in "+
				"namespace '%s'", gpuBurnImageName[clusterArchitecture], nvidiagpu.BurnNamespace)

			By("Get Cluster Architecture from first GPU enabled worker node")
			glog.V(gpuparams.GpuLogLevel).Infof("Getting cluster architecture from nodes with "+
				"gpuWorkerNodeSelector: %v", gpuWorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, gpuWorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)

			glog.V(gpuparams.GpuLogLevel).Infof("cluster architecture for GPU enabled worker node is: %s",
				clusterArch)

			gpuBurnPod2, err := gpuburn.CreateGPUBurnPod(inittools.APIClient, nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace,
				gpuBurnImageName[(clusterArch)], nvidiagpu.BurnPodPostUpgradeCreationTimeout)
			Expect(err).ToNot(HaveOccurred(), "Error re-building gpu burn pod object after "+
				"upgrade: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Re-deploying gpu-burn pod '%s' in namespace '%s'",
				nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace)

			_, err = inittools.APIClient.Pods(nvidiagpu.BurnNamespace).Create(context.TODO(), gpuBurnPod2,
				metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred(), "Error re-deploying gpu-burn '%s' after operator"+
				" upgrade in namespace '%s': %v", nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The re-deployed post upgrade gpuBurnPod has name: %s has "+
				"status: %v ", gpuBurnPod2.Name, gpuBurnPod2.Status)

			By("Get the re-deployed gpu-burn pod with label \"app=gpu-burn-app\"")
			gpuBurnPod2Name, err := get.GetFirstPodNameWithLabel(inittools.APIClient, nvidiagpu.BurnNamespace, nvidiagpu.BurnPodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting re-deployed gpu-burn pod with label "+
				"'app=gpu-burn-app' from namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", gpuBurnPod2Name)

			By("Pull the re-created gpu-burn pod object from the cluster")
			gpuBurnPod2Pulled, err := pod.Pull(inittools.APIClient, gpuBurnPod2.Name, nvidiagpu.BurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling re-deployed gpu-burn pod from "+
				"namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)

			defer func() {
				if cleanupAfterTest {
					_, err := gpuBurnPod2Pulled.Delete()
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			By(fmt.Sprintf("Wait for up to %s for re-deployed burn pod to be in Running phase", nvidiagpu.RedeployedBurnPodRunningTimeout))
			err = gpuBurnPod2Pulled.WaitUntilInStatus(corev1.PodRunning, nvidiagpu.RedeployedBurnPodRunningTimeout)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for re-deployed gpu-burn pod in "+
				"namespace '%s' to go to Running phase:  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Running phase")

			By(fmt.Sprintf("Wait for up to %s for re-deployed burn pod to run to completion and be in Succeeded phase/Completed status", nvidiagpu.RedeployedBurnPodSuccessTimeout))
			err = gpuBurnPod2Pulled.WaitUntilInStatus(corev1.PodSucceeded, nvidiagpu.RedeployedBurnPodSuccessTimeout)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' in "+
				"namespace '%s'to go Succeeded phase/Completed status:  %v ", nvidiagpu.BurnPodName, nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Succeeded Phase/Completed status")

			By("Get the gpu-burn pod logs")
			glog.V(gpuparams.GpuLogLevel).Infof("Get the re-created gpu-burn pod logs")

			gpuBurnPod2Logs, err := gpuBurnPod2Pulled.GetLog(nvidiagpu.RedeployedBurnLogCollectionPeriod, "gpu-burn-ctr")

			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
				"from gpu burn namespace '%s' :  %v ", nvidiagpu.BurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod '%s' logs:\n%s",
				gpuBurnPod2Pulled.Definition.Name, gpuBurnPod2Logs)

			By("Parse the re-created gpu-burn pod logs and check for successful execution")
			match1a := strings.Contains(gpuBurnPod2Logs, "GPU 0: OK")
			match2a := strings.Contains(gpuBurnPod2Logs, "100.0%  proc'd:")

			Expect(match1a && match2a).ToNot(BeFalse(), "Re-deployed gpu-burn pod execution was FAILED")
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod execution was successful")

		})

	})
})

func deleteOLMPods(apiClient *clients.Settings) error {

	olmNamespace := "openshift-operator-lifecycle-manager"
	glog.V(gpuparams.GpuLogLevel).Info("Deleting catalog operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=catalog-operator"}); err != nil {
		return err
	}

	glog.V(gpuparams.GpuLogLevel).Info("Deleting OLM operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=olm-operator"}); err != nil {
		return err
	}

	return nil
}
