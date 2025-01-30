package nfd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/golang/glog"
	nfdv1 "github.com/openshift/cluster-nfd-operator/api/v1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Builder provides a struct for NodeFeatureDiscovery object
// from the cluster and a NodeFeatureDiscovery definition.
type Builder struct {
	// Builder definition. Used to create
	// Builder object with minimum set of required elements.
	Definition *nfdv1.NodeFeatureDiscovery
	// Created Builder object on the cluster.
	Object *nfdv1.NodeFeatureDiscovery
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before Builder object is created.
	errorMsg string
}

// NewBuilderFromObjectString creates a Builder object from CSV alm-examples.
func NewBuilderFromObjectString(apiClient *clients.Settings, almExample string) *Builder {
	glog.V(100).Infof(
		"Initializing new Builder structure from almExample string")

	nodeFeatureDiscovery, err := getNodeFeatureDiscoveryFromAlmExample(almExample)

	glog.V(100).Infof(
		"Initializing Builder definition to NodeFeatureDiscovery object")

	builder := Builder{
		apiClient:  apiClient,
		Definition: nodeFeatureDiscovery,
	}

	if err != nil {
		glog.V(100).Infof(
			"Error initializing NodeFeatureDiscovery from alm-examples: %s", err.Error())

		builder.errorMsg = fmt.Sprintf("Error initializing NodeFeatureDiscovery from alm-examples: %s",
			err.Error())
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The NodeFeatureDiscovery object definition is nil")

		builder.errorMsg = "NodeFeatureDiscovery definition is nil"
	}

	return &builder
}

// Get returns NodeFeatureDiscovery object if found.
func (builder *Builder) Get() (*nfdv1.NodeFeatureDiscovery, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof("Collecting NodeFeatureDiscovery object %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	nodeFeatureDiscovery := &nfdv1.NodeFeatureDiscovery{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name:      builder.Definition.Name,
		Namespace: builder.Definition.Namespace,
	}, nodeFeatureDiscovery)

	if err != nil {
		glog.V(100).Infof("NodeFeatureDiscovery object %s doesn't exist in namespace %s",
			builder.Definition.Name, builder.Definition.Namespace)

		return nil, err
	}

	return nodeFeatureDiscovery, err
}

