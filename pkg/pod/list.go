package pod

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// List returns pod inventory in the given namespace.
func List(apiClient *clients.Settings, nsname string, options ...v1.ListOptions) ([]*Builder, error) {
	if nsname == "" {
		glog.V(100).Infof("pod 'nsname' parameter can not be empty")

		return nil, fmt.Errorf("failed to list pods, 'nsname' parameter is empty")
	}

	logMessage := fmt.Sprintf("Listing pods in the nsname %s", nsname)
	passedOptions := v1.ListOptions{}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	} else if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be empty or single-valued")

		return nil, fmt.Errorf("error: more than one ListOptions was passed")
	}

	glog.V(100).Infof(logMessage)

	podList, err := apiClient.Pods(nsname).List(context.TODO(), passedOptions)
	if err != nil {
		glog.V(100).Infof("Failed to list pods in the nsname %s due to %s", nsname, err.Error())

		return nil, err
	}

	var podObjects []*Builder

	for _, runningPod := range podList.Items {
		copiedPod := runningPod
		podBuilder := &Builder{
			apiClient:  apiClient,
			Object:     &copiedPod,
			Definition: &copiedPod,
		}

		podObjects = append(podObjects, podBuilder)
	}

	return podObjects, nil
}

// ListInAllNamespaces returns a cluster-wide pod inventory.
func ListInAllNamespaces(apiClient *clients.Settings, options ...v1.ListOptions) ([]*Builder, error) {
	logMessage := "Listing all pods in all namespaces"
	passedOptions := v1.ListOptions{}

	if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be empty or single-valued")

		return nil, fmt.Errorf("error: more than one ListOptions was passed")
	}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	}

	glog.V(100).Infof(logMessage)

	podList, err := apiClient.Pods("").List(context.TODO(), passedOptions)

	if err != nil {
		glog.V(100).Infof("Failed to list all pods due to %s", err.Error())

		return nil, err
	}

	var podObjects []*Builder

	for _, runningPod := range podList.Items {
		copiedPod := runningPod
		podBuilder := &Builder{
			apiClient:  apiClient,
			Object:     &copiedPod,
			Definition: &copiedPod,
		}

		podObjects = append(podObjects, podBuilder)
	}

	return podObjects, nil
}

// ListByNamePattern returns pod inventory in the given namespace filtered by name pattern.
func ListByNamePattern(apiClient *clients.Settings, namePattern, nsname string) ([]*Builder, error) {
	glog.V(100).Infof("Listing pods in the nsname %s filtered by the name pattern %s", nsname, namePattern)

	if nsname == "" {
		glog.V(100).Infof("pod 'nsname' parameter can not be empty")

		return nil, fmt.Errorf("failed to list pods, 'nsname' parameter is empty")
	}

	podList, err := apiClient.Pods(nsname).List(context.TODO(), v1.ListOptions{})

	if err != nil {
		glog.V(100).Infof("Failed to list pods filtered by the name pattern %s in the nsname %s due to %s",
			namePattern, nsname, err.Error())

		return nil, err
	}

	var podObjects []*Builder

	for _, runningPod := range podList.Items {
		if strings.Contains(runningPod.Name, namePattern) {
			copiedPod := runningPod
			podBuilder := &Builder{
				apiClient:  apiClient,
				Object:     &copiedPod,
				Definition: &copiedPod,
			}

			podObjects = append(podObjects, podBuilder)
		}
	}

	return podObjects, nil
}

// WaitForAllPodsInNamespaceRunning wait until all pods in namespace that match options are in running state.
func WaitForAllPodsInNamespaceRunning(
	apiClient *clients.Settings,
	nsname string,
	timeout time.Duration,
	options ...v1.ListOptions) (bool, error) {
	if nsname == "" {
		glog.V(100).Infof("'nsname' parameter can not be empty")

		return false, fmt.Errorf("failed to list pods, 'nsname' parameter is empty")
	}

	logMessage := fmt.Sprintf("Waiting for all pods in %s namespace", nsname)
	passedOptions := v1.ListOptions{}

	if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be empty or single-valued")

		return false, fmt.Errorf("error: more than one ListOptions was passed")
	}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	}

	glog.V(100).Infof(logMessage + " are in running state")

	podList, err := List(apiClient, nsname, passedOptions)
	if err != nil {
		glog.V(100).Infof("Failed to list all pods due to %s", err.Error())

		return false, err
	}

	for _, podObj := range podList {
		err = podObj.WaitUntilRunning(timeout)
		if err != nil {
			glog.V(100).Infof("Timout was reached while waiting for all pods in running state: %s", err.Error())

			return false, err
		}
	}

	return true, nil
}
