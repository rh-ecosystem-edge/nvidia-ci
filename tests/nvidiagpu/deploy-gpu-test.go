package nvidiagpu

import (
	"context"
	"encoding/json"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"

	"github.com/openshift-kni/eco-goinfra/pkg/machine"
	nfddeploy "github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"

	"strings"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nvidiagpu"
	"github.com/openshift-kni/eco-goinfra/pkg/olm"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"
	gpuburn "github.com/rh-ecosystem-edge/nvidia-ci/internal/gpu-burn"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	gpuInstallPlanApproval v1alpha1.Approval = "Automatic"

	gpuWorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel:           "",
		"feature.node.kubernetes.io/pci-10de.present": "true",
	}

	gpuBurnImageName = map[string]string{
		"amd64": "quay.io/wabouham/gpu_burn_amd64:ubi9",
		"arm64": "quay.io/wabouham/gpu_burn_arm64:ubi9",
	}

	machineSetNamespace         = "openshift-machine-api"
	replicas              int32 = 1
	workerMachineSetLabel       = "machine.openshift.io/cluster-api-machine-role"

	nfdCleanupAfterInstall bool = false

	// NvidiaGPUConfig provides access to general configuration parameters.
	nvidiaGPUConfig        *nvidiagpuconfig.NvidiaGPUConfig
	gpuScaleCluster        bool = false
	gpuCatalogSource            = "undefined"
	gpuSubscriptionChannel      = "undefined"
)

const (
	nfdOperatorNamespace      = "openshift-nfd"
	nfdCatalogSource          = "redhat-operators"
	nfdCatalogSourceNamespace = "openshift-marketplace"
	nfdOperatorDeploymentName = "nfd-controller-manager"
	nfdPackage                = "nfd"
	nfdCRName                 = "nfd-instance"

	nvidiaGPUNamespace        = "nvidia-gpu-operator"
	nfdRhcosLabel             = "feature.node.kubernetes.io/system-os_release.ID"
	nfdRhcosLabelValue        = "rhcos"
	nvidiaGPULabel            = "feature.node.kubernetes.io/pci-10de.present"
	gpuOperatorGroupName      = "gpu-og"
	gpuOperatorDeployment     = "gpu-operator"
	gpuSubscriptionName       = "gpu-subscription"
	gpuSubscriptionNamespace  = "nvidia-gpu-operator"
	gpuCatalogSourceDefault   = "certified-operators"
	gpuCatalogSourceNamespace = "openshift-marketplace"
	gpuPackage                = "gpu-operator-certified"
	gpuClusterPolicyName      = "gpu-cluster-policy"
	gpuBurnNamespace          = "test-gpu-burn"
	gpuBurnPodName            = "gpu-burn-pod"
	gpuBurnPodLabel           = "app=gpu-burn-app"
	gpuBurnConfigmapName      = "gpu-burn-entrypoint"
)

