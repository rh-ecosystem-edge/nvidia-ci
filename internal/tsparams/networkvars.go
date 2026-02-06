package tsparams

import (
	nvidianetworkv1alpha1 "github.com/Mellanox/network-operator/api/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
)

var (
	// NetworkLabels represents the range of labels that can be used for test cases selection.
	NetworkLabels = append(networkparams.Labels, NetworkLabelSuite)

	// NetworkReporterNamespacesToDump tells to the reporter from where to collect logs.
	NetworkReporterNamespacesToDump = map[string]string{
		"openshift-nfd":           "nfd-operator",
		"nvidia-network-operator": "network-operator",
	}

	// NetworkReporterCRDsToDump tells to the reporter what CRs to dump.
	NetworkReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidianetworkv1alpha1.NicClusterPolicyList{}},
	}
)
