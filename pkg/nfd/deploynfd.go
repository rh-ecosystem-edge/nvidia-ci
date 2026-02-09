package nfd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/logging"

	"github.com/golang/glog"
	. "github.com/onsi/gomega" //nolint:staticcheck
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/get"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	nvidiagpuwait "github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// github.com/rh-ecosystem-edge/nvidia-ci/tests/nvidiagpu/internal
const (
	nfdOperatorNamespace                        = "openshift-nfd"
	nfdOperatorGroupName                        = "nfd-og"
	nfdSubscriptionName                         = "nfd-subscription"
	nfdCatalogSourceNamespace                   = "openshift-marketplace"
	nfdOperatorDeploymentName                   = "nfd-controller-manager"
	nfdPackage                                  = "nfd"
	nfdChannel                                  = "stable"
	nfdInstallPlanApproval    v1alpha1.Approval = "Automatic"
	nfdCRDeploymentName                         = "nfd-master"
)

// CreateNFDNamespace creates and labels NFD namespace.
func CreateNFDNamespace(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Check if NFD Operator namespace exists, otherwise created it")

	nfdNsBuilder := namespace.NewBuilder(apiClient, nfdOperatorNamespace)

	glog.V(gpuparams.GpuLogLevel).Infof("Creating the namespace:  %v", nfdOperatorNamespace)

	createdNfdNsBuilder, err := nfdNsBuilder.Create()

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error creating NFD namespace '%s' :  %v ", createdNfdNsBuilder.Definition.Name, err)

		return err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Successfully created NFD namespace '%s'",
		createdNfdNsBuilder.Object.Name)

	glog.V(gpuparams.GpuLogLevel).Infof("Labeling the newly created NFD namespace '%s'",
		nfdNsBuilder.Object.Name)

	labeledNfdNsBuilder := createdNfdNsBuilder.WithMultipleLabels(map[string]string{
		"openshift.io/cluster-monitoring":    "true",
		"pod-security.kubernetes.io/enforce": "privileged",
	})

	newLabeledNfdNsBuilder, err := labeledNfdNsBuilder.Update()

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error labeling NFD namespace %s: %v", newLabeledNfdNsBuilder.Definition.Name, err)

		return err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("The NFD labeled namespace has "+
		"labels:  %v", newLabeledNfdNsBuilder.Object.Labels)

	return nil
}

// CreateNFDOperatorGroup creates NFD OperatorGroup in NFD namespace.
func CreateNFDOperatorGroup(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Create the NFD operatorgroup")

	nfdOgBuilder := olm.NewOperatorGroupBuilder(apiClient, nfdOperatorGroupName, nfdOperatorNamespace)

	if nfdOgBuilder.Exists() {
		glog.V(gpuparams.GpuLogLevel).Infof("The nfdOgBuilder that exists has name:  %v",
			nfdOgBuilder.Object.Name)
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("Create a new NFD OperatorGroup with name:  %s",
			nfdOperatorGroupName)

		nfdOgBuilderCreated, err := nfdOgBuilder.Create()

		if err != nil {
			glog.V(gpuparams.GpuLogLevel).Infof("error creating NFD operatorgroup %v :  %v ",
				nfdOgBuilderCreated.Definition.Name, err)

			return err
		}
	}

	return nil
}

// CreateNFDSubscription creates NFD Subscription in NFD namespace.
func CreateNFDSubscription(apiClient *clients.Settings, nfdCatalogSource string) error {
	glog.V(gpuparams.GpuLogLevel).Info("Create Subscription in NFD Operator Namespace")

	nfdSubBuilder := olm.NewSubscriptionBuilder(apiClient, nfdSubscriptionName, nfdOperatorNamespace,
		nfdCatalogSource, nfdCatalogSourceNamespace, nfdPackage)

	nfdSubBuilder.WithChannel(nfdChannel)
	nfdSubBuilder.WithInstallPlanApproval(nfdInstallPlanApproval)

	glog.V(gpuparams.GpuLogLevel).Infof("Creating the NFD subscription, i.e Deploy the NFD operator")

	createdNfdSub, err := nfdSubBuilder.Create()

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error creating NFD subscription %v :  %v ",
			createdNfdSub.Definition.Name, err)

		return err
	}

	if createdNfdSub.Exists() {
		glog.V(gpuparams.GpuLogLevel).Infof("Newly created NFD subscription: %s was successfully created",
			createdNfdSub.Object.Name)
		glog.V(gpuparams.GpuLogLevel).Infof("The newly created subscription: %s in namespace: %v "+
			"has current CSV:  %v", createdNfdSub.Object.Name, createdNfdSub.Object.Namespace,
			createdNfdSub.Object.Status.CurrentCSV)
	} else {
		return fmt.Errorf("could not determine the current CSV from newly created subscription: %s in"+
			" namespace %s", createdNfdSub.Object.Name, createdNfdSub.Object.Namespace)
	}

	return nil
}

