package tsparams

import (
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiagpuv1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	Labels = append(gpuparams.Labels, LabelSuite)

	// ReporterNamespacesToDump tells to the reporter from where to collect logs.
	ReporterNamespacesToDump = map[string]string{
		"openshift-nfd":       "nfd-operator",
		"nvidia-gpu-operator": "gpu-operator",
		GPUTestNamespace:      "test-gpu-burn",
	}

	// ReporterCRDsToDump tells to the reporter what CRs to dump. GPUCluster is intentionally
	// not included: no generated Go type for it is available yet in the vendored
	// github.com/NVIDIA/gpu-operator module (see pkg/nvidiagpu/gpucluster.go), and
	// k8sreporter.CRData requires a typed, scheme-registered client.ObjectList.
	ReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &nvidiagpuv1.ClusterPolicyList{}},
		{Cr: &nvidiagpuv1alpha1.NVIDIADriverList{}},
	}
)
