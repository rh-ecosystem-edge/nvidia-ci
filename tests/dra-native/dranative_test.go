package dranative

import (
	"context"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/dranativeconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	internalnfd "github.com/rh-ecosystem-edge/nvidia-ci/internal/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/mig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfdcheck"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"
)

// smiExecTimeout bounds each 'nvidia-smi' pod-exec call in the SMI validation step below.
const smiExecTimeout = 1 * time.Minute

var (
	nfdInstance = operatorconfig.NewCustomConfig()

	dranativeCfg *dranativeconfig.DRANativeConfig
	nfdConfig    *internalnfd.NFDConfig

	cleanupAfterTest = true

	// installResult and nvidiaDriverName are populated by the "Deploy..." It below and
	// consumed by AfterAll's cleanupDRANativeResources; they stay nil/empty if the It is
	// skipped before reaching the corresponding step.
	installResult    *deploy.OLMInstallResult
	nvidiaDriverName string
)

var _ = Describe("DRA Native", Ordered, Label("dra", "dra-native"), func() {
	BeforeAll(func() {
		dranativeCfg = dranativeconfig.NewDRANativeConfig()
		Expect(dranativeCfg).ToNot(BeNil(), "Failed to load DRANativeConfig")

		cleanupAfterTest = dranativeCfg.CleanupAfterTest

		var err error
		nfdConfig, err = internalnfd.NewNFDConfig()
		Expect(err).ToNot(HaveOccurred(), "Failed to load NFDConfig: %v", err)

		if nfdConfig.FallbackCatalogSourceIndexImage != "" {
			nfdInstance.CustomCatalogSourceIndexImage = nfdConfig.FallbackCatalogSourceIndexImage
			nfdInstance.CreateCustomCatalogsource = true
			nfdInstance.CustomCatalogSource = nfd.CatalogSourceDefault + "-custom"
		}

		By("Report OpenShift version")

		ocpVersion, err := inittools.GetOpenShiftVersion()
		if err != nil {
			glog.Errorf("Error getting OpenShift version: %v", err)
		}

		By("Ensure NFD is installed")
		nfd.EnsureNFDIsInstalled(inittools.APIClient, nfdInstance, ocpVersion, gpuparams.GpuLogLevel)
	})

	AfterAll(func() {
		if nfdInstance.CleanupAfterInstall && cleanupAfterTest {
			err := nfd.Cleanup(inittools.APIClient)
			Expect(err).ToNot(HaveOccurred(), "Error cleaning up NFD resources: %v", err)
		}

		if cleanupAfterTest {
			cleanupDRANativeResources()
		}
	})

	It("Deploy NVIDIA GPU Operator with native DRA (NVIDIADriver/GPUCluster)",
		Label("nvidia-ci", "dra-native"), func() {
			nfdcheck.CheckNfdInstallation(inittools.APIClient, nfd.OSLabel, nfd.GetAllowedOSLabels(),
				inittools.GeneralConfig.WorkerLabelMap, gpuparams.GpuLogLevel)

			By("Check if at least one worker node is GPU enabled")

			gpuNodeFound, err := check.NodeWithLabel(inittools.APIClient, nvidiagpu.NvidiaGPULabel,
				inittools.GeneralConfig.WorkerLabelMap)
			Expect(err).ToNot(HaveOccurred(), "error checking for GPU labeled worker nodes: %v", err)

			if !gpuNodeFound {
				Skip("No GPU labeled worker nodes were found")
			}

			catalogSource := nvidiagpu.CatalogSourceDefault
			if dranativeCfg.CatalogSource != "" {
				catalogSource = dranativeCfg.CatalogSource
			}

			By("Resolve GPU Operator subscription channel")

			channel := dranativeCfg.SubscriptionChannel
			if channel == "" {
				pkgManifest, err := olm.PullPackageManifestByCatalog(inittools.APIClient, nvidiagpu.Package,
					nvidiagpu.CatalogSourceNamespace, catalogSource)
				Expect(err).ToNot(HaveOccurred(), "error pulling GPU packagemanifest '%s' from catalog '%s': %v",
					nvidiagpu.Package, catalogSource, err)

				channel = pkgManifest.Object.Status.DefaultChannel
			}

			By("Deploy GPU Operator via OLM")

			result, err := deploy.InstallOperatorFromCatalog(inittools.APIClient, gpuparams.GpuLogLevel, deploy.OLMInstallConfig{
				Namespace: nvidiagpu.NvidiaGPUNamespace,
				NamespaceLabels: map[string]string{
					"openshift.io/cluster-monitoring":    "true",
					"pod-security.kubernetes.io/enforce": "privileged",
				},
				OperatorGroupName:      nvidiagpu.OperatorGroupName,
				SubscriptionName:       nvidiagpu.SubscriptionName,
				PackageName:            nvidiagpu.Package,
				CatalogSource:          catalogSource,
				CatalogSourceNamespace: nvidiagpu.CatalogSourceNamespace,
				Channel:                channel,
				InstallPlanApproval:    operatorsv1alpha1.Approval("Automatic"),
				DeploymentName:         nvidiagpu.OperatorDeployment,
			})
			installResult = result
			Expect(err).ToNot(HaveOccurred(), "error installing GPU Operator: %v", err)

			By("Verify installed GPU Operator supports native DRA (GPUCluster CRD served)")

			gpuClusterCRDServed, err := nvidiagpu.IsGPUClusterCRDServed(inittools.APIClient)
			Expect(err).ToNot(HaveOccurred(), "error checking GPUCluster CRD availability: %v", err)

			if !gpuClusterCRDServed {
				Skip("Installed GPU Operator version does not serve the GPUCluster CRD " +
					"(requires GPU Operator >= 26.7.0); skipping native DRA test")
			}

			By("Deploy minimal NVIDIADriver")

			nvidiaDriverBuilder := nvidiagpu.NewNVIDIADriverBuilderFromObjectString(inittools.APIClient, result.AlmExamples)
			createdNVIDIADriverBuilder, err := nvidiaDriverBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating NVIDIADriver from alm-examples: %v", err)

			nvidiaDriverName = createdNVIDIADriverBuilder.Definition.Name

			By("Wait for NVIDIADriver to be ready")

			err = wait.NVIDIADriverReady(inittools.APIClient, nvidiaDriverName,
				nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
			Expect(err).ToNot(HaveOccurred(), "error waiting for NVIDIADriver '%s' to be ready: %v", nvidiaDriverName, err)

			By("Deploy minimal GPUCluster")

			gpuClusterBuilder := nvidiagpu.NewGPUClusterBuilderFromObjectString(inittools.APIClient, result.AlmExamples)
			_, err = gpuClusterBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating GPUCluster from alm-examples: %v", err)

			By("Wait for GPUCluster to be ready")

			err = wait.GPUClusterReady(inittools.APIClient, nvidiagpu.GPUClusterName,
				nvidiagpu.ClusterPolicyReadyCheckInterval, nvidiagpu.ClusterPolicyReadyTimeout)
			Expect(err).ToNot(HaveOccurred(), "error waiting for GPUCluster to be ready: %v", err)

			By("Verify GPU functionality via nvidia-smi on driver pods")

			driverPods, err := inittools.APIClient.Pods(nvidiagpu.NvidiaGPUNamespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/component=nvidia-driver",
			})
			Expect(err).ToNot(HaveOccurred(), "error listing NVIDIA driver pods: %v", err)
			Expect(driverPods.Items).ToNot(BeEmpty(), "No NVIDIA driver pods found in namespace %s",
				nvidiagpu.NvidiaGPUNamespace)

			for _, driverPod := range driverPods.Items {
				glog.V(gpuparams.GpuLogLevel).Infof("Executing nvidia-smi on driver pod %s", driverPod.Name)

				output, err := mig.ExecCmdInPod(inittools.APIClient, driverPod.Name, nvidiagpu.NvidiaGPUNamespace,
					[]string{"nvidia-smi"}, smiExecTimeout)
				Expect(err).ToNot(HaveOccurred(), "error executing nvidia-smi on pod %s: %v", driverPod.Name, err)
				Expect(output).ToNot(BeEmpty(), "nvidia-smi output is empty from pod %s", driverPod.Name)

				glog.V(gpuparams.GpuLogLevel).Infof("nvidia-smi output from pod %s:\n%s", driverPod.Name, output)
			}
		})
})

