package dummy

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
)

type Config struct {
	ReportSuccess bool `envconfig:"DUMMY_REPORT_SUCCESS" default:"true"`
}

var config *Config

var _ = Describe("Dummy", Ordered, Label("dummy"), func() {

	Context("Dummy for testing basic CI image functionality without access to a GPU", Label("dummy"), func() {

		config = new(Config)
		err := envconfig.Process("dummy_", config)
		Expect(err).ShouldNot(HaveOccurred())

		success := config.ReportSuccess

		It("Report simple test result", Label(fmt.Sprintf("dummy-test-result:%t", success)), func() {
			Expect(success).To(BeTrue(), "failed according to the value of DUMMY_REPORT_SUCCESS")
		})

		ocpVersion, err := inittools.GetOpenShiftVersion()
		if err != nil {
			glog.Error("Error getting OpenShift version: ", err)
		} else if err := inittools.GeneralConfig.WriteReport("ocp.version", []byte(ocpVersion)); err != nil {
			glog.Error("Error writing an OpenShift version file: ", err)
		}
	})
})
