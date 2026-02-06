package nfd

import (
	"log"

	yaml "go.yaml.in/yaml/v3"
)

// Config worker-config.
type Config struct {
	Sources Sources `yaml:"sources"`
}

// CPUConfig cpu feature config.
type CPUConfig struct {
	CPUID struct {
		AttributeBlacklist []string `yaml:"attributeBlacklist,omitempty"`
		AttributeWhitelist []string `yaml:"attributeWhitelist,omitempty"`
	} `yaml:"cpuid,omitempty"`
}

// PCIDevice pci config.
type PCIDevice struct {
	DeviceClassWhitelist []string `yaml:"deviceClassWhitelist,omitempty"`
	DeviceLabelFields    []string `yaml:"deviceLabelFields,omitempty"`
}

// Sources contains all sources.
type Sources struct {
	CPU    *CPUConfig    `yaml:"cpu,omitempty"`
	PCI    *PCIDevice    `yaml:"pci,omitempty"`
	USB    []interface{} `yaml:"usb,omitempty"`    // Add the necessary struct for USB if needed
	Custom []interface{} `yaml:"custom,omitempty"` // Add the necessary struct for Custom if needed
}

func NewConfig(config string) *Config {
	cfg := &Config{}
	err := yaml.Unmarshal([]byte(config), cfg)
	if err != nil {
		log.Printf("Unmarshal: %v", err)
	}
	return cfg
}

// SetPciWhitelistConfig updates the PCI device whitelist and label fields.
func (cfg *Config) SetPciWhitelistConfig(DeviceClassWhitelist, DeviceLabelFields []string) {

	if cfg.Sources.PCI == nil {
		cfg.Sources.PCI = &PCIDevice{}
	}
	cfg.Sources.PCI.DeviceClassWhitelist = cfg.Sources.PCI.DeviceClassWhitelist[:0]
	cfg.Sources.PCI.DeviceLabelFields = cfg.Sources.PCI.DeviceLabelFields[:0]
	cfg.Sources.PCI.DeviceClassWhitelist = append(cfg.Sources.PCI.DeviceClassWhitelist, DeviceClassWhitelist...)
	cfg.Sources.PCI.DeviceLabelFields = append(cfg.Sources.PCI.DeviceLabelFields, DeviceLabelFields...)
}

func (cfg *Config) GetYamlString() (string, error) {
	modifiedCPUYAML, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(modifiedCPUYAML), nil
}
