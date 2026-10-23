package sriovnetwork

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/glog"
	sriovnetworkapi "github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork/api"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// SriovNetworkBuilder provides a struct for SriovNetwork object containing connection to the
// cluster and the SriovNetwork definition.
type SriovNetworkBuilder struct {
	// SriovNetworkBuilder definition. Used to create
	// SriovNetworkBuilder object with minimum set of required elements.
	Definition *sriovnetworkapi.SriovNetwork
	// Created SriovNetworkBuilder object on the cluster.
	Object *sriovnetworkapi.SriovNetwork
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before SriovNetworkBuilder object is created.
	errorMsg string
}

// NewSriovNetworkBuilder returns a SriovNetworkBuilder struct.
func NewSriovNetworkBuilder(apiClient *clients.Settings, name, nsname string) *SriovNetworkBuilder {
	glog.V(100).Infof(
		"Initializing new SriovNetworkBuilder structure with name: %s, namespace: %s", name, nsname)

	builder := &SriovNetworkBuilder{
		apiClient: apiClient,
		Definition: &sriovnetworkapi.SriovNetwork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The Name of the SriovNetwork is empty")

		builder.errorMsg = "SriovNetwork 'name' cannot be empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The Namespace of the SriovNetwork is empty")

		builder.errorMsg = "SriovNetwork 'nsname' cannot be empty"
	}

	return builder
}

// Get returns SriovNetwork object if found.
func (builder *SriovNetworkBuilder) Get() (*sriovnetworkapi.SriovNetwork, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof("Collecting SriovNetwork object %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	sriovNetwork := &sriovnetworkapi.SriovNetwork{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name:      builder.Definition.Name,
		Namespace: builder.Definition.Namespace,
	}, sriovNetwork)

	if err != nil {
		glog.V(100).Infof("SriovNetwork object %s doesn't exist in namespace %s",
			builder.Definition.Name, builder.Definition.Namespace)

		return nil, err
	}

	return sriovNetwork, nil
}

// PullSriovNetwork loads an existing SriovNetwork into SriovNetworkBuilder struct.
func PullSriovNetwork(apiClient *clients.Settings, name, nsname string) (*SriovNetworkBuilder, error) {
	glog.V(100).Infof("Pulling existing SriovNetwork name %s in namespace %s", name, nsname)

	builder := &SriovNetworkBuilder{
		apiClient: apiClient,
		Definition: &sriovnetworkapi.SriovNetwork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		builder.errorMsg = "SriovNetwork 'name' cannot be empty"

		return nil, errors.New(builder.errorMsg)
	}

	if nsname == "" {
		builder.errorMsg = "SriovNetwork 'nsname' cannot be empty"

		return nil, errors.New(builder.errorMsg)
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("sriovNetwork object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// Exists checks whether the given SriovNetwork exists.
func (builder *SriovNetworkBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if SriovNetwork %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect SriovNetwork object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a SriovNetwork.
func (builder *SriovNetworkBuilder) Delete() (*SriovNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting SriovNetwork %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return builder, nil
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Object)

	if err != nil {
		return builder, fmt.Errorf("cannot delete sriovNetwork: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a SriovNetwork in the cluster and stores the created object in struct.
func (builder *SriovNetworkBuilder) Create() (*SriovNetworkBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the SriovNetwork %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		} else {
			glog.V(100).Infof("Error creating the SriovNetwork '%s' : '%s'",
				builder.Definition.Name, err.Error())
		}
	}

	return builder, err
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *SriovNetworkBuilder) validate() (bool, error) {
	resourceCRD := "SriovNetwork"

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
