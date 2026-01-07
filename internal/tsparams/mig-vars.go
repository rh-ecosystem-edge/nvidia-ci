package tsparams

import (
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	MigLabels = append(gpuparams.Labels, LabelSuite)

	// ReporterNamespacesToDump tells to the reporter from where to collect logs.
	MigReporterNamespacesToDump = map[string]string{
		"openshift-nfd":       "nfd-operator",
		"nvidia-gpu-operator": "gpu-operator",
		"mig-testing":         "mig-testing",
	}

	// ReporterCRDsToDump tells to the reporter what CRs to dump.
	MigReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidiagpuv1.ClusterPolicyList{}},
	}
)
