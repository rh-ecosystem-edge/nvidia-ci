package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golang/glog"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// SetBuilder provides a struct for MachineSet object from the cluster and a MachineSet definition.
type SetBuilder struct {
	// SetBuilder definition. Used to create
	// MachineSet object with minimum set of required elements.
	Definition *machinev1beta1.MachineSet
	// Created SetBuilder object on the cluster.
	Object *machinev1beta1.MachineSet
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before SetBuilder object is created.
	errorMsg string
	// string to store the public cloud
	publicCloud string
}

const (
	// AwsCloud const definition.
	AwsCloud = "aws"
	// GcpCloud const definition.
	GcpCloud = "gcp"
	// AzureCloud const definition.
	AzureCloud = "azure"
)

// NewSetBuilderFromCopy returns an SetBuilder struct from a copied MachineSet.
func NewSetBuilderFromCopy(
	apiClient *clients.Settings,
	nsName string,
	instanceType string,
	workerLabel string,
	replicas int32) *SetBuilder {
	glog.V(100).Infof("Initializing new SetBuilder structure from copied MachineSet with the following"+
		" params: namespace: %s, instanceType: %s, workerLabel: %s, and replicas: %v", nsName, instanceType,
		workerLabel, replicas)

	builder := SetBuilder{
		apiClient: apiClient,
	}

	newSetBuilder, err := createNewWorkerMachineSetFromCopy(apiClient, nsName, instanceType, workerLabel, replicas)

	if err != nil {
		glog.V(100).Infof("Error initializing MachineSet from copy: %s", err.Error())

		builder.errorMsg = fmt.Sprintf("Error initializing MachineSet from copy: %s", err.Error())

		return &builder
	}

	builder.Definition = newSetBuilder.Definition

	err = builder.getPublicCloudKind()

	if err != nil {
		builder.errorMsg = fmt.Sprintf("error getting the public cloud kind: %v", err.Error())
	}

	glog.V(100).Infof("Updating copied MachineSet provider instanceType to: %s", instanceType)

	err = builder.ChangeCloudProviderInstanceType(instanceType)

	if err != nil {
		builder.errorMsg = fmt.Sprintf("error changing the instanceType: %v", err.Error())
	}

	if nsName == "" {
		glog.V(100).Infof("The Namespace of the MachineSet is empty")

		builder.errorMsg = "MachineSet 'nsName' cannot be empty"
	}

	if instanceType == "" {
		glog.V(100).Infof("The instanceType of the MachineSet is empty")

		builder.errorMsg = "MachineSet 'instanceType' cannot be empty"
	}

	if replicas == 0 {
		glog.V(100).Infof("The replicas of the MachineSet is zero")

		builder.errorMsg = "MachineSet 'replicas' cannot be zero"
	}

	if workerLabel == "" {
		glog.V(100).Infof("The workerLabel of the MachineSet is empty")

		builder.errorMsg = "MachineSet 'workerLabel' cannot be empty"
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The MachineSet object definition is nil")

		builder.errorMsg = "MachineSet 'Object.Definition' is nil"
	}

	return &builder
}

