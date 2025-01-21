package nvidiagpu

import (
	"context"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/deploy"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createAndLabelNamespace(gpuBurnNsBuilder *namespace.Builder, gpuBurnNamespace string) {
	glog.V(gpuparams.GpuLogLevel).Infof("Creating the gpu burn namespace '%s'",
		gpuBurnNamespace)
	createdGPUBurnNsBuilder, err := gpuBurnNsBuilder.Create()
	Expect(err).ToNot(HaveOccurred(), "error creating gpu burn "+
		"namespace '%s' :  %v ", gpuBurnNamespace, err)

	glog.V(gpuparams.GpuLogLevel).Infof("Successfully created namespace '%s'",
		createdGPUBurnNsBuilder.Object.Name)

	glog.V(gpuparams.GpuLogLevel).Infof("Labeling the newly created namespace '%s'",
		createdGPUBurnNsBuilder.Object.Name)

	labeledGPUBurnNsBuilder := createdGPUBurnNsBuilder.WithMultipleLabels(map[string]string{
		"openshift.io/cluster-monitoring":    "true",
		"pod-security.kubernetes.io/enforce": "privileged",
	})

	newGPUBurnLabeledNsBuilder, err := labeledGPUBurnNsBuilder.Update()
	Expect(err).ToNot(HaveOccurred(), "error labeling namespace %v :  %v ",
		newGPUBurnLabeledNsBuilder.Definition.Name, err)

	glog.V(gpuparams.GpuLogLevel).Infof("The nvidia-gpu-operator labeled namespace has "+
		"labels:  %v", newGPUBurnLabeledNsBuilder.Object.Labels)
}

func createNFDDeployment() bool {

	By("Deploy NFD Subscription in NFD namespace")
	err := deploy.CreateNFDSubscription(inittools.APIClient, nfdCatalogSource)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD Subscription:  %v", err)

	By("Sleep for 2 minutes to allow the NFD Operator deployment to be created")
	glog.V(gpuparams.GpuLogLevel).Infof("Sleep for 2 minutes to allow the NFD Operator deployment" +
		" to be created")
	time.Sleep(2 * time.Minute)

	By("Wait up to 5 mins for NFD Operator deployment to be created")
	nfdDeploymentCreated := wait.DeploymentCreated(inittools.APIClient, nfdOperatorDeploymentName, nfdOperatorNamespace,
		30*time.Second, 5*time.Minute)
	Expect(nfdDeploymentCreated).ToNot(BeFalse(), "timed out waiting to deploy "+
		"NFD operator")

	By("Check if NFD Operator has been deployed")
	nfdDeployed, err := deploy.CheckNFDOperatorDeployed(inittools.APIClient, 240*time.Second)
	Expect(err).ToNot(HaveOccurred(), "error deploying NFD Operator in"+
		" NFD namespace:  %v", err)
	return nfdDeployed
}

func deleteOLMPods(apiClient *clients.Settings) error {

	olmNamespace := "openshift-operator-lifecycle-manager"
	glog.V(gpuparams.GpuLogLevel).Info("Deleting catalog operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=catalog-operator"}); err != nil {
		return err
	}

	glog.V(gpuparams.GpuLogLevel).Info("Deleting OLM operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=olm-operator"}); err != nil {
		return err
	}

	return nil
}
