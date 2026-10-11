package check

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

const (
	nfdOperatorNamespace  = "openshift-nfd"
	nfdOperatorDeployment = "nfd-controller-manager"
	nfdMasterDeployment   = "nfd-master"

	// resourceAPIGroup and deviceClassesResource identify the DRA DeviceClass CRD
	// (resource.k8s.io, resource "deviceclasses"), used by DeviceClassServed.
	resourceAPIGroup      = "resource.k8s.io"
	deviceClassesResource = "deviceclasses"
)

// AllNodeLabel checks if label is present on all nodes matching nodeSelector.
func AllNodeLabel(apiClient *clients.Settings, nodeLabel string, nodeLabelValues []string, nodeSelector map[string]string) (bool, error) {
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

		if slices.Contains(nodeLabelValues, labelValue) {
			glog.V(gpuparams.GpuLogLevel).Infof("Found label %v that contains %v with label value %s on "+
				"node %v", nodeLabel, nodeLabel, labelValue, node.Object.Name)

			foundLabels++
			// if all nodes matching nodeSelector have this label with label value.
			if foundLabels == len(nodeBuilder) {
				return true, nil
			}
		}
	}

	err = fmt.Errorf("not all (%v) nodes have the label '%s' with a value in '%v'", len(nodeBuilder),
		nodeLabel, nodeLabelValues)

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

// KubernetesVersionAtLeast returns true if the cluster's Kubernetes server version is
// greater than or equal to the given major.minor (e.g. KubernetesVersionAtLeast(c, 1, 34)),
// along with the raw server version string for logging. Used to gate DRA features that
// depend on a specific Kubernetes release - e.g. the Partitionable Devices feature
// (KEP-4815) that the DRA driver's "DynamicMIG" feature gate relies on, only available
// from Kubernetes 1.34 onward.
func KubernetesVersionAtLeast(apiClient *clients.Settings, major, minor int) (bool, string, error) {
	serverVersion, err := apiClient.K8sClient.Discovery().ServerVersion()
	if err != nil {
		return false, "", fmt.Errorf("failed to get Kubernetes server version: %w", err)
	}

	actualMajor, err := strconv.Atoi(strings.TrimRight(serverVersion.Major, "+"))
	if err != nil {
		return false, serverVersion.GitVersion, fmt.Errorf(
			"failed to parse server major version %q: %w", serverVersion.Major, err)
	}

	actualMinor, err := strconv.Atoi(strings.TrimRight(serverVersion.Minor, "+"))
	if err != nil {
		return false, serverVersion.GitVersion, fmt.Errorf(
			"failed to parse server minor version %q: %w", serverVersion.Minor, err)
	}

	meets := actualMajor > major || (actualMajor == major && actualMinor >= minor)

	glog.V(gpuparams.GpuLogLevel).Infof(
		"Cluster Kubernetes server version is %s (parsed major=%d, minor=%d); required >= %d.%d: %v",
		serverVersion.GitVersion, actualMajor, actualMinor, major, minor, meets)

	return meets, serverVersion.GitVersion, nil
}

// DeviceClassServed returns true if a DeviceClass with the given name exists in the
// cluster's resource.k8s.io API group, at whichever version the API server prefers. Used
// to verify a DRA driver capability (e.g. the mig.nvidia.com DeviceClass for MIG
// allocation) is actually being rendered before attempting to use it, since DeviceClass
// existence depends on which capabilities the installed DRA driver deploys.
func DeviceClassServed(apiClient *clients.Settings, deviceClassName string) (bool, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(apiClient.Config)
	if err != nil {
		return false, fmt.Errorf("failed to create discovery client: %w", err)
	}

	groups, resourceLists, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return false, fmt.Errorf("failed to get API groups and resources: %w", err)
	}

	var preferredVersion string

	for _, group := range groups {
		if group.Name == resourceAPIGroup {
			preferredVersion = group.PreferredVersion.Version

			break
		}
	}

	if preferredVersion == "" {
		glog.V(gpuparams.GpuLogLevel).Infof("API group '%s' not found in cluster", resourceAPIGroup)

		return false, nil
	}

	groupVersion := fmt.Sprintf("%s/%s", resourceAPIGroup, preferredVersion)
	deviceClassesServed := false

	for _, resourceList := range resourceLists {
		if resourceList.GroupVersion != groupVersion {
			continue
		}

		for _, res := range resourceList.APIResources {
			if res.Name == deviceClassesResource {
				deviceClassesServed = true
			}
		}

		break
	}

	if !deviceClassesServed {
		glog.V(gpuparams.GpuLogLevel).Infof("'%s' resource not found in %s", deviceClassesResource, groupVersion)

		return false, nil
	}

	gvr := schema.GroupVersionResource{Group: resourceAPIGroup, Version: preferredVersion, Resource: deviceClassesResource}

	_, err = apiClient.Resource(gvr).Get(context.TODO(), deviceClassName, v1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to get DeviceClass '%s': %w", deviceClassName, err)
	}

	return true, nil
}
