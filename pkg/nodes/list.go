package nodes

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	backoff = 2 * time.Second
)

// List returns node inventory.
func List(apiClient *clients.Settings, options ...v1.ListOptions) ([]*Builder, error) {
	passedOptions := v1.ListOptions{}
	logMessage := "Listing all node resources"

	if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be empty or single-valued")

		return nil, fmt.Errorf("error: more than one ListOptions was passed")
	}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	}

	glog.V(100).Infof(logMessage)

	nodeList, err := apiClient.CoreV1Interface.Nodes().List(context.TODO(), passedOptions)
	if err != nil {
		glog.V(100).Infof("Failed to list nodes due to %s", err.Error())

		return nil, err
	}

	var nodeObjects []*Builder

	for _, runningNode := range nodeList.Items {
		copiedNode := runningNode
		nodeBuilder := &Builder{
			apiClient:  apiClient.K8sClient,
			Object:     &copiedNode,
			Definition: &copiedNode,
		}

		nodeObjects = append(nodeObjects, nodeBuilder)
	}

	return nodeObjects, nil
}

// ListExternalIPv4Networks returns a list of node's external ipv4 addresses.
func ListExternalIPv4Networks(apiClient *clients.Settings, options ...v1.ListOptions) ([]string, error) {
	glog.V(100).Infof("Collecting node's external ipv4 addresses")

	var ipV4ExternalAddresses []string

	nodeBuilders, err := List(apiClient, options...)
	if err != nil {
		return nil, err
	}

	for _, node := range nodeBuilders {
		extNodeNetwork, err := node.ExternalIPv4Network()
		if err != nil {
			glog.V(100).Infof("Failed to collect external ip address from node %s", node.Object.Name)

			return nil, fmt.Errorf(
				"error getting external IPv4 address from node %s due to %w", node.Definition.Name, err)
		}
		ipV4ExternalAddresses = append(ipV4ExternalAddresses, extNodeNetwork)
	}

	return ipV4ExternalAddresses, nil
}

// WaitForAllNodesAreReady waits for all nodes to be Ready for a time duration up to the timeout.
func WaitForAllNodesAreReady(apiClient *clients.Settings,
	timeout time.Duration,
	options ...v1.ListOptions) (bool, error) {
	glog.V(100).Infof("Waiting for all nodes to be in the Ready state for up to a duration of %v",
		timeout)

	nodesList, err := List(apiClient, options...)
	if err != nil {
		glog.V(100).Infof("Failed to list all nodes due to %s", err.Error())

		return false, err
	}

	err = wait.PollUntilContextTimeout(
		context.TODO(), backoff, timeout, true, func(ctx context.Context) (done bool, err error) {
			for _, node := range nodesList {
				ready, err := node.IsReady()
				if err != nil {
					glog.V(100).Infof("Node %v has error %w", node.Object.Name, err)

					return false, err
				}

				if !ready {
					glog.V(100).Infof("Node %s not Ready", node.Object.Name)

					return false, nil
				}
			}

			return true, nil
		})

	if err == nil {
		glog.V(100).Infof("All nodes were found in the Ready State during availableDuration: %v",
			timeout)

		return true, nil
	}

	// Here err is "timed out waiting for the condition"
	glog.V(100).Infof("Not all nodes were found in the Ready State during availableDuration: %v",
		err)

	return false, err
}

// WaitForAllNodesToReboot waits for all nodes to start and finish reboot up to the timeout.
func WaitForAllNodesToReboot(apiClient *clients.Settings,
	globalRebootTimeout time.Duration,
	options ...v1.ListOptions) (bool, error) {
	glog.V(100).Infof("Waiting for all nodes in the list to reboot and return to the Ready condition")

	nodesList, err := List(apiClient, options...)
	if err != nil {
		glog.V(100).Infof("Failed to list all nodes due to %s", err.Error())

		return false, err
	}

	globalStartTime := time.Now().Unix()
	readyNodes := []string{}
	rebootedNodes := []string{}
	err = wait.PollUntilContextTimeout(
		context.TODO(), backoff, globalRebootTimeout, true, func(ctx context.Context) (done bool, err error) {
			for _, node := range nodesList {
				if !slices.Contains(readyNodes, node.Object.Name) {
					ready, err := node.IsReady()
					if err != nil {
						return false, err
					}

					rebooted := slices.Contains(rebootedNodes, node.Object.Name)
					if !ready && !rebooted {
						glog.V(100).Infof("Node %s was rebooted and is starting to recover", node.Object.Name)

						rebootedNodes = append(rebootedNodes, node.Object.Name)
					}

					if ready && rebooted {
						glog.V(100).Infof("Node %s was successfully rebooted after: %v",
							time.Now().Unix()-globalStartTime)

						readyNodes = append(readyNodes, node.Object.Name)
					}
				}
			}

			return len(readyNodes) == len(nodesList), nil
		})

	if err == nil {
		globalRebootDuration := time.Now().Unix() - globalStartTime
		glog.V(100).Infof("All nodes were successfully rebooted during: %v", globalRebootDuration)

		return true, nil
	}

	glog.V(100).Infof("Not all nodes were rebooted, timeout %v reached: %v", globalRebootTimeout, err)

	return false, err
}
