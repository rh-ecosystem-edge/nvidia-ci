package olm

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/glog"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	olmv1 "github.com/operator-framework/api/pkg/operators/v1"
)

// OperatorGroupBuilder provides a struct for OperatorGroup object containing connection to the
// cluster and the OperatorGroup definition.
type OperatorGroupBuilder struct {
	// OperatorGroup definition. Used to create OperatorGroup object with minimum set of required elements.
	Definition *olmv1.OperatorGroup
	// Created OperatorGroup object on the cluster.
	Object *olmv1.OperatorGroup
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before OperatorGroup object is created.
	errorMsg string
}

// NewOperatorGroupBuilder returns an OperatorGroupBuilder struct.
func NewOperatorGroupBuilder(apiClient *clients.Settings, groupName, nsName string) *OperatorGroupBuilder {
	glog.V(100).Infof(
		"Initializing new OperatorGroupBuilder structure with the following params: %s, %s", groupName, nsName)

	builder := &OperatorGroupBuilder{
		apiClient: apiClient,
		Definition: &olmv1.OperatorGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:         groupName,
				Namespace:    nsName,
				GenerateName: fmt.Sprintf("%v-", groupName),
			},
			Spec: olmv1.OperatorGroupSpec{
				TargetNamespaces: []string{nsName},
			},
		},
	}

	if groupName == "" {
		glog.V(100).Infof("The Name of the OperatorGroup is empty")

		builder.errorMsg = "OperatorGroup 'groupName' cannot be empty"
	}

	if nsName == "" {
		glog.V(100).Infof("The Namespace of the OperatorGroup is empty")

		builder.errorMsg = "OperatorGroup 'Namespace' cannot be empty"
	}

	return builder
}

// Create makes an OperatorGroup in cluster and stores the created object in struct.
func (builder *OperatorGroupBuilder) Create() (*OperatorGroupBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the OperatorGroup %s",
		builder.Definition.Name)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.OperatorGroups(builder.Definition.Namespace).Create(context.TODO(),
			builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Exists checks whether the given OperatorGroup exists.
func (builder *OperatorGroupBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if OperatorGroup %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error

	builder.Object, err = builder.apiClient.OperatorGroups(builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes an OperatorGroup.
func (builder *OperatorGroupBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting OperatorGroup %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.OperatorGroups(builder.Definition.Namespace).Delete(context.TODO(), builder.Object.Name,
		metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// Update modifies the existing OperatorGroup with the OperatorGroup definition in OperatorGroupBuilder.
func (builder *OperatorGroupBuilder) Update() (*OperatorGroupBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating OperatorGroup %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.OperatorGroups(builder.Definition.Namespace).Update(
		context.TODO(), builder.Definition, metav1.UpdateOptions{})

	return builder, err
}

// PullOperatorGroup loads existing OperatorGroup from cluster into the OperatorGroupBuilder struct.
func PullOperatorGroup(apiClient *clients.Settings, groupName, nsName string) (*OperatorGroupBuilder, error) {
	glog.V(100).Infof("Pulling existing OperatorGroup %s from cluster in namespace %s",
		groupName, nsName)

	builder := &OperatorGroupBuilder{
		apiClient: apiClient,
		Definition: &olmv1.OperatorGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:         groupName,
				Namespace:    nsName,
				GenerateName: fmt.Sprintf("%v-", groupName),
			},
		},
	}

	if groupName == "" {
		glog.V(100).Infof("The name of the OperatorGroup is empty")

		builder.errorMsg = "OperatorGroup 'Name' cannot be empty"
	}

	if nsName == "" {
		glog.V(100).Infof("The namespace of the OperatorGroup is empty")

		builder.errorMsg = "OperatorGroup 'Namespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("OperatorGroup object named %s doesn't exist", nsName)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *OperatorGroupBuilder) validate() (bool, error) {
	resourceCRD := "OperatorGroup"

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
