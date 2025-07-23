package olm

import (
	"context"
	"errors"
	"fmt"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/golang/glog"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstallPlanBuilder provides a struct for installplan object from the cluster and an installplan definition.
type InstallPlanBuilder struct {
	// Installplan definition, used to create the installplan object.
	Definition *v1alpha1.InstallPlan
	// Created installplan object.
	Object *v1alpha1.InstallPlan
	// Used in functions that define or mutate installplan definition. errorMsg is processed
	// before the installplan object is created
	errorMsg string
	// api client to interact with the cluster.
	apiClient *clients.Settings
}

// NewInstallPlanBuilder creates new instance of InstallPlanBuilder.
func NewInstallPlanBuilder(apiClient *clients.Settings, name, nsname string) *InstallPlanBuilder {
	glog.V(100).Infof("Initializing new %s installplan structure", name)

	builder := InstallPlanBuilder{
		apiClient: apiClient,
		Definition: &v1alpha1.InstallPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the installplan is empty")

		builder.errorMsg = "installplan 'name' cannot be empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The nsname of the installplan is empty")

		builder.errorMsg = "installplan 'nsname' cannot be empty"
	}

	return &builder
}

// Create makes an InstallPlanBuilder in cluster and stores the created object in struct.
func (builder *InstallPlanBuilder) Create() (*InstallPlanBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the InstallPlan %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.InstallPlans(builder.Definition.Namespace).Create(context.TODO(),
			builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Exists checks whether the given installplan exists.
func (builder *InstallPlanBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if installplan %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.InstallPlans(builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes an installplan.
func (builder *InstallPlanBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting installplan %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.InstallPlans(builder.Definition.Namespace).Delete(context.TODO(),
		builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// Update modifies the existing InstallPlanBuilder with the InstallPlan definition in InstallPlanBuilder.
func (builder *InstallPlanBuilder) Update() (*InstallPlanBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating installPlan %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.InstallPlans(builder.Definition.Namespace).Update(
		context.TODO(), builder.Definition, metav1.UpdateOptions{})

	return builder, err
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *InstallPlanBuilder) validate() (bool, error) {
	resourceCRD := "installplan"

	if builder == nil {
		glog.V(100).Infof("The builder %s is uninitialized", resourceCRD)

		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The %s is undefined", resourceCRD)

		builder.errorMsg = msg.UndefinedCrdObjectErrString(resourceCRD)
	}

	if builder.apiClient == nil {
		glog.V(100).Infof("The builder %s apiclient is nil", resourceCRD)

		builder.errorMsg = fmt.Sprintf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		glog.V(100).Infof("The builder %s has error message: %w", resourceCRD, builder.errorMsg)

		return false, errors.New(builder.errorMsg)
	}

	return true, nil
}
