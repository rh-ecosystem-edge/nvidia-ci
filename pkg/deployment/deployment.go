package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	multus "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1Typed "k8s.io/client-go/kubernetes/typed/apps/v1"
)

// Builder provides struct for deployment object containing connection to the cluster and the deployment definitions.
type Builder struct {
	// Deployment definition. Used to create the deployment object.
	Definition *appsv1.Deployment
	// Created deployment object
	Object *appsv1.Deployment
	// Used in functions that define or mutate deployment definition. errorMsg is processed before the deployment
	// object is created.
	errorMsg  string
	apiClient appsv1Typed.AppsV1Interface
}

// AdditionalOptions additional options for deployment object.
type AdditionalOptions func(builder *Builder) (*Builder, error)

// NewBuilder creates a new instance of Builder.
func NewBuilder(
	apiClient *clients.Settings, name, nsname string, labels map[string]string, containerSpec *corev1.Container) *Builder {
	glog.V(100).Infof(
		"Initializing new deployment structure with the following params: "+
			"name: %s, namespace: %s, labels: %s, containerSpec %v",
		name, nsname, labels, containerSpec)

	builder := Builder{
		apiClient: apiClient.AppsV1Interface,
		Definition: &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
				},
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	builder.WithAdditionalContainerSpecs([]corev1.Container{*containerSpec})

	if name == "" {
		glog.V(100).Infof("The name of the deployment is empty")

		builder.errorMsg = "deployment 'name' cannot be empty"
	}

	if nsname == "" {
		glog.V(100).Infof("The namespace of the deployment is empty")

		builder.errorMsg = "deployment 'namespace' cannot be empty"
	}

	if len(labels) == 0 {
		glog.V(100).Infof("There are no labels for the deployment")

		builder.errorMsg = "deployment 'labels' cannot be empty"
	}

	return &builder
}

// Pull loads an existing deployment into Builder struct.
func Pull(apiClient *clients.Settings, name, nsname string) (*Builder, error) {
	// Safeguard against nil apiClient interfaces.
	if apiClient == nil {
		glog.V(100).Infof("The apiClient is nil")

		return nil, fmt.Errorf("apiClient cannot be nil")
	}

	glog.V(100).Infof("Pulling existing deployment name: %s under namespace: %s", name, nsname)

	builder := Builder{
		apiClient: apiClient.AppsV1Interface,
		Definition: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: nsname,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the deployment is empty")

		return nil, fmt.Errorf("deployment 'name' cannot be empty")
	}

	if nsname == "" {
		glog.V(100).Infof("The namespace of the deployment is empty")

		return nil, fmt.Errorf("deployment 'namespace' cannot be empty")
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("deployment object %s doesn't exist in namespace %s", name, nsname)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// WithNodeSelector applies a nodeSelector to the deployment definition.
func (builder *Builder) WithNodeSelector(selector map[string]string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying nodeSelector %s to deployment %s in namespace %s",
		selector, builder.Definition.Name, builder.Definition.Namespace)

	builder.Definition.Spec.Template.Spec.NodeSelector = selector

	return builder
}

// WithReplicas sets the desired number of replicas in the deployment definition.
func (builder *Builder) WithReplicas(replicas int32) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting %d replicas in deployment %s in namespace %s",
		replicas, builder.Definition.Name, builder.Definition.Namespace)

	builder.Definition.Spec.Replicas = &replicas

	return builder
}

// WithAdditionalContainerSpecs appends a list of container specs to the deployment definition.
func (builder *Builder) WithAdditionalContainerSpecs(specs []corev1.Container) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Appending a list of container specs %v to deployment %s in namespace %s",
		specs, builder.Definition.Name, builder.Definition.Namespace)

	if len(specs) == 0 {
		glog.V(100).Infof("The container specs are empty")

		builder.errorMsg = "cannot accept empty list as container specs"

		return builder
	}

	builder.Definition.Spec.Template.Spec.Containers = append(builder.Definition.Spec.Template.Spec.Containers, specs...)

	return builder
}

// WithSecondaryNetwork applies Multus secondary network configuration on deployment definition.
func (builder *Builder) WithSecondaryNetwork(networks []*multus.NetworkSelectionElement) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying secondary networks %v to deployment %s", networks, builder.Definition.Name)

	if len(networks) == 0 {
		builder.errorMsg = "can not apply empty networks list"

		return builder
	}

	netAnnotation, err := json.Marshal(networks)

	if err != nil {
		builder.errorMsg = fmt.Sprintf("error to unmarshal networks annotation due to: %s", err.Error())

		return builder
	}

	builder.Definition.Spec.Template.ObjectMeta.Annotations = map[string]string{
		"k8s.v1.cni.cncf.io/networks": string(netAnnotation)}

	return builder
}

