package namespace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

// Builder provides struct for namespace object containing connection to the cluster and the namespace definitions.
type Builder struct {
	// Namespace definition. Used to create namespace object.
	Definition *corev1.Namespace
	// Created namespace object
	Object *corev1.Namespace
	// Used in functions that define or mutate namespace definition. errorMsg is processed before the namespace
	// object is created
	errorMsg  string
	apiClient *clients.Settings
}

// AdditionalOptions additional options for namespace object.
type AdditionalOptions func(builder *Builder) (*Builder, error)

// NewBuilder creates new instance of Builder.
func NewBuilder(apiClient *clients.Settings, name string) *Builder {
	glog.V(100).Infof(
		"Initializing new namespace structure with the following param: %s", name)

	builder := Builder{
		apiClient: apiClient,
		Definition: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the namespace is empty")

		builder.errorMsg = "namespace 'name' cannot be empty"
	}

	return &builder
}

// WithLabel redefines namespace definition with the given label.
func (builder *Builder) WithLabel(key string, value string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Labeling the namespace %s with %s=%s", builder.Definition.Name, key, value)

	if key == "" {
		glog.V(100).Infof("The key can't be empty")

		builder.errorMsg = "'key' cannot be empty"

		return builder
	}

	if builder.Definition.Labels == nil {
		builder.Definition.Labels = map[string]string{}
	}

	builder.Definition.Labels[key] = value

	return builder
}

// WithMultipleLabels redefines namespace definition with the given labels.
func (builder *Builder) WithMultipleLabels(labels map[string]string) *Builder {
	for k, v := range labels {
		builder.WithLabel(k, v)
	}

	return builder
}

// WithOptions creates namespace with generic mutation options.
func (builder *Builder) WithOptions(options ...AdditionalOptions) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting namespace additional options")

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

// Create makes a namespace in the cluster and stores the created object in struct.
func (builder *Builder) Create() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating namespace %s", builder.Definition.Name)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.Namespaces().Create(
			context.TODO(), builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Update renovates the existing namespace object with the namespace definition in builder.
func (builder *Builder) Update() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the namespace %s with the namespace definition in the builder", builder.Definition.Name)

	var err error
	builder.Object, err = builder.apiClient.Namespaces().Update(
		context.TODO(), builder.Definition, metav1.UpdateOptions{})

	return builder, err
}

// Delete removes a namespace.
func (builder *Builder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting namespace %s", builder.Definition.Name)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.Namespaces().Delete(context.TODO(), builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// DeleteAndWait deletes a namespace and waits until it's removed from the cluster.
func (builder *Builder) DeleteAndWait(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting namespace %s and waiting for the removal to complete", builder.Definition.Name)

	if err := builder.Delete(); err != nil {
		return err
	}

	return wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			_, err := builder.apiClient.Namespaces().Get(context.TODO(), builder.Definition.Name, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return true, nil
			}

			return false, nil
		})
}

// Exists checks whether the given namespace exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if namespace %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.apiClient.Namespaces().Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Pull loads existing namespace in to Builder struct.
func Pull(apiClient *clients.Settings, nsname string) (*Builder, error) {
	glog.V(100).Infof("Pulling existing namespace: %s from cluster", nsname)

	builder := Builder{
		apiClient: apiClient,
		Definition: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsname,
			},
		},
	}

	if nsname == "" {
		builder.errorMsg = "'namespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("namespace object %s doesn't exist", nsname)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// CleanObjects removes given objects from the namespace.
func (builder *Builder) CleanObjects(cleanTimeout time.Duration, objects ...schema.GroupVersionResource) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Clean namespace: %s", builder.Definition.Name)

	if len(objects) == 0 {
		return fmt.Errorf("failed to remove empty list of object from namespace %s",
			builder.Definition.Name)
	}

	if !builder.Exists() {
		return fmt.Errorf("failed to remove resources from non-existent namespace %s",
			builder.Definition.Name)
	}

	for _, resource := range objects {
		glog.V(100).Infof("Clean all resources: %s in namespace: %s",
			resource.Resource, builder.Definition.Name)

		err := builder.apiClient.Resource(resource).Namespace(builder.Definition.Name).DeleteCollection(
			context.TODO(), metav1.DeleteOptions{
				GracePeriodSeconds: ptr.To(int64(0)),
			}, metav1.ListOptions{})

		if err != nil {
			glog.V(100).Infof("Failed to remove resources: %s in namespace: %s",
				resource.Resource, builder.Definition.Name)

			return err
		}

		err = wait.PollUntilContextTimeout(
			context.TODO(), 3*time.Second, cleanTimeout, true, func(ctx context.Context) (bool, error) {
				objList, err := builder.apiClient.Resource(resource).Namespace(builder.Definition.Name).List(
					context.TODO(), metav1.ListOptions{})

				if err != nil || len(objList.Items) > 1 {
					// avoid timeout due to default automatically created openshift
					// configmaps: kube-root-ca.crt openshift-service-ca.crt
					if resource.Resource == "configmaps" {
						return builder.hasOnlyDefaultConfigMaps(objList, err)
					}

					return false, err
				}

				return true, err
			})

		if err != nil {
			glog.V(100).Infof("Failed to remove resources: %s in namespace: %s",
				resource.Resource, builder.Definition.Name)

			return err
		}
	}

	return nil
}

// hasOnlyDefaultConfigMaps returns true if only default configMaps are present in a namespace.
func (builder *Builder) hasOnlyDefaultConfigMaps(objList *unstructured.UnstructuredList, err error) (bool, error) {
	if valid, err := builder.validate(); !valid {
		return false, err
	}

	if err != nil {
		return false, err
	}

	if len(objList.Items) != 2 {
		return false, err
	}

	var existingConfigMaps []string
	for _, configMap := range objList.Items {
		existingConfigMaps = append(existingConfigMaps, configMap.GetName())
	}

	// return false if existing configmaps are NOT default pre-deployed openshift configmaps
	if !slices.Contains(existingConfigMaps, "kube-root-ca.crt") ||
		!slices.Contains(existingConfigMaps, "openshift-service-ca.crt") {
		return false, err
	}

	return true, nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "NameSpace"

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
