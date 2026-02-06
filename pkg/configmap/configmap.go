package configmap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1Typed "k8s.io/client-go/kubernetes/typed/core/v1"
)

// Builder provides struct for configmap object containing connection to the cluster and the configmap definitions.
type Builder struct {
	// ConfigMap definition. Used to create configmap object.
	Definition *corev1.ConfigMap
	// Created configmap object.
	Object *corev1.ConfigMap
	// Used in functions that defines or mutates configmap definition. errorMsg is processed before the configmap
	// object is created.
	errorMsg  string
	apiClient corev1Typed.CoreV1Interface
}

// AdditionalOptions additional options for configmap object.
type AdditionalOptions func(builder *Builder) (*Builder, error)

// Pull retrieves an existing configmap object from the cluster.
func Pull(apiClient *clients.Settings, name, nsname string) (*Builder, error) {
	builder := Builder{
		apiClient: apiClient.CoreV1Interface,
		Definition: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the configmap is empty")

		return nil, fmt.Errorf("configmap 'name' cannot be empty")
	}

	if nsname == "" {
		glog.V(100).Infof("The namespace of the configmap is empty")

		return nil, fmt.Errorf("configmap 'nsname' cannot be empty")
	}

	glog.V(100).Infof(
		"Pulling configmap object name:%s in namespace: %s", name, nsname)

	if !builder.Exists() {
		return nil, fmt.Errorf("configmap object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// NewBuilder creates a new instance of Builder.
func NewBuilder(apiClient *clients.Settings, name, nsname string) *Builder {
	glog.V(100).Infof(
		"Initializing new configmap structure with the following params: %s, %s", name, nsname)

	builder := &Builder{
		apiClient: apiClient.CoreV1Interface,
		Definition: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the configmap is empty")

		builder.errorMsg = "configmap 'name' cannot be empty"

		return builder
	}

	if nsname == "" {
		glog.V(100).Infof("The namespace of the configmap is empty")

		builder.errorMsg = "configmap 'nsname' cannot be empty"

		return builder
	}

	return builder
}

// Create makes a configmap in cluster and stores the created object in struct.
func (builder *Builder) Create() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the configmap %s in namespace %s", builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.ConfigMaps(builder.Definition.Namespace).Create(
			context.TODO(), builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Delete removes a configmap.
func (builder *Builder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting the configmap %s from namespace %s", builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.ConfigMaps(builder.Definition.Namespace).Delete(
		context.TODO(), builder.Object.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	builder.Object = nil

	return nil
}

// WaitUntilDeleted waits for the duration of the defined timeout or until the configmap is deleted.
func (builder *Builder) WaitUntilDeleted(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until configmap %s in namespace %s is deleted",
		builder.Definition.Name, builder.Definition.Namespace)

	err := wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, false, func(ctx context.Context) (bool, error) {
			_, err := builder.apiClient.ConfigMaps(builder.Definition.Namespace).Get(
				ctx, builder.Definition.Name, metav1.GetOptions{})
			if err == nil {
				glog.V(100).Infof("configmap %s/%s still present", builder.Definition.Namespace, builder.Definition.Name)

				return false, nil
			}

			if k8serrors.IsNotFound(err) {
				glog.V(100).Infof("configmap %s/%s is gone", builder.Definition.Namespace, builder.Definition.Name)

				return true, nil
			}

			glog.V(100).Infof("failed to get configmap %s/%s: %v", builder.Definition.Namespace, builder.Definition.Name, err)

			return false, err
		})

	return err
}

// Exists checks whether the given configmap exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if configmap %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.ConfigMaps(builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// WithData defines the data placed in the configmap.
func (builder *Builder) WithData(data map[string]string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof(
		"Creating configmap %s in namespace %s with this data: %s",
		builder.Definition.Name, builder.Definition.Namespace, data)

	if len(data) == 0 {
		builder.errorMsg = "'data' cannot be empty"

		return builder
	}

	builder.Definition.Data = data

	return builder
}

// WithOptions creates configmap with generic mutation options.
func (builder *Builder) WithOptions(options ...AdditionalOptions) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting configmap additional options")

	for _, option := range options {
		if option != nil {
			builder, err := option(builder)

			if err != nil {
				glog.V(100).Infof("Error occurred in mutation function")

				builder.errorMsg = err.Error()

				return builder
			}
		}
	}

	return builder
}

// GetGVR returns configmap's GroupVersionResource which could be used for Clean function.
func GetGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: "", Version: "v1", Resource: "configmaps",
	}
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "ConfigMap"

	if builder == nil {
		glog.V(100).Infof("The %s builder is uninitialized", resourceCRD)

		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The %s is undefined", resourceCRD)

		return false, errors.New(msg.UndefinedCrdObjectErrString(resourceCRD))
	}

	if builder.apiClient == nil {
		glog.V(100).Infof("The %s builder apiclient is nil", resourceCRD)

		return false, fmt.Errorf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		glog.V(100).Infof("The %s builder has error message: %s", resourceCRD, builder.errorMsg)

		return false, errors.New(builder.errorMsg)
	}

	return true, nil
}
