package deploy

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
)

// Default timeouts/poll intervals used by InstallOperatorFromCatalog when the corresponding
// OLMInstallConfig field is left at its zero value.
const (
	defaultDeploymentCreationCheckInterval = 30 * time.Second
	defaultDeploymentCreationTimeout       = 4 * time.Minute
	defaultDeploymentReadyTimeout          = 4 * time.Minute
	defaultCSVSucceededCheckInterval       = 60 * time.Second
	defaultCSVSucceededTimeout             = 15 * time.Minute
)

// OLMInstallConfig describes the parameters needed to install an operator via OLM
// (Namespace + OperatorGroup + Subscription) from a catalog source, and to wait for its
// Deployment and ClusterServiceVersion to become ready. It covers only the common
// catalog-source install path: it does not support bundle-based deployment or creating a
// custom CatalogSource from a fallback index image.
type OLMInstallConfig struct {
	// Namespace is the namespace in which the operator, OperatorGroup and Subscription are
	// created. Created (and labeled with NamespaceLabels) if it does not already exist.
	Namespace string
	// NamespaceLabels are labels applied to Namespace when it is created by this call.
	NamespaceLabels map[string]string
	// OperatorGroupName is the name of the OperatorGroup to create in Namespace.
	OperatorGroupName string
	// SubscriptionName is the name of the Subscription to create in Namespace.
	SubscriptionName string
	// PackageName is the operator package name (e.g. "gpu-operator-certified").
	PackageName string
	// CatalogSource and CatalogSourceNamespace identify where the package is published.
	CatalogSource          string
	CatalogSourceNamespace string
	// Channel is the subscription channel to subscribe to. It must be resolved by the
	// caller (e.g. via olm.PullPackageManifestByCatalog) before calling this function; it
	// cannot be left empty.
	Channel string
	// InstallPlanApproval controls how the Subscription's InstallPlan is approved.
	InstallPlanApproval operatorsv1alpha1.Approval
	// DeploymentName is the name of the operator Deployment to wait for.
	DeploymentName string

	// The following fields override the corresponding default timeout/poll interval when set
	// to a positive value.
	DeploymentCreationCheckInterval time.Duration
	DeploymentCreationTimeout       time.Duration
	DeploymentReadyTimeout          time.Duration
	CSVSucceededCheckInterval       time.Duration
	CSVSucceededTimeout             time.Duration
}

// OLMInstallResult captures the objects created/observed by InstallOperatorFromCatalog, so
// that callers can both inspect them (e.g. to build alm-examples-derived CRs) and tear them
// down again.
type OLMInstallResult struct {
	NamespaceBuilder     *namespace.Builder
	OperatorGroupBuilder *olm.OperatorGroupBuilder
	SubscriptionBuilder  *olm.SubscriptionBuilder
	CSVBuilder           *olm.ClusterServiceVersionBuilder
	// AlmExamples is the raw 'alm-examples' annotation content of the installed CSV.
	AlmExamples string
}

