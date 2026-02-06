package deployment

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// List returns deployment inventory in the given namespace.
func List(apiClient *clients.Settings, nsname string, options ...metav1.ListOptions) ([]*Builder, error) {
	if nsname == "" {
		glog.V(100).Infof("deployment 'nsname' parameter can not be empty")

		return nil, fmt.Errorf("failed to list deployments, 'nsname' parameter is empty")
	}

	passedOptions := metav1.ListOptions{}
	logMessage := fmt.Sprintf("Listing deployments in the namespace %s", nsname)

	if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be empty or single-valued")

		return nil, fmt.Errorf("error: more than one ListOptions was passed")
	}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	}

	glog.V(100).Infof(logMessage)

	deploymentList, err := apiClient.Deployments(nsname).List(context.TODO(), passedOptions)
	if err != nil {
		glog.V(100).Infof("Failed to list deployments in the namespace %s due to %s", nsname, err.Error())

		return nil, err
	}

	var deploymentObjects []*Builder

	for _, runningDeployment := range deploymentList.Items {
		copiedDeployment := runningDeployment
		deploymentBuilder := &Builder{
			apiClient:  apiClient.AppsV1Interface,
			Object:     &copiedDeployment,
			Definition: &copiedDeployment,
		}

		deploymentObjects = append(deploymentObjects, deploymentBuilder)
	}

	return deploymentObjects, nil
}

// ListInAllNamespaces returns deployment inventory in the all the namespaces.
func ListInAllNamespaces(apiClient *clients.Settings, options ...metav1.ListOptions) ([]*Builder, error) {
	passedOptions := metav1.ListOptions{}
	logMessage := "Listing deployments in all namespaces"

	if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be either empty or single-valued")

		return nil, fmt.Errorf("error: more than one ListOptions was passed")
	}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	}

	glog.V(100).Infof(logMessage)

	deploymentList, err := apiClient.Deployments("").List(context.TODO(), passedOptions)

	if err != nil {
		glog.V(100).Infof("Failed to list deployments in all namespaces due to %s", err.Error())

		return nil, err
	}

	var deploymentObjects []*Builder

	for _, runningDeployment := range deploymentList.Items {
		copiedDeployment := runningDeployment
		deploymentBuilder := &Builder{
			apiClient:  apiClient.AppsV1Interface,
			Object:     &copiedDeployment,
			Definition: &copiedDeployment,
		}

		deploymentObjects = append(deploymentObjects, deploymentBuilder)
	}

	return deploymentObjects, nil
}
