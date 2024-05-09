package get

import (
	"fmt"
	"regexp"

	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/olm"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
)

// github.com/rh-ecosystem-edge/nvidia-ci/tests

// InstalledCSVFromSubscription returns installedCSV from Subscription.
func InstalledCSVFromSubscription(apiClient *clients.Settings, gpuSubscriptionName,
	gpuSubscriptionNamespace string) (string, error) {
	subPulled, err := olm.PullSubscription(apiClient, gpuSubscriptionName, gpuSubscriptionNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error pulling Subscription %s from cluster in namespace %s", gpuSubscriptionName,
			gpuSubscriptionNamespace)

		return "", err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"InstalledCSV %s extracted from Subscription %s from cluster in namespace %s",
		subPulled.Object.Status.InstalledCSV, gpuSubscriptionName, gpuSubscriptionNamespace)

	return subPulled.Object.Status.InstalledCSV, nil
}

// CurrentCSVFromSubscription returns installedCSV from Subscription.
func CurrentCSVFromSubscription(apiClient *clients.Settings, gpuSubscriptionName,
	gpuSubscriptionNamespace string) (string, error) {
	subPulled, err := olm.PullSubscription(apiClient, gpuSubscriptionName, gpuSubscriptionNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error pulling Subscription %s from cluster in namespace %s", gpuSubscriptionName,
			gpuSubscriptionNamespace)

		return "", err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"CurrentCSV %s extracted from Subscription %s from cluster in namespace %s",
		subPulled.Object.Status.CurrentCSV, gpuSubscriptionName, gpuSubscriptionNamespace)

	return subPulled.Object.Status.CurrentCSV, nil
}

// GetFirstPodNameWithLabel returns a the first pod name matching pod labelSelector in specified namespace.
func GetFirstPodNameWithLabel(apiClient *clients.Settings, podNamespace, podLabelSelector string) (string, error) {
	podList, err := pod.List(apiClient, podNamespace, v1.ListOptions{LabelSelector: podLabelSelector})

	glog.V(gpuparams.GpuLogLevel).Infof("Length of podList matching podLabelSelector is '%v'", len(podList))
	glog.V(gpuparams.GpuLogLevel).Infof("podList[0] matching podLabelSelector is '%v'",
		podList[0].Definition.Name)

	return podList[0].Definition.Name, err
}

// GetClusterArchitecture returns first node architecture of the nodes that match nodeSelector (e.g. worker nodes).
func GetClusterArchitecture(apiClient *clients.Settings, nodeSelector map[string]string) (string, error) {
	nodeBuilder, err := nodes.List(apiClient, v1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})

	// Check if at least one node matching the nodeSelector has the specific nodeLabel label set to true
	// For example, look in all the worker nodes for specific label
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("could not discover %v nodes", nodeSelector)

		return "", err
	}

	nodeLabel := "kubernetes.io/arch"

	for _, node := range nodeBuilder {
		labelValue, ok := node.Object.Labels[nodeLabel]

		if ok {
			glog.V(gpuparams.GpuLogLevel).Infof("Found label '%v' with label value '%v' on node '%v'",
				nodeLabel, labelValue, node.Object.Name)

			return labelValue, nil
		}
	}

	err = fmt.Errorf("could not find one node with label '%s'", nodeLabel)

	return "", err
}

func ExtractVersionFromCSV(csv string) string {
	regex := *regexp.MustCompile(`(v\d+\.\d+\.\d+)`)
	matches := regex.FindAllString(csv, -1)
	if len(matches) == 0 {
		return csv
	}
	return matches[0]
}
