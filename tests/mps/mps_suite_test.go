package mps

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/golang/glog"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/reporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _, currentFile, _, _ = runtime.Caller(0)

func TestMPS(t *testing.T) {
	_, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = inittools.GeneralConfig.GetJunitReportPath(currentFile)

	RegisterFailHandler(Fail)
	RunSpecs(t, "MPS", Label("nvidia-ci", "mps"), reporterConfig)
}

var _ = JustAfterEach(func() {
	specReport := CurrentSpecReport()
	reporter.ReportIfFailed(
		specReport, currentFile, map[string]string{
			"nvidia-gpu-operator": "gpu-operator",
			"test-mps":            "mps-test",
		}, nil, clients.SetScheme)

	dumpDir := inittools.GeneralConfig.GetDumpFailedTestReportLocation(currentFile)
	if dumpDir != "" {
		artifactDir := fmt.Sprintf("%s/mps-must-gather", dumpDir)
		if err := reporter.MustGatherIfFailed(specReport, artifactDir, 5*time.Minute); err != nil {
			glog.Errorf("Error running MustGatherIfFailed, %v", err)
		}
	}
})
