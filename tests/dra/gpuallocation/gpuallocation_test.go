package gpuallocation

import (
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/tests/dra/shared"
	"helm.sh/helm/v3/pkg/action"
)

var _ = Describe("DRA Driver Installation", Ordered, Label("dra", "dra-gpu"), func() {
	var actionConfig *action.Configuration

	BeforeAll(func() {
		err := shared.VerifyDRAPrerequisites(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to verify DRA prerequisites")

		By("Verifying device plugin is disabled")
		isEnabled, err := shared.IsDevicePluginEnabled(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to check device plugin state")
		Expect(isEnabled).To(BeFalse(), "Device plugin must be disabled for GPU allocation tests")

		// Create Helm action config once for all operations
		actionConfig, err = shared.NewActionConfig(inittools.APIClient, shared.DRADriverNamespace, gpuparams.GpuLogLevel)
		Expect(err).ToNot(HaveOccurred(), "Failed to create Helm action configuration")

		// For GPU allocation tests, explicitly enable GPU resources
		customValues := shared.NewDRAValues().
			WithGPUResources(true).
			WithGPUResourcesOverride(true)

		err = shared.InstallDRADriver(actionConfig, shared.LatestVersion, customValues)
		Expect(err).ToNot(HaveOccurred(), "Failed to install DRA driver")
	})

	AfterAll(func() {
		By("Cleaning up DRA driver")
		glog.V(gpuparams.GpuLogLevel).Infof("Starting DRA driver cleanup")
		if actionConfig != nil {
			err := shared.UninstallDRADriver(actionConfig)
			Expect(err).ToNot(HaveOccurred(), "Failed to uninstall DRA driver")
		}
		glog.V(gpuparams.GpuLogLevel).Infof("DRA driver cleanup completed successfully")
	})

	Context("When DRA driver is installed", func() {
		It("Should have ResourceClass resources available", func() {
			// This is a placeholder - you'll add actual tests here
			// The driver is already installed in BeforeAll
		})

		// Add more It blocks here to test various DRA functionality
	})
})
