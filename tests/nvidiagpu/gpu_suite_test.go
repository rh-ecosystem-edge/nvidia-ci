package nvidiagpu

import (
	"github.com/golang/glog"
	"os"
	"runtime"
	"testing"
	"time"

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

type mustGatherConfig struct {
	envVar     string
	reportPath string
}

var _ = JustAfterEach(func() {
	specReport := CurrentSpecReport()
	reporter.ReportIfFailed(
		specReport, currentFile, tsparams.ReporterNamespacesToDump, tsparams.ReporterCRDsToDump, clients.SetScheme)

	mustGathers := []mustGatherConfig{
		{envVar: "PATH_TO_NFD_MUST_GATHER_SCRIPT", reportPath: "nfd-must-gather"},
		{envVar: "PATH_TO_GPU_MUST_GATHER_SCRIPT", reportPath: "gpu-must-gather"},
	}

	// Running each must gather script that is in mustGathers
	for _, mg := range mustGathers {
		scriptPath := os.Getenv(mg.envVar)
		if scriptPath == "" {
			continue
		}

		artifactDir := inittools.GeneralConfig.GetReportPath(mg.reportPath)
		if err := reporter.RunMustGather(artifactDir, scriptPath, 5*time.Minute); err != nil {
			glog.Errorf("Failed to collect must-gather for %s (%s): %v", mg.reportPath, mg.envVar, err)
		}
	}
})
