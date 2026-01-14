package mig

import (
//	"flag"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/mig"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
)

var (
	nfdInstance = operatorconfig.NewCustomConfig()
	burn        = nvidiagpu.NewDefaultGPUBurnConfig()

	WorkerNodeSelector = map[string]string{
		inittools.GeneralConfig.WorkerLabel: "",
		nvidiagpu.NvidiaGPULabel:            "true",
	}

	BurnImageName = map[string]string{
		"amd64": "quay.io/wabouham/gpu_burn_amd64:ubi9",
		"arm64": "quay.io/wabouham/gpu_burn_arm64:ubi9",
	}

	// NvidiaGPUConfig provides access to general configuration parameters.
	nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig

	ScaleCluster        = false
	UseSingleMIGProfile = false
	UseMixedMIGProfile  = false
	SingleMigProfile    = UndefinedValue
	MixedMigProfile     = UndefinedValue

	cleanupAfterTest = false
)

// var (
// 	testDelay    int
// )

// func init() {
// 	// Register flags before Ginkgo parses them
// 	flag.IntVar(&testDelay, "test-delay", 0, "delay in seconds between pod creation on mixed-mig testcase")
// }


var _ = Describe("MIG", Ordered, Label(tsparams.LabelSuite), func() {

	Context("MIG Test Cases", Label("mig-test-cases"), func() {

		BeforeAll(func() {
			glog.V(gpuparams.Gpu10LogLevel).Infof("Start of the test case, BeforeAll")
			nvidiaGPUConfig = nvidiagpuconfig.NewNvidiaGPUConfig()
			Expect(nvidiaGPUConfig).ToNot(BeNil(), "Failed to initialize NvidiaGPUConfig")

			cleanupAfterTest = nvidiaGPUConfig.CleanupAfterTest
			By("Report OpenShift version")
			ReportOpenShiftVersionAndEnsureNFD(nfdInstance)
		})

		BeforeEach(func() {
			glog.V(gpuparams.Gpu100LogLevel).Infof("BeforeEach")
			glog.V(0).Infof("Verboselevel: %s GPUloglevel: %d",
				inittools.GeneralConfig.VerboseLevel, gpuparams.GpuLogLevel)
		})

		AfterEach(func() {
			glog.V(gpuparams.Gpu100LogLevel).Infof("AfterEach")
		})

		AfterAll(func() {
			glog.V(gpuparams.Gpu10LogLevel).Infof("cleanup in AfterAll")
			if nfdInstance.CleanupAfterInstall && cleanupAfterTest {
				err := nfd.Cleanup(inittools.APIClient)
				Expect(err).ToNot(HaveOccurred(), "Error cleaning up NFD resources: %v", err)
			}
			// Cleanup GPU Operator Resources
			mig.CleanupGPUOperatorResources(cleanupAfterTest, burn.Namespace)
		})

		It("Test GPU workload with single strategy MIG Configuration", Label("single-mig"), func() {
			// Skip if single-mig label is not in the ginkgo label filter
			if !mig.IsLabelInFilter("single-mig") {
				glog.V(gpuparams.GpuLogLevel).Infof("Skipping test: 'single-mig' label not present in ginkgo label filter")
				Skip("Test skipped: 'single-mig' label not present in ginkgo label filter")
			}
			mig.TestSingleMIGGPUWorkload(nvidiaGPUConfig, burn, BurnImageName, WorkerNodeSelector, cleanupAfterTest)
		})

		It("Test GPU workload with mixed strategy MIG Configuration", Label("mixed-mig"), func() {
			// Skip if mixed-mig label is not in the ginkgo label filter
			if !mig.IsLabelInFilter("mixed-mig") {
				glog.V(gpuparams.GpuLogLevel).Infof("Skipping test: 'mixed-mig' label not present in ginkgo label filter")
				Skip("Test skipped: 'mixed-mig' label not present in ginkgo label filter")
			}
			mig.TestMixedMIGGPUWorkload(nvidiaGPUConfig, burn, BurnImageName, WorkerNodeSelector, cleanupAfterTest)
		})
	})
})

// reportOpenShiftVersionAndEnsureNFD reports the OpenShift version, writes it to a report file,
// and ensures that Node Feature Discovery (NFD) is installed.
func ReportOpenShiftVersionAndEnsureNFD(nfdInstance *operatorconfig.CustomConfig) {
	glog.V(gpuparams.Gpu10LogLevel).Infof("Report OpenShift version and ensure NFD")
	ocpVersion, err := inittools.GetOpenShiftVersion()
	glog.V(gpuparams.GpuLogLevel).Infof("Current OpenShift cluster version is: '%s'", ocpVersion)

	if err != nil {
		glog.Error("Error getting OpenShift version: ", err)
	} else if err := inittools.GeneralConfig.WriteReport(OpenShiftVersionFile, []byte(ocpVersion)); err != nil {
		glog.Error("Error writing an OpenShift version file: ", err)
	}

	nfd.EnsureNFDIsInstalled(inittools.APIClient, nfdInstance, ocpVersion, gpuparams.GpuLogLevel)
}
