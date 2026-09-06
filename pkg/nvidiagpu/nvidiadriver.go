package nvidiagpu

import (
	"context"
	"errors"
	"fmt"

	nvidiagpuv1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sjson "k8s.io/apimachinery/pkg/util/json"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// NVIDIADriverBuilder provides a struct for NVIDIADriver object from the cluster and a
// NVIDIADriver definition. NVIDIADriver is one of the two CRs (alongside GPUCluster)
// introduced by GPU Operator 26.7.0 as part of its native/DRA-based software-enablement
// stack, replacing ClusterPolicy's driver management for that mode.
type NVIDIADriverBuilder struct {
	// Definition used to create the object with the minimum set of required elements.
	Definition *nvidiagpuv1alpha1.NVIDIADriver
	// Object is the created object on the cluster.
	Object *nvidiagpuv1alpha1.NVIDIADriver
	// apiClient is used to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before the object is created.
	errorMsg string
}

// NewNVIDIADriverBuilderFromObjectString creates a minimal NVIDIADriverBuilder object from
// the NVIDIADriver sample found in the GPU Operator CSV's alm-examples.
func NewNVIDIADriverBuilderFromObjectString(apiClient *clients.Settings, almExample string) *NVIDIADriverBuilder {
	glog.V(100).Infof("Initializing new NVIDIADriverBuilder structure from almExample string")

	var nvidiaDriver nvidiagpuv1alpha1.NVIDIADriver

	nvidiaDriverExample, err := olm.GetALMExampleByKind(almExample, nvidiagpuv1alpha1.NVIDIADriverCRDName)
	if err != nil {
		return newNVIDIADriverBuilder(apiClient, &nvidiaDriver, err)
	}

	err = k8sjson.Unmarshal(nvidiaDriverExample, &nvidiaDriver)

	return newNVIDIADriverBuilder(apiClient, &nvidiaDriver, err)
}

func newNVIDIADriverBuilder(
	apiClient *clients.Settings, nvidiaDriver *nvidiagpuv1alpha1.NVIDIADriver, err error) *NVIDIADriverBuilder {
	glog.V(100).Infof(
		"Initializing new NVIDIADriverBuilder structure with NVIDIADriver name: %s", nvidiaDriver.Name)

	builder := NVIDIADriverBuilder{
		apiClient:  apiClient,
		Definition: nvidiaDriver,
	}

	if err != nil {
		glog.V(100).Infof(
			"Error initializing NVIDIADriver from alm-examples: %s", err.Error())

		builder.errorMsg = fmt.Sprintf("Error initializing NVIDIADriver from alm-examples: %s", err.Error())
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The NVIDIADriver object definition is nil")

		builder.errorMsg = "NVIDIADriver 'Object.Definition' is nil"
	}

	return &builder
}

// Get returns the NVIDIADriver object if found.
func (builder *NVIDIADriverBuilder) Get() (*nvidiagpuv1alpha1.NVIDIADriver, error) {
	if valid, err := builder.validate(); !valid {
		return nil, err
	}

	glog.V(100).Infof("Collecting NVIDIADriver object %s", builder.Definition.Name)

	nvidiaDriver := &nvidiagpuv1alpha1.NVIDIADriver{}
	err := builder.apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name: builder.Definition.Name,
	}, nvidiaDriver)

	if err != nil {
		glog.V(100).Infof("NVIDIADriver object %s doesn't exist", builder.Definition.Name)

		return nil, err
	}

	return nvidiaDriver, err
}

// PullNVIDIADriver loads an existing NVIDIADriver into a NVIDIADriverBuilder struct.
func PullNVIDIADriver(apiClient *clients.Settings, name string) (*NVIDIADriverBuilder, error) {
	glog.V(100).Infof("Pulling existing NVIDIADriver name: %s", name)

	if name == "" {
		return nil, fmt.Errorf("NVIDIADriver 'name' cannot be empty")
	}

	builder := &NVIDIADriverBuilder{
		apiClient: apiClient,
		Definition: &nvidiagpuv1alpha1.NVIDIADriver{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		},
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("NVIDIADriver object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// Exists checks whether the given NVIDIADriver exists.
func (builder *NVIDIADriverBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if NVIDIADriver %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.Get()

	if err != nil {
		glog.V(100).Infof("Failed to collect NVIDIADriver object due to %s", err.Error())
	}

	return err == nil
}

// Delete removes a NVIDIADriver.
func (builder *NVIDIADriverBuilder) Delete() (*NVIDIADriverBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting NVIDIADriver %s", builder.Definition.Name)

	if !builder.Exists() {
		return builder, nil
	}

	err := builder.apiClient.Delete(context.TODO(), builder.Object)
	if err != nil && !k8serrors.IsNotFound(err) {
		return builder, fmt.Errorf("cannot delete NVIDIADriver: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// Create makes a NVIDIADriver in the cluster and stores the created object in the struct.
func (builder *NVIDIADriverBuilder) Create() (*NVIDIADriverBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the NVIDIADriver %s", builder.Definition.Name)

	if builder.Exists() {
		return builder, nil
	}

	err := builder.apiClient.Create(context.TODO(), builder.Definition)
	if err == nil {
		builder.Object = builder.Definition
	}

	return builder, err
}

// validate checks that the builder and its definition are properly initialized before
// accessing any member fields.
func (builder *NVIDIADriverBuilder) validate() (bool, error) {
	resourceCRD := "NVIDIADriver"

	if builder == nil {
		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		builder.errorMsg = msg.UndefinedCrdObjectErrString(resourceCRD)
	}

	if builder.apiClient == nil {
		builder.errorMsg = fmt.Sprintf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		return false, errors.New(builder.errorMsg)
	}

	return true, nil
}
