package computedomain

import (
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/reporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
)

var _, currentFile, _, _ = runtime.Caller(0)

func TestComputeDomain(t *testing.T) {
	_, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = inittools.GeneralConfig.GetJunitReportPath(currentFile)

	RegisterFailHandler(Fail)
	RunSpecs(t, "ComputeDomain", Label("dra", "dra-imex"), reporterConfig)
}

var _ = JustAfterEach(func() {
	reporterNamespaces := map[string]string{
		"nvidia-dra-driver-gpu": "dra-driver",
	}

	reporter.ReportIfFailed(
		CurrentSpecReport(), currentFile, reporterNamespaces, nil, clients.SetScheme)
})
