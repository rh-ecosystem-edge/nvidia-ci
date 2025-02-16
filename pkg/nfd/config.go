package nfd

import (
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	"time"
)

type CustomConfig struct {
	CustomCatalogSourceIndexImage string
	CreateCustomCatalogsource     bool

	CustomCatalogSource string
	CatalogSource       string
	CleanupAfterInstall bool
}

func NewCustomConfig() *CustomConfig {
	return &CustomConfig{
		CustomCatalogSourceIndexImage: UndefinedValue,
		CreateCustomCatalogsource:     false,

		CustomCatalogSource: UndefinedValue,
		CatalogSource:       UndefinedValue,
		CleanupAfterInstall: false,
	}
}

// NfdParams holds all the configuration details required to install or manage
// the Node Feature Discovery (NFD) operator on a cluster.
type NfdParams struct {
	// OLM package name (as seen in the package manifest)
	Package string

	// Where the NFD operator is typically found in the default OperatorHub
	CatalogSourceDefault   string
	CatalogSourceNamespace string

	// Whether to create a custom CatalogSource if the default one doesn't contain NFD
	CreateCustomCatalogsource bool

	// Custom CatalogSource details (used if CreateCustomCatalogsource is true)
	CustomCatalogSource            string
	CustomCatalogSourceIndexImage  string
	CustomCatalogSourceDisplayName string

	// Operator installation details
	OperatorDeploymentName string
	OperatorNamespace      string

	// Time intervals for checking operator readiness
	OperatorCheckInterval time.Duration
	OperatorTimeout       time.Duration

	// Flag indicating whether to remove/clean up NFD after the installation/test
	CleanupAfterInstall bool
}
