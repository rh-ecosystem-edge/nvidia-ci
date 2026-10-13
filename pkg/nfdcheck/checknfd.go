package nfdcheck

import (
	"fmt"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"k8s.io/apimachinery/pkg/labels"
	"time"
	"slices"
	corev1 "k8s.io/api/core/v1"
)

func CheckNfdInstallation(apiClient *clients.Settings, label string, allowedLabelValues []string, workerLabelMap map[string]string, logLevel int) {
	By(fmt.Sprintf("Check if NFD is installed using label: %s", label))
	err0 := wait.WaitForNodes(apiClient, labels.Set(workerLabelMap),
    func(node *corev1.Node) (bool, error) {
        val, ok := node.Labels[label]
        if !ok {
            return false, nil
        }
        return slices.Contains(allowedLabelValues, val), nil
    },
    10*time.Second, 3*time.Minute)
	Expect(err0).ToNot(HaveOccurred(), "error waiting for nodes: %v", err0)
	nfdLabelDetected, err := check.AllNodeLabel(apiClient, label, allowedLabelValues, workerLabelMap)
	Expect(err).ToNot(HaveOccurred(), "error calling check.NodeLabel: %v", err)
	Expect(nfdLabelDetected).NotTo(BeFalse(), "NFD node label check failed to match label %s and label values %v on all nodes", label, allowedLabelValues)
	glog.V(glog.Level(logLevel)).Infof("The check for NFD label returned: %v", nfdLabelDetected)

	isNfdInstalled, err := check.NFDDeploymentsReady(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error checking if NFD deployments are ready: %v", err)
	glog.V(glog.Level(logLevel)).Infof("The check for NFD deployments ready returned: %v", isNfdInstalled)
}
