package pod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/golang/glog"
	multus "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
)

// Builder provides a struct for pod object from the cluster and a pod definition.
type Builder struct {
	// Pod definition, used to create the pod object.
	Definition *corev1.Pod
	// Created pod object.
	Object *corev1.Pod
	// Used to store latest error message upon defining or mutating pod definition.
	errorMsg string
	// api client to interact with the cluster.
	apiClient *clients.Settings
}

// AdditionalOptions additional options for pod object.
type AdditionalOptions func(builder *Builder) (*Builder, error)

// NewBuilder creates a new instance of Builder.
func NewBuilder(apiClient *clients.Settings, name, nsname, image string) *Builder {
	glog.V(100).Infof(
		"Initializing new pod structure with the following params: "+
			"name: %s, namespace: %s, image: %s",
		name, nsname, image)

	builder := &Builder{
		apiClient:  apiClient,
		Definition: getDefinition(name, nsname),
	}

	if name == "" {
		glog.V(100).Infof("The name of the pod is empty")

		builder.errorMsg = "pod's name is empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The namespace of the pod is empty")

		builder.errorMsg = "namespace's name is empty"
	}

	if image == "" {
		glog.V(100).Infof("The image of the pod is empty")

		builder.errorMsg = "pod's image is empty"
	}

	defaultContainer, err := NewContainerBuilder("test", image, []string{"/bin/bash", "-c", "sleep INF"}).GetContainerCfg()
	if err != nil {
		glog.V(100).Infof("Failed to define the default container settings")

		builder.errorMsg = err.Error()
	}

	builder.Definition.Spec.Containers = append(builder.Definition.Spec.Containers, *defaultContainer)

	return builder
}

// Pull loads an existing pod into the Builder struct.
func Pull(apiClient *clients.Settings, name, nsname string) (*Builder, error) {
	glog.V(100).Infof("Pulling existing pod name: %s namespace:%s", name, nsname)

	builder := Builder{
		apiClient: apiClient,
		Definition: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the pod is empty")

		builder.errorMsg = "pod 'name' cannot be empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The namespace of the pod is empty")

		builder.errorMsg = "pod 'namespace' cannot be empty"
	}

	if builder.errorMsg != "" {
		return nil, fmt.Errorf("faield to pull pod object due to the following error: %s", builder.errorMsg)
	}

	if !builder.Exists() {
		glog.V(100).Infof("Failed to pull pod object %s from namespace %s. Object doesn't exist",
			name, nsname)

		return nil, fmt.Errorf("pod object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// DefineOnNode adds nodeName to the pod's definition.
func (builder *Builder) DefineOnNode(nodeName string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Adding nodeName %s to the definition of pod %s in namespace %s",
		nodeName, builder.Definition.Name, builder.Definition.Namespace)

	if builder.Object != nil {
		glog.V(100).Infof("The pod is already running on node %s", builder.Object.Spec.NodeName)

		builder.errorMsg = fmt.Sprintf(
			"can not redefine running pod. pod already running on node %s", builder.Object.Spec.NodeName)
	}

	if nodeName == "" {
		glog.V(100).Infof("The node name is empty")

		builder.errorMsg = "can not define pod on empty node"
	}

	if builder.errorMsg == "" {
		builder.Definition.Spec.NodeName = nodeName
	}

	return builder
}

// Create makes a pod according to the pod definition and stores the created object in the pod builder.
func (builder *Builder) Create() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating pod %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.Pods(builder.Definition.Namespace).Create(
			context.TODO(), builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Delete removes the pod object and resets the builder object.
func (builder *Builder) Delete() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting pod %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return builder, fmt.Errorf("pod cannot be deleted because it does not exist")
	}

	err := builder.apiClient.Pods(builder.Definition.Namespace).Delete(
		context.TODO(), builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return builder, fmt.Errorf("can not delete pod: %w", err)
	}

	builder.Object = nil

	return builder, nil
}

// DeleteAndWait deletes the pod object and waits until the pod is deleted.
func (builder *Builder) DeleteAndWait(timeout time.Duration) (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Deleting pod %s in namespace %s and waiting for the defined period until it's removed",
		builder.Definition.Name, builder.Definition.Namespace)

	builder, err := builder.Delete()
	if err != nil {
		return builder, err
	}

	err = builder.WaitUntilDeleted(timeout)

	if err != nil {
		return builder, err
	}

	return builder, nil
}

