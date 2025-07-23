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

// IPoIBNetworkBuilder  provides a struct for IPoIBNetwork object
// from the cluster and a IPoIBNetwork definition.
type IPoIBNetworkBuilder struct {
	// IPoIBNetworkBuilder  definition. Used to create
	// IPoIBNetworkBuilder  object with minimum set of required elements.
	Definition *nvidianetworkv1alpha1.IPoIBNetwork
	// Created IPoIBNetworkBuilder  object on the cluster.
	Object *nvidianetworkv1alpha1.IPoIBNetwork
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before IPoIBNetworkBuilder  object is created.
	errorMsg string
}

// NewIPoIBNetworkBuilderFromObjectString creates a IPoIBNetworkBuilder  object from CSV alm-examples.
func NewIPoIBNetworkBuilderFromObjectString(apiClient *clients.Settings, almExample string) *IPoIBNetworkBuilder {
	glog.V(100).Infof(
		"Initializing new IPoIBNetworkBuilder  structure from almExample string")

	IPoIBNetwork, err := getIPoIBNetworkFromAlmExample(almExample)

	if err != nil {
		glog.V(100).Infof(
			"Error initializing IPoIBNetwork from alm-examples: %s", err.Error())

		builder := IPoIBNetworkBuilder{
			apiClient: apiClient,
			errorMsg:  fmt.Sprintf("Error initializing IPoIBNetwork from alm-examples: %s", err.Error()),
		}

		return &builder
	}

	if IPoIBNetwork == nil {
		builder := IPoIBNetworkBuilder{
			apiClient: apiClient,
			errorMsg:  "IPoIBNetwork is nil after parsing almExample",
		}
		return &builder
	}

	glog.V(100).Infof(
		"Initializing new IPoIBNetworkBuilder  structure from almExample string with IPoIBNetwork name: %s",
		IPoIBNetwork.Name)

	builder := IPoIBNetworkBuilder{
		apiClient:  apiClient,
		Definition: IPoIBNetwork,
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The IPoIBNetwork object definition is nil")

		builder.errorMsg = "IPoIBNetwork 'Object.Definition' is nil"
	}

	return &builder
}

// Get returns IPoIBNetwork object if found.
func (builder *IPoIBNetworkBuilder) Get() (*nvidianetworkv1alpha1.IPoIBNetwork, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof(
		"Collecting IPoIBNetwork object %s", builder.Definition.Name)

	IPoIBNetwork := &nvidianetworkv1alpha1.IPoIBNetwork{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name: builder.Definition.Name,
	}, IPoIBNetwork)

	if err != nil {
		glog.V(100).Infof(
			"IPoIBNetwork object %s doesn't exist", builder.Definition.Name)

		return nil, err
	}

	return IPoIBNetwork, err
}

// PullIPoIBNetwork loads an existing IPoIBNetwork into IPoIBNetworkBuilder  struct.
func PullIPoIBNetwork(apiClient *clients.Settings, name string) (*IPoIBNetworkBuilder, error) {
	glog.V(100).Infof("Pulling existing IPoIBNetwork name: %s", name)

	builder := IPoIBNetworkBuilder{
		apiClient: apiClient,
		Definition: &nvidianetworkv1alpha1.IPoIBNetwork{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("IPoIBNetwork name is empty")

		builder.errorMsg = "IPoIBNetwork 'name' cannot be empty"
		return nil, errors.New(builder.errorMsg)
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("IPoIBNetwork object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given IPoIBNetwork exists.
func (builder *IPoIBNetworkBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if IPoIBNetwork %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect IPoIBNetwork object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a IPoIBNetwork.
func (builder *IPoIBNetworkBuilder) Delete() (*IPoIBNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting IPoIBNetwork %s", builder.Definition.Name)

	if !builder.Exists() {
		return builder, fmt.Errorf("IPoIBNetwork cannot be deleted because it does not exist")
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Definition)

	if err != nil {
		return builder, fmt.Errorf("cannot delete IPoIBNetwork: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a IPoIBNetwork in the cluster and stores the created object in struct.
func (builder *IPoIBNetworkBuilder) Create() (*IPoIBNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the IPoIBNetwork %s", builder.Definition.Name)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		} else {
			glog.V(100).Infof("Error creating the IPoIBNetwork '%s' : '%s'",
				builder.Definition.Name, err.Error())
		}
	}

	return builder, err
}

// Update renovates the existing IPoIBNetwork object with the definition in builder.
func (builder *IPoIBNetworkBuilder) Update(force bool) (*IPoIBNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the IPoIBNetwork object named:  %s", builder.Definition.Name)

	err := builder.apiClient.Update(context.TODO(), builder.Definition)

	if err != nil {
		if force {
			glog.V(100).Infof(msg.FailToUpdateNotification("IPoIBNetwork", builder.Definition.Name))

			builder, err := builder.Delete()

			if err != nil {
				glog.V(100).Infof(
					msg.FailToUpdateError("IPoIBNetwork", builder.Definition.Name))

				return nil, err
			}

			return builder.Create()
		}
	}

	return builder, err
}

// getIPoIBNetworkFromAlmExample extracts the IPoIBNetwork from the alm-examples block.
func getIPoIBNetworkFromAlmExample(almExample string) (*nvidianetworkv1alpha1.IPoIBNetwork, error) {
	IPoIBNetworkList := &nvidianetworkv1alpha1.IPoIBNetworkList{}

	if almExample == "" {
		return nil, fmt.Errorf("almExample is an empty string")
	}

	err := json.Unmarshal([]byte(almExample), &IPoIBNetworkList.Items)

	if err != nil {
		glog.V(100).Infof("Error unmarshalling IPoIBNetwork from almExamples: '%s'", err.Error())

		return nil, err
	}

	if len(IPoIBNetworkList.Items) == 0 {
		return nil, fmt.Errorf("failed to get alm examples")
	}

	for i := range IPoIBNetworkList.Items {
		if IPoIBNetworkList.Items[i].Kind == "IPoIBNetwork" {
			return &IPoIBNetworkList.Items[i], nil
		}
	}

	return nil, fmt.Errorf("IPoIBNetwork not found in alm examples")
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *IPoIBNetworkBuilder) validate() (bool, error) {
	resourceCRD := nvidianetworkv1alpha1.IPoIBNetworkCRDName
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
