package dummy

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type Config struct {
	ReportSuccess bool `envconfig:"DUMMY_REPORT_SUCCESS" default:"true"`
}

var (
	config *Config
)

var _ = Describe("Dummy", Ordered, Label("dummy"), func() {

	Context("Dummy for testing basic CI image functionality without access to a GPU", Label("dummy"), func() {

		config = new(Config)
		err := envconfig.Process("dummy_", config)
		Expect(err).ShouldNot(HaveOccurred())

		success := config.ReportSuccess

		It("Report simple test result", Label(fmt.Sprintf("dummy-test-result:%t", success)), func() {
			Expect(success).To(BeTrue(), "failed according to the value of DUMMY_REPORT_SUCCESS")
		})
	})
})