// CreateAndWaitUntilRunning creates the pod object and waits until the pod is running.
func (builder *Builder) CreateAndWaitUntilRunning(timeout time.Duration) (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating pod %s in namespace %s and waiting for the defined period until it's ready",
		builder.Definition.Name, builder.Definition.Namespace)

	builder, err := builder.Create()
	if err != nil {
		return builder, err
	}

	err = builder.WaitUntilRunning(timeout)

	if err != nil {
		return builder, err
	}

	return builder, nil
}

// WaitUntilRunning waits for the duration of the defined timeout or until the pod is running.
func (builder *Builder) WaitUntilRunning(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until pod %s in namespace %s is running",
		builder.Definition.Name, builder.Definition.Namespace)

	return builder.WaitUntilInStatus(corev1.PodRunning, timeout)
}

// WaitUntilInStatus waits for the duration of the defined timeout or until the pod gets to a specific status.
func (builder *Builder) WaitUntilInStatus(status corev1.PodPhase, timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until pod %s in namespace %s has status %v",
		builder.Definition.Name, builder.Definition.Namespace, status)

	return wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			updatePod, err := builder.apiClient.Pods(builder.Object.Namespace).Get(
				context.TODO(), builder.Object.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}

			return updatePod.Status.Phase == status, nil
		})
}

// WaitUntilDeleted waits for the duration of the defined timeout or until the pod is deleted.
func (builder *Builder) WaitUntilDeleted(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until pod %s in namespace %s is deleted",
		builder.Definition.Name, builder.Definition.Namespace)

	err := wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, false, func(ctx context.Context) (bool, error) {
			_, err := builder.apiClient.Pods(builder.Definition.Namespace).Get(
				ctx, builder.Definition.Name, metav1.GetOptions{})
			if err == nil {
				glog.V(100).Infof("pod %s/%s still present", builder.Definition.Namespace, builder.Definition.Name)

				return false, nil
			}

			if k8serrors.IsNotFound(err) {
				glog.V(100).Infof("pod %s/%s is gone", builder.Definition.Namespace, builder.Definition.Name)

				return true, nil
			}

			glog.V(100).Infof("failed to get pod %s/%s: %v", builder.Definition.Namespace, builder.Definition.Name, err)

			return false, err
		})

	return err
}

// WaitUntilReady waits for the duration of the defined timeout or until the pod reaches the Ready condition.
func (builder *Builder) WaitUntilReady(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until pod %s in namespace %s is Ready",
		builder.Definition.Name, builder.Definition.Namespace)

	return builder.WaitUntilCondition(corev1.PodReady, timeout)
}

// WaitUntilCondition waits for the duration of the defined timeout or until the pod gets to a specific condition.
func (builder *Builder) WaitUntilCondition(condition corev1.PodConditionType, timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until pod %s in namespace %s has condition %v",
		builder.Definition.Name, builder.Definition.Namespace, condition)

	return wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			updatePod, err := builder.apiClient.Pods(builder.Object.Namespace).Get(
				context.TODO(), builder.Object.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}

			for _, cond := range updatePod.Status.Conditions {
				if cond.Type == condition && cond.Status == corev1.ConditionTrue {
					return true, nil
				}
			}

			return false, nil
		})
}

// ExecCommand runs command in the pod and returns the buffer output.
func (builder *Builder) ExecCommand(command []string, containerName ...string) (bytes.Buffer, error) {
	if valid, err := builder.validate(); !valid {
		return bytes.Buffer{}, err
	}

	var (
		buffer bytes.Buffer
		cName  string
	)

	if len(containerName) > 0 {
		cName = containerName[0]
	} else {
		cName = builder.Definition.Spec.Containers[0].Name
	}

	glog.V(100).Infof("Execute command %v in the pod %s container %s in namespace %s",
		command, builder.Object.Name, cName, builder.Object.Namespace)

	req := builder.apiClient.CoreV1Interface.RESTClient().
		Post().
		Namespace(builder.Object.Namespace).
		Resource("pods").
		Name(builder.Object.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: cName,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(builder.apiClient.Config, "POST", req.URL())

	if err != nil {
		return buffer, err
	}

	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &buffer,
		Stderr: os.Stderr,
		Tty:    true,
	})

	if err != nil {
		return buffer, err
	}

	return buffer, nil
}

