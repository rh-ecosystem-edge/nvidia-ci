package nfd

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

// EnsureNFDIsInstalled ensures that the Node Feature Discovery (NFD) operator
// is installed on the cluster. If not, it attempts to deploy the operator and,
// if necessary, creates a custom CatalogSource to make NFD available.
func EnsureNFDIsInstalled(apiClient *clients.Settings, Nfd *CustomConfig, ocpVersion string, level glog.Level) {
	By("Check if NFD is installed")
	nfdInstalled, err := check.NFDDeploymentsReady(apiClient)

	if nfdInstalled && err == nil {
		glog.V(gpuparams.GpuLogLevel).Infof("The check for ready NFD deployments is: %v", nfdInstalled)
		glog.V(gpuparams.GpuLogLevel).Infof("NFD operators and operands are already installed on " +
			"this cluster")
	} else {
		glog.V(level).Infof("NFD is not currently installed on this cluster")
		glog.V(level).Infof("Deploying NFD Operator and CR instance on this cluster")

		Nfd.CleanupAfterInstall = true

		By("Check if 'nfd' packagemanifest exists in 'redhat-operators' default catalog")
		nfdPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(apiClient,
			Package, CatalogSourceNamespace, CatalogSourceDefault)

		if nfdPkgManifestBuilderByCatalog == nil {
			glog.V(level).Infof("NFD packagemanifest was not found in the default '%s'"+
				" catalog.", CatalogSourceDefault)

			if Nfd.CreateCustomCatalogsource {
				glog.V(level).Infof("Creating custom catalogsource '%s' for NFD "+
					"catalog.", Nfd.CustomCatalogSource)
				glog.V(level).Infof("Creating custom catalogsource '%s' for NFD "+
					"Operator with index image '%s'", Nfd.CustomCatalogSource, Nfd.CustomCatalogSourceIndexImage)

				nfdCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
					Nfd.CustomCatalogSource, CatalogSourceNamespace, Nfd.CustomCatalogSourceIndexImage,
					CustomCatalogSourceDisplayName, CustomNFDCatalogSourcePublisherName)

				Expect(nfdCustomCatalogSourceBuilder).ToNot(BeNil(), "error creating custom "+
					"NFD catalogsource %s:  %v", Package, Nfd.CustomCatalogSource, err)

				createdNFDCustomCatalogSourceBuilder, err := nfdCustomCatalogSourceBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "error creating custom NFD "+
					"catalogsource '%s':  %v", Package, Nfd.CustomCatalogSource, err)

				Expect(createdNFDCustomCatalogSourceBuilder).ToNot(BeNil(), "Failed to "+
					" create custom NFD catalogsource '%s'", Nfd.CustomCatalogSource)

				By(fmt.Sprintf("Sleep for %s to allow the NFD custom catalogsource to be created", nvidiagpu.SleepDuration.String()))
				time.Sleep(nvidiagpu.SleepDuration)

				glog.V(level).Infof("Wait up to %s for custom NFD catalogsource '%s' to be ready", nvidiagpu.WaitDuration.String(), createdNFDCustomCatalogSourceBuilder.Definition.Name)

				Expect(createdNFDCustomCatalogSourceBuilder.IsReady(nvidiagpu.WaitDuration)).NotTo(BeFalse())

				nfdPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalogWithTimeout(inittools.APIClient,
					Package, CatalogSourceNamespace, Nfd.CustomCatalogSource, 30*time.Second, 5*time.Minute)

				Expect(err).ToNot(HaveOccurred(), "error getting NFD packagemanifest '%s' "+
					"from custom catalog '%s':  %v", Package, Nfd.CustomCatalogSource, err)

				Nfd.CatalogSource = Nfd.CustomCatalogSource
				nfdChannel := nfdPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
				glog.V(level).Infof("NFD channel '%s' retrieved from packagemanifest "+
					"of custom catalogsource '%s'", nfdChannel, Nfd.CustomCatalogSource)

			} else {
				Skip("NFD packagemanifest not found in default 'redhat-operators' catalogsource, " +
					"and flag to deploy custom catalogsource is false")
			}

		} else {
			glog.V(level).Infof("The nfd packagemanifest '%s' was found in the default"+
				" catalog '%s'", nfdPkgManifestBuilderByCatalog.Object.Name, CatalogSourceDefault)

			Nfd.CatalogSource = CatalogSourceDefault
			nfdChannel := nfdPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
			glog.V(level).Infof("The NFD channel retrieved from packagemanifest is:  %v",
				nfdChannel)

		}
		// i dont need to pass fd.OperatorDeploymentName,
		//					nfd.OperatorNamespace, nfd.NFDOperatorCheckInterval,
		//					nfd.NFDOperatorTimeout
		// if the DeployNFDOperatorWithRetries is in the nfd package
		DeployNFDOperatorWithRetries(inittools.APIClient, Nfd, OperatorDeploymentName,
			OperatorNamespace, NFDOperatorCheckInterval,
			NFDOperatorTimeout, level, ocpVersion)
	}
}

func DeployNFDOperatorWithRetries(apiClient *clients.Settings, nfdInstance *CustomConfig, operatorDeploymentName, operatorNamespace string, nfdCheckInterval, nfdTimeout time.Duration, logLevel glog.Level, ocpVersion string) {
	By("Deploy NFD Operator in NFD namespace")
	err := CreateNFDNamespace(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD Namespace: %v", err)

	By("Deploy NFD OperatorGroup in NFD namespace")
	err = CreateNFDOperatorGroup(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD OperatorGroup: %v", err)

	nfdDeployed := CreateNFDDeployment(apiClient, nfdInstance.CatalogSource, operatorDeploymentName, operatorNamespace, nfdCheckInterval, nfdTimeout, logging.Level(logLevel))
	if !nfdDeployed {
		By(fmt.Sprintf("Applying workaround for NFD failing to deploy on OCP %s", ocpVersion))

		err = DeleteNFDSubscription(apiClient)
		Expect(err).ToNot(HaveOccurred(), "error deleting NFD subscription: %v", err)

		err = DeleteAnyNFDCSV(apiClient)
		Expect(err).ToNot(HaveOccurred(), "error deleting NFD CSV: %v", err)

		err = DeleteOLMPods(apiClient, logging.Level(logLevel))
		Expect(err).ToNot(HaveOccurred(), "error deleting OLM pods for operator cache workaround: %v", err)

		glog.V(logLevel).Info("Re-trying NFD deployment")

		nfdDeployed = CreateNFDDeployment(apiClient, nfdInstance.CatalogSource, operatorDeploymentName, operatorNamespace, nfdCheckInterval, nfdTimeout, logging.Level(logLevel))
		Expect(nfdDeployed).ToNot(BeFalse(), "failed to deploy NFD operator")
	}

	By("Deploy NFD CR instance in NFD namespace")
	err = DeployCRInstance(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error deploying NFD CR instance in NFD namespace: %v", err)
}

func DeleteOLMPods(apiClient *clients.Settings, logLevel logging.Level) error {
	olmNamespace := "openshift-operator-lifecycle-manager"
	glog.V(glog.Level(logLevel)).Info("Deleting catalog operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=catalog-operator"}); err != nil {
		glog.Errorf("Error deleting catalog operator pods: %v", err)
		return err
	}

	glog.V(glog.Level(logLevel)).Info("Deleting OLM operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=olm-operator"}); err != nil {
		glog.Errorf("Error deleting OLM operator pods: %v", err)
		return err
	}

	return nil
}
