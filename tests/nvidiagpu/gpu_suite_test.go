package nvidiagpu

import (
	"fmt"
	"github.com/golang/glog"
	"os"
	"os/exec"
	"runtime"
	"testing"

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
	reporter.ReportIfFailed(
		CurrentSpecReport(), currentFile, tsparams.ReporterNamespacesToDump, tsparams.ReporterCRDsToDump, clients.SetScheme)
	dumpDir := inittools.GeneralConfig.GetDumpFailedTestReportLocation(currentFile)
	cmd := exec.Command("./must-gather.sh")
	cmd.Env = append(os.Environ(), "ARTIFACT_DIR="+dumpDir)
	_, err := cmd.CombinedOutput()
	if err != nil {
		glog.V(100).Infof("Error running must-gather.sh script %v", err)
	}
})
