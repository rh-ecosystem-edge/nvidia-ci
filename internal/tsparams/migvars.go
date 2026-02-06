package tsparams

import (
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	MigLabels = append(gpuparams.MigLabels, LabelSuite)

	// ReporterNamespacesToDump tells to the reporter from where to collect logs.
	MigReporterNamespacesToDump = map[string]string{
		"openshift-nfd":       "nfd-operator",
		"nvidia-gpu-operator": "gpu-operator",
		"mig-testing":         "test-gpu-burn",
	}

	// ReporterCRDsToDump tells to the reporter what CRs to dump.
	MigReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidiagpuv1.ClusterPolicyList{}},
	}
)
