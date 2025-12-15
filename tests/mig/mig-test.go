package mig

import (
	// "context"
	// "encoding/json"
	// "fmt"
	// "math/rand"
	// "strings"
	// "time"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"

	// "github.com/rh-ecosystem-edge/nvidia-ci/internal/nfd"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/mig"

	// "github.com/rh-ecosystem-edge/nvidia-ci/pkg/mig"
	nfd "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nfd"

	// "github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	// "github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"

	// "github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	// "github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"

	// "github.com/operator-framework/api/pkg/operators/v1alpha1"
	// "github.com/rh-ecosystem-edge/nvidia-ci/internal/check"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	// "github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	// corev1 "k8s.io/api/core/v1"
)

var (
	nfdInstance = operatorconfig.NewCustomConfig()
	burn        = nvidiagpu.NewDefaultGPUBurnConfig()

	// InstallPlanApproval v1alpha1.Approval = "Automatic"

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
	// nfdConfig       *internalNFD.NFDConfig

	ScaleCluster        = false
	UseSingleMIGProfile = false
	UseMixedMIGProfile  = false
	SingleMigProfile    = UndefinedValue
	MixedMigProfile     = UndefinedValue

	cleanupAfterTest = true
	// CurrentCSV        = ""
	// CurrentCSVVersion = ""
)

var _ = Describe("MIG", Ordered, Label(tsparams.LabelSuite), func() {

	Context("MIG Test Cases", Label("mig-test-cases"), func() {

		BeforeAll(func() {
			glog.V(gpuparams.Gpu10LogLevel).Infof("Start of the test case, BeforeAll")
			nvidiaGPUConfig = nvidiagpuconfig.NewNvidiaGPUConfig()

			// nfdConfig, _ = internalNFD.NewNFDConfig()
			cleanupAfterTest = nvidiaGPUConfig.CleanupAfterTest
			By("Report OpenShift version")
			mig.ReportOpenShiftVersionAndEnsureNFD(nfdInstance)
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

		It("Test GPU Burn with single strategy MIG Configuration", Label("gpu-burn-mig"), func() {
			mig.TestSingleMIGGPUBurn(nvidiaGPUConfig, burn, BurnImageName, WorkerNodeSelector, cleanupAfterTest)
		})

		It("Test GPU Burn with mixed strategy MIG Configuration", Label("gpu-mixed-mig"), func() {
			glog.V(gpuparams.Gpu10LogLevel).Infof("gpu-mixed-mig testcase not yet implemented")
			// mig.TestMixedMIGGPUBurn(nvidiaGPUConfig, burn, BurnImageName, WorkerNodeSelector, cleanupAfterTest)
		})
	})
})
