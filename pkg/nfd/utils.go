package nfd

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Dot import is standard for Ginkgo tests
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Dot import is standard for Gomega assertions
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/operatorconfig" //nolint:revive,staticcheck // Dot import for test configuration
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/logging"
)

// EnsureNFDIsInstalled ensures that the Node Feature Discovery (NFD) operator
// is installed on the cluster. If not, it attempts to deploy the operator and,
// if necessary, creates a custom CatalogSource to make NFD available.
func EnsureNFDIsInstalled(apiClient *clients.Settings, nfd *CustomConfig, ocpVersion string, level glog.Level) {
	By("Check if NFD is installed")
	nfdInstalled, err := check.NFDDeploymentsReady(apiClient)

	if nfdInstalled && err == nil {
		glog.V(gpuparams.GpuLogLevel).Infof("The check for ready NFD deployments is: %v", nfdInstalled)
		glog.V(gpuparams.GpuLogLevel).Infof("NFD operators and operands are already installed on " +
			"this cluster")
	} else {
		glog.V(level).Infof("NFD is not currently installed on this cluster")
		glog.V(level).Infof("Deploying NFD Operator and CR instance on this cluster")

		nfd.CleanupAfterInstall = true

		if nfd.CreateCustomCatalogsource {
			glog.V(level).Infof("Creating custom catalogsource '%s' for NFD "+
				"Operator with index image '%s'", nfd.CustomCatalogSource, nfd.CustomCatalogSourceIndexImage)

			nfdCustomCatalogSourceBuilder := olm.NewCatalogSourceBuilderWithIndexImage(inittools.APIClient,
				nfd.CustomCatalogSource, CatalogSourceNamespace, nfd.CustomCatalogSourceIndexImage,
				CustomCatalogSourceDisplayName, CustomNFDCatalogSourcePublisherName)

			Expect(nfdCustomCatalogSourceBuilder).ToNot(BeNil(), "error creating custom "+
				"NFD catalogsource %s", nfd.CustomCatalogSource)

			createdNFDCustomCatalogSourceBuilder, err := nfdCustomCatalogSourceBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating custom NFD "+
				"catalogsource '%s':  %v", nfd.CustomCatalogSource, err)

			Expect(createdNFDCustomCatalogSourceBuilder).ToNot(BeNil(), "Failed to "+
				" create custom NFD catalogsource '%s'", nfd.CustomCatalogSource)

			By(fmt.Sprintf("Sleep for %s to allow the NFD custom catalogsource to be created", nvidiagpu.SleepDuration.String()))
			time.Sleep(nvidiagpu.SleepDuration)

			glog.V(level).Infof("Wait up to %s for custom NFD catalogsource '%s' to be ready", nvidiagpu.WaitDuration.String(), createdNFDCustomCatalogSourceBuilder.Definition.Name)

			Expect(createdNFDCustomCatalogSourceBuilder.IsReady(nvidiagpu.WaitDuration)).NotTo(BeFalse())

			nfdPkgManifestBuilderByCustomCatalog, err := olm.PullPackageManifestByCatalogWithTimeout(inittools.APIClient,
				Package, CatalogSourceNamespace, nfd.CustomCatalogSource, 30*time.Second, 5*time.Minute)

			Expect(err).ToNot(HaveOccurred(), "error getting NFD packagemanifest '%s' "+
				"from custom catalog '%s':  %v", Package, nfd.CustomCatalogSource, err)

			nfd.CatalogSource = nfd.CustomCatalogSource
			nfdChannel := nfdPkgManifestBuilderByCustomCatalog.Object.Status.DefaultChannel
			glog.V(level).Infof("NFD channel '%s' retrieved from packagemanifest "+
				"of custom catalogsource '%s'", nfdChannel, nfd.CustomCatalogSource)

		} else {

			By("Check if 'nfd' packagemanifest exists in 'redhat-operators' default catalog")
			nfdPkgManifestBuilderByCatalog, err := olm.PullPackageManifestByCatalog(apiClient,
				Package, CatalogSourceNamespace, CatalogSourceDefault)

			Expect(err).ToNot(HaveOccurred(), "error getting NFD packagemanifest '%s' "+
				"from default catalog '%s':  %v", Package, CatalogSourceDefault, err)

			if nfdPkgManifestBuilderByCatalog == nil {
				glog.V(level).Infof("NFD packagemanifest was not found in the default '%s'"+
					" catalog.", CatalogSourceDefault)
				Skip("NFD packagemanifest not found in default 'redhat-operators' catalogsource, " +
					"and no custom catalogsource is defined")
			}

			glog.V(level).Infof("The nfd packagemanifest '%s' was found in the default"+
				" catalog '%s'", nfdPkgManifestBuilderByCatalog.Object.Name, CatalogSourceDefault)

			nfd.CatalogSource = CatalogSourceDefault
			nfdChannel := nfdPkgManifestBuilderByCatalog.Object.Status.DefaultChannel
			glog.V(level).Infof("The NFD channel retrieved from packagemanifest is:  %v",
				nfdChannel)

		}

		DeployNFDOperatorWithRetries(inittools.APIClient, nfd, level, ocpVersion)
	}
}

func DeployNFDOperatorWithRetries(apiClient *clients.Settings, nfdInstance *CustomConfig, logLevel glog.Level, ocpVersion string) {
	By("Deploy NFD Operator in NFD namespace")
	err := CreateNFDNamespace(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD Namespace: %v", err)

	By("Deploy NFD OperatorGroup in NFD namespace")
	err = CreateNFDOperatorGroup(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error creating NFD OperatorGroup: %v", err)

	nfdDeployed := CreateNFDDeployment(apiClient, nfdInstance.CatalogSource, logging.Level(logLevel))
	if !nfdDeployed {
		By(fmt.Sprintf("Applying workaround for NFD failing to deploy on OCP %s", ocpVersion))

		err = DeleteNFDSubscription(apiClient)
		Expect(err).ToNot(HaveOccurred(), "error deleting NFD subscription: %v", err)

		err = DeleteAnyNFDCSV(apiClient)
		Expect(err).ToNot(HaveOccurred(), "error deleting NFD CSV: %v", err)

		err = olm.DeleteOLMPods(apiClient, logging.Level(logLevel))
		Expect(err).ToNot(HaveOccurred(), "error deleting OLM pods for operator cache workaround: %v", err)

		glog.V(logLevel).Info("Re-trying NFD deployment")

		nfdDeployed = CreateNFDDeployment(apiClient, nfdInstance.CatalogSource, logging.Level(logLevel))
		Expect(nfdDeployed).ToNot(BeFalse(), "failed to deploy NFD operator")
	}

	By("Deploy NFD CR instance in NFD namespace")
	err = DeployCRInstance(apiClient)
	Expect(err).ToNot(HaveOccurred(), "error deploying NFD CR instance in NFD namespace: %v", err)
}
