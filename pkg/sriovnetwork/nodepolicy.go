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

// SriovNetworkNodePolicyBuilder provides a struct for SriovNetworkNodePolicy object containing connection to
// the cluster and the SriovNetworkNodePolicy definition.
type SriovNetworkNodePolicyBuilder struct {
	// SriovNetworkNodePolicyBuilder definition. Used to create
	// SriovNetworkNodePolicyBuilder object with minimum set of required elements.
	Definition *sriovnetworkapi.SriovNetworkNodePolicy
	// Created SriovNetworkNodePolicyBuilder object on the cluster.
	Object *sriovnetworkapi.SriovNetworkNodePolicy
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before SriovNetworkNodePolicyBuilder object is created.
	errorMsg string
}

// NewSriovNetworkNodePolicyBuilder returns a SriovNetworkNodePolicyBuilder struct.
func NewSriovNetworkNodePolicyBuilder(apiClient *clients.Settings, name, nsname string) *SriovNetworkNodePolicyBuilder {
	glog.V(100).Infof(
		"Initializing new SriovNetworkNodePolicyBuilder structure with name: %s, namespace: %s", name, nsname)

	builder := &SriovNetworkNodePolicyBuilder{
		apiClient: apiClient,
		Definition: &sriovnetworkapi.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The Name of the SriovNetworkNodePolicy is empty")

		builder.errorMsg = "SriovNetworkNodePolicy 'name' cannot be empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The Namespace of the SriovNetworkNodePolicy is empty")

		builder.errorMsg = "SriovNetworkNodePolicy 'nsname' cannot be empty"
	}

	return builder
}

// Get returns SriovNetworkNodePolicy object if found.
func (builder *SriovNetworkNodePolicyBuilder) Get() (*sriovnetworkapi.SriovNetworkNodePolicy, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof("Collecting SriovNetworkNodePolicy object %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	sriovNetworkNodePolicy := &sriovnetworkapi.SriovNetworkNodePolicy{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name:      builder.Definition.Name,
		Namespace: builder.Definition.Namespace,
	}, sriovNetworkNodePolicy)

	if err != nil {
		glog.V(100).Infof("SriovNetworkNodePolicy object %s doesn't exist in namespace %s",
			builder.Definition.Name, builder.Definition.Namespace)

		return nil, err
	}

	return sriovNetworkNodePolicy, nil
}

// PullSriovNetworkNodePolicy loads an existing SriovNetworkNodePolicy into SriovNetworkNodePolicyBuilder struct.
func PullSriovNetworkNodePolicy(apiClient *clients.Settings, name, nsname string) (*SriovNetworkNodePolicyBuilder, error) {
	glog.V(100).Infof("Pulling existing SriovNetworkNodePolicy name %s in namespace %s", name, nsname)

	builder := &SriovNetworkNodePolicyBuilder{
		apiClient: apiClient,
		Definition: &sriovnetworkapi.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		builder.errorMsg = "SriovNetworkNodePolicy 'name' cannot be empty"

		return nil, errors.New(builder.errorMsg)
	}

	if nsname == "" {
		builder.errorMsg = "SriovNetworkNodePolicy 'nsname' cannot be empty"

		return nil, errors.New(builder.errorMsg)
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("sriovNetworkNodePolicy object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// Exists checks whether the given SriovNetworkNodePolicy exists.
func (builder *SriovNetworkNodePolicyBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if SriovNetworkNodePolicy %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect SriovNetworkNodePolicy object due to %s", err.Error())
	}

	return err == nil
}

// Delete removes a SriovNetworkNodePolicy.
func (builder *SriovNetworkNodePolicyBuilder) Delete() (*SriovNetworkNodePolicyBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting SriovNetworkNodePolicy %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return builder, nil
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Object)

	if err != nil {
		return builder, fmt.Errorf("cannot delete sriovNetworkNodePolicy: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a SriovNetworkNodePolicy in the cluster and stores the created object in struct.
func (builder *SriovNetworkNodePolicyBuilder) Create() (*SriovNetworkNodePolicyBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the SriovNetworkNodePolicy %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		} else {
			glog.V(100).Infof("Error creating the SriovNetworkNodePolicy '%s' : '%s'",
				builder.Definition.Name, err.Error())
		}
	}

	return builder, err
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *SriovNetworkNodePolicyBuilder) validate() (bool, error) {
	resourceCRD := "SriovNetworkNodePolicy"

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
