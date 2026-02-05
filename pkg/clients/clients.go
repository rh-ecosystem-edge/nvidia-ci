package clients

import (
	"fmt"
	"log"
	"os"

	"github.com/golang/glog"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	clientConfigV1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	v1security "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	olm2 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/scheme"

	olmv1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1"
	olm "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"

	pkgManifestV1 "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/operators/v1"
	clientPkgManifestV1 "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/client/clientset/versioned/typed/operators/v1"

	apiExt "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsV1Client "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	operatorV1 "github.com/openshift/api/operator/v1"

	"k8s.io/client-go/kubernetes/scheme"
	coreV1Client "k8s.io/client-go/kubernetes/typed/core/v1"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiagpuv1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"

	nvidianetworkv1alpha1 "github.com/Mellanox/network-operator/api/v1alpha1"

	machinev1beta1client "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"
	operatorv1alpha1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1alpha1"
	nfdv1 "github.com/openshift/cluster-nfd-operator/api/v1"
)

// Settings provides the struct to talk with relevant API.
type Settings struct {
	KubeconfigPath string
	K8sClient      kubernetes.Interface
	coreV1Client.CoreV1Interface
	clientConfigV1.ConfigV1Interface
	appsV1Client.AppsV1Interface
	Config *rest.Config
	runtimeClient.Client
	v1security.SecurityV1Interface
	olm.OperatorsV1alpha1Interface
	dynamic.Interface
	olmv1.OperatorsV1Interface
	PackageManifestInterface clientPkgManifestV1.PackagesV1Interface
	operatorv1alpha1.OperatorV1alpha1Interface
	machinev1beta1client.MachineV1beta1Interface
}

// New returns a *Settings with the given kubeconfig.
//
//nolint:funlen
func New(kubeconfig string) *Settings {
	var (
		config *rest.Config
		err    error
	)

	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	if kubeconfig != "" {
		log.Printf("Loading kube client config from path %q", kubeconfig)

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		log.Print("Using in-cluster kube client config")

		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil
	}

	clientSet := &Settings{}
	clientSet.CoreV1Interface = coreV1Client.NewForConfigOrDie(config)
	clientSet.ConfigV1Interface = clientConfigV1.NewForConfigOrDie(config)
	clientSet.AppsV1Interface = appsV1Client.NewForConfigOrDie(config)
	clientSet.OperatorsV1alpha1Interface = olm.NewForConfigOrDie(config)
	clientSet.Interface = dynamic.NewForConfigOrDie(config)
	clientSet.OperatorsV1Interface = olmv1.NewForConfigOrDie(config)
	clientSet.PackageManifestInterface = clientPkgManifestV1.NewForConfigOrDie(config)
	clientSet.SecurityV1Interface = v1security.NewForConfigOrDie(config)
	clientSet.OperatorV1alpha1Interface = operatorv1alpha1.NewForConfigOrDie(config)
	clientSet.MachineV1beta1Interface = machinev1beta1client.NewForConfigOrDie(config)
	clientSet.K8sClient = kubernetes.NewForConfigOrDie(config)
	clientSet.Config = config

	crScheme := runtime.NewScheme()
	err = SetScheme(crScheme)

	if err != nil {
		log.Print("Error to load apiClient scheme")

		return nil
	}

	clientSet.Client, err = runtimeClient.New(config, runtimeClient.Options{
		Scheme: crScheme,
	})

	if err != nil {
		log.Print("Error to create apiClient")

		return nil
	}

	clientSet.KubeconfigPath = kubeconfig

	return clientSet
}

// SetScheme returns mutated apiClient's scheme.
//
//nolint:funlen
func SetScheme(crScheme *runtime.Scheme) error {
	if err := scheme.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := apiExt.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := operatorV1.Install(crScheme); err != nil {
		return err
	}

	if err := olm2.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := nvidiagpuv1.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := nvidiagpuv1alpha1.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := nvidianetworkv1alpha1.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := nfdv1.AddToScheme(crScheme); err != nil {
		return err
	}

	if err := pkgManifestV1.AddToScheme(crScheme); err != nil {
		return err
	}

	return nil
}

// GetAPIClient implements the cluster.APIClientGetter interface.
func (settings *Settings) GetAPIClient() (*Settings, error) {
	if settings == nil {
		glog.V(100).Infof("APIClient is nil")

		return nil, fmt.Errorf("APIClient cannot be nil")
	}

	return settings, nil
}
