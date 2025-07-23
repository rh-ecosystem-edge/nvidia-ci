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

// NicClusterPolicyBuilder provides a struct for NicClusterPolicy object
// from the cluster and a NicClusterPolicy definition.
type NicClusterPolicyBuilder struct {
	// NicClusterPolicyBuilder definition. Used to create
	// NicClusterPolicyBuilder object with minimum set of required elements.
	Definition *nvidianetworkv1alpha1.NicClusterPolicy
	// Created NicClusterPolicyBuilder object on the cluster.
	Object *nvidianetworkv1alpha1.NicClusterPolicy
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before NicClusterPolicyBuilder object is created.
	errorMsg string
}

// NewNicClusterPolicyBuilderFromObjectString creates a NicClusterPolicyBuilder object from CSV alm-examples.
func NewNicClusterPolicyBuilderFromObjectString(apiClient *clients.Settings, almExample string) *NicClusterPolicyBuilder {
	glog.V(100).Infof(
		"Initializing new NicClusterPolicyBuilder structure from almExample string")

	nicClusterPolicy, err := getNicClusterPolicyFromAlmExample(almExample)

	if err != nil {
		glog.V(100).Infof(
			"Error initializing NicClusterPolicy from alm-examples: %s", err.Error())

		builder := NicClusterPolicyBuilder{
			apiClient: apiClient,
			errorMsg:  fmt.Sprintf("Error initializing NicClusterPolicy from alm-examples: %s", err.Error()),
		}

		return &builder
	}

	if nicClusterPolicy == nil {
		builder := NicClusterPolicyBuilder{
			apiClient: apiClient,
			errorMsg:  "NicClusterPolicy is nil after parsing almExample",
		}
		return &builder
	}

	glog.V(100).Infof(
		"Initializing new NicClusterPolicyBuilder structure from almExample string with NicClusterPolicy name: %s",
		nicClusterPolicy.Name)

	builder := NicClusterPolicyBuilder{
		apiClient:  apiClient,
		Definition: nicClusterPolicy,
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The NicClusterPolicy object definition is nil")

		builder.errorMsg = "NicClusterPolicy 'Object.Definition' is nil"
	}

	return &builder
}

// Get returns nicclusterPolicy object if found.
func (builder *NicClusterPolicyBuilder) Get() (*nvidianetworkv1alpha1.NicClusterPolicy, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof(
		"Collecting NicClusterPolicy object %s", builder.Definition.Name)

	nicClusterPolicy := &nvidianetworkv1alpha1.NicClusterPolicy{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name: builder.Definition.Name,
	}, nicClusterPolicy)

	if err != nil {
		glog.V(100).Infof(
			"NicClusterPolicy object %s doesn't exist", builder.Definition.Name)

		return nil, err
	}

	return nicClusterPolicy, err
}

// PullNicClusterPolicy loads an existing NicClusterPolicy into NicClusterPolicyBuilder struct.
func PullNicClusterPolicy(apiClient *clients.Settings, name string) (*NicClusterPolicyBuilder, error) {
	glog.V(100).Infof("Pulling existing nicClusterPolicy name: %s", name)

	builder := NicClusterPolicyBuilder{
		apiClient: apiClient,
		Definition: &nvidianetworkv1alpha1.NicClusterPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("NicClusterPolicy name is empty")

		builder.errorMsg = "NicClusterPolicy 'name' cannot be empty"
		return nil, errors.New(builder.errorMsg)
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("NicClusterPolicy object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given NicClusterPolicy exists.
func (builder *NicClusterPolicyBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if NicClusterPolicy %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect NicClusterPolicy object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a NicClusterPolicy.
func (builder *NicClusterPolicyBuilder) Delete() (*NicClusterPolicyBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting NicClusterPolicy %s", builder.Definition.Name)

	if !builder.Exists() {
		return builder, fmt.Errorf("nicclusterpolicy cannot be deleted because it does not exist")
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Definition)

	if err != nil {
		return builder, fmt.Errorf("cannot delete nicclusterpolicy: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a NicClusterPolicy in the cluster and stores the created object in struct.
func (builder *NicClusterPolicyBuilder) Create() (*NicClusterPolicyBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the NicClusterPolicy %s", builder.Definition.Name)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		} else {
			glog.V(100).Infof("Error creating the NicClusterPolicy '%s' : '%s'",
				builder.Definition.Name, err.Error())
		}
	}

	return builder, err
}

// Update renovates the existing NicClusterPolicy object with the definition in builder.
func (builder *NicClusterPolicyBuilder) Update(force bool) (*NicClusterPolicyBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the NicClusterPolicy object named:  %s", builder.Definition.Name)

	err := builder.apiClient.Update(context.TODO(), builder.Definition)

	if err != nil {
		if force {
			glog.V(100).Infof(msg.FailToUpdateNotification("nicclusterpolicy", builder.Definition.Name))

			builder, err := builder.Delete()

			if err != nil {
				glog.V(100).Infof(
					msg.FailToUpdateError("nicclusterpolicy", builder.Definition.Name))

				return nil, err
			}

			return builder.Create()
		}
	}

	return builder, err
}

// getNicClusterPolicyFromAlmExample extracts the NicClusterPolicy from the alm-examples block.
func getNicClusterPolicyFromAlmExample(almExample string) (*nvidianetworkv1alpha1.NicClusterPolicy, error) {
	nicClusterPolicyList := &nvidianetworkv1alpha1.NicClusterPolicyList{}

	if almExample == "" {
		return nil, fmt.Errorf("almExample is an empty string")
	}

	err := json.Unmarshal([]byte(almExample), &nicClusterPolicyList.Items)

	if err != nil {
		glog.V(100).Infof("Error unmarshalling NicClusterPolicy from almExamples: '%s'", err.Error())

		return nil, err
	}

	if len(nicClusterPolicyList.Items) == 0 {
		return nil, fmt.Errorf("failed to get alm examples")
	}

	for i := range nicClusterPolicyList.Items {
		if nicClusterPolicyList.Items[i].Kind == "NicClusterPolicy" {
			return &nicClusterPolicyList.Items[i], nil
		}
	}

	return nil, fmt.Errorf("NicClusterPolicy not found in alm examples")
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *NicClusterPolicyBuilder) validate() (bool, error) {
	resourceCRD := nvidianetworkv1alpha1.NicClusterPolicyCRDName
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