// InstallOperatorFromCatalog installs an operator via OLM from a catalog source: it ensures
// the target namespace exists, creates an OperatorGroup and a Subscription, waits for the
// operator Deployment to be created and ready, waits for the resulting CSV to succeed, and
// returns the CSV's alm-examples along with the builders for every object it created so the
// caller can perform its own teardown.
func InstallOperatorFromCatalog(apiClient *clients.Settings, logLevel glog.Level,
	cfg OLMInstallConfig) (*OLMInstallResult, error) {
	if cfg.Channel == "" {
		return nil, fmt.Errorf("OLMInstallConfig.Channel must not be empty")
	}

	result := &OLMInstallResult{}

	glog.V(logLevel).Infof("Ensuring namespace %q exists", cfg.Namespace)

	nsBuilder := namespace.NewBuilder(apiClient, cfg.Namespace)
	if nsBuilder.Exists() {
		glog.V(logLevel).Infof("Namespace %q already exists", cfg.Namespace)
	} else {
		createdNsBuilder, err := nsBuilder.Create()
		if err != nil {
			return result, fmt.Errorf("failed to create namespace %q: %w", cfg.Namespace, err)
		}

		nsBuilder = createdNsBuilder

		if len(cfg.NamespaceLabels) > 0 {
			nsBuilder, err = nsBuilder.WithMultipleLabels(cfg.NamespaceLabels).Update()
			if err != nil {
				return result, fmt.Errorf("failed to label namespace %q: %w", cfg.Namespace, err)
			}
		}
	}

	result.NamespaceBuilder = nsBuilder

	glog.V(logLevel).Infof("Ensuring OperatorGroup %q exists in namespace %q", cfg.OperatorGroupName, cfg.Namespace)

	ogBuilder := olm.NewOperatorGroupBuilder(apiClient, cfg.OperatorGroupName, cfg.Namespace)
	if ogBuilder.Exists() {
		glog.V(logLevel).Infof("OperatorGroup %q already exists", cfg.OperatorGroupName)
	} else {
		createdOgBuilder, err := ogBuilder.Create()
		if err != nil {
			return result, fmt.Errorf("failed to create OperatorGroup %q: %w", cfg.OperatorGroupName, err)
		}

		ogBuilder = createdOgBuilder
	}

	result.OperatorGroupBuilder = ogBuilder

	glog.V(logLevel).Infof("Creating Subscription %q in namespace %q (catalogSource=%q, channel=%q)",
		cfg.SubscriptionName, cfg.Namespace, cfg.CatalogSource, cfg.Channel)

	subBuilder := olm.NewSubscriptionBuilder(apiClient, cfg.SubscriptionName, cfg.Namespace,
		cfg.CatalogSource, cfg.CatalogSourceNamespace, cfg.PackageName)
	subBuilder.WithChannel(cfg.Channel)
	subBuilder.WithInstallPlanApproval(cfg.InstallPlanApproval)

	createdSubBuilder, err := subBuilder.Create()
	if err != nil {
		return result, fmt.Errorf("failed to create Subscription %q: %w", cfg.SubscriptionName, err)
	}

	result.SubscriptionBuilder = createdSubBuilder

	deploymentCreationCheckInterval := durationOrDefault(cfg.DeploymentCreationCheckInterval, defaultDeploymentCreationCheckInterval)
	deploymentCreationTimeout := durationOrDefault(cfg.DeploymentCreationTimeout, defaultDeploymentCreationTimeout)
	deploymentReadyTimeout := durationOrDefault(cfg.DeploymentReadyTimeout, defaultDeploymentReadyTimeout)
	csvCheckInterval := durationOrDefault(cfg.CSVSucceededCheckInterval, defaultCSVSucceededCheckInterval)
	csvTimeout := durationOrDefault(cfg.CSVSucceededTimeout, defaultCSVSucceededTimeout)

	glog.V(logLevel).Infof("Waiting up to %s for Deployment %q to be created in namespace %q",
		deploymentCreationTimeout, cfg.DeploymentName, cfg.Namespace)

	if !wait.DeploymentCreated(apiClient, cfg.DeploymentName, cfg.Namespace,
		deploymentCreationCheckInterval, deploymentCreationTimeout) {
		return result, fmt.Errorf("timed out waiting for Deployment %q to be created in namespace %q",
			cfg.DeploymentName, cfg.Namespace)
	}

	operatorDeployment, err := deployment.Pull(apiClient, cfg.DeploymentName, cfg.Namespace)
	if err != nil {
		return result, fmt.Errorf("failed to pull Deployment %q in namespace %q: %w",
			cfg.DeploymentName, cfg.Namespace, err)
	}

	if !operatorDeployment.IsReady(deploymentReadyTimeout) {
		return result, fmt.Errorf("Deployment %q in namespace %q did not become ready within %s",
			cfg.DeploymentName, cfg.Namespace, deploymentReadyTimeout)
	}

	glog.V(logLevel).Infof("Listing ClusterServiceVersions in namespace %q", cfg.Namespace)

	csvBuilderList, err := olm.ListClusterServiceVersion(apiClient, cfg.Namespace)
	if err != nil {
		return result, fmt.Errorf("failed to list ClusterServiceVersions in namespace %q: %w", cfg.Namespace, err)
	}

	if len(csvBuilderList) != 1 {
		return result, fmt.Errorf("expected exactly one ClusterServiceVersion in namespace %q, found %d",
			cfg.Namespace, len(csvBuilderList))
	}

	csvName := csvBuilderList[0].Definition.Name

	glog.V(logLevel).Infof("Waiting up to %s for CSV %q to reach the Succeeded phase", csvTimeout, csvName)

	if err := wait.CSVSucceeded(apiClient, csvName, cfg.Namespace, csvCheckInterval, csvTimeout); err != nil {
		return result, fmt.Errorf("CSV %q in namespace %q did not succeed: %w", csvName, cfg.Namespace, err)
	}

	csvBuilder, err := olm.PullClusterServiceVersion(apiClient, csvName, cfg.Namespace)
	if err != nil {
		return result, fmt.Errorf("failed to pull CSV %q in namespace %q: %w", csvName, cfg.Namespace, err)
	}

	result.CSVBuilder = csvBuilder

	almExamples, err := csvBuilder.GetAlmExamples()
	if err != nil {
		return result, fmt.Errorf("failed to get alm-examples from CSV %q: %w", csvName, err)
	}

	result.AlmExamples = almExamples

	return result, nil
}

// durationOrDefault returns d if it is positive, or def otherwise.
func durationOrDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}

	return d
}
