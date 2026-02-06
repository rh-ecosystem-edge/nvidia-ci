package olm

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListCatalogSources returns catalogsource inventory in the given namespace.
func ListCatalogSources(apiClient *clients.Settings, nsname string, options ...metav1.ListOptions) ([]*CatalogSourceBuilder, error) {
	if nsname == "" {
		glog.V(100).Infof("catalogsource 'namespace' parameter can not be empty")
		return nil, fmt.Errorf("failed to list catalogsource, 'namespace' parameter is empty")
	}

	passedOptions := metav1.ListOptions{}
	logMessage := fmt.Sprintf("Listing catalogsource in the namespace %s", nsname)

	if len(options) > 1 {
		glog.V(100).Infof("'options' parameter must be empty or single-valued")

		return nil, fmt.Errorf("error: more than one ListOptions was passed")
	}

	if len(options) == 1 {
		passedOptions = options[0]
		logMessage += fmt.Sprintf(" with the options %v", passedOptions)
	}

	glog.V(100).Infof(logMessage)

	catalogSourceList, err := apiClient.OperatorsV1alpha1Interface.CatalogSources(nsname).List(
		context.TODO(), passedOptions)

	if err != nil {
		glog.V(100).Infof("Failed to list catalogsources in the namespace %s due to %s", nsname, err.Error())

		return nil, err
	}

	var catalogSourceObjects []*CatalogSourceBuilder

	for _, existingCatalogSource := range catalogSourceList.Items {
		copiedCatalogSource := existingCatalogSource
		catalogSourceBuilder := &CatalogSourceBuilder{
			apiClient:  apiClient,
			Object:     &copiedCatalogSource,
			Definition: &copiedCatalogSource,
		}

		catalogSourceObjects = append(catalogSourceObjects, catalogSourceBuilder)
	}

	return catalogSourceObjects, nil
}
