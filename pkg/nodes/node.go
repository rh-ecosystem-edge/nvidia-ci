package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/drain"
)

const (
	isTrue = "True"
)

// Builder provides struct for Node object containing connection to the cluster and the list of Node definitions.
type Builder struct {
	Definition  *corev1.Node
	Object      *corev1.Node
	apiClient   kubernetes.Interface
	errorMsg    string
	drainHelper *drain.Helper
}

// SetDrainHelper builds drain Helper that contains parameters to control the behaviour of drain.
func (builder *Builder) SetDrainHelper(
	force bool,
	ignoreDaemonsets bool,
	deleteLocalData bool,
	gracePeriod int,
	skipWaitForDeleteTimeoutSeconds int,
	timeout time.Duration,
) {
	glog.V(100).Infof("Creating new DrainOptions config")

	msg := fmt.Sprintf("Node draining configuration: 'force': %v,", force)
	msg += fmt.Sprintf(" 'gracePeriod': %d seconds,", gracePeriod)
	msg += fmt.Sprintf(" 'skipWaitForDeletionTimeout': %d seconds,", skipWaitForDeleteTimeoutSeconds)
	msg += fmt.Sprintf(" 'ignoreAllDaemonSets': %v,", ignoreDaemonsets)
	msg += fmt.Sprintf(" 'timeout': %v,", timeout)
	msg += fmt.Sprintf(" 'deleteEmptyDir': %v", deleteLocalData)

	glog.V(100).Infof(msg)

	builder.drainHelper = &drain.Helper{
		Ctx:    context.TODO(),
		Client: builder.apiClient,
		// Delete pods that do not declare a controller.
		Force: force,
		// GracePeriodSeconds is how long to wait for a pod to terminate.
		GracePeriodSeconds: gracePeriod,
		// Ignore DaemonSet-managed pods
		IgnoreAllDaemonSets: ignoreDaemonsets,
		// The length of time to wait before giving up
		Timeout: timeout,
		// Local data from emptyDir volumes will be deleted
		// when the node is drained
		DeleteEmptyDirData: deleteLocalData,
		Out:                os.Stdout,
		ErrOut:             os.Stderr,
		// If pod DeletionTimestamp older than N seconds, skip waiting for the pod.
		SkipWaitForDeleteTimeoutSeconds: skipWaitForDeleteTimeoutSeconds,
	}
}

// Drain evicts or deletes all pods.
func (builder *Builder) Drain() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	builder.ensureDrainHelperIsSet()
	glog.V(100).Infof("Draining node %s", builder.Definition.Name)

	return drain.RunNodeDrain(builder.drainHelper, builder.Definition.Name)
}

// Cordon marks node as unschedulable.
func (builder *Builder) Cordon() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	builder.ensureDrainHelperIsSet()
	glog.V(100).Infof("Cordoning node %s", builder.Definition.Name)

	return drain.RunCordonOrUncordon(builder.drainHelper, builder.Definition, true)
}

// Uncordon marks node as schedulable.
func (builder *Builder) Uncordon() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	builder.ensureDrainHelperIsSet()
	glog.V(100).Infof("Uncordoning node %s", builder.Definition.Name)

	return drain.RunCordonOrUncordon(builder.drainHelper, builder.Definition, false)
}

// AdditionalOptions additional options for node object.
type AdditionalOptions func(builder *Builder) (*Builder, error)

// Pull gathers existing node from cluster.
func Pull(apiClient *clients.Settings, nodeName string) (*Builder, error) {
	glog.V(100).Infof("Pulling existing node object: %s", nodeName)

	builder := Builder{
		apiClient: apiClient.K8sClient,
		Definition: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		},
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("node object %s doesn't exist", nodeName)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Update renovates the existing node object with the node definition in builder.
func (builder *Builder) Update() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating configuration of node %s", builder.Definition.Name)

	if !builder.Exists() {
		return nil, fmt.Errorf("node %s object doesn't exist", builder.Definition.Name)
	}

	builder.Definition.CreationTimestamp = metav1.Time{}
	builder.Definition.ResourceVersion = ""

	var err error
	builder.Object, err = builder.apiClient.CoreV1().Nodes().Update(
		context.TODO(), builder.Definition, metav1.UpdateOptions{})

	return builder, err
}

// Exists checks whether the given node exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if node %s exists", builder.Definition.Name)

	var err error
	builder.Object, err = builder.apiClient.CoreV1().Nodes().Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes node from the cluster.
func (builder *Builder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting the node %s", builder.Definition.Name)

	if !builder.Exists() {
		return fmt.Errorf("node cannot be deleted because it does not exist")
	}

	err := builder.apiClient.CoreV1().Nodes().Delete(
		context.TODO(),
		builder.Definition.Name,
		metav1.DeleteOptions{})

	if err != nil {
		return fmt.Errorf("can not delete node %s due to %w", builder.Definition.Name, err)
	}

	builder.Object = nil

	return nil
}