// Copy returns the contents of a file or path from a specified container into a buffer.
// Setting the tar option returns a tar archive of the specified path.
func (builder *Builder) Copy(path, containerName string, tar bool) (bytes.Buffer, error) {
	if valid, err := builder.validate(); !valid {
		return bytes.Buffer{}, err
	}

	glog.V(100).Infof("Copying %s from %s in the pod",
		path, containerName)

	var command []string
	if tar {
		command = []string{
			"tar",
			"cf",
			"-",
			path,
		}
	} else {
		command = []string{
			"cat",
			path,
		}
	}

	var buffer bytes.Buffer

	req := builder.apiClient.CoreV1Interface.RESTClient().
		Post().
		Namespace(builder.Object.Namespace).
		Resource("pods").
		Name(builder.Object.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	tlsConfig, err := rest.TLSConfigFor(builder.apiClient.Config)
	if err != nil {
		return bytes.Buffer{}, err
	}

	proxy := http.ProxyFromEnvironment
	if builder.apiClient.Config.Proxy != nil {
		proxy = builder.apiClient.Config.Proxy
	}

	// More verbose setup of remotecommand executor required in order to tweak PingPeriod.
	// By default many large files are not copied in their entirety without disabling PingPeriod during the copy.
	// https://github.com/kubernetes/kubernetes/issues/60140#issuecomment-1411477275
	upgradeRoundTripper, err := spdy.NewRoundTripperWithConfig(spdy.RoundTripperConfig{
		TLS:        tlsConfig,
		Proxier:    proxy,
		PingPeriod: 0,
	})
	if err != nil {
		return buffer, err
	}

	wrapper, err := rest.HTTPWrappersForConfig(builder.apiClient.Config, upgradeRoundTripper)
	if err != nil {
		return bytes.Buffer{}, err
	}

	exec, err := remotecommand.NewSPDYExecutorForTransports(wrapper, upgradeRoundTripper, "POST", req.URL())
	if err != nil {
		return buffer, err
	}

	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &buffer,
		Stderr: os.Stderr,
		Tty:    false,
	})

	if err != nil {
		return buffer, err
	}

	return buffer, nil
}

// Exists checks whether the given pod exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if pod %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.Pods(builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// RedefineDefaultCMD redefines default command in pod's definition.
func (builder *Builder) RedefineDefaultCMD(command []string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Redefining default pod's container cmd with the new %v", command)

	builder.isMutationAllowed("cmd")

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.Containers[0].Command = command

	return builder
}

// WithRestartPolicy applies restart policy to pod's definition.
func (builder *Builder) WithRestartPolicy(restartPolicy corev1.RestartPolicy) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Redefining pod's RestartPolicy to %v", restartPolicy)

	builder.isMutationAllowed("RestartPolicy")

	if restartPolicy == "" {
		glog.V(100).Infof(
			"Failed to set RestartPolicy on pod %s in namespace %s. RestartPolicy can not be empty",
			builder.Definition.Name, builder.Definition.Namespace)

		builder.errorMsg = "can not define pod with empty restart policy"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.RestartPolicy = restartPolicy

	return builder
}

// WithTolerationToMaster sets toleration policy which allows pod to be running on master node.
func (builder *Builder) WithTolerationToMaster() *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Appending pod's %s with toleration to master node", builder.Definition.Name)

	builder.isMutationAllowed("toleration to master node")

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.Tolerations = []corev1.Toleration{
		{
			Key:    "node-role.kubernetes.io/master",
			Effect: "NoSchedule",
		},
	}

	return builder
}

// WithTolerationToControlPlane sets toleration policy which allows pod to be running on control plane node.
func (builder *Builder) WithTolerationToControlPlane() *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Appending pod's %s with toleration to control plane node", builder.Definition.Name)

	builder.isMutationAllowed("toleration to control plane node")

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.Tolerations = []corev1.Toleration{
		{
			Key:    "node-role.kubernetes.io/control-plane",
			Effect: "NoSchedule",
		},
	}

	return builder
}

// WithToleration adds a toleration configuration inside the pod.
func (builder *Builder) WithToleration(toleration corev1.Toleration) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Updating pod %s with toleration %v", builder.Definition.Name, toleration)

	builder.isMutationAllowed("custom toleration")

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.Tolerations = append(builder.Definition.Spec.Tolerations, toleration)

	return builder
}

