package nvidiagpu

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/golang/glog"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/reporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _, currentFile, _, _ = runtime.Caller(0)

func TestGPUDeploy(t *testing.T) {
	_, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = inittools.GeneralConfig.GetJunitReportPath(currentFile)

	RegisterFailHandler(Fail)
	RunSpecs(t, "GPU", Label(tsparams.Labels...), reporterConfig)
}

var _ = JustAfterEach(func() {
	specReport := CurrentSpecReport()
	reporter.ReportIfFailed(
		specReport, currentFile, tsparams.ReporterNamespacesToDump, tsparams.ReporterCRDsToDump, clients.SetScheme)
})

var _ = AfterSuite(func() {
	scriptPath := os.Getenv("PATH_TO_MUST_GATHER_SCRIPT")
	if scriptPath != "" {
		artifactDir := inittools.GeneralConfig.GetReportPath("gpu-operator-tests-must-gather")
		if err := reporter.RunMustGather(artifactDir, scriptPath, 5*time.Minute); err != nil {
			glog.Errorf("Failed to collect must-gather: %v", err)
		}
	}
})