// WithHugePages sets hugePages on all containers inside the deployment.
func (builder *Builder) WithHugePages() *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying hugePages configuration to all containers in deployment: %s",
		builder.Definition.Name)

	// If volumes are not defined, create an empty list of volumes.
	if builder.Definition.Spec.Template.Spec.Volumes == nil {
		builder.Definition.Spec.Template.Spec.Volumes = []corev1.Volume{}
	}

	// Append hugepages volume to the deployment.
	builder.Definition.Spec.Template.Spec.Volumes = append(builder.Definition.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "hugepages", VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{Medium: "HugePages"}}})

	for idx := range builder.Definition.Spec.Template.Spec.Containers {
		// If volumeMounts are not defined, create an empty list of volumeMounts.
		if builder.Definition.Spec.Template.Spec.Containers[idx].VolumeMounts == nil {
			builder.Definition.Spec.Template.Spec.Containers[idx].VolumeMounts = []corev1.VolumeMount{}
		}

		// Append hugepages volume mount to the deployment.
		builder.Definition.Spec.Template.Spec.Containers[idx].VolumeMounts = append(
			builder.Definition.Spec.Template.Spec.Containers[idx].VolumeMounts,
			corev1.VolumeMount{Name: "hugepages", MountPath: "/mnt/huge"})
	}

	return builder
}

// WithSecurityContext sets SecurityContext on deployment definition.
func (builder *Builder) WithSecurityContext(securityContext *corev1.PodSecurityContext) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Applying SecurityContext configuration on deployment %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if securityContext == nil {
		glog.V(100).Infof("The 'securityContext' of the deployment is empty")

		builder.errorMsg = "'securityContext' parameter is empty"

		return builder
	}

	builder.Definition.Spec.Template.Spec.SecurityContext = securityContext

	return builder
}

// WithLabel applies label to deployment's definition.
func (builder *Builder) WithLabel(labelKey, labelValue string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof(fmt.Sprintf("Defining deployment's label to %s:%s", labelKey, labelValue))

	if labelKey == "" {
		glog.V(100).Infof("The 'labelKey' of the deployment is empty")

		builder.errorMsg = "can not apply empty labelKey"

		return builder
	}

	if builder.Definition.Spec.Template.Labels == nil {
		builder.Definition.Spec.Template.Labels = map[string]string{}
	}

	builder.Definition.Spec.Template.Labels[labelKey] = labelValue

	return builder
}

// WithServiceAccountName sets the ServiceAccountName on deployment definition.
func (builder *Builder) WithServiceAccountName(serviceAccountName string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting ServiceAccount %s on deployment %s in namespace %s",
		serviceAccountName, builder.Definition.Name, builder.Definition.Namespace)

	if serviceAccountName == "" {
		glog.V(100).Infof("The 'serviceAccount' of the deployment is empty")

		builder.errorMsg = "can not apply empty serviceAccount"

		return builder
	}

	builder.Definition.Spec.Template.Spec.ServiceAccountName = serviceAccountName

	return builder
}

// WithVolume attaches given volume to the deployment.
func (builder *Builder) WithVolume(deployVolume corev1.Volume) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	if deployVolume.Name == "" {
		glog.V(100).Infof("The volume's name cannot be empty")

		builder.errorMsg = "The volume's name cannot be empty"

		return builder
	}

	glog.V(100).Infof("Adding volume %s to deployment %s in namespace %s",
		deployVolume.Name, builder.Definition.Name, builder.Definition.Namespace)

	builder.Definition.Spec.Template.Spec.Volumes = append(
		builder.Definition.Spec.Template.Spec.Volumes,
		deployVolume)

	return builder
}

// WithSchedulerName configures a scheduler to process pod's scheduling.
func (builder *Builder) WithSchedulerName(schedulerName string) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	if schedulerName == "" {
		glog.V(100).Infof("Scheduler's name cannot be empty")

		builder.errorMsg = "Scheduler's name cannot be empty"

		return builder
	}

	glog.V(100).Infof("Setting scheduler %s for deployment %s in namespace %s",
		schedulerName, builder.Definition.Name, builder.Definition.Namespace)

	builder.Definition.Spec.Template.Spec.SchedulerName = schedulerName

	return builder
}

