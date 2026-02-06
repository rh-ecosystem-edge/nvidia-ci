package olm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/golang/glog"
	oplmV1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	errCatalogSourceNameEmpty   = "catalogsource 'name' cannot be empty"
	errCatalogSourceNsnameEmpty = "catalogsource 'nsname' cannot be empty"
)

// CatalogSourceBuilder provides a struct for catalogsource object
// from the cluster and a catalogsource definition.
type CatalogSourceBuilder struct {
	// CatalogSource definition. Used to create
	// CatalogSource object with minimum set of required elements.
	Definition *oplmV1alpha1.CatalogSource
	// Created CatalogSource object on the cluster.
	Object *oplmV1alpha1.CatalogSource
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before CatalogSourceBuilder object is created.
	errorMsg string
}

// NewCatalogSourceBuilder creates new instance of CatalogSourceBuilder.
func NewCatalogSourceBuilder(apiClient *clients.Settings, name, nsname string) *CatalogSourceBuilder {
	glog.V(100).Infof("Initializing new %s catalogsource structure", name)

	builder := CatalogSourceBuilder{
		apiClient: apiClient,
		Definition: &oplmV1alpha1.CatalogSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the catalogsource is empty")

		builder.errorMsg = errCatalogSourceNameEmpty
	}

	if nsname == "" {
		glog.V(100).Infof("The nsname of the catalogsource is empty")

		builder.errorMsg = errCatalogSourceNsnameEmpty
	}

	return &builder
}

// NewCatalogSourceBuilderWithIndexImage creates new instance of CatalogSourceBuilder.
func NewCatalogSourceBuilderWithIndexImage(apiClient *clients.Settings,
	name, nsname, indexImage, displayName, publisher string) *CatalogSourceBuilder {
	glog.V(100).Infof("Initializing new catalogsource structure with "+
		"name '%s', namespace '%s', index image '%s', display name '%s', and publisher '%s'",
		name, nsname, indexImage, displayName, publisher)

	builder := CatalogSourceBuilder{
		apiClient: apiClient,
		Definition: &oplmV1alpha1.CatalogSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
			Spec: oplmV1alpha1.CatalogSourceSpec{
				SourceType:  oplmV1alpha1.SourceTypeGrpc,
				Image:       indexImage,
				DisplayName: displayName,
				Publisher:   publisher,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the catalogsource is empty")

		builder.errorMsg = errCatalogSourceNameEmpty
	}

	if nsname == "" {
		glog.V(100).Infof("The nsname of the catalogsource is empty")

		builder.errorMsg = errCatalogSourceNsnameEmpty
	}

	if displayName == "" {
		glog.V(100).Infof("The display name of the catalogsource is empty")

		builder.errorMsg = "catalogsource 'display' cannot be empty"
	}

	if publisher == "" {
		glog.V(100).Infof("The publisher of the catalogsource is empty")

		builder.errorMsg = "catalogsource 'publisher' cannot be empty"
	}

	return &builder
}

// PullCatalogSource loads an existing catalogsource into Builder struct.
func PullCatalogSource(apiClient *clients.Settings, name, nsname string) (*CatalogSourceBuilder,
	error) {
	glog.V(100).Infof("Pulling existing catalogsource name %s in namespace %s", name, nsname)

	builder := CatalogSourceBuilder{
		apiClient: apiClient,
		Definition: &oplmV1alpha1.CatalogSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		builder.errorMsg = errCatalogSourceNameEmpty
	}

	if nsname == "" {
		builder.errorMsg = "catalogsource 'namespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("catalogsource object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Create makes an CatalogSourceBuilder in cluster and stores the created object in struct.
func (builder *CatalogSourceBuilder) Create() (*CatalogSourceBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the catalogsource %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.CatalogSources(builder.Definition.Namespace).Create(context.TODO(),
			builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Exists checks whether the given catalogsource exists.
func (builder *CatalogSourceBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if catalogSource %s exists",
		builder.Definition.Name)

	var err error
	builder.Object, err = builder.apiClient.OperatorsV1alpha1Interface.CatalogSources(
		builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a catalogsource.
func (builder *CatalogSourceBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting catalogsource %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.CatalogSources(builder.Definition.Namespace).Delete(context.TODO(),
		builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// IsReady periodically checks if catalogsource is in Ready state.
func (builder *CatalogSourceBuilder) IsReady(timeout time.Duration) bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Running periodic check until catalogsource '%s' in namespace '%s' is ready",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return false
	}

	err := wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			var err error
			builder.Object, err = builder.apiClient.CatalogSources(builder.Definition.Namespace).Get(
				context.TODO(), builder.Definition.Name, metav1.GetOptions{})

			if err != nil {
				return false, err
			}

			if builder.Object != nil && builder.Object.Status.GRPCConnectionState != nil && builder.Object.Status.GRPCConnectionState.LastObservedState == "READY" {
				return true, nil
			}

			return false, nil
		})

	return err == nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *CatalogSourceBuilder) validate() (bool, error) {
	resourceCRD := "catalogsource"

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
