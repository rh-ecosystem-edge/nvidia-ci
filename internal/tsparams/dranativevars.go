package tsparams

import (
	nvidiagpuv1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
)

var (
	// DRANativeLabels represents the range of labels that can be used for dra-native test
	// cases selection.
	DRANativeLabels = append(gpuparams.Labels, "dra", "dra-native")

	// DRANativeReporterNamespacesToDump tells the reporter from where to collect logs when a
	// dra-native test fails.
	DRANativeReporterNamespacesToDump = map[string]string{
		"openshift-nfd":       "nfd-operator",
		"nvidia-gpu-operator": "gpu-operator",
	}

	// DRANativeReporterCRDsToDump tells the reporter what CRs to dump when a dra-native test
	// fails. GPUCluster is intentionally not included here: no generated Go type for it is
	// available yet in the vendored github.com/NVIDIA/gpu-operator module (see
	// pkg/nvidiagpu/gpucluster.go), and k8sreporter.CRData requires a typed,
	// scheme-registered client.ObjectList.
	DRANativeReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidiagpuv1alpha1.NVIDIADriverList{}},
	}
)
