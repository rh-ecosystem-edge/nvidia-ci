package check

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	nfdOperatorNamespace  = "openshift-nfd"
	nfdOperatorDeployment = "nfd-controller-manager"
	nfdMasterDeployment   = "nfd-master"
)

// AllNodeLabel checks if label is present on all nodes matching nodeSelector.
func AllNodeLabel(apiClient *clients.Settings, nodeLabel, nodeLabelValue string,
	nodeSelector map[string]string) (bool, error) {
	nodeBuilder, err := nodes.List(apiClient, v1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})

	// in all the nodes that match the nodeSelectors, look for specific label
	// For example, look in all the worker nodes for a specific label with specific value
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("could not discover %v nodes, error encountered: '%v'",
			nodeSelector, err)

		return false, err
	}

	// Sample label: feature.node.kubernetes.io/system-os_release.ID=rhcos.
	foundLabels := 0

	for _, node := range nodeBuilder {
		labelValue := node.Object.Labels[nodeLabel]

		if labelValue == nodeLabelValue {
			glog.V(gpuparams.GpuLogLevel).Infof("Found label %v that contains %v with label value % s on "+
				"node %v", nodeLabel, nodeLabel, nodeLabelValue, node.Object.Name)

			foundLabels++
			// if all nodes matching nodeSelector have this label with label value.
			if foundLabels == len(nodeBuilder) {
				return true, nil
			}
		}
	}

	err = fmt.Errorf("not all (%v) nodes have the label '%s' with value '%s'", len(nodeBuilder),
		nodeLabel, nodeLabelValue)

	return false, err
}

// NodeWithLabel checks if label is present on at least one node matching nodeSelector.
func NodeWithLabel(apiClient *clients.Settings, nodeLabel string, nodeSelector map[string]string) (bool, error) {
	nodeBuilder, err := nodes.List(apiClient, v1.ListOptions{LabelSelector: labels.Set(nodeSelector).String()})

	// Check if at least one node matching the nodeSelector has the specific nodeLabel label set to true
	// For example, look in all the worker nodes for specific label
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("could not discover %v nodes", nodeSelector)

		return false, err
	}

	for _, node := range nodeBuilder {
		labelValue, ok := node.Object.Labels[nodeLabel]

		if ok {
			glog.V(gpuparams.GpuLogLevel).Infof("Found label '%v' with label value '%v' on node '%v'",
				nodeLabel, labelValue, node.Object.Name)

			return true, nil
		}
	}

	err = fmt.Errorf("could not find one node with label '%s' set to true", nodeLabel)

	return false, err
}

// NFDDeploymentsReady verifies if NFD Operator and Operand deployments are ready in the openshift-nfd namespace.
func NFDDeploymentsReady(apiClient *clients.Settings) (bool, error) {
	// here check if the 2 NFD deployments in openshift-nfd namespace are ready, first nfd operator
	nfdOperatorDeployment, err1 := deployment.Pull(apiClient, nfdOperatorDeployment, nfdOperatorNamespace)

	if err1 != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error trying to pull NFD operator deployment:  %v ", err1)

		return false, err1
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pulled NFD operator deployment is:  %v ",
		nfdOperatorDeployment.Definition.Name)

	// Check nfd-master deployment.
	nfdMasterDeployment, err2 := deployment.Pull(apiClient, nfdMasterDeployment, nfdOperatorNamespace)

	if err2 != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error trying to pull NFD master deployment:  %v ", err2)

		return false, err2
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pulled NFD operand master deployment:  %v ",
		nfdMasterDeployment.Definition.Name)

	if nfdOperatorDeployment.IsReady(180*time.Second) && nfdMasterDeployment.IsReady(180*time.Second) {
		glog.V(gpuparams.GpuLogLevel).Infof("NFD operator '%s' and operand '%s' deployments are ready",
			nfdOperatorDeployment.Definition.Name, nfdMasterDeployment.Definition.Name)

		return true, nil
	}

	return false, fmt.Errorf("failed to check if NFD deployments were ready")
}
