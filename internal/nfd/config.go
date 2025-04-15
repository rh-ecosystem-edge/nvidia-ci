package nfd

import (
	"log"

	"github.com/kelseyhightower/envconfig"
)

// NFDConfig contains only the fallback catalog source index image for NFD.
type NFDConfig struct {
	FallbackCatalogSourceIndexImage string `envconfig:"NFD_FALLBACK_CATALOGSOURCE_INDEX_IMAGE"`
}

// NewNFDConfig returns an instance of NFDConfig.
func NewNFDConfig() *NFDConfig {
	log.Print("Creating new NFDConfig")

	nfdConfig := new(NFDConfig)

	// Process environment variables with prefix "NFD_"
	err := envconfig.Process("NFD_", nfdConfig)
	if err != nil {
		log.Printf("Failed to instantiate nfdConfig: %v", err)
		return nil
	}

	return nfdConfig
}
