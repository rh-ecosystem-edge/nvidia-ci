package nvidianetwork

import (
	"context"
	"errors"
	"fmt"

	nvidianetworkv1alpha1 "github.com/Mellanox/network-operator/api/v1alpha1"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// MacvlanNetworkBuilder  provides a struct for MacvlanNetwork object
// from the cluster and a MacvlanNetwork definition.
type MacvlanNetworkBuilder struct {
	// MacvlanNetworkBuilder  definition. Used to create
	// MacvlanNetworkBuilder  object with minimum set of required elements.
	Definition *nvidianetworkv1alpha1.MacvlanNetwork
	// Created MacvlanNetworkBuilder  object on the cluster.
	Object *nvidianetworkv1alpha1.MacvlanNetwork
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before MacvlanNetworkBuilder  object is created.
	errorMsg string
}

// NewMacvlanNetworkBuilderFromObjectString creates a MacvlanNetworkBuilder  object from CSV alm-examples.
func NewMacvlanNetworkBuilderFromObjectString(apiClient *clients.Settings, almExample string) *MacvlanNetworkBuilder {
	glog.V(100).Infof(
		"Initializing new MacvlanNetworkBuilder  structure from almExample string")

	macvlanNetwork, err := getMacvlanNetworkFromAlmExample(almExample)

	if err != nil {
		glog.V(100).Infof(
			"Error initializing MacvlanNetwork from alm-examples: %s", err.Error())

		builder := MacvlanNetworkBuilder{
			apiClient: apiClient,
			errorMsg:  fmt.Sprintf("Error initializing MacvlanNetwork from alm-examples: %s", err.Error()),
		}

		return &builder
	}

	if macvlanNetwork == nil {
		builder := MacvlanNetworkBuilder{
			apiClient: apiClient,
			errorMsg:  "MacvlanNetwork is nil after parsing almExample",
		}
		return &builder
	}

	glog.V(100).Infof(
		"Initializing new MacvlanNetworkBuilder  structure from almExample string with MacvlanNetwork name: %s",
		macvlanNetwork.Name)

	builder := MacvlanNetworkBuilder{
		apiClient:  apiClient,
		Definition: macvlanNetwork,
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The MacvlanNetwork object definition is nil")

		builder.errorMsg = "MacvlanNetwork 'Object.Definition' is nil"
	}

	return &builder
}

// Get returns MacvlanNetwork object if found.
func (builder *MacvlanNetworkBuilder) Get() (*nvidianetworkv1alpha1.MacvlanNetwork, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof(
		"Collecting MacvlanNetwork object %s", builder.Definition.Name)

	MacvlanNetwork := &nvidianetworkv1alpha1.MacvlanNetwork{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name: builder.Definition.Name,
	}, MacvlanNetwork)

	if err != nil {
		glog.V(100).Infof(
			"MacvlanNetwork object %s doesn't exist", builder.Definition.Name)

		return nil, err
	}

	return MacvlanNetwork, err
}

// PullMacvlanNetwork loads an existing MacvlanNetwork into MacvlanNetworkBuilder  struct.
func PullMacvlanNetwork(apiClient *clients.Settings, name string) (*MacvlanNetworkBuilder, error) {
	glog.V(100).Infof("Pulling existing MacvlanNetwork name: %s", name)

	builder := MacvlanNetworkBuilder{
		apiClient: apiClient,
		Definition: &nvidianetworkv1alpha1.MacvlanNetwork{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("MacvlanNetwork name is empty")

		builder.errorMsg = "MacvlanNetwork 'name' cannot be empty"
		return nil, errors.New(builder.errorMsg)
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("MacvlanNetwork object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given MacvlanNetwork exists.
func (builder *MacvlanNetworkBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if MacvlanNetwork %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect MacvlanNetwork object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a MacvlanNetwork.
func (builder *MacvlanNetworkBuilder) Delete() (*MacvlanNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting MacvlanNetwork %s", builder.Definition.Name)

	if !builder.Exists() {
		return builder, errors.New("MacvlanNetwork cannot be deleted because it does not exist")
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Definition)

	if err != nil {
		return builder, fmt.Errorf("cannot delete MacvlanNetwork: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a MacvlanNetwork in the cluster and stores the created object in struct.
func (builder *MacvlanNetworkBuilder) Create() (*MacvlanNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the MacvlanNetwork %s", builder.Definition.Name)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		} else {
			glog.V(100).Infof("Error creating the MacvlanNetwork '%s' : '%s'",
				builder.Definition.Name, err.Error())
		}
	}

	return builder, err
}

// Update renovates the existing MacvlanNetwork object with the definition in builder.
func (builder *MacvlanNetworkBuilder) Update(force bool) (*MacvlanNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the MacvlanNetwork object named:  %s", builder.Definition.Name)

	err := builder.apiClient.Update(context.TODO(), builder.Definition)

	if err != nil {
		if force {
			glog.V(100).Infof(msg.FailToUpdateNotification("MacvlanNetwork", builder.Definition.Name))

			builder, err := builder.Delete()

			if err != nil {
				glog.V(100).Infof(
					msg.FailToUpdateError("MacvlanNetwork", builder.Definition.Name))

				return nil, err
			}

			return builder.Create()
		}
	}

	return builder, err
}

// getMacvlanNetworkFromAlmExample extracts the MacvlanNetwork from the alm-examples block.
func getMacvlanNetworkFromAlmExample(almExample string) (*nvidianetworkv1alpha1.MacvlanNetwork, error) {
	MacvlanNetworkList := &nvidianetworkv1alpha1.MacvlanNetworkList{}

	if almExample == "" {
		return nil, errors.New("almExample is an empty string")
	}

	err := json.Unmarshal([]byte(almExample), &MacvlanNetworkList.Items)

	if err != nil {
		glog.V(100).Infof("Error unmarshalling MacvlanNetwork from almExamples: '%s'", err.Error())

		return nil, err
	}

	if len(MacvlanNetworkList.Items) == 0 {
		return nil, errors.New("failed to get alm examples")
	}

	for i := range MacvlanNetworkList.Items {
		if MacvlanNetworkList.Items[i].Kind == "MacvlanNetwork" {
			return &MacvlanNetworkList.Items[i], nil
		}
	}

	return nil, errors.New("MacvlanNetwork not found in alm examples")
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *MacvlanNetworkBuilder) validate() (bool, error) {
	resourceCRD := nvidianetworkv1alpha1.MacvlanNetworkCRDName
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
