// Package dranativeconfig contains environment-driven configuration for the dra-native test
// suite (tests/dra-native), which validates GPU Operator's native/CR-based DRA-enablement
// stack (NVIDIADriver + GPUCluster), introduced in GPU Operator 26.7.0.
//
// This is kept separate from nvidiagpuconfig so that CI can target a different GPU Operator
// catalog source/channel for this suite than for the ClusterPolicy-based base GPU suite,
// even when both are run in the same ginkgo invocation (env vars are process-wide).
package dranativeconfig

import (
	"github.com/golang/glog"
	"github.com/kelseyhightower/envconfig"
)

// DRANativeConfig contains environment information related to the dra-native test suite.
type DRANativeConfig struct {
	// CatalogSource is the OLM catalog source to install the GPU Operator from. Defaults to
	// nvidiagpu.CatalogSourceDefault ("certified-operators") when unset.
	CatalogSource string `envconfig:"DRANATIVE_CATALOGSOURCE"`
	// SubscriptionChannel is the OLM subscription channel to install the GPU Operator from.
	// It must resolve to a GPU Operator version >= 26.7.0 for this suite to exercise
	// anything meaningful; if unset, the package's default channel is used.
	SubscriptionChannel string `envconfig:"DRANATIVE_SUBSCRIPTION_CHANNEL"`
	// CleanupAfterTest controls whether resources created by this suite (GPUCluster,
	// NVIDIADriver, CSV, Subscription, OperatorGroup, Namespace, NFD) are deleted afterwards.
	CleanupAfterTest bool `envconfig:"DRANATIVE_CLEANUP" default:"true"`
}

// NewDRANativeConfig returns an instance of DRANativeConfig, populated from the DRANATIVE_
// prefixed environment variables. Logs at V(100) and returns nil on failure.
func NewDRANativeConfig() *DRANativeConfig {
	log := glog.V(100)
	log.Info("Creating new DRANativeConfig")

	cfg := &DRANativeConfig{}
	if err := envconfig.Process("", cfg); err != nil {
		glog.V(100).Infof("Failed to instantiate DRANativeConfig: %v", err)

		return nil
	}

	log.Info("DRANativeConfig created successfully")

	return cfg
}