// CheckNFDOperatorDeployed checks that NFD Operator is successfully deployed in NFD namespace.
func CheckNFDOperatorDeployed(apiClient *clients.Settings, waitTime time.Duration) (bool, error) {
	glog.V(gpuparams.GpuLogLevel).Infof("Check if the NFD operator deployment is ready")

	nfdOperatorDeployment, err := deployment.Pull(apiClient, nfdOperatorDeploymentName, nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error trying to pull NFD operator "+
			"deployment is: %v", err)

		return false, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pulled NFD operator deployment is:  %v ",
		nfdOperatorDeployment.Definition.Name)

	if nfdOperatorDeployment.IsReady(waitTime) {
		glog.V(gpuparams.GpuLogLevel).Infof("Pulled NFD operator deployment is:  %v is Ready ",
			nfdOperatorDeployment.Definition.Name)
	} else {
		return false, fmt.Errorf("NFD operator deployment:  %v is still not Ready "+
			"after waiting %v time duration", nfdOperatorDeployment.Definition.Name, waitTime)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Get currentCSV from NFD subscription")

	nfdCurrentCSVFromSub, err := get.CurrentCSVFromSubscription(apiClient, nfdSubscriptionName,
		nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error pulling NFD currentCSV from cluster:  %v", err)

		return false, err
	}

	if nfdCurrentCSVFromSub == "" {
		glog.V(gpuparams.GpuLogLevel).Infof("NFD currentCSV from subscription is null:  %s",
			nfdCurrentCSVFromSub)

		return false, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("currentCSV %s extracted from NFD Subscription %s",
		nfdCurrentCSVFromSub, nfdSubscriptionName)

	glog.V(gpuparams.GpuLogLevel).Infof("Wait for NFD ClusterServiceVersion to be in " +
		"Succeeded phase")
	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for NFD ClusterServiceVersion to be Succeeded phase")

	err = nvidiagpuwait.CSVSucceeded(
		apiClient, nfdCurrentCSVFromSub, nfdOperatorNamespace, 60*time.Second, 5*time.Minute)

	glog.V(gpuparams.GpuLogLevel).Infof("error waiting for NFD ClusterServiceVersion to be "+
		"in Succeeded phase:  %v ", err)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error waiting for NFD ClusterServiceVersion"+
			" to be in Succeeded phase:  %v ", err)

		return false, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pull existing CSV in NFD Operator Namespace")

	clusterNfdCSV, err := olm.PullClusterServiceVersion(apiClient, nfdCurrentCSVFromSub, nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error pulling CSV %v from cluster:  %v",
			nfdCurrentCSVFromSub, err)

		return false, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("NFD clusterCSV from cluster lastUpdatedTime is : %v ",
		clusterNfdCSV.Definition.Status.LastUpdateTime)

	glog.V(gpuparams.GpuLogLevel).Infof("clusterCSV from cluster Phase is : \"%v\"",
		clusterNfdCSV.Definition.Status.Phase)

	succeeded := v1alpha1.ClusterServiceVersionPhase("Succeeded")

	if clusterNfdCSV.Definition.Status.Phase != succeeded {
		glog.V(gpuparams.GpuLogLevel).Infof("CSV Phase is not succeeded")

		return false, fmt.Errorf("CSV Phase is not 'succeeded'")
	}

	return true, nil
}