// PullSet loads an existing MachineSet into Builder struct.
func PullSet(apiClient *clients.Settings, name, namespace string) (*SetBuilder, error) {
	glog.V(100).Infof("Pulling existing machineSet name %s in namespace %s", name, namespace)

	builder := SetBuilder{
		apiClient: apiClient,
		Definition: &machinev1beta1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}

	if name == "" {
		builder.errorMsg = "MachineSet 'name' cannot be empty"
	}

	if namespace == "" {
		builder.errorMsg = "MachineSet 'namespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("machineSet object %s doesn't exist in namespace %s", name, namespace)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Exists checks whether the given MachineSet exists.
func (builder *SetBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof(
		"Checking if MachineSet %s exists in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.MachineSets(builder.Definition.Namespace).Get(context.TODO(),
		builder.Definition.Name, metav1.GetOptions{})

	if err != nil {
		glog.V(100).Infof("Failed to collect MachineSet object due to %s", err.Error())
	}

	return err == nil || !k8serrors.IsNotFound(err)
}

// Create makes a MachineSet in cluster and stores the created object in struct.
func (builder *SetBuilder) Create() (*SetBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the MachineSet %s", builder.Definition.Name)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.MachineSets(builder.Definition.Namespace).Create(
			context.TODO(), builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Delete removes a MachineSet object from a cluster.
func (builder *SetBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting the MachineSet object %s",
		builder.Definition.Name)

	if !builder.Exists() {
		return fmt.Errorf("machineSet cannot be deleted because it does not exist")
	}

	err := builder.apiClient.MachineSets(builder.Object.Namespace).Delete(
		context.TODO(), builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return fmt.Errorf("cannot delete MachineSet: %w", err)
	}

	return err
}

// WaitForMachineSetReady waits until MachineSet first replica is Ready.
func WaitForMachineSetReady(
	apiClient *clients.Settings,
	namespace,
	machineSetName string,
	timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), 30*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			machineSetPulled, err := PullSet(apiClient, namespace, machineSetName)

			if err != nil {
				glog.V(100).Infof("MachineSet pull from cluster error: %v\n", err)

				return false, err
			}

			if machineSetPulled.Object.Status.ReadyReplicas > 0 &&
				machineSetPulled.Object.Status.Replicas == machineSetPulled.Object.Status.ReadyReplicas {
				glog.V(100).Infof("MachineSet %s has %v replicas in Ready state",
					machineSetPulled.Object.Name, machineSetPulled.Object.Status.ReadyReplicas)

				// this exits out of the wait.PollUntilContextTimeout()
				return true, nil
			}

			glog.V(100).Infof("MachineSet %s has %v replicas in Ready state",
				machineSetPulled.Object.Name, machineSetPulled.Object.Status.ReadyReplicas)

			return false, err
		})
}

// ChangeCloudProviderInstanceType calls the cloud-specific function to change the ProviderSpec instance type param.
func (builder *SetBuilder) ChangeCloudProviderInstanceType(instanceType string) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Updating the cloud provider instance type field")

	switch builder.publicCloud {
	case AwsCloud:
		glog.V(100).Infof("Updating ProviderSpec InstanceType param for AWS public cloud")

		err := builder.AWSChangeProviderInstanceType(instanceType)

		if err != nil {
			return fmt.Errorf("error from func AWSChangeProviderInstanceType(instanceType): %w", err)
		}

	case GcpCloud:
		glog.V(100).Infof("Updating ProviderSpec MachineType and OnHostTerminate params for " +
			"GCP public cloud")

		err := builder.GCPChangeProviderMachineType(instanceType)

		if err != nil {
			return fmt.Errorf("error from func GCPChangeProviderMachineType(instanceType): %w", err)
		}

	case AzureCloud:
		glog.V(100).Infof("Updating ProviderSpec VMSize param for Azure public cloud")

		err := builder.AzureChangeProviderVMSize(instanceType)

		if err != nil {
			return fmt.Errorf("error from func AzureChangeProviderVMSize(instanceType): %w", err)
		}

	default:
		glog.V(100).Infof("Public cloud '%s' is not supported, must be 'aws', 'gcp' or azure'")

		return fmt.Errorf("could not find supported public cloud")
	}

	return nil
}

// AWSChangeProviderInstanceType changes the ProviderSpec InstanceType param for AWS public cloud.
func (builder *SetBuilder) AWSChangeProviderInstanceType(instanceType string) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	if instanceType == "" {
		return fmt.Errorf("instanceType parameter cannot be empty")
	}

	byteArray, err := json.Marshal(builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		return fmt.Errorf("error marshalling machineSet providerSpec.Value into byte array: %w", err)
	}

	glog.V(100).Infof("Updating ProviderSpec InstanceType param '%s' for AWS public cloud",
		instanceType)

	var AWSProviderSpecObject *machinev1beta1.AWSMachineProviderConfig
	err = json.Unmarshal(byteArray, &AWSProviderSpecObject)

	if err != nil {
		glog.V(100).Infof("error unmarshalling byte array into AWSMachineProviderConfig object: %v", err)

		return fmt.Errorf("could not update InstanceType param: %w", err)
	}

	glog.V(100).Infof("Setting AWSMachineProviderConfig.InstanceType param value to: %s", instanceType)

	AWSProviderSpecObject.InstanceType = instanceType

	byteArrayAWS, err := json.Marshal(AWSProviderSpecObject)

	if err != nil {
		glog.V(100).Infof("error marshalling AWSMachineProviderConfig object into byte array: %v", err)

		return fmt.Errorf("could not update InstanceType param: %w", err)
	}

	err = json.Unmarshal(byteArrayAWS, builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		glog.V(100).Infof("error unmarshalling AWSMachineProviderConfig byte array into "+
			"ProviderSpec.Value object: %v", err)

		return fmt.Errorf("could not update InstanceType param: %w", err)
	}

	return nil
}

