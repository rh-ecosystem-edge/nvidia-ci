package tsparams

import (
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	MpsLabels = append(gpuparams.Labels, LabelSuite)

	// ReporterNamespacesToDump tells to the reporter from where to collect logs.
	MpsReporterNamespacesToDump = map[string]string{
		"openshift-nfd":       "nfd-operator",
		"nvidia-gpu-operator": "gpu-operator",
		"mps-testing":         "mps-testing",
	}

	// ReporterCRDsToDump tells to the reporter what CRs to dump.
	MpsReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidiagpuv1.ClusterPolicyList{}},
	}
)