// DeployCRInstance deploys NodeFeatureDiscovery instance from current CSV almExamples.
func DeployCRInstance(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Get ALM examples block form NFD CSV")
	glog.V(gpuparams.GpuLogLevel).Infof("Get currentCSV from NFD subscription")

	nfdCurrentCSVFromSub, err := get.CurrentCSVFromSubscription(apiClient, nfdSubscriptionName,
		nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error from getting CurrentCSVFromSubscription:  %v ", err)

		return err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pull existing CSV in NFD Operator Namespace")

	clusterNfdCSV, err := olm.PullClusterServiceVersion(apiClient, nfdCurrentCSVFromSub, nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error from PullClusterServiceVersion:  %v ", err)

		return err
	}

	almExamples, err := clusterNfdCSV.GetAlmExamples()

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error from pulling almExamples from NFD CSV:  %v ", err)

		return err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("almExamples block from cluster NFD CSV is : %v ", almExamples)

	glog.V(gpuparams.GpuLogLevel).Infof("Creating NodeFeatureDiscovery instance from CSV almExamples")

	nodeFeatureDiscoveryBuilder := NewBuilderFromObjectString(apiClient, almExamples)

	_, err = nodeFeatureDiscoveryBuilder.Create()

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error Creating NodeFeatureDiscovery instance from CSV "+
			"almExamples  %v ", err)

		return err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Waiting for NFD CR deployment '%s' to be created", nfdCRDeploymentName)

	nfdCRDeploymentCreated := nvidiagpuwait.DeploymentCreated(apiClient, nfdCRDeploymentName, nfdOperatorNamespace,
		30*time.Second, 4*time.Minute)

	if !nfdCRDeploymentCreated {
		glog.V(gpuparams.GpuLogLevel).Infof("timed out waiting to deploy NFD CR deployment")

		return fmt.Errorf("timed out waiting to deploy NFD CR deployment")
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Check if the NFD CR deployment is ready")

	nfdCRDeployment, err := deployment.Pull(apiClient, nfdCRDeploymentName, nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("Error pulling NFD CR deployment  %v ", err)

		return err
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Pulled NFD CR deployment is:  %v ",
		nfdCRDeployment.Definition.Name)

	if nfdCRDeployment.IsReady(180 * time.Second) {
		glog.V(gpuparams.GpuLogLevel).Infof("Pulled NFD operator deployment is:  %v is Ready ",
			nfdCRDeployment.Definition.Name)
	} else {
		return fmt.Errorf("NFD CR deployment is not ready after wait period")
	}

	return nil
}

// GetNFDCRJson outputs the NFD CR instance json file.
func GetNFDCRJson(apiClient *clients.Settings, nfdCRName string, nfdNamespace string) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Pull the NodeFeatureDiscovery just created from cluster, " +
		"with updated fields")

	pulledNodeFeatureDiscovery, err := Pull(apiClient, nfdCRName, nfdNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error pulling NodeFeatureDiscovery %s from "+
			"cluster: %v ", nfdCRName, err)

		return err
	}

	nfdCRJson, err := json.MarshalIndent(pulledNodeFeatureDiscovery, "", " ")

	if err == nil {
		glog.V(gpuparams.GpuLogLevel).Infof("The NodeFeatureDiscovery just created has name:  %v",
			pulledNodeFeatureDiscovery.Definition.Name)
		glog.V(gpuparams.GpuLogLevel).Infof("The NodeFeatureDiscovery just created marshalled "+
			"in json: %v", string(nfdCRJson))
	} else {
		glog.V(gpuparams.GpuLogLevel).Infof("Error Marshalling NodeFeatureDiscovery into json:  %v",
			err)
	}

	return nil
}

// NFDCRDeleteAndWait deletes NodeFeatureDiscovery instance and waits until it is deleted.
func NFDCRDeleteAndWait(apiClient *clients.Settings) error {
	// return wait.PollImmediate(DeletionPollInterval, timeout, func() (bool, error) {
	return wait.PollUntilContextTimeout(
		context.TODO(), DeletionPollInterval, DeletionTimeoutDuration, false, func(ctx context.Context) (bool, error) {
			nfdCR, err := Pull(apiClient, CRName, OperatorNamespace)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("NodeFeatureDiscovery pull from cluster error: %s\n", err)

				return false, err
			}

			_, err = nfdCR.Delete()
			if err != nil {
				return false, err
			}

			if !nfdCR.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("NodeFeatureDiscovery instance '%s' in namespace '%s' does "+
					"not exist", CRName, OperatorNamespace)

				// this exists out of the wait.PollImmediate()
				return true, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("NodeFeatureDiscovery instance %s in namespace %s still exists",
				nfdCR.Object.Name, nfdCR.Object.Namespace)

			return false, err
		})
}

// DeleteNFDNamespace creates and labels NFD namespace.
func DeleteNFDNamespace(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Deleting NFD namespace '%s'", nfdOperatorNamespace)

	pulledNFDNsBuilder, err := namespace.Pull(apiClient, nfdOperatorNamespace)

	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof("error pulling NFD namespace '%s' :  %v ",
			nfdOperatorNamespace, err)

		return err
	}

	err = pulledNFDNsBuilder.Delete()

	return err
}