// WithOptions creates deployment with generic mutation options.
func (builder *Builder) WithOptions(options ...AdditionalOptions) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting deployment additional options")

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

// Create generates a deployment in cluster and stores the created object in struct.
func (builder *Builder) Create() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating deployment %s in namespace %s", builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.Deployments(builder.Definition.Namespace).Create(
			context.TODO(), builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Update renovates the existing deployment object with the deployment definition in builder.
func (builder *Builder) Update() (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating deployment %s in namespace %s", builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.Deployments(builder.Definition.Namespace).Update(
		context.TODO(), builder.Definition, metav1.UpdateOptions{})

	return builder, err
}

// Delete removes a deployment.
func (builder *Builder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting deployment %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		builder.Object = nil

		return nil
	}

	err := builder.apiClient.Deployments(builder.Definition.Namespace).Delete(
		context.TODO(), builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// CreateAndWaitUntilReady creates a deployment in the cluster and waits until the deployment is available.
func (builder *Builder) CreateAndWaitUntilReady(timeout time.Duration) (*Builder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating deployment %s in namespace %s and waiting for the defined period until it's ready",
		builder.Definition.Name, builder.Definition.Namespace)

	if _, err := builder.Create(); err != nil {
		return nil, err
	}

	if builder.IsReady(timeout) {
		return builder, nil
	}

	return nil, fmt.Errorf("deployment %s in namespace %s is not ready",
		builder.Definition.Name, builder.Definition.Namespace,
	)
}

// IsReady periodically checks if deployment is in ready status.
func (builder *Builder) IsReady(timeout time.Duration) bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Running periodic check until deployment %s in namespace %s is ready",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return false
	}

	err := wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			var err error
			builder.Object, err = builder.apiClient.Deployments(builder.Definition.Namespace).Get(
				context.TODO(), builder.Definition.Name, metav1.GetOptions{})

			if err != nil {
				return false, err
			}

			if builder.Object.Status.ReadyReplicas > 0 && builder.Object.Status.Replicas == builder.Object.Status.ReadyReplicas {
				return true, nil
			}

			return false, nil
		})

	return err == nil
}

// DeleteAndWait deletes a deployment and waits until it is removed from the cluster.
func (builder *Builder) DeleteAndWait(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting deployment %s in namespace %s and waiting for the defined period until it's removed",
		builder.Definition.Name, builder.Definition.Namespace)

	if err := builder.Delete(); err != nil {
		return err
	}

	// Polls the deployment every second until it's removed.
	return wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			_, err := builder.apiClient.Deployments(builder.Definition.Namespace).Get(
				context.TODO(), builder.Definition.Name, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return true, nil
			}

			return false, nil
		})
}

// Exists checks whether the given deployment exists.
func (builder *Builder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if deployment %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	builder.Object, err = builder.apiClient.Deployments(builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// WaitUntilCondition waits for the duration of the defined timeout or until the
// deployment gets to a specific condition.
func (builder *Builder) WaitUntilCondition(condition appsv1.DeploymentConditionType, timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Waiting for the defined period until deployment %s in namespace %s has condition %v",
		builder.Definition.Name, builder.Definition.Namespace, condition)

	if !builder.Exists() {
		return fmt.Errorf("cannot wait for deployment condition because it does not exist")
	}

	return wait.PollUntilContextTimeout(
		context.TODO(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			updateDeployment, err := builder.apiClient.Deployments(builder.Definition.Namespace).Get(
				context.TODO(), builder.Definition.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}

			for _, cond := range updateDeployment.Status.Conditions {
				if cond.Type == condition && cond.Status == corev1.ConditionTrue {
					return true, nil
				}
			}

			return false, nil
		})
}

// GetGVR returns deployment's GroupVersionResource which could be used for Clean function.
func GetGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *Builder) validate() (bool, error) {
	resourceCRD := "ClusterDeployment"

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

// WithToleration applies a toleration to the deployment's definition.
func (builder *Builder) WithToleration(toleration corev1.Toleration) *Builder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	if toleration == (corev1.Toleration{}) {
		glog.V(100).Infof("The toleration cannot be empty")

		builder.errorMsg = "The toleration cannot be empty"

		return builder
	}

	glog.V(100).Infof("Adding TaintToleration %v to deployment %s in namespace %s",
		toleration, builder.Definition.Name, builder.Definition.Namespace)

	builder.Definition.Spec.Template.Spec.Tolerations = append(
		builder.Definition.Spec.Template.Spec.Tolerations,
		toleration)

	return builder
}
