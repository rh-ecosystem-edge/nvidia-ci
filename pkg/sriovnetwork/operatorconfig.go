package sriovnetwork

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/glog"
	sriovnetworkapi "github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork/api"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// SriovOperatorConfigBuilder provides a struct for SriovOperatorConfig object containing connection to the
// cluster and the SriovOperatorConfig definition.
type SriovOperatorConfigBuilder struct {
	// SriovOperatorConfigBuilder definition. Used to create
	// SriovOperatorConfigBuilder object with minimum set of required elements.
	Definition *sriovnetworkapi.SriovOperatorConfig
	// Created SriovOperatorConfigBuilder object on the cluster.
	Object *sriovnetworkapi.SriovOperatorConfig
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before SriovOperatorConfigBuilder object is created.
	errorMsg string
}

// NewSriovOperatorConfigBuilder returns a SriovOperatorConfigBuilder struct.
func NewSriovOperatorConfigBuilder(apiClient *clients.Settings, name, nsname string) *SriovOperatorConfigBuilder {
	glog.V(100).Infof(
		"Initializing new SriovOperatorConfigBuilder structure with name: %s, namespace: %s", name, nsname)

	builder := &SriovOperatorConfigBuilder{
		apiClient: apiClient,
		Definition: &sriovnetworkapi.SriovOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The Name of the SriovOperatorConfig is empty")

		builder.errorMsg = "SriovOperatorConfig 'name' cannot be empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The Namespace of the SriovOperatorConfig is empty")

		builder.errorMsg = "SriovOperatorConfig 'nsname' cannot be empty"
	}

	return builder
}

// Get returns SriovOperatorConfig object if found.
func (builder *SriovOperatorConfigBuilder) Get() (*sriovnetworkapi.SriovOperatorConfig, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof("Collecting SriovOperatorConfig object %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	sriovOperatorConfig := &sriovnetworkapi.SriovOperatorConfig{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name:      builder.Definition.Name,
		Namespace: builder.Definition.Namespace,
	}, sriovOperatorConfig)

	if err != nil {
		glog.V(100).Infof("SriovOperatorConfig object %s doesn't exist in namespace %s",
			builder.Definition.Name, builder.Definition.Namespace)

		return nil, err
	}

	return sriovOperatorConfig, nil
}

// PullSriovOperatorConfig loads an existing SriovOperatorConfig into SriovOperatorConfigBuilder struct.
func PullSriovOperatorConfig(apiClient *clients.Settings, name, nsname string) (*SriovOperatorConfigBuilder, error) {
	glog.V(100).Infof("Pulling existing SriovOperatorConfig name %s in namespace %s", name, nsname)

	builder := &SriovOperatorConfigBuilder{
		apiClient: apiClient,
		Definition: &sriovnetworkapi.SriovOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		builder.errorMsg = "SriovOperatorConfig 'name' cannot be empty"

		return nil, errors.New(builder.errorMsg)
	}

	if nsname == "" {
		builder.errorMsg = "SriovOperatorConfig 'nsname' cannot be empty"

		return nil, errors.New(builder.errorMsg)
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("sriovOperatorConfig object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// Exists checks whether the given SriovOperatorConfig exists.
func (builder *SriovOperatorConfigBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if SriovOperatorConfig %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect SriovOperatorConfig object due to %s", err.Error())
	}

	return err == nil
}

// Delete removes a SriovOperatorConfig.
func (builder *SriovOperatorConfigBuilder) Delete() (*SriovOperatorConfigBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting SriovOperatorConfig %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return builder, nil
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Object)

	if err != nil {
		return builder, fmt.Errorf("cannot delete sriovOperatorConfig: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a SriovOperatorConfig in the cluster and stores the created object in struct.
func (builder *SriovOperatorConfigBuilder) Create() (*SriovOperatorConfigBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the SriovOperatorConfig %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		} else {
			glog.V(100).Infof("Error creating the SriovOperatorConfig '%s' : '%s'",
				builder.Definition.Name, err.Error())
		}
	}

	return builder, err
}

// Update renovates the existing SriovOperatorConfig object with the definition in builder.
func (builder *SriovOperatorConfigBuilder) Update() (*SriovOperatorConfigBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the SriovOperatorConfig object named: %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	err := builder.apiClient.Update(context.TODO(), builder.Definition)

	if err != nil {
		glog.V(100).Infof(msg.FailToUpdateNotification("sriovOperatorConfig", builder.Definition.Name,
			builder.Definition.Namespace))

		return builder, err
	}

	builder.Object = builder.Definition

	return builder, nil
}

// Patch sends a merge patch computed against original, applying only the fields that
// builder.Definition changed relative to it. Unlike Update, this does not replace the whole
// spec, so fields of the real upstream SriovOperatorConfigSpec that this package's trimmed local
// type does not know about are left untouched on the server.
func (builder *SriovOperatorConfigBuilder) Patch(
	original *sriovnetworkapi.SriovOperatorConfig) (*SriovOperatorConfigBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Patching the SriovOperatorConfig object named: %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	err := builder.apiClient.Patch(context.TODO(), builder.Definition, goclient.MergeFrom(original))

	if err != nil {
		glog.V(100).Infof(msg.FailToUpdateNotification("sriovOperatorConfig", builder.Definition.Name,
			builder.Definition.Namespace))

		return builder, err
	}

	builder.Object = builder.Definition

	return builder, nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *SriovOperatorConfigBuilder) validate() (bool, error) {
	resourceCRD := "SriovOperatorConfig"

	if builder == nil {
		glog.V(100).Infof("The %s builder is uninitialized", resourceCRD)

		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The %s is undefined", resourceCRD)

		builder.errorMsg = msg.UndefinedCrdObjectErrString(resourceCRD)
	}

	if builder.apiClient == nil {
		glog.V(100).Infof("The %s builder apiclient is nil", resourceCRD)

		builder.errorMsg = fmt.Sprintf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		glog.V(100).Infof("The %s builder has error message: %s", resourceCRD, builder.errorMsg)

		return false, errors.New(builder.errorMsg)
	}

	return true, nil
}