// DeleteNFDOperatorGroup creates NFD OperatorGroup in NFD namespace.
func DeleteNFDOperatorGroup(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Deleting NFD OperatorGroup '%s' in namespace '%s'",
		nfdOperatorGroupName, nfdOperatorNamespace)

	pulledNFDOg, err := olm.PullOperatorGroup(apiClient, nfdOperatorGroupName, nfdOperatorNamespace)

	if !pulledNFDOg.Exists() {
		glog.V(gpuparams.GpuLogLevel).Infof("The NFD OperatorGroup %s does not exist", nfdOperatorGroupName)

		return err
	}

	err = pulledNFDOg.Delete()

	return err
}

// DeleteNFDSubscription Deletes NFD Subscription in NFD namespace.
func DeleteNFDSubscription(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Info("Deleting NFD Subscription '%s' in namespace '%s'",
		nfdSubscriptionName, nfdOperatorNamespace)

	pulledNFDSub, err := olm.PullSubscription(apiClient, nfdSubscriptionName, nfdOperatorNamespace)

	if !pulledNFDSub.Exists() {
		glog.V(gpuparams.GpuLogLevel).Infof("The NFD Subscription %s does not exist", nfdOperatorGroupName)

		return err
	}

	err = pulledNFDSub.Delete()

	return err
}

// DeleteNFDCSV Deletes NFD CSV in NFD namespace.
func DeleteNFDCSV(apiClient *clients.Settings) error {
	glog.V(gpuparams.GpuLogLevel).Infof("Deleting currently installed NFD CSV")

	nfdCurrentCSVFromSub, err := get.CurrentCSVFromSubscription(apiClient, nfdSubscriptionName,
		nfdOperatorNamespace)

	if err != nil {
		return fmt.Errorf("error trying to get current NFD CSV from subscription '%w'", err)
	}

	if nfdCurrentCSVFromSub == "" {
		return fmt.Errorf("current NFD CSV name is empty string '%s'", nfdCurrentCSVFromSub)
	}

	clusterNfdCSV, err := olm.PullClusterServiceVersion(apiClient, nfdCurrentCSVFromSub, nfdOperatorNamespace)

	if err != nil {
		return fmt.Errorf("error pulling CSV %v from cluster:  %w", nfdCurrentCSVFromSub, err)
	}

	err = clusterNfdCSV.Delete()

	return err
}

// DeleteAnyNFDCSV Deletes all CSVs that belong to a the NFD subscription (by name) in the NFD subscription namespace.
// Equivalent to `oc delete csv -n <namespace> -l operators.coreos.com/<name>.<namespace>`
func DeleteAnyNFDCSV(apiClient *clients.Settings) error {
	csvList, err := apiClient.ClusterServiceVersions(nfdOperatorNamespace).List(context.TODO(),
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("operators.coreos.com/%s.%s", nfdPackage, nfdOperatorNamespace),
		})

	if err != nil {
		return err
	}

	for _, csv := range csvList.Items {
		glog.V(gpuparams.GpuLogLevel).Infof("Attempt deleting NFD CSV %s in namespace %s", csv.Name,
			nfdOperatorNamespace)
		if err := apiClient.ClusterServiceVersions(nfdOperatorNamespace).Delete(context.TODO(), csv.Name,
			metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func CreateNFDDeployment(apiClient *clients.Settings, catalogSource string, logLevel logging.Level) bool {
	glog.V(glog.Level(logLevel)).Info("Deploying NFD Subscription")
	err := CreateNFDSubscription(apiClient, catalogSource)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD Subscription: %v", err)

	glog.V(glog.Level(logLevel)).Info("Sleeping for 2 minutes to allow the NFD Operator deployment to stabilize")
	time.Sleep(2 * time.Minute)

	glog.V(glog.Level(logLevel)).Infof("Waiting up to %v for NFD Operator deployment to be fully created", NFDOperatorTimeout)
	nfdDeploymentCreated := nvidiagpuwait.DeploymentCreated(apiClient, OperatorDeploymentName, OperatorNamespace, NFDOperatorCheckInterval, NFDOperatorTimeout)
	Expect(nfdDeploymentCreated).ToNot(BeFalse(), "timed out waiting for NFD operator deployment")

	glog.V(glog.Level(logLevel)).Info("Checking if NFD Operator deployment is active")
	nfdDeployed, err := CheckNFDOperatorDeployed(apiClient, 4*time.Minute)
	Expect(err).ToNot(HaveOccurred(), "error deploying NFD Operator in NFD namespace: %v", err)

	return nfdDeployed
}
