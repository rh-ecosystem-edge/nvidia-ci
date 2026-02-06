package nfdcheck

import (
	"fmt"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Dot import is standard for Ginkgo tests
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Dot import is standard for Gomega assertions
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
)

func CheckNfdInstallation(apiClient *clients.Settings, label string, allowedLabelValues []string, workerLabelMap map[string]string, logLevel int) {
	By(fmt.Sprintf("Check if NFD is installed using label: %s", label))
	nfdLabelDetected, err := check.AllNodeLabel(apiClient, label, allowedLabelValues, workerLabelMap)
	Expect(err).ToNot(HaveOccurred(), "error calling check.NodeLabel: %v", err)
	Expect(nfdLabelDetected).NotTo(BeFalse(), "NFD node label check failed to match label %s and label values %v on all nodes", label, allowedLabelValues)
	glog.V(glog.Level(logLevel)).Infof("The check for NFD label returned: %v", nfdLabelDetected)

	isNfdInstalled, err := check.NFDDeploymentsReady(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error checking if NFD deployments are ready: %v", err)
	glog.V(glog.Level(logLevel)).Infof("The check for NFD deployments ready returned: %v", isNfdInstalled)
}
