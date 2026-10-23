package tsparams

import (
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/sriovparams"
	sriovnetworkapi "github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork/api"
)

var (
	// SriovLabels represents the range of labels that can be used for test cases selection.
	SriovLabels = append(sriovparams.Labels, SriovLabelSuite)

	// SriovReporterNamespacesToDump tells to the reporter from where to collect logs.
	SriovReporterNamespacesToDump = map[string]string{
		"openshift-sriov-network-operator": "sriov-network-operator",
	}

	// SriovReporterCRDsToDump tells to the reporter what CRs to dump.
	SriovReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &sriovnetworkapi.SriovOperatorConfigList{}},
		{Cr: &sriovnetworkapi.SriovNetworkNodePolicyList{}},
		{Cr: &sriovnetworkapi.SriovNetworkList{}},
		{Cr: &sriovnetworkapi.SriovNetworkNodeStateList{}},
	}
)
