package operatorconfig

import (
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global" //nolint:staticcheck
)

type CustomConfig struct {
	CustomCatalogSourceIndexImage string
	CreateCustomCatalogsource     bool

	CustomCatalogSource string
	CatalogSource       string
	CleanupAfterInstall bool
}

// NewCustomConfig creates a new CustomConfig instance with default settings.
// All string fields are initialized to UndefinedValue and boolean fields to false.
func NewCustomConfig() *CustomConfig {
	return &CustomConfig{
		CustomCatalogSourceIndexImage: UndefinedValue,
		CreateCustomCatalogsource:     false,

		CustomCatalogSource: UndefinedValue,
		CatalogSource:       UndefinedValue,
		CleanupAfterInstall: false,
	}
}