var _ = Describe("GPU", Ordered, Label(tsparams.LabelSuite), func() {

	Context("DeployGpu", Label("deploy-gpu-with-dtk"), func() {

		nvidiaGPUConfig = nvidiagpuconfig.NewNvidiaGPUConfig()

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
					" is not set, using default GPU catalogsource '%s'", gpuCatalogSourceDefault)
				gpuCatalogSource = gpuCatalogSourceDefault
			} else {
				gpuCatalogSource = nvidiaGPUConfig.CatalogSource
				glog.V(gpuparams.GpuLogLevel).Infof("GPU catalogsource now set to env variable "+
					"NVIDIAGPU_CATALOGSOURCE value '%s'", gpuCatalogSource)
			}

			if nvidiaGPUConfig.SubscriptionChannel == "" {
				glog.V(gpuparams.GpuLogLevel).Infof("env variable NVIDIAGPU_SUBSCRIPTION_CHANNEL" +
					" is not set, will deploy latest channel")
				gpuSubscriptionChannel = "undefined"
			} else {
				gpuSubscriptionChannel = nvidiaGPUConfig.SubscriptionChannel
				glog.V(gpuparams.GpuLogLevel).Infof("GPU Subscription Channel now set to env variable "+
					"NVIDIAGPU_SUBSCRIPTION_CHANNEL value '%s'", gpuSubscriptionChannel)
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

				nfdCleanupAfterInstall = true

				By("Check if 'nfd' packagemanifest exists in 'redhat-operators' catalog")
				nfdPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
					nfdPackage, nfdCatalogSourceNamespace, nfdCatalogSource)
				Expect(err).ToNot(HaveOccurred(), "error getting NFD packagemanifest %s "+
					"from catalog %s:  %v", nfdPackage, nfdCatalogSource, err)

				if nfdPkgManifestBuilderByCatalog == nil {
					Skip("NFD packagemanifest not found in catalogsource")
				}

				glog.V(gpuparams.GpuLogLevel).Infof("The nfd packagemanifest name returned: %s",
					nfdPkgManifestBuilderByCatalog.Object.Name)

				nfdChannel := nfdPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
				glog.V(gpuparams.GpuLogLevel).Infof("The NFD channel retrieved from packagemanifest is:  %v",
					nfdChannel)

				By("Deploy NFD Operator in NFD namespace")
				err = nfddeploy.CreateNFDNamespace(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating  NFD Namespace: %v", err)

				By("Deploy NFD OperatorGroup in NFD namespace")
				err = nfddeploy.CreateNFDOperatorGroup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating NFD OperatorGroup:  %v", err)

				By("Deploy NFD Subscription in NFD namespace")
				err = nfddeploy.CreateNFDSubscription(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error creating NFD Subscription:  %v", err)

				By("Sleep for 2 minutes to allow the NFD Operator deployment to be created")
				glog.V(gpuparams.GpuLogLevel).Infof("Sleep for 2 minutes to allow the NFD Operator deployment" +
					" to be created")
				time.Sleep(2 * time.Minute)

				By("Wait up to 4 mins for NFD Operator deployment to be created")
				nfdDeploymentCreated := wait.DeploymentCreated(inittools.APIClient, nfdOperatorDeploymentName, nfdOperatorNamespace,
					30*time.Second, 4*time.Minute)
				Expect(nfdDeploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy "+
					"NFD operator")

				By("Deploy NFD Subscription in NFD namespace")
				nfdDeployed, err := nfddeploy.CheckNFDOperatorDeployed(inittools.APIClient, 120*time.Second)
				Expect(err).ToNot(HaveOccurred(), "error deploying NFD Operator in"+
					" NFD namespace:  %v", err)
				Expect(nfdDeployed).ToNot(BeFalse(), "failed to deploy NFD operator")

				By("Deploy NFD CR instance in NFD namespace")
				err = nfddeploy.DeployCRInstance(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "error deploying NFD CR instance in"+
					" NFD namespace:  %v", err)

			}
		})

		BeforeEach(func() {

		})

		AfterEach(func() {

		})

		AfterAll(func() {

			if nfdCleanupAfterInstall {
				// Here need to check if NFD CR is deployed, otherwise Deleting a non-existing CR will throw an error
				// skipping error check for now cause any failure before entire NFD stack
				By("Delete NFD CR instance in NFD namespace")
				_ = nfddeploy.NFDCRDeleteAndWait(inittools.APIClient, nfdCRName, nfdOperatorNamespace, 30*time.Second, 5*time.Minute)

				By("Delete NFD CSV")
				_ = nfddeploy.DeleteNFDCSV(inittools.APIClient)

				By("Delete NFD Subscription in NFD namespace")
				_ = nfddeploy.DeleteNFDSubscription(inittools.APIClient)

				By("Delete NFD OperatorGroup in NFD namespace")
				_ = nfddeploy.DeleteNFDOperatorGroup(inittools.APIClient)

				By("Delete NFD Namespace in NFD namespace")
				_ = nfddeploy.DeleteNFDNamespace(inittools.APIClient)
			}
		})

		It("Deploy NVIDIA GPU Operator with DTK", Label("nvidia-ci:gpu"), func() {

			By("Check if NFD is installed %s")
			nfdLabelDetected, err := check.AllNodeLabel(inittools.APIClient, nfdRhcosLabel, nfdRhcosLabelValue,
				inittools.GeneralConfig.WorkerLabelMap)

			Expect(err).ToNot(HaveOccurred(), "error calling check.NodeLabel:  %v ", err)
			Expect(nfdLabelDetected).NotTo(BeFalse(), "NFD node label check failed to match "+
				"label %s and label value %s on all nodes", nfdRhcosLabel, nfdRhcosLabelValue)
			glog.V(gpuparams.GpuLogLevel).Infof("The check for NFD label returned: %v", nfdLabelDetected)

			isNfdInstalled, err := check.NFDDeploymentsReady(inittools.APIClient)
			Expect(err).ToNot(HaveOccurred(), "error checking if NFD deployments are ready:  "+
				"%v ", err)
			glog.V(gpuparams.GpuLogLevel).Infof("The check for NFD deployments ready returned: %v",
				isNfdInstalled)

			By("Check if at least one worker node is GPU enabled")
			gpuNodeFound, _ := check.NodeWithLabel(inittools.APIClient, nvidiaGPULabel, inittools.GeneralConfig.WorkerLabelMap)

			glog.V(gpuparams.GpuLogLevel).Infof("The check for Nvidia GPU label returned: %v", gpuNodeFound)

			if !gpuNodeFound && gpuScaleCluster {
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
					machineSetNamespace, 15*time.Minute)

				Expect(err).ToNot(HaveOccurred(), "Failed to detect at least one replica"+
					" of MachineSet %s in Ready state during 15 min polling interval: %v",
					pulledMachineSetBuilder.Definition.ObjectMeta.Name, err)

				defer func() {
					err := pulledMachineSetBuilder.Delete()
					Expect(err).ToNot(HaveOccurred())

					// later add wait for machineset to be deleted
				}()
			}

			By("Sleep for 2 minutes to allow GPU worker node to be labeled by NFD")
			glog.V(gpuparams.GpuLogLevel).Infof("Sleep for 2 minutes to allow the GPU worker nodes" +
				" to be labeled by NFD")
			time.Sleep(2 * time.Minute)

			By("Get Cluster Architecture from first GPU enabled worker node")
			glog.V(gpuparams.GpuLogLevel).Infof("Getting cluster architecture from nodes with "+
				"gpuWorkerNodeSelector: %v", gpuWorkerNodeSelector)
			clusterArch, err := get.GetClusterArchitecture(inittools.APIClient, gpuWorkerNodeSelector)
			Expect(err).ToNot(HaveOccurred(), "error getting cluster architecture:  %v ", err)
			glog.V(gpuparams.GpuLogLevel).Infof("cluster architecture for GPU enabled worker node is: %s",
				clusterArch)

			By("Check if 'gpu-operator-certified' packagemanifest exists in GPU catalog")
			glog.V(gpuparams.GpuLogLevel).Infof("Using GPU catalogsource '%s'", gpuCatalogSource)

			gpuPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(inittools.APIClient,
				gpuPackage, gpuCatalogSourceNamespace, gpuCatalogSource)
			Expect(err).ToNot(HaveOccurred(), "error getting GPU packagemanifest %s from catalog %s:"+
				"  %v", gpuPackage, gpuCatalogSource, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The gpu packagemanifest name returned: %s",
				gpuPkgManifestBuilderByCatalog.Object.Name)

			By("Get the GPU Default Channel from Packagemanifest")
			gpuChannelDefault := gpuPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
			glog.V(gpuparams.GpuLogLevel).Infof("The default gpu channel retrieved from packagemanifest is:  %v",
				gpuChannelDefault)

			By("Check if NVIDIA GPU Operator namespace exists, otherwise created it and label it")
			nsBuilder := namespace.NewBuilder(inittools.APIClient, nvidiaGPUNamespace)
			if nsBuilder.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("The namespace '%s' already exists",
					nsBuilder.Object.Name)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating the namespace:  %v", nvidiaGPUNamespace)
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
				err := nsBuilder.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Create OperatorGroup in NVIDIA GPU Operator Namespace")
			ogBuilder := olm.NewOperatorGroupBuilder(inittools.APIClient, gpuOperatorGroupName, nvidiaGPUNamespace)
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
				err := ogBuilder.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Create Subscription in NVIDIA GPU Operator Namespace")
			subBuilder := olm.NewSubscriptionBuilder(inittools.APIClient, gpuSubscriptionName, gpuSubscriptionNamespace,
				gpuCatalogSource, gpuCatalogSourceNamespace, gpuPackage)

			if gpuSubscriptionChannel != "undefined" {
				glog.V(gpuparams.GpuLogLevel).Infof("Setting the subscription channel to: '%s'",
					gpuSubscriptionChannel)
				subBuilder.WithChannel(gpuSubscriptionChannel)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Setting the subscription channel to default channel: '%s'",
					gpuChannelDefault)
				subBuilder.WithChannel(gpuChannelDefault)
			}

			subBuilder.WithInstallPlanApproval(gpuInstallPlanApproval)

			glog.V(gpuparams.GpuLogLevel).Infof("Creating the subscription, i.e Deploy the GPU operator")
			createdSub, err := subBuilder.Create()

			Expect(err).ToNot(HaveOccurred(), "error creating subscription %v :  %v ",
				createdSub.Definition.Name, err)

			glog.V(gpuparams.GpuLogLevel).Infof("Newly created subscription: %s was successfully created",
				createdSub.Object.Name)

			if createdSub.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("The newly created subscription: %s in namespace: %v "+
					"has current CSV:  %v", createdSub.Object.Name, createdSub.Object.Namespace,
					createdSub.Definition.Status.CurrentCSV)
			}

			defer func() {
				err := createdSub.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Sleep for 2 minutes to allow the GPU Operator deployment to be created")
			glog.V(gpuparams.GpuLogLevel).Infof("Sleep for 2 minutes to allow the GPU Operator deployment" +
				" to be created")
			time.Sleep(2 * time.Minute)

			By("Wait for up to 4 minutes for GPU Operator deployment to be created")
			gpuDeploymentCreated := wait.DeploymentCreated(inittools.APIClient, gpuOperatorDeployment, nvidiaGPUNamespace,
				30*time.Second, 4*time.Minute)
			Expect(gpuDeploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy "+
				"GPU operator")

			By("Check if the GPU operator deployment is ready")
			gpuOperatorDeployment, err := deployment.Pull(inittools.APIClient, gpuOperatorDeployment, nvidiaGPUNamespace)

			Expect(err).ToNot(HaveOccurred(), "Error trying to pull GPU operator "+
				"deployment is: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Pulled GPU operator deployment is:  %v ",
				gpuOperatorDeployment.Definition.Name)

			if gpuOperatorDeployment.IsReady(4 * time.Minute) {
				glog.V(gpuparams.GpuLogLevel).Infof("Pulled GPU operator deployment '%s' is Ready",
					gpuOperatorDeployment.Definition.Name)
			}

			By("Get currentCSV from subscription")
			gpuCurrentCSVFromSub, err := get.CurrentCSVFromSubscription(inittools.APIClient, gpuSubscriptionName,
				gpuSubscriptionNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling currentCSV from cluster:  %v", err)
			Expect(gpuCurrentCSVFromSub).ToNot(BeEmpty())
			glog.V(gpuparams.GpuLogLevel).Infof("currentCSV %s extracted from Subscripion %s",
				gpuCurrentCSVFromSub, gpuSubscriptionName)

			By("Wait for ClusterServiceVersion to be in Succeeded phase")
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterServiceVersion to be Succeeded phase")
			err = wait.CSVSucceeded(inittools.APIClient, gpuCurrentCSVFromSub, nvidiaGPUNamespace, 60*time.Second,
				5*time.Minute)
			glog.V(gpuparams.GpuLogLevel).Infof("error waiting for ClusterServiceVersion to be "+
				"in Succeeded phase:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterServiceVersion to be "+
				"in Succeeded phase:  %v ", err)

			By("Pull existing CSV in NVIDIA GPU Operator Namespace")
			clusterCSV, err := olm.PullClusterServiceVersion(inittools.APIClient, gpuCurrentCSVFromSub, nvidiaGPUNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling CSV %v from cluster:  %v",
				gpuCurrentCSVFromSub, err)

			glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster lastUpdatedTime is : %v ",
				clusterCSV.Definition.Status.LastUpdateTime)

			glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster Phase is : \"%v\"",
				clusterCSV.Definition.Status.Phase)

			succeeded := v1alpha1.ClusterServiceVersionPhase("Succeeded")
			Expect(clusterCSV.Definition.Status.Phase).To(Equal(succeeded), "CSV Phase is not "+
				"succeeded")

			defer func() {
				err := clusterCSV.Delete()
				Expect(err).ToNot(HaveOccurred())
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
			glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy '%' successfully created",
				createdClusterPolicyBuilder.Definition.Name)

			defer func() {
				_, err := createdClusterPolicyBuilder.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Pull the ClusterPolicy just created from cluster, with updated fields")
			pulledClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, gpuClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: "+
				" %v ", gpuClusterPolicyName, err)

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

			By("Wait up to 12 minutes for ClusterPolicy to be ready")
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting for ClusterPolicy to be ready")
			err = wait.ClusterPolicyReady(inittools.APIClient, gpuClusterPolicyName, 60*time.Second, 12*time.Minute)

			glog.V(gpuparams.GpuLogLevel).Infof("error waiting for ClusterPolicy to be Ready:  %v ", err)
			Expect(err).ToNot(HaveOccurred(), "error waiting for ClusterPolicy to be Ready:  %v ",
				err)

			By("Pull the ready ClusterPolicy from cluster, with updated fields")
			pulledReadyClusterPolicy, err := nvidiagpu.Pull(inittools.APIClient, gpuClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error pulling ClusterPolicy %s from cluster: "+
				" %v ", gpuClusterPolicyName, err)

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
			gpuBurnNsBuilder := namespace.NewBuilder(inittools.APIClient, gpuBurnNamespace)
			if gpuBurnNsBuilder.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("The namespace '%s' already exists",
					gpuBurnNsBuilder.Object.Name)
			} else {
				glog.V(gpuparams.GpuLogLevel).Infof("Creating the gpu burn namespace '%s'",
					gpuBurnNamespace)
				createdGPUBurnNsBuilder, err := gpuBurnNsBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
					"namespace '%s' :  %v ", gpuBurnNamespace, err)

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
				err := gpuBurnNsBuilder.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Deploy GPU Burn configmap in test-gpu-burn namespace")
			gpuBurnConfigMap, err := gpuburn.CreateGPUBurnConfigMap(inittools.APIClient, gpuBurnConfigmapName,
				gpuBurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "Error Creating gpu burn configmap: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("The created gpuBurnConfigMap has name: %s",
				gpuBurnConfigMap.Name)

			configmapBuilder, err := configmap.Pull(inittools.APIClient, gpuBurnConfigmapName, gpuBurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "Error pulling gpu-burn configmap '%s' from "+
				"namespace '%s': %v", gpuBurnConfigmapName, gpuBurnNamespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The pulled gpuBurnConfigMap has name: %s",
				configmapBuilder.Definition.Name)

			defer func() {
				err := configmapBuilder.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Deploy gpu-burn pod in test-gpu-burn namespace")
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod image name is: '%s', in namespace '%s'",
				gpuBurnImageName[clusterArch])

			gpuBurnPod, err := gpuburn.CreateGPUBurnPod(inittools.APIClient, gpuBurnPodName, gpuBurnNamespace,
				gpuBurnImageName[(clusterArch)], 5*time.Minute)
			Expect(err).ToNot(HaveOccurred(), "Error creating gpu burn pod: %v", err)

			glog.V(gpuparams.GpuLogLevel).Infof("Creating gpu-burn pod '%s' in namespace '%s'",
				gpuBurnPodName, gpuBurnNamespace)

			_, err = inittools.APIClient.Pods(gpuBurnPod.Namespace).Create(context.TODO(), gpuBurnPod, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred(), "Error creating gpu-burn '%s' in "+
				"namespace '%s': %v", gpuBurnPodName, gpuBurnNamespace, err)

			glog.V(gpuparams.GpuLogLevel).Infof("The created gpuBurnPod has name: %s has status: %v ",
				gpuBurnPod.Name, gpuBurnPod.Status)

			By("Get the gpu-burn pod with label \"app=gpu-burn-app\"")
			gpuPodName, err := get.GetFirstPodNameWithLabel(inittools.APIClient, gpuBurnNamespace, gpuBurnPodLabel)
			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod with label "+
				"'app=gpu-burn-app' from namespace '%s' :  %v ", gpuBurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpuPodName is %s ", gpuPodName)

			By("Pull the gpu-burn pod object from the cluster")
			gpuPodPulled, err := pod.Pull(inittools.APIClient, gpuPodName, gpuBurnNamespace)
			Expect(err).ToNot(HaveOccurred(), "error pulling gpu-burn pod from "+
				"namespace '%s' :  %v ", gpuBurnNamespace, err)

			defer func() {
				_, err := gpuPodPulled.Delete()
				Expect(err).ToNot(HaveOccurred())
			}()

			By("Wait for up to 3 minutes for gpu-burn pod to be in Running phase")
			err = gpuPodPulled.WaitUntilInStatus(corev1.PodRunning, 3*time.Minute)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod in "+
				"namespace '%s' to go to Running phase:  %v ", gpuBurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Running phase")

			By("Wait for gpu-burn pod to run to completion and be in Succeeded phase/Completed status")
			err = gpuPodPulled.WaitUntilInStatus(corev1.PodSucceeded, 8*time.Minute)
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for gpu-burn pod '%s' in "+
				"namespace '%s'to go Succeeded phase/Completed status:  %v ", gpuBurnPodName, gpuBurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("gpu-burn pod now in Succeeded Phase/Completed status")

			By("Get the gpu-burn pod logs")
			glog.V(gpuparams.GpuLogLevel).Infof("Get the gpu-burn pod logs")

			gpuBurnLogs, err := gpuPodPulled.GetLog(500*time.Second, "gpu-burn-ctr")

			Expect(err).ToNot(HaveOccurred(), "error getting gpu-burn pod '%s' logs "+
				"from gpu burn namespace '%s' :  %v ", gpuBurnNamespace, err)
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod '%s' logs:\n%s",
				gpuPodPulled.Definition.Name, gpuBurnLogs)

			By("Parse the gpu-burn pod logs and check for successful execution")
			match1 := strings.Contains(gpuBurnLogs, "GPU 0: OK")
			match2 := strings.Contains(gpuBurnLogs, "100.0%  proc'd:")

			Expect(match1 && match2).ToNot(BeFalse(), "gpu-burn pod execution was FAILED")
			glog.V(gpuparams.GpuLogLevel).Infof("Gpu-burn pod execution was successful")
		})
	})
})
