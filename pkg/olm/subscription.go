package olm

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/glog"
	operatorsV1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SubscriptionBuilder provides a struct for Subscription object containing connection to the
// cluster and the Subscription definition.
type SubscriptionBuilder struct {
	// Subscription definition. Used to create Subscription object with minimum set of required elements.
	Definition *operatorsV1alpha1.Subscription
	// Created Subscription object on the cluster.
	Object *operatorsV1alpha1.Subscription
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before Subscription object is created.
	errorMsg string
}

// NewSubscriptionBuilder returns a SubscriptionBuilder.
func NewSubscriptionBuilder(apiClient *clients.Settings, subName, subNamespace, catalogSource, catalogSourceNamespace,
	packageName string) *SubscriptionBuilder {
	glog.V(100).Infof(
		"Initializing new SubscriptionBuilder structure with the following params, subName: %s, "+
			"subNamespace: %s, catalogSource: %s, catalogSourceNamespace: %s, packageName: %s ",
		subName, subNamespace, catalogSource, catalogSourceNamespace, packageName)

	builder := &SubscriptionBuilder{
		apiClient: apiClient,
		Definition: &operatorsV1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subName,
				Namespace: subNamespace,
			},
			Spec: &operatorsV1alpha1.SubscriptionSpec{
				CatalogSource:          catalogSource,
				CatalogSourceNamespace: catalogSourceNamespace,
				Package:                packageName,
			},
		},
	}

	if subName == "" {
		glog.V(100).Infof("The Name of the Subscription is empty")

		builder.errorMsg = "Subscription 'subName' cannot be empty"
	}

	if subNamespace == "" {
		glog.V(100).Infof("The Namespace of the Subscription is empty")

		builder.errorMsg = "Subscription 'subNamespace' cannot be empty"
	}

	if catalogSource == "" {
		glog.V(100).Infof("The Catalogsource of the Subscription is empty")

		builder.errorMsg = "Subscription 'catalogSource' cannot be empty"
	}

	if catalogSourceNamespace == "" {
		glog.V(100).Infof("The Catalogsource namespace of the Subscription is empty")

		builder.errorMsg = "Subscription 'catalogSourceNamespace' cannot be empty"
	}

	if packageName == "" {
		glog.V(100).Infof("The Package name of the Subscription is empty")

		builder.errorMsg = "Subscription 'packageName' cannot be empty"
	}

	return builder
}

// WithChannel adds the specific channel to the Subscription.
func (builder *SubscriptionBuilder) WithChannel(channel string) *SubscriptionBuilder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Defining Subscription builder object with channel: %s", channel)

	if channel == "" {
		builder.errorMsg = "can not redefine subscription with empty channel"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.Channel = channel

	return builder
}

// WithStartingCSV adds the specific startingCSV to the Subscription.
func (builder *SubscriptionBuilder) WithStartingCSV(startingCSV string) *SubscriptionBuilder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Defining Subscription builder object with startingCSV: %s",
		startingCSV)

	if startingCSV == "" {
		builder.errorMsg = "can not redefine subscription with empty startingCSV"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.StartingCSV = startingCSV

	return builder
}

// WithInstallPlanApproval adds the specific installPlanApproval to the Subscription.
func (builder *SubscriptionBuilder) WithInstallPlanApproval(
	installPlanApproval operatorsV1alpha1.Approval) *SubscriptionBuilder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Defining Subscription builder object with "+
		"installPlanApproval: %s", installPlanApproval)

	if installPlanApproval != "Automatic" && installPlanApproval != "Manual" {
		glog.V(100).Infof("The InstallPlanApproval of the Subscription must be either \"Automatic\" " +
			"or \"Manual\"")

		builder.errorMsg = "Subscription 'installPlanApproval' must be either \"Automatic\" or \"Manual\""
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.InstallPlanApproval = installPlanApproval

	return builder
}

// Create makes an Subscription in cluster and stores the created object in struct.
func (builder *SubscriptionBuilder) Create() (*SubscriptionBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the Subscription %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.Subscriptions(builder.Definition.Namespace).Create(context.TODO(),
			builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Exists checks whether the given Subscription exists.
func (builder *SubscriptionBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if Subscription %s exists in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	var err error

	builder.Object, err = builder.apiClient.Subscriptions(builder.Definition.Namespace).Get(
		context.TODO(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// Delete removes a Subscription.
func (builder *SubscriptionBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting Subscription %s in namespace %s", builder.Definition.Name,
		builder.Definition.Namespace)

	if !builder.Exists() {
		return nil
	}

	err := builder.apiClient.Subscriptions(builder.Definition.Namespace).Delete(context.TODO(), builder.Object.Name,
		metav1.DeleteOptions{})

	if err != nil {
		return err
	}

	builder.Object = nil

	return err
}

// Update modifies the existing Subscription with the Subscription definition in SubscriptionBuilder.
func (builder *SubscriptionBuilder) Update() (*SubscriptionBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Updating Subscription %s in namespace %s",
		builder.Definition.Name, builder.Definition.Namespace)

	if !builder.Exists() {
		return nil, fmt.Errorf("subscription named %s in namespace %s doesn't exist",
			builder.Definition.Name, builder.Definition.Namespace)
	}

	var err error

	builder.Object, err = builder.apiClient.Subscriptions(builder.Definition.Namespace).Update(
		context.TODO(), builder.Definition, metav1.UpdateOptions{})

	return builder, err
}

// PullSubscription loads existing Subscription from cluster into the SubscriptionBuilder struct.
func PullSubscription(apiClient *clients.Settings, subName, subNamespace string) (*SubscriptionBuilder, error) {
	glog.V(100).Infof("Pulling existing Subscription %s from cluster in namespace %s",
		subName, subNamespace)

	builder := &SubscriptionBuilder{
		apiClient: apiClient,
		Definition: &operatorsV1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subName,
				Namespace: subNamespace,
			},
		},
	}

	if subName == "" {
		glog.V(100).Infof("The name of the Subscription is empty")

		builder.errorMsg = "Subscription 'subName' cannot be empty"
	}

	if subNamespace == "" {
		glog.V(100).Infof("The namespace of the Subscription is empty")

		builder.errorMsg = "Subscription 'subNamespace' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("subscription object named %s doesn't exist", subName)
	}

	builder.Definition = builder.Object

	return builder, nil
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *SubscriptionBuilder) validate() (bool, error) {
	resourceCRD := "Subscription"

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
