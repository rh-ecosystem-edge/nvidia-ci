package shared

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// simpleRESTClientGetter provides a minimal RESTClientGetter implementation
// that directly uses an existing rest.Config and stores the original APIClient for retrieval.
type simpleRESTClientGetter struct {
	apiClient *clients.Settings
	namespace string
}

func (s *simpleRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return s.apiClient.Config, nil
}

func (s *simpleRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(s.apiClient.Config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (s *simpleRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := s.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	return mapper, nil
}

func (s *simpleRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return &simpleClientConfig{config: s.apiClient.Config, namespace: s.namespace}
}

// APIClient returns the original clients.Settings that was used to create this getter.
func (s *simpleRESTClientGetter) APIClient() *clients.Settings {
	return s.apiClient
}

// simpleClientConfig provides a minimal ClientConfig implementation
type simpleClientConfig struct {
	config    *rest.Config
	namespace string
}

func (s *simpleClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, nil
}

func (s *simpleClientConfig) ClientConfig() (*rest.Config, error) {
	return s.config, nil
}

func (s *simpleClientConfig) Namespace() (string, bool, error) {
	return s.namespace, false, nil
}

func (s *simpleClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return clientcmd.NewDefaultClientConfigLoadingRules()
}

// NewActionConfig creates a Helm action configuration using an existing Kubernetes client.
// This function provides the bridge between our existing APIClient and Helm's requirements.
func NewActionConfig(apiClient *clients.Settings, namespace string, logLevel glog.Level) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	// Use our simple getter that directly provides the rest.Config and stores apiClient
	restClientGetter := &simpleRESTClientGetter{
		apiClient: apiClient,
		namespace: namespace,
	}

	// Provide a log function for Helm (required, cannot be nil)
	logFunc := func(format string, v ...interface{}) {
		glog.V(logLevel).Infof(format, v...)
	}

	if err := actionConfig.Init(restClientGetter, namespace, "secret", logFunc); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	return actionConfig, nil
}

// GetAPIClient retrieves the original clients.Settings from an action.Configuration.
// Returns nil if the configuration wasn't created with NewActionConfig.
func GetAPIClient(actionConfig *action.Configuration) *clients.Settings {
	if getter, ok := actionConfig.RESTClientGetter.(*simpleRESTClientGetter); ok {
		return getter.APIClient()
	}
	return nil
}
