package nfdcheck

import (
	"fmt"
	"slices"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
)

func CheckNfdInstallation(apiClient *clients.Settings, label string, allowedLabelValues []string, workerLabelMap map[string]string, logLevel int) {
	By(fmt.Sprintf("Check if NFD is installed using label: %s", label))
	err := wait.WaitForNodes(apiClient, labels.Set(workerLabelMap),
		func(node *corev1.Node) (bool, error) {
			val, ok := node.Labels[label]
			if !ok {
				return false, nil
			}
			return slices.Contains(allowedLabelValues, val), nil
		}, 10*time.Second, 3*time.Minute)
	Expect(err).ToNot(HaveOccurred(), "error waiting for nodes: %v", err)
	nfdLabelDetected, err := check.AllNodeLabel(apiClient, label, allowedLabelValues, workerLabelMap)
	Expect(err).ToNot(HaveOccurred(), "error calling check.NodeLabel: %v", err)
	Expect(nfdLabelDetected).NotTo(BeFalse(), "NFD node label check failed to match label %s and label values %v on all nodes", label, allowedLabelValues)
	glog.V(glog.Level(logLevel)).Infof("The check for NFD label returned: %v", nfdLabelDetected)

	isNfdInstalled, err := check.NFDDeploymentsReady(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error checking if NFD deployments are ready: %v", err)
	glog.V(glog.Level(logLevel)).Infof("The check for NFD deployments ready returned: %v", isNfdInstalled)
}