// GCPChangeProviderMachineType changes the ProviderSpec MachineType param for GCP public cloud.
func (builder *SetBuilder) GCPChangeProviderMachineType(machineType string) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	if machineType == "" {
		return fmt.Errorf("machineType parameter cannot be empty")
	}

	byteArray, err := json.Marshal(builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		return fmt.Errorf("error marshalling machineSet providerSpec.Value into byte array: %w", err)
	}

	glog.V(100).Infof("Updating ProviderSpec MachineType param '%s' for GCP public cloud", machineType)

	var GCPProviderSpecObject *machinev1beta1.GCPMachineProviderSpec
	err = json.Unmarshal(byteArray, &GCPProviderSpecObject)

	if err != nil {
		glog.V(100).Infof("error unmarshalling byte array into GCPMachineProviderSpec object: %v", err)

		return fmt.Errorf("could not update MachineType param: %w", err)
	}

	glog.V(100).Infof("Setting GCPMachineProviderConfig.MachineType param value to: %s", machineType)

	GCPProviderSpecObject.MachineType = machineType

	byteArrayGCP, err := json.Marshal(GCPProviderSpecObject)

	if err != nil {
		glog.V(100).Infof("error marshalling GCPMachineProviderSpec object into byte array: %v", err)

		return fmt.Errorf("could not update MachineType param: %w", err)
	}

	err = json.Unmarshal(byteArrayGCP, builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		glog.V(100).Infof("error unmarshalling ProviderSpec byte array into ProviderSpec.Value: %v", err)

		return fmt.Errorf("could not update MachineType param: %w", err)
	}

	return nil
}

// AzureChangeProviderVMSize changes the ProviderSpec VMSize param for Azure public cloud.
func (builder *SetBuilder) AzureChangeProviderVMSize(vmSize string) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	if vmSize == "" {
		return fmt.Errorf("vmSize parameter cannot be empty")
	}

	byteArray, err := json.Marshal(builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		return fmt.Errorf("error marshalling machineSet providerSpec.Value into byte array: %w", err)
	}

	glog.V(100).Infof("Updating ProviderSpec Value VMSize param '%s' for Azure public cloud",
		vmSize)

	var AzureProviderSpecObject *machinev1beta1.AzureMachineProviderSpec
	err = json.Unmarshal(byteArray, &AzureProviderSpecObject)

	if err != nil {
		glog.V(100).Infof("error unmarshalling byte array into AzureMachineProviderSpec object: %v", err)

		return fmt.Errorf("could not update VMSize param: %w", err)
	}

	glog.V(100).Infof("Setting AzureMachineProviderSpec.VMSize param value to: %s", vmSize)

	AzureProviderSpecObject.VMSize = vmSize

	byteArrayAzure, err := json.Marshal(AzureProviderSpecObject)

	if err != nil {
		glog.V(100).Infof("error marshalling AzureMachineProviderSpec object into byte array: %v", err)

		return fmt.Errorf("could not update VMSize param: %w", err)
	}

	err = json.Unmarshal(byteArrayAzure, builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		glog.V(100).Infof("error unmarshalling AzureMachineProviderSpec byte array into "+
			"ProviderSpec.Value object: %v", err)

		return fmt.Errorf("could not update VMSize param: %w", err)
	}

	return nil
}

