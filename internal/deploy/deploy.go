package deploy

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/golang/glog"
	"github.com/kelseyhightower/envconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	_ "go.uber.org/mock/mockgen/model"
)

//go:generate mockgen -package=deploy -destination mock_corev1.go k8s.io/client-go/kubernetes/typed/core/v1 CoreV1Interface,NamespaceInterface
//go:generate mockgen -package=deploy -destination mock_appsv1.go k8s.io/client-go/kubernetes/typed/apps/v1 AppsV1Interface,DeploymentInterface

type BundleConfig struct {
	BundleImage string `required:"true" envconfig:"NVIDIAGPU_BUNDLE_IMAGE" default:"registry.gitlab.com/nvidia/kubernetes/gpu-operator/staging/gpu-operator-bundle:master-latest"`
}

//go:generate mockgen -source=deploy.go -package=deploy -destination=mock_deploy.go
type Deploy interface {
	GetBundleConfig(logLevel glog.Level) (*BundleConfig, error)
	CreateAndLabelNamespaceIfNeeded(logLevel glog.Level, targetNs string, labels map[string]string) (*namespace.Builder, error)
	DeployBundle(logLevel glog.Level, bundleConfig *BundleConfig, ns string, timeout time.Duration) error
	WaitForReadyStatus(logLevel glog.Level, name, ns string, timeout time.Duration) error
}

type deploy struct {
	client *clients.Settings
}

func NewDeploy(client *clients.Settings) Deploy {
	return deploy{
		client: client,
	}
}

func (d deploy) GetBundleConfig(logLevel glog.Level) (*BundleConfig, error) {

	glog.V(logLevel).Infof("Getting the GPU bundle configs")

	var bundleConfig BundleConfig

	if err := envconfig.Process("gpu", &bundleConfig); err != nil {
		return nil, fmt.Errorf("failed to instantiate bundle configs: %v", err)
	}

	return &bundleConfig, nil
}

func (d deploy) CreateAndLabelNamespaceIfNeeded(logLevel glog.Level, ns string,
	labels map[string]string) (*namespace.Builder, error) {

	nsBuilder := namespace.NewBuilder(d.client, ns)

	if nsBuilder.Exists() {
		glog.V(logLevel).Infof("The namespace '%s' already exists", ns)
		return nsBuilder, nil
	}

	glog.V(logLevel).Infof("Creating the namespace: %s", ns)
	createdNsBuilder, err := nsBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace %s: %v", ns, err)
	}
	glog.V(logLevel).Infof("Successfully created namespace '%s'", ns)

	glog.V(logLevel).Infof("Labeling the newly created namespace '%s'", ns)
	nsBuilder, err = createdNsBuilder.WithMultipleLabels(labels).Update()
	if err != nil {
		return nil, fmt.Errorf("failed to label namespace %s with labels %v: %v", ns, labels, err)
	}
	glog.V(logLevel).Infof("Successfully labeled the namespace %s", ns)

	return nsBuilder, nil
}

func (d deploy) DeployBundle(logLevel glog.Level, bundleConfig *BundleConfig, ns string, timeout time.Duration) error {

	cmd := exec.Command("operator-sdk", "run", "bundle", bundleConfig.BundleImage,
		"--namespace", ns, "--timeout", timeout.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to wait for operator-sdk to run the bundle: %v", err)
	}

	return nil
}

func (d deploy) WaitForReadyStatus(logLevel glog.Level, name, ns string, timeout time.Duration) error {

	dep, err := deployment.Pull(d.client, name, ns)
	if err != nil {
		return fmt.Errorf("failed to pull deployment %s in namespace %s", name, ns)
	}

	if !dep.IsReady(timeout) {
		return fmt.Errorf("timed out waiting for deployment %s in namespace %s to be ready", name, ns)
	}

	return nil
}