// cleanupDRANativeResources tears down every resource this suite may have created: GPUCluster,
// NVIDIADriver, and (via installResult) the CSV/Subscription/OperatorGroup/Namespace created by
// deploy.InstallOperatorFromCatalog. It tolerates any of these never having been created (e.g.
// because the It was skipped early).
func cleanupDRANativeResources() {
	By("Deleting GPUCluster")

	if gpuClusterBuilder, err := nvidiagpu.PullGPUCluster(inittools.APIClient, nvidiagpu.GPUClusterName); err == nil {
		if _, err := gpuClusterBuilder.Delete(); err != nil {
			glog.Errorf("Error deleting GPUCluster: %v", err)
		}
	}

	By("Deleting NVIDIADriver")

	if nvidiaDriverName != "" {
		if nvidiaDriverBuilder, err := nvidiagpu.PullNVIDIADriver(inittools.APIClient, nvidiaDriverName); err == nil {
			if _, err := nvidiaDriverBuilder.Delete(); err != nil {
				glog.Errorf("Error deleting NVIDIADriver: %v", err)
			}
		}
	}

	if installResult == nil {
		return
	}

	By("Deleting CSV")

	if installResult.CSVBuilder != nil {
		if err := installResult.CSVBuilder.Delete(); err != nil {
			glog.Errorf("Error deleting CSV: %v", err)
		}
	}

	By("Deleting Subscription")

	if installResult.SubscriptionBuilder != nil {
		if err := installResult.SubscriptionBuilder.Delete(); err != nil {
			glog.Errorf("Error deleting Subscription: %v", err)
		}
	}

	By("Deleting OperatorGroup")

	if installResult.OperatorGroupBuilder != nil {
		if err := installResult.OperatorGroupBuilder.Delete(); err != nil {
			glog.Errorf("Error deleting OperatorGroup: %v", err)
		}
	}

	By("Deleting GPU Operator Namespace")

	if installResult.NamespaceBuilder != nil {
		if err := installResult.NamespaceBuilder.Delete(); err != nil {
			glog.Errorf("Error deleting namespace: %v", err)
		}
	}
}