// WithNodeSelector adds a nodeSelector configuration inside the pod.
func (builder *Builder) WithNodeSelector(nodeSelector map[string]string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Redefining pod %s in namespace %s with nodeSelector %v",
		builder.Definition.Name, builder.Definition.Namespace, nodeSelector)

	builder.isMutationAllowed("nodeSelector")

	if len(nodeSelector) == 0 {
		glog.V(100).Infof(
			"Failed to set nodeSelector on pod %s in namespace %s. nodeSelector can not be empty",
			builder.Definition.Name, builder.Definition.Namespace)

		builder.errorMsg = "can not define pod with empty nodeSelector"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.NodeSelector = nodeSelector

	return builder
}

// WithPrivilegedFlag sets privileged flag on all containers.
func (builder *Builder) WithPrivilegedFlag() *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying privileged flag to all pod's: %s containers", builder.Definition.Name)

	builder.isMutationAllowed("privileged container flag")

	if builder.errorMsg != "" {
		return builder
	}

	for idx := range builder.Definition.Spec.Containers {
		builder.Definition.Spec.Containers[idx].SecurityContext = &corev1.SecurityContext{}
		trueFlag := true
		builder.Definition.Spec.Containers[idx].SecurityContext.Privileged = &trueFlag
	}

	return builder
}

// WithVolume attaches given volume to a pod.
func (builder *Builder) WithVolume(volume corev1.Volume) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	if volume.Name == "" {
		glog.V(100).Infof("The volume's Name cannot be empty")

		builder.errorMsg = "The volume's Name cannot be empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	glog.V(100).Infof("Adding volume %s to pod %s in namespace %s",
		volume.Name, builder.Definition.Name, builder.Definition.Namespace)

	builder.Definition.Spec.Volumes = append(builder.Definition.Spec.Volumes, volume)

	return builder
}

// WithLocalVolume attaches given volume to all pod's containers.
func (builder *Builder) WithLocalVolume(volumeName, mountPath string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Configuring volume %s for all pod's: %s containers. MountPath %s",
		volumeName, builder.Definition.Name, mountPath)

	builder.isMutationAllowed("LocalVolume")

	if volumeName == "" {
		glog.V(100).Infof("The 'volumeName' of the pod is empty")

		builder.errorMsg = "'volumeName' parameter is empty"
	}

	if mountPath == "" {
		glog.V(100).Infof("The 'mountPath' of the pod is empty")

		builder.errorMsg = "'mountPath' parameter is empty"
	}

	mountConfig := corev1.VolumeMount{Name: volumeName, MountPath: mountPath, ReadOnly: false}

	builder.isMountAlreadyInUseInPod(mountConfig)

	if builder.errorMsg != "" {
		return builder
	}

	for index := range builder.Definition.Spec.Containers {
		builder.Definition.Spec.Containers[index].VolumeMounts = append(
			builder.Definition.Spec.Containers[index].VolumeMounts, mountConfig)
	}

	if len(builder.Definition.Spec.InitContainers) > 0 {
		for index := range builder.Definition.Spec.InitContainers {
			builder.Definition.Spec.InitContainers[index].VolumeMounts = append(
				builder.Definition.Spec.InitContainers[index].VolumeMounts, mountConfig)
		}
	}

	builder.Definition.Spec.Volumes = append(builder.Definition.Spec.Volumes,
		corev1.Volume{Name: mountConfig.Name, VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: mountConfig.Name,
				},
			},
		}})

	return builder
}

// WithAdditionalContainer appends additional container to pod.
func (builder *Builder) WithAdditionalContainer(container *corev1.Container) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Adding new container %v to pod %s", container, builder.Definition.Name)
	builder.isMutationAllowed("additional container")

	if container == nil {
		builder.errorMsg = "'container' parameter cannot be empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.Containers = append(builder.Definition.Spec.Containers, *container)

	return builder
}

// WithAdditionalInitContainer appends additional init container to pod.
func (builder *Builder) WithAdditionalInitContainer(container *corev1.Container) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Adding new container %v to pod %s in namespace %s",
		container, builder.Definition.Name, builder.Definition.Namespace)
	builder.isMutationAllowed("additional container")

	if container == nil {
		glog.V(100).Infof("The 'container' parameter of the pod is empty")

		builder.errorMsg = "'container' parameter cannot be empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.InitContainers = append(builder.Definition.Spec.InitContainers, *container)

	return builder
}

