package nvidiagpu

import (
	"context"
	"fmt"
	"strings"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sjson "k8s.io/apimachinery/pkg/util/json"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Builder provides a struct for ClusterPolicy object
// from the cluster and a ClusterPolicy definition.
type Builder struct {
	// Builder definition. Used to create
	// Builder object with minimum set of required elements.
	Definition *nvidiagpuv1.ClusterPolicy
	// Created Builder object on the cluster.
	Object *nvidiagpuv1.ClusterPolicy
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before Builder object is created.
	errorMsg string
}

// NewBuilderFromObjectString creates a Builder object from CSV alm-examples.
func NewBuilderFromObjectString(apiClient *clients.Settings, almExample string) *Builder {
	glog.V(100).Infof("Initializing new Builder structure from almExample string")
	var clusterPolicy nvidiagpuv1.ClusterPolicy
	clusterPolicyExample, err := olm.GetALMExampleItem(0, almExample)
	if err != nil {
		return newBuilder(apiClient, &clusterPolicy, err)
	}

	err = k8sjson.Unmarshal(clusterPolicyExample, &clusterPolicy)
	return newBuilder(apiClient, &clusterPolicy, err)
}

// NewBuilderFromObjectStringAndPatch creates a Builder object from CSV alm-examples and applies an RFC6902 JSON patch to it.
func NewBuilderFromObjectStringAndPatch(apiClient *clients.Settings, almExample, patchJSON string) *Builder {
	glog.V(100).Infof("Initializing new Builder structure from almExample string and a patch JSON")
	var clusterPolicy nvidiagpuv1.ClusterPolicy
	if strings.TrimSpace(patchJSON) == "" {
		err := fmt.Errorf("patch JSON cannot be an empty string")
		return newBuilder(apiClient, &clusterPolicy, err)
	}

	clusterPolicyExample, err := olm.GetALMExampleItem(0, almExample)
	if err != nil {
		return newBuilder(apiClient, &clusterPolicy, err)
	}

	patch, err := jsonpatch.DecodePatch([]byte(patchJSON))
	if err != nil {
		return newBuilder(apiClient, &clusterPolicy, fmt.Errorf("invalid JSON patch: %w", err))
	}

	glog.V(100).Infof("Applying patch to the default cluster policy")
	modifiedExample, err := patch.Apply(clusterPolicyExample)
	if err != nil {
		return newBuilder(apiClient, &clusterPolicy, err)
	}

	err = k8sjson.Unmarshal(modifiedExample, &clusterPolicy)
	return newBuilder(apiClient, &clusterPolicy, err)
}

func newBuilder(apiClient *clients.Settings, clusterPolicy *nvidiagpuv1.ClusterPolicy, err error) *Builder {
	glog.V(100).Infof(
		"Initializing new Builder structure with clusterPolicy name: %s", clusterPolicy.Name)

	builder := Builder{
		apiClient:  apiClient,
		Definition: clusterPolicy,
	}

	if err != nil {
		glog.V(100).Infof(
			"Error initializing ClusterPolicy from alm-examples: %s", err.Error())

		builder.errorMsg = fmt.Sprintf("Error initializing ClusterPolicy from alm-examples: %s", err.Error())
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The ClusterPolicy object definition is nil")

		builder.errorMsg = "ClusterPolicy 'Object.Definition' is nil"
	}

	return &builder
}

// Get returns clusterPolicy object if found.
func (builder *Builder) Get() (*nvidiagpuv1.ClusterPolicy, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof(
		"Collecting ClusterPolicy object %s", builder.Definition.Name)

	clusterPolicy := &nvidiagpuv1.ClusterPolicy{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name: builder.Definition.Name,
	}, clusterPolicy)

	if err != nil {
		glog.V(100).Infof(
			"ClusterPolicy object %s doesn't exist", builder.Definition.Name)

		return nil, err
	}

	return clusterPolicy, err
}

// Pull loads an existing clusterPolicy into Builder struct.
func Pull(apiClient *clients.Settings, name string) (*Builder, error) {
	glog.V(100).Infof("Pulling existing clusterPolicy name: %s", name)

	builder := Builder{
		apiClient: apiClient,
		Definition: &nvidiagpuv1.ClusterPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("ClusterPolicy name is empty")

		builder.errorMsg = "ClusterPolicy 'name' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("ClusterPolicy object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given ClusterPolicy exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if ClusterPolicy %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect ClusterPolicy object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a ClusterPolicy.
func (builder *Builder) Delete() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting ClusterPolicy %s", builder.Definition.Name)

	if !builder.Exists() {
		return builder, fmt.Errorf("clusterpolicy cannot be deleted because it does not exist")
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Definition)

	if err != nil {
		return builder, fmt.Errorf("cannot delete clusterpolicy: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a ClusterPolicy in the cluster and stores the created object in struct.
func (builder *Builder) Create() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the ClusterPolicy %s", builder.Definition.Name)

	var err error
	if !builder.Exists() {
		err = builder.apiClient.Create(context.TODO(), builder.Definition)

		if err == nil {
			builder.Object = builder.Definition
		}
	}

	return builder, err
}

// Update renovates the existing ClusterPolicy object with the definition in builder.
func (builder *Builder) Update(force bool) (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating the ClusterPolicy object named:  %s", builder.Definition.Name)

	err := builder.apiClient.Update(context.TODO(), builder.Definition)

	if err != nil {
		if force {
			glog.V(100).Infof(msg.FailToUpdateNotification("clusterpolicy", builder.Definition.Name))

			builder, err := builder.Delete()

			if err != nil {
				glog.V(100).Infof(
					msg.FailToUpdateError("clusterpolicy", builder.Definition.Name))

				return nil, err
			}

			return builder.Create()
		}
	}

	return builder, err
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "ClusterPolicy"

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

		return false, fmt.Errorf(builder.errorMsg)
	}

	return true, nil
}
