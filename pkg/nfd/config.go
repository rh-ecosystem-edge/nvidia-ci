package nfd

import . "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"

type CustomConfig struct {
	CustomCatalogSourceIndexImage string
	CreateCustomCatalogsource     bool
	CustomCatalogSource           string
	CatalogSource                 string
	CleanupAfterInstall           bool
}

func NewCustomConfig() *CustomConfig {
	return &CustomConfig{
		CustomCatalogSourceIndexImage: UndefinedValue,
		CreateCustomCatalogsource:     false,
		CustomCatalogSource:           UndefinedValue,
		CatalogSource:                 UndefinedValue,
		CleanupAfterInstall:           false,
	}
}