// WithSecondaryNetwork applies Multus secondary network on pod definition.
func (builder *Builder) WithSecondaryNetwork(network []*multus.NetworkSelectionElement) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying secondary network %v to pod %s", network, builder.Definition.Name)

	builder.isMutationAllowed("secondary network")
	if builder.errorMsg != "" {
		return builder
	}

	netAnnotation, err := json.Marshal(network)
	if err != nil {
		builder.errorMsg = fmt.Sprintf("error to unmarshal network annotation due to: %s", err.Error())
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Annotations = map[string]string{"k8s.v1.cni.cncf.io/networks": string(netAnnotation)}

	return builder
}

// WithHostNetwork applies HostNetwork to pod's definition.
func (builder *Builder) WithHostNetwork() *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying HostNetwork flag to pod's %s configuration", builder.Definition.Name)

	builder.isMutationAllowed("HostNetwork")

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.HostNetwork = true

	return builder
}

// WithHostPid configures a pod's access to the host process ID namespace based on a boolean parameter.
func (builder *Builder) WithHostPid(hostPid bool) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying HostPID flag to the configuration of pod: %s in namespace: %s",
		builder.Definition.Name, builder.Definition.Namespace)

	builder.isMutationAllowed("HostPID")

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.HostPID = hostPid

	return builder
}

// RedefineDefaultContainer redefines default container with the new one.
func (builder *Builder) RedefineDefaultContainer(container corev1.Container) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Redefining default pod %s container in namespace %s using new container %v",
		builder.Definition.Name, builder.Definition.Namespace, container)

	builder.isMutationAllowed("default container")

	builder.Definition.Spec.Containers[0] = container

	return builder
}

// WithHugePages sets hugePages on all containers inside the pod.
func (builder *Builder) WithHugePages() *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying hugePages configuration to all containers in pod: %s", builder.Definition.Name)

	builder.isMutationAllowed("hugepages")

	if builder.Definition.Spec.Volumes != nil {
		builder.Definition.Spec.Volumes = append(builder.Definition.Spec.Volumes, corev1.Volume{
			Name: "hugepages", VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "HugePages"}}})
	} else {
		builder.Definition.Spec.Volumes = []corev1.Volume{
			{Name: "hugepages", VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "HugePages"}},
			},
		}
	}

	for idx := range builder.Definition.Spec.Containers {
		if builder.Definition.Spec.Containers[idx].VolumeMounts != nil {
			builder.Definition.Spec.Containers[idx].VolumeMounts = append(
				builder.Definition.Spec.Containers[idx].VolumeMounts,
				corev1.VolumeMount{Name: "hugepages", MountPath: "/mnt/huge"})
		} else {
			builder.Definition.Spec.Containers[idx].VolumeMounts = []corev1.VolumeMount{{
				Name:      "hugepages",
				MountPath: "/mnt/huge",
			},
			}
		}
	}

	return builder
}

// WithSecurityContext sets SecurityContext on pod definition.
func (builder *Builder) WithSecurityContext(securityContext *corev1.PodSecurityContext) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying SecurityContext configuration on pod %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if securityContext == nil {
		glog.V(100).Infof("The 'securityContext' of the pod is empty")

		builder.errorMsg = "'securityContext' parameter is empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.isMutationAllowed("SecurityContext")

	builder.Definition.Spec.SecurityContext = securityContext

	return builder
}

// PullImage pulls image for given pod's container and removes it.
func (builder *Builder) PullImage(timeout time.Duration, testCmd []string) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof(
		"Pulling container image %s to node: %s", builder.Definition.Spec.Containers[0].Image,
		builder.Definition.Spec.NodeName)

	builder.WithRestartPolicy(corev1.RestartPolicyNever)
	builder.RedefineDefaultCMD(testCmd)
	_, err := builder.Create()

	if err != nil {
		glog.V(100).Infof(
			"Failed to create pod %s in namespace %s and pull image %s to node: %s",
			builder.Definition.Name, builder.Definition.Namespace, builder.Definition.Spec.Containers[0].Image,
			builder.Definition.Spec.NodeName)

		return err
	}

	statusErr := builder.WaitUntilInStatus(corev1.PodSucceeded, timeout)

	if statusErr != nil {
		glog.V(100).Infof(
			"Pod status timeout %s. Pod is not in status Succeeded in namespace %s. "+
				"Fail to confirm that image %s was pulled to node: %s",
			builder.Definition.Name, builder.Definition.Namespace, builder.Definition.Spec.Containers[0].Image,
			builder.Definition.Spec.NodeName)

		_, err = builder.Delete()

		if err != nil {
			glog.V(100).Infof(
				"Failed to remove pod %s in namespace %s from node: %s",
				builder.Definition.Name, builder.Definition.Namespace, builder.Definition.Spec.NodeName)

			return err
		}

		return statusErr
	}

	_, err = builder.Delete()

	return err
}

