package nvidianetwork

import (
	"runtime"
	"testing"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/reporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _, currentFile, _, _ = runtime.Caller(0)

func TestNNODeploy(t *testing.T) {
	_, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = inittools.GeneralConfig.GetJunitReportPath(currentFile)

	RegisterFailHandler(Fail)
	RunSpecs(t, "NNO", Label(tsparams.NetworkLabels...), reporterConfig)
}

var _ = JustAfterEach(func() {
	if err := writeNNOTestStepResult(); err != nil {
		glog.Errorf("failed to write NNO step result artifact: %v", err)
	}
	reporter.ReportIfFailed(
		CurrentSpecReport(), currentFile, tsparams.NetworkReporterNamespacesToDump, tsparams.NetworkReporterCRDsToDump,
		clients.SetScheme)
})
