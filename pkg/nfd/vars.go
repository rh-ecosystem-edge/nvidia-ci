package nfd

import . "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"

type Config struct {
	CustomCatalogSourceIndexImage string
	CreateCustomCatalogsource     bool
	CustomCatalogSource           string
	CatalogSource                 string
	CleanupAfterInstall           bool
}

func NewConfig() *Config {
	return &Config{
		CustomCatalogSourceIndexImage: UndefinedValue, // Use the constant as the default
		CreateCustomCatalogsource:     false,          // Default value as specified
		CustomCatalogSource:           UndefinedValue, // Use the constant as the default
		CatalogSource:                 UndefinedValue, // Use the constant as the default
		CleanupAfterInstall:           false,          // Default value as specified
	}
}