// WithNewLabel defines the new label placed in the Node metadata.
func (builder *Builder) WithNewLabel(key, value string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Adding label %s=%s to node %s ", key, value, builder.Definition.Name)

	if key == "" {
		glog.V(100).Infof("Failed to apply label with an empty key to node %s", builder.Definition.Name)
		builder.errorMsg = "error to set empty key to node"
	}

	if builder.errorMsg != "" {
		return builder
	}

	if builder.Definition.Labels == nil {
		builder.Definition.Labels = map[string]string{key: value}
	} else {
		_, labelExist := builder.Definition.Labels[key]
		if !labelExist {
			builder.Definition.Labels[key] = value
		} else {
			builder.errorMsg = fmt.Sprintf("cannot overwrite existing node label: %s", key)
		}
	}

	return builder
}

// WithOptions creates node with generic mutation options.
func (builder *Builder) WithOptions(options ...AdditionalOptions) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting node additional options")

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

// RemoveLabel removes given label from Node metadata.
func (builder *Builder) RemoveLabel(key, value string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Removing label %s=%s from node %s", key, value, builder.Definition.Name)

	if key == "" {
		glog.V(100).Infof("Failed to remove empty label's key from node %s", builder.Definition.Name)
		builder.errorMsg = "error to remove empty key from node"
	}

	if builder.errorMsg != "" {
		return builder
	}

	delete(builder.Definition.Labels, key)

	return builder
}

// ExternalIPv4Network returns nodes external ip address.
func (builder *Builder) ExternalIPv4Network() (string, error) {
	if valid, err := builder.validate(); !valid {
		return "", err
	}

	glog.V(100).Infof("Collecting node's external ipv4 addresses")

	if builder.Object == nil {
		builder.errorMsg = "error to collect external networks from node"
	}

	if builder.errorMsg != "" {
		return "", errors.New(builder.errorMsg)
	}

	var extNetwork ExternalNetworks
	err := json.Unmarshal([]byte(builder.Object.Annotations[ovnExternalAddresses]), &extNetwork)

	if err != nil {
		return "",
			fmt.Errorf("error to unmarshal node %s, annotation %s due to %w", builder.Object.Name, ovnExternalAddresses, err)
	}

	return extNetwork.IPv4, nil
}

// IsReady check if the Node is Ready.
func (builder *Builder) IsReady() (bool, error) {
	if valid, err := builder.validate(); !valid {
		return false, err
	}

	glog.V(100).Infof("Verify %s node availability", builder.Definition.Name)

	if !builder.Exists() {
		return false, fmt.Errorf("%s node object doesn't exist", builder.Definition.Name)
	}

	for _, condition := range builder.Object.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == isTrue, nil
		}
	}

	return false, fmt.Errorf("the Ready condition could not be found for node %s", builder.Definition.Name)
}

// WaitUntilConditionTrue waits for timeout duration or until node gets to a specific status.
func (builder *Builder) WaitUntilConditionTrue(
	conditionType corev1.NodeConditionType, timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	err := wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			if !builder.Exists() {
				return false, fmt.Errorf("node %s object doesn't exist", builder.Definition.Name)
			}

			for _, condition := range builder.Object.Status.Conditions {
				if condition.Type == conditionType {
					return condition.Status == isTrue, nil
				}
			}

			return false, fmt.Errorf("the %s condition could not be found for node %s",
				builder.Definition.Name, conditionType)
		})

	if err == nil {
		return nil
	}

	return fmt.Errorf("%s node condition %s never became True due to %w",
		builder.Definition.Name, conditionType, err)
}

// WaitUntilConditionUnknown waits for timeout duration or until node change specific status.
func (builder *Builder) WaitUntilConditionUnknown(
	conditionType corev1.NodeConditionType, timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	err := wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			if !builder.Exists() {
				return false, fmt.Errorf("node %s object doesn't exist", builder.Definition.Name)
			}

			for _, condition := range builder.Object.Status.Conditions {
				if condition.Type == conditionType {
					return condition.Status != "Unknown", nil
				}
			}

			return false, fmt.Errorf("the %s condition could not be found for node %s",
				builder.Definition.Name, conditionType)
		})

	if err == nil {
		return nil
	}

	return fmt.Errorf("%s node condition %s never became Unknown due to %w",
		builder.Definition.Name, conditionType, err)
}

// WaitUntilReady waits for timeout duration or until node is Ready.
func (builder *Builder) WaitUntilReady(timeout time.Duration) error {
	return builder.WaitUntilConditionTrue(corev1.NodeReady, timeout)
}

// WaitUntilNotReady waits for timeout duration or until node is NotReady.
func (builder *Builder) WaitUntilNotReady(timeout time.Duration) error {
	return builder.WaitUntilConditionUnknown(corev1.NodeReady, timeout)
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "Node"

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

// ensureDrainHelperIsSet ensures that drainHelper is always set.
func (builder *Builder) ensureDrainHelperIsSet() {
	if builder.drainHelper == nil {
		glog.V(100).Infof(
			"DrainHelper is not initialized for node %s. Init DrainHelper with defaul parameters",
			builder.Definition.Name)
		builder.SetDrainHelper(true, true, true, 300, 180, 10*time.Minute)
	}
}