// WithLabel applies label to pod's definition.
func (builder *Builder) WithLabel(labelKey, labelValue string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof(fmt.Sprintf("Defining pod's label to %s:%s", labelKey, labelValue))

	builder.isMutationAllowed("Labels")

	if labelKey == "" {
		builder.errorMsg = "can not apply empty labelKey"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Labels = map[string]string{labelKey: labelValue}

	return builder
}

// WithLabels applies a set of labels to a Pod's definition.
func (builder *Builder) WithLabels(labels map[string]string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	if len(labels) == 0 {
		builder.errorMsg = "can not apply empty set of labels to pod's definition"

		return builder
	}

	builder.isMutationAllowed("Labels")

	if builder.errorMsg != "" {
		return builder
	}

	glog.V(100).Infof(fmt.Sprintf("Defining pod labels: %q", labels))

	builder.Definition.Labels = labels

	return builder
}

// WithOptions creates pod with generic mutation options.
func (builder *Builder) WithOptions(options ...AdditionalOptions) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting pod additional options")

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

// GetLog connects to a pod and fetches log.
func (builder *Builder) GetLog(logStartTime time.Duration, containerName string) (string, error) {
	if valid, err := builder.validate(); !valid {
		return "", err
	}

	logStart := int64(logStartTime.Seconds())
	req := builder.apiClient.Pods(builder.Definition.Namespace).GetLogs(builder.Definition.Name, &corev1.PodLogOptions{
		SinceSeconds: &logStart, Container: containerName})
	log, err := req.Stream(context.TODO())

	if err != nil {
		return "", err
	}

	defer func() {
		_ = log.Close()
	}()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, log)

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// GetFullLog connects to a pod and fetches the full log since pod creation.
func (builder *Builder) GetFullLog(containerName string) (string, error) {
	if valid, err := builder.validate(); !valid {
		return "", err
	}

	logStream, err := builder.apiClient.Pods(builder.Definition.Namespace).GetLogs(builder.Definition.Name,
		&corev1.PodLogOptions{Container: containerName}).Stream(context.TODO())

	if err != nil {
		return "", err
	}

	defer func() {
		_ = logStream.Close()
	}()

	logBuffer := new(bytes.Buffer)
	_, err = io.Copy(logBuffer, logStream)

	if err != nil {
		return "", err
	}

	return logBuffer.String(), nil
}

// GetGVR returns pod's GroupVersionResource which could be used for Clean function.
func GetGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
}

func getDefinition(name, nsName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: nsName},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
		},
	}
}

func (builder *Builder) isMutationAllowed(configToMutate string) {
	_, _ = builder.validate()

	if builder.Object != nil {
		glog.V(100).Infof(
			"Failed to redefine %s for running pod %s in namespace %s",
			builder.Definition.Name, configToMutate, builder.Definition.Namespace)

		builder.errorMsg = fmt.Sprintf(
			"can not redefine running pod. pod already running on node %s", builder.Object.Spec.NodeName)
	}
}

func (builder *Builder) isMountAlreadyInUseInPod(newMount corev1.VolumeMount) {
	if valid, _ := builder.validate(); valid {
		for index := range builder.Definition.Spec.Containers {
			if builder.Definition.Spec.Containers[index].VolumeMounts != nil {
				if isMountInUse(builder.Definition.Spec.Containers[index].VolumeMounts, newMount) {
					builder.errorMsg = fmt.Sprintf("given mount %v already mounted to pod's container %s",
						newMount.Name, builder.Definition.Spec.Containers[index].Name)
				}
			}
		}
	}
}

func isMountInUse(containerMounts []corev1.VolumeMount, newMount corev1.VolumeMount) bool {
	for _, containerMount := range containerMounts {
		if containerMount.Name == newMount.Name && containerMount.MountPath == newMount.MountPath {
			return true
		}
	}

	return false
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "Pod"

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