// Pull loads an existing NodeFeatureDiscovery into Builder struct.
func Pull(apiClient *clients.Settings, name, namespace string) (*Builder, error) {
	glog.V(100).Infof("Pulling existing nodeFeatureDiscovery name: %s in namespace: %s", name, namespace)

	builder := Builder{
		apiClient: apiClient,
		Definition: &nfdv1.NodeFeatureDiscovery{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("NodeFeatureDiscovery name is empty")

		builder.errorMsg = "NodeFeatureDiscovery 'name' cannot be empty"
	}

	if namespace == "" {
		glog.V(100).Infof("NodeFeatureDiscovery namespace is empty")

		builder.errorMsg = "NodeFeatureDiscovery 'namespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("NodeFeatureDiscovery object %s doesn't exist in namespace %s", name, namespace)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given NodeFeatureDiscovery exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if NodeFeatureDiscovery %s exists in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect NodeFeatureDiscovery object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a NodeFeatureDiscovery.
func (builder *Builder) Delete() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting NodeFeatureDiscovery %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	if !builder.Exists() {
		return builder, fmt.Errorf("NodeFeatureDiscovery cannot be deleted because it does not exist")
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Definition)

	if err != nil {
		return builder, fmt.Errorf("cannot delete NodeFeaturediscovery: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a NodeFeatureDiscovery in the cluster and stores the created object in struct.
func (builder *Builder) Create() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the NodeFeatureDiscovery %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		}
	}

	return builder, err
}

// Update renovates the existing NodeFeatureDiscovery object with the definition in builder.
func (builder *Builder) Update(force bool) (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the NodeFeatureDiscovery object named: %s in namespace: %s",
		builder.Definition.Name, builder.Definition.Namespace)

	err := builder.apiClient.Update(context.TODO(), builder.Definition)

	if err != nil {
		if force {
			glog.V(100).Infof(
				msg.FailToUpdateNotification("NodeFeatureDiscovery", builder.Definition.Name, builder.Definition.Namespace))

			builder, err := builder.Delete()

			if err != nil {
				glog.V(100).Infof(
					msg.FailToUpdateError("NodeFeatureDiscovery", builder.Definition.Name, builder.Definition.Namespace))

				return nil, err
			}

			return builder.Create()
		}
	}

	return builder, err
}

// getNodeFeatureDiscoveryFromAlmExample extracts the NodeFeatureDiscovery from the alm-examples block.
func getNodeFeatureDiscoveryFromAlmExample(almExample string) (*nfdv1.NodeFeatureDiscovery, error) {
	nodeFeatureDiscoveryList := &nfdv1.NodeFeatureDiscoveryList{}

	if almExample == "" {
		return nil, fmt.Errorf("almExample is an empty string")
	}

	err := json.Unmarshal([]byte(almExample), &nodeFeatureDiscoveryList.Items)

	if err != nil {
		return nil, err
	}

	if len(nodeFeatureDiscoveryList.Items) == 0 {
		return nil, fmt.Errorf("failed to get alm examples")
	}

	return &nodeFeatureDiscoveryList.Items[0], nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "NodeFeatureDiscovery"

	if builder == nil {
		glog.V(100).Infof("The %s builder is uninitialized", resourceCRD)

		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The %s is undefined", resourceCRD)

		return false, fmt.Errorf(msg.UndefinedCrdObjectErrString(resourceCRD))
	}

	if builder.apiClient == nil {
		glog.V(100).Infof("The %s builder apiclient is nil", resourceCRD)

		return false, fmt.Errorf("%s builder cannot have nil apiClient", resourceCRD)
	}

	return true, nil
}

// UpdatePciDevices updates the PCI device whitelist configuration in the worker config.
// Parameters:
//   - deviceClassWhitelist: List of PCI device class IDs to whitelist
//   - deviceLabelFields: List of fields to be used as labels for the PCI devices
//
// Returns an error if the configuration update fails or if invalid parameters are provided.
func (builder *Builder) UpdatePciDevices(deviceClassWhitelist []string, deviceLabelFields []string) error {

	if deviceClassWhitelist == nil || deviceLabelFields == nil {
		return fmt.Errorf("deviceClassWhitelist and deviceLabelFields cannot be nil")
	}

	if builder.Definition == nil || builder.Definition.Spec.WorkerConfig.ConfigData == "" {
		return fmt.Errorf("worker config data is not initialized")
	}
	// Validate device class format
	for _, class := range deviceClassWhitelist {
		if len(class) != 4 || !isValidHexString(class) {
			return fmt.Errorf("invalid device class ID format: %s. Expected 4-digit hexadecimal", class)
		}
	}

	// Validate label fields
	allowedFields := map[string]bool{"class": true, "vendor": true, "device": true, "subsystem_vendor": true, "subsystem_device": true}
	for _, field := range deviceLabelFields {
		if !allowedFields[field] {
			return fmt.Errorf("invalid label field: %s. Must be one of: class, vendor, device, subsystem_vendor, subsystem_device", field)
		}
	}
	cfg := NewConfig(builder.Definition.Spec.WorkerConfig.ConfigData)
	cfg.SetPciWhitelistConfig(deviceClassWhitelist, deviceLabelFields)

	var err error
	builder.Definition.Spec.WorkerConfig.ConfigData, err = cfg.GetYamlString()
	if err != nil {
		return err
	}
	return nil
}

func isValidHexString(s string) bool {
	_, err := strconv.ParseUint(s, 16, 16)
	return err == nil
}
