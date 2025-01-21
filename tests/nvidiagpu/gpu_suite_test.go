package nvidiagpu

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/reporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"runtime"
	"testing"
	"time"

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

	dumpDir := inittools.GeneralConfig.GetDumpFailedTestReportLocation(currentFile)
	if dumpDir != "" {
		artifactDir := fmt.Sprintf("%s/gpu-must-gather", dumpDir)
		if err := reporter.MustGatherIfFailed(specReport, artifactDir, 5*time.Minute); err != nil {
			glog.Errorf("Error running MustGatherIfFailed, %v", err)
		}
	}
})
