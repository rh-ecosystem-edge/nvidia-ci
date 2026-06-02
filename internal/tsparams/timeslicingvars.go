package tsparams

import (
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
)

var (
	// TimeSlicingLabels represents the range of labels that can be used for test cases selection.
	TimeSlicingLabels = append(gpuparams.Labels, LabelSuite)

	// TimeSlicingReporterNamespacesToDump tells to the reporter from where to collect logs.
	TimeSlicingReporterNamespacesToDump = map[string]string{
		"openshift-nfd":       "nfd-operator",
		"nvidia-gpu-operator": "gpu-operator",
		"test-timeslicing":    "test-timeslicing",
	}

	// TimeSlicingReporterCRDsToDump tells to the reporter what CRs to dump.
	TimeSlicingReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidiagpuv1.ClusterPolicyList{}},
	}
)
