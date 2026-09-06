package nvidiagpu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

// gpuClusterGVR identifies the GPUCluster CRD (nvidia.com/v1alpha1, resource "gpuclusters").
// A generated Go type for GPUCluster is not yet available in the vendored
// github.com/NVIDIA/gpu-operator module, so GPUClusterBuilder interacts with it as a
// dynamic/unstructured resource instead of a typed struct (unlike ClusterPolicy/NVIDIADriver).
var gpuClusterGVR = schema.GroupVersionResource{
	Group:    GPUClusterAPIGroup,
	Version:  GPUClusterAPIVersion,
	Resource: GPUClusterResource,
}

// IsGPUClusterCRDServed returns true if the GPUCluster CRD is being served by the API server.
// This is used to distinguish GPU Operator versions >= 26.7.0 (which introduce GPUCluster)
// from older versions where the "nvidia.com/v1alpha1" group only serves NVIDIADriver.
func IsGPUClusterCRDServed(apiClient *clients.Settings) (bool, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(apiClient.Config)
	if err != nil {
		return false, fmt.Errorf("failed to create discovery client: %w", err)
	}

	groupVersion := fmt.Sprintf("%s/%s", GPUClusterAPIGroup, GPUClusterAPIVersion)

	resourceList, err := discoveryClient.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		glog.V(100).Infof("API group/version %s not found: %s", groupVersion, err.Error())

		return false, nil
	}

	for _, resource := range resourceList.APIResources {
		if resource.Name == GPUClusterResource {
			return true, nil
		}
	}

	return false, nil
}

// GPUClusterBuilder provides a struct for the (singleton) GPUCluster object from the cluster
// and a GPUCluster definition. GPUCluster is one of the two CRs (alongside NVIDIADriver)
// introduced by GPU Operator 26.7.0 as part of its native/DRA-based software-enablement
// stack, replacing ClusterPolicy's driver/DRA-driver management for that mode.
type GPUClusterBuilder struct {
	// Definition used to create the object with the minimum set of required elements.
	Definition *unstructured.Unstructured
	// Object is the created object on the cluster.
	Object *unstructured.Unstructured
	// apiClient is used to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before the object is created.
	errorMsg string
}

// NewGPUClusterBuilderFromObjectString creates a minimal GPUClusterBuilder object from the
// GPUCluster sample found in the GPU Operator CSV's alm-examples. The object's name is forced
// to GPUClusterName, since GPUCluster is a singleton and upstream enforces
// metadata.name == "gpu-cluster" via CRD validation.
func NewGPUClusterBuilderFromObjectString(apiClient *clients.Settings, almExample string) *GPUClusterBuilder {
	glog.V(100).Infof("Initializing new GPUClusterBuilder structure from almExample string")

	gpuClusterExample, err := olm.GetALMExampleByKind(almExample, GPUClusterKind)
	if err != nil {
		return newGPUClusterBuilder(apiClient, nil, err)
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(gpuClusterExample, &fields); err != nil {
		return newGPUClusterBuilder(apiClient, nil, fmt.Errorf("failed to unmarshal GPUCluster alm-example: %w", err))
	}

	gpuCluster := &unstructured.Unstructured{Object: fields}
	gpuCluster.SetAPIVersion(fmt.Sprintf("%s/%s", GPUClusterAPIGroup, GPUClusterAPIVersion))
	gpuCluster.SetKind(GPUClusterKind)
	gpuCluster.SetName(GPUClusterName)
	gpuCluster.SetNamespace("")

	return newGPUClusterBuilder(apiClient, gpuCluster, nil)
}

func newGPUClusterBuilder(
	apiClient *clients.Settings, gpuCluster *unstructured.Unstructured, err error) *GPUClusterBuilder {
	builder := &GPUClusterBuilder{
		apiClient:  apiClient,
		Definition: gpuCluster,
	}

	if err != nil {
		glog.V(100).Infof("Error initializing GPUCluster from alm-examples: %s", err.Error())

		builder.errorMsg = fmt.Sprintf("Error initializing GPUCluster from alm-examples: %s", err.Error())
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The GPUCluster object definition is nil")

		builder.errorMsg = "GPUCluster 'Object.Definition' is nil"
	}

	return builder
}

// resourceClient returns the dynamic ResourceInterface used to CRUD GPUCluster objects.
// GPUCluster is cluster-scoped, so no namespace is set.
func (builder *GPUClusterBuilder) resourceClient() dynamic.ResourceInterface {
	return builder.apiClient.Resource(gpuClusterGVR)
}

// Get returns the GPUCluster object if found.
func (builder *GPUClusterBuilder) Get() (*unstructured.Unstructured, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof("Collecting GPUCluster object %s", builder.Definition.GetName())

	gpuCluster, err := builder.resourceClient().Get(context.TODO(), builder.Definition.GetName(), metav1.GetOptions{})
	if err != nil {
		glog.V(100).Infof("GPUCluster object %s doesn't exist", builder.Definition.GetName())

		return nil, err
	}

	return gpuCluster, nil
}

// PullGPUCluster loads an existing GPUCluster into a GPUClusterBuilder struct.
func PullGPUCluster(apiClient *clients.Settings, name string) (*GPUClusterBuilder, error) {
	glog.V(100).Infof("Pulling existing GPUCluster name: %s", name)

	if name == "" {
		return nil, fmt.Errorf("GPUCluster 'name' cannot be empty")
	}

	def := &unstructured.Unstructured{}
	def.SetAPIVersion(fmt.Sprintf("%s/%s", GPUClusterAPIGroup, GPUClusterAPIVersion))
	def.SetKind(GPUClusterKind)
	def.SetName(name)

	builder := &GPUClusterBuilder{
		apiClient:  apiClient,
		Definition: def,
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("GPUCluster object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// Exists checks whether the given GPUCluster exists.
func (builder *GPUClusterBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if GPUCluster %s exists", builder.Definition.GetName())

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect GPUCluster object due to %s", err.Error())
	}

	return err == nil
}

// Delete removes a GPUCluster.
func (builder *GPUClusterBuilder) Delete() (*GPUClusterBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting GPUCluster %s", builder.Definition.GetName())

	if !builder.Exists() {
		return builder, nil
	}

	err := builder.resourceClient().Delete(context.TODO(), builder.Definition.GetName(), metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return builder, fmt.Errorf("cannot delete GPUCluster: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a GPUCluster in the cluster and stores the created object in the struct.
func (builder *GPUClusterBuilder) Create() (*GPUClusterBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the GPUCluster %s", builder.Definition.GetName())

	if builder.Exists() {
		return builder, nil
	}

	created, err := builder.resourceClient().Create(context.TODO(), builder.Definition, metav1.CreateOptions{})
	if err == nil {
		builder.Object = created
	}

	return builder, err
}

// State returns the GPUCluster's status.state field ("ready", "notReady", "disabled", or ""
// if the object or field is not present).
func (builder *GPUClusterBuilder) State() string {
	if builder.Object == nil {
		return ""
	}

	state, _, _ := unstructured.NestedString(builder.Object.Object, "status", "state")

	return state
}

// validate checks that the builder and its definition are properly initialized before
// accessing any member fields.
func (builder *GPUClusterBuilder) validate() (bool, error) {
	resourceCRD := "GPUCluster"

	if builder == nil {
		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		builder.errorMsg = msg.UndefinedCrdObjectErrString(resourceCRD)
	}

	if builder.apiClient == nil {
		builder.errorMsg = fmt.Sprintf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		return false, errors.New(builder.errorMsg)
	}

	return true, nil
}
