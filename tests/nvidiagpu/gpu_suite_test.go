package nvidiagpu

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang/glog"
	"os"
	"os/exec"
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

var _ = JustAfterEach(func() {
	reporter.ReportIfFailed(
		CurrentSpecReport(), currentFile, tsparams.ReporterNamespacesToDump, tsparams.ReporterCRDsToDump, clients.SetScheme)

	dumpDir := inittools.GeneralConfig.GetDumpFailedTestReportLocation(currentFile)
	if dumpDir != "" {
		artifactDir := fmt.Sprintf("ARTIFACT_DIR=%s/gpu-must-gather", dumpDir)
		mustGatherScriptPath := os.Getenv("PATH_TO_MUST_GATHER_SCRIPT")
		if mustGatherScriptPath == "" {
			glog.Error("PATH_TO_MUST_GATHER_SCRIPT environment variable is not set")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, mustGatherScriptPath)
		cmd.Env = append(os.Environ(), artifactDir)
		output, err := cmd.CombinedOutput()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			glog.Errorf("gpu-operator-must-gather.sh script timed out: %v", ctx.Err())
			return
		}
		if err != nil {
			glog.Errorf("Error running gpu-operator-must-gather.sh script: %v\nOutput: %s", err, output)
		} else {
			glog.V(100).Infof("Must-gather script output: %s", output)
		}
	}
})