// createNewWorkerMachineSetFromCopy returns a SetBuilder object which was copied from an exiting one on cluster.
func createNewWorkerMachineSetFromCopy(
	apiClient *clients.Settings,
	namespace string,
	instanceType string,
	workerLabel string,
	replicas int32) (*SetBuilder, error) {
	workerSetBuilders, err := ListWorkerMachineSets(apiClient, namespace, workerLabel)

	if err != nil {
		return nil, fmt.Errorf("could not list worker MachineSets: %w", err)
	}

	if len(workerSetBuilders) == 0 {
		glog.V(100).Infof("The array of worker MachineSets is empty")

		return nil, fmt.Errorf("no worker MachineSets were found")
	}

	// picking the first worker SetBuilder in array
	baseSetBuilder := workerSetBuilders[0]

	glog.V(100).Infof("Creating new SetBuilder copy of first existing worker MachineSet: %s",
		baseSetBuilder.Definition.Name)

	copiedSetBuilder := &SetBuilder{
		apiClient: apiClient,
		Definition: &machinev1beta1.MachineSet{
			ObjectMeta: *baseSetBuilder.Definition.ObjectMeta.DeepCopy(),
			Spec:       *baseSetBuilder.Definition.Spec.DeepCopy(),
		},
	}

	glog.V(100).Infof("Renaming copied SetBuilder to: %s",
		copiedSetBuilder.Definition.Name)

	// replace dots in name with dashes.  Cannot have dots or underscores in machineSet name, must also be lower case
	copiedSetBuilder.Definition.Name = fmt.Sprintf("%v-%v",
		copiedSetBuilder.Definition.Name,
		strings.ToLower(regexp.MustCompile(`[\.|\_]`).ReplaceAllString(instanceType, "-")))

	glog.V(100).Infof("Updating copied MachineSet name in metadata, selector and template parameters")

	copiedSetBuilder.Definition.UID = ""
	copiedSetBuilder.Definition.ResourceVersion = ""

	// change spec labels
	copiedSetBuilder.Definition.Spec.Selector.MatchLabels["machine.openshift.io/cluster-api-machineset"] =
		copiedSetBuilder.Definition.Name
	copiedSetBuilder.Definition.Spec.Template.Labels["machine.openshift.io/cluster-api-machineset"] =
		copiedSetBuilder.Definition.Name

	glog.V(100).Infof("Updating copied MachineSet replicas value to: %v", replicas)
	copiedSetBuilder.Definition.Spec.Replicas = &replicas

	return copiedSetBuilder, nil
}

// getPublicCloudKind determines the public cloud kind and stores it in the builder struct.
func (builder *SetBuilder) getPublicCloudKind() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Determining the public cloud kind")

	providerSpecMap := make(map[string]interface{})

	// Value field is of type *runtime.RawExtension
	byteArray, err := json.Marshal(builder.Definition.Spec.Template.Spec.ProviderSpec.Value)

	if err != nil {
		builder.errorMsg = fmt.Sprintf("error determining public cloud kind: %v", err)

		return fmt.Errorf("error marshalling the providerSpec Value element into a byte array")
	}

	err = json.Unmarshal(byteArray, &providerSpecMap)

	if err != nil {
		builder.errorMsg = fmt.Sprintf("error determining public cloud kind: %v", err)

		return fmt.Errorf("error unmarshalling the byte array into a providerSpec map")
	}

	// publicCloudKind is of type: map[string]interface{}
	publicCloudKind := providerSpecMap["kind"]

	publicCloud, ok := publicCloudKind.(string)

	if !ok {
		return fmt.Errorf("failed to detect public cloud kind")
	}

	glog.V(100).Infof("ProviderSpec kind param is '%s'", publicCloud)

	switch publicCloud {
	case "AWSMachineProviderConfig":
		builder.publicCloud = AwsCloud
	case "GCPMachineProviderSpec":
		builder.publicCloud = GcpCloud
	case "AzureMachineProviderSpec":
		builder.publicCloud = AzureCloud
	default:
		builder.errorMsg = "unsupported cloud platform. Supported public cloud are AWS, GCP, and Azure"

		return fmt.Errorf("unsupported cloud platform. Supported public cloud are AWS, GCP, and Azure")
	}

	return nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *SetBuilder) validate() (bool, error) {
	resourceCRD := "MachineSet"

	if builder == nil {
		glog.V(100).Infof("The %s builder is uninitialized", resourceCRD)

		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The %s is undefined", resourceCRD)

		builder.errorMsg = msg.UndefinedCrdObjectErrString(resourceCRD)
	}

	if builder.apiClient == nil {
		glog.V(100).Infof("The %s builder apiClient is nil", resourceCRD)

		builder.errorMsg = fmt.Sprintf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		glog.V(100).Infof("The %s builder has error message: %s", resourceCRD, builder.errorMsg)

		return false, errors.New(builder.errorMsg)
	}

	return true, nil
}
