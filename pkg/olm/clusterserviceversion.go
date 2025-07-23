package olm

import (
	"context"
	"errors"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/golang/glog"
	oplmV1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterServiceVersionBuilder provides a struct for clusterserviceversion object
// from the cluster and a clusterserviceversion definition.
type ClusterServiceVersionBuilder struct {
	// ClusterServiceVersionBuilder definition. Used to create
	// ClusterServiceVersionBuilder object with minimum set of required elements.
	Definition *oplmV1alpha1.ClusterServiceVersion
	// Created ClusterServiceVersionBuilder object on the cluster.
	Object *oplmV1alpha1.ClusterServiceVersion
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before ClusterServiceVersionBuilder object is created.
	errorMsg string
}

// PullClusterServiceVersion loads an existing clusterserviceversion into Builder struct.
func PullClusterServiceVersion(apiClient *clients.Settings, name, namespace string) (*ClusterServiceVersionBuilder,
	error) {
	glog.V(100).Infof("Pulling existing clusterserviceversion name %s in namespace %s", name, namespace)

	builder := ClusterServiceVersionBuilder{
		apiClient: apiClient,
		Definition: &oplmV1alpha1.ClusterServiceVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}

	if name == "" {
		builder.errorMsg = "clusterserviceversion 'name' cannot be empty"
	}

	if namespace == "" {
		builder.errorMsg = "clusterserviceversion 'namespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("clusterserviceversion object %s doesn't exist in namespace %s", name, namespace)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given clusterserviceversion exists.
func (builder *ClusterServiceVersionBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if clusterserviceversion %s exists",
		builder.Definition.Name)

	var err error
	builder.Object, err = builder.apiClient.OperatorsV1alpha1Interface.ClusterServiceVersions(
		builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a clusterserviceversion.
func (builder *ClusterServiceVersionBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting clusterserviceversion %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.ClusterServiceVersions(builder.Definition.Namespace).Delete(context.TODO(),
		builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// GetAlmExamples extracts and returns the alm-examples block from the clusterserviceversion.
func (builder *ClusterServiceVersionBuilder) GetAlmExamples() (string, error) {
	if valid, err := builder.validate(); !valid {
		return "", err
	}

	glog.V(100).Infof("Extracting the 'alm-examples' section from clusterserviceversion %s in "+
		"namespace %s", builder.Definition.Name, builder.Definition.Namespace)

	almExamples := "alm-examples"

	if builder.Exists() {
		annotations := builder.Object.ObjectMeta.GetAnnotations()

		if example, ok := annotations[almExamples]; ok {
			return example, nil
		}
	}

	return "", fmt.Errorf("%s not found in given clusterserviceversion named %v",
		almExamples, builder.Definition.Name)
}

// IsSuccessful checks if the clusterserviceversion is Successful.
func (builder *ClusterServiceVersionBuilder) IsSuccessful() (bool, error) {
	if valid, err := builder.validate(); !valid {
		return false, err
	}

	glog.V(100).Infof("Verify clusterserviceversion %s in namespace %s is Successful",
		builder.Definition.Name, builder.Definition.Namespace)

	phase, err := builder.GetPhase()

	if err != nil {
		return false, fmt.Errorf("failed to get phase value for %s clusterserviceversion in %s namespace due to %w",
			builder.Definition.Name, builder.Definition.Namespace, err)
	}

	return phase == "Succeeded", nil
}

// GetPhase gets current clusterserviceversion phase.
func (builder *ClusterServiceVersionBuilder) GetPhase() (oplmV1alpha1.ClusterServiceVersionPhase, error) {
	if valid, err := builder.validate(); !valid {
		return "", err
	}

	glog.V(100).Infof("Get clusterserviceversion %s phase in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return "", fmt.Errorf("%s clusterserviceversion not found in %s namespace",
			builder.Definition.Name, builder.Definition.Namespace)
	}

	return builder.Object.Status.Phase, nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *ClusterServiceVersionBuilder) validate() (bool, error) {
	resourceCRD := "ClusterServiceVersion"

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
