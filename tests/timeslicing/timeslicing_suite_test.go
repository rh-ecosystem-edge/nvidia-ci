package timeslicing

import (
	"runtime"
	"testing"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/reporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
)

var _, currentFile, _, _ = runtime.Caller(0)

func TestTimeSlicing(t *testing.T) {
	_, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = inittools.GeneralConfig.GetJunitReportPath(currentFile)

	RegisterFailHandler(Fail)
	RunSpecs(t, "TimeSlicing", Label("nvidia-ci", "timeslicing"), reporterConfig)
}

var _ = JustAfterEach(func() {
	specReport := CurrentSpecReport()
	reporter.ReportIfFailed(
		specReport, currentFile, tsparams.TimeSlicingReporterNamespacesToDump,
		tsparams.TimeSlicingReporterCRDsToDump, clients.SetScheme)
})
