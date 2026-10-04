package nvidianetwork

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nnopairreport"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidianetwork"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
)

const (
	nnoReportStatusRunning  = "running"
	nnoReportStatusNotFound = "not_found"
	nnoReportStatusPassed   = "passed"
	nnoReportStatusFailed   = "failed"
	nnoReportStatusNotRun   = "not_run"
	nnoReportStatusSkipped  = "skipped"

	nnoStepSharedDevice     = "network-operator-e2e-shared-device"
	nnoStepSharedDeviceRDMA = "network-operator-e2e-shared-device-rdma"
	nnoStepGPUDirect        = "network-operator-e2e-gpudirect"
	nnoStepPrecompiled      = "network-operator-e2e-precompiled"
	nnoStepRDMASuffix       = "-rdma"
	nnoStepGPUDirectMarker  = "gpudirect"
	nnoDriverCheckFound     = "found"
	nnoCheckOCPVersion      = "ocp_version"
	nnoCheckNNOCSV          = "nno_csv"
	nnoCheckRDMAShared      = "rdma_shared_device"
)

func nnoArtifactStepName(envName string, isGPUDirect, isStandardRDMA, precompiled bool) string {
	derived := nnoDerivedStepName(isGPUDirect, isStandardRDMA, precompiled)
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return derived
	}
	switch {
	case isGPUDirect:
		if strings.Contains(envName, nnoStepGPUDirectMarker) {
			return envName
		}
		return envName + "-" + nnoStepGPUDirectMarker
	case isStandardRDMA:
		if strings.HasSuffix(envName, nnoStepRDMASuffix) && !strings.Contains(envName, nnoStepGPUDirectMarker) {
			return envName
		}
		return envName + nnoStepRDMASuffix
	default:
		if strings.HasSuffix(envName, nnoStepRDMASuffix) || strings.Contains(envName, nnoStepGPUDirectMarker) {
			return derived
		}
		return envName
	}
}

func nnoDerivedStepName(isGPUDirect, isStandardRDMA, precompiled bool) string {
	switch {
	case isGPUDirect:
		return nnoStepGPUDirect
	case isStandardRDMA:
		return nnoStepSharedDeviceRDMA
	case precompiled:
		return nnoStepPrecompiled
	default:
		return nnoStepSharedDevice
	}
}

func TestNNOArtifactStepName(t *testing.T) {
	const sharedOverride = "custom-step"
	tests := []struct {
		name         string
		envName      string
		gpuDirect    bool
		standardRDMA bool
		precompiled  bool
		want         string
	}{
		{name: "deploy default", want: nnoStepSharedDevice},
		{name: "standard rdma default", standardRDMA: true, want: nnoStepSharedDeviceRDMA},
		{name: "gpudirect default", gpuDirect: true, want: nnoStepGPUDirect},
		{name: "precompiled default", precompiled: true, want: nnoStepPrecompiled},
		{name: "override names deploy", envName: nnoStepSharedDevice, want: nnoStepSharedDevice},
		{name: "override names gpudirect", envName: nnoStepGPUDirect, gpuDirect: true, want: nnoStepGPUDirect},
		{name: "shared override suffixes gpudirect", envName: sharedOverride, gpuDirect: true, want: sharedOverride + "-" + nnoStepGPUDirectMarker},
		{name: "shared override suffixes standard rdma", envName: sharedOverride, standardRDMA: true, want: sharedOverride + nnoStepRDMASuffix},
		{name: "deploy ignores rdma override", envName: nnoStepSharedDeviceRDMA, want: nnoStepSharedDevice},
		{name: "standard rdma keeps rdma override", envName: nnoStepSharedDeviceRDMA, standardRDMA: true, want: nnoStepSharedDeviceRDMA},
		{name: "deploy ignores gpudirect override", envName: nnoStepGPUDirect, want: nnoStepSharedDevice},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nnoArtifactStepName(test.envName, test.gpuDirect, test.standardRDMA, test.precompiled)
			if got != test.want {
				t.Fatalf("nnoArtifactStepName() = %q, want %q", got, test.want)
			}
		})
	}
}

func writeNNOTestStepResult() error {
	artifactDir := strings.TrimSpace(os.Getenv("ARTIFACT_DIR"))
	if artifactDir == "" {
		return nil
	}

	report := CurrentSpecReport()
	hasDeployLabel := false
	hasSharedDeviceLabel := false
	for _, label := range report.LeafNodeLabels {
		switch label {
		case "deploy":
			hasDeployLabel = true
		case "rdma-shared-dev":
			hasSharedDeviceLabel = true
		}
	}
	isGPUDirect := hasSharedDeviceLabel && rdmaGPUDirect
	isStandardRDMA := hasSharedDeviceLabel && !rdmaGPUDirect
	if !hasDeployLabel && !hasSharedDeviceLabel {
		return nil
	}

	precompiled := nvidiaNetworkConfig != nil && nvidiaNetworkConfig.UsePrecompiledOFED
	step := nnoArtifactStepName(os.Getenv("NNO_STEP_NAME"), isGPUDirect, isStandardRDMA, precompiled)

	specStatus := report.State.String()
	status := nnoStepStatus(specStatus)
	checks := make(map[string]string, len(nnoStepChecks)+3)
	for name, checkStatus := range nnoStepChecks {
		if checkStatus == nnoReportStatusRunning {
			checkStatus = status
		}
		checks[name] = checkStatus
	}
	if isGPUDirect {
		if rdmaStatus, found := checks[nnoCheckRDMAShared]; found {
			checks["rdma_gpudirect"] = rdmaStatus
			delete(checks, nnoCheckRDMAShared)
		}
	}
	if _, found := checks[nnoCheckOCPVersion]; !found {
		checks[nnoCheckOCPVersion] = nnoReportStatusNotFound
	}
	if _, found := checks[nnoCheckNNOCSV]; !found {
		checks[nnoCheckNNOCSV] = nnoReportStatusNotFound
	}

	ocpVersion := currentOCPVersion
	if strings.TrimSpace(ocpVersion) == "" {
		ocpVersion, _ = inittools.GetOpenShiftVersion()
	}
	nnoCSVVersion := currentNNOCSVVersion
	if strings.TrimSpace(nnoCSVVersion) == "" {
		nnoCSVVersion = observedNNOCSVVersion()
	}
	if strings.TrimSpace(ocpVersion) != "" {
		checks[nnoCheckOCPVersion] = nnoReportStatusPassed
	}
	if strings.TrimSpace(nnoCSVVersion) != "" {
		checks[nnoCheckNNOCSV] = nnoReportStatusPassed
	}

	ofedDriver := observedOFEDDriver()
	if ofedDriver == nil {
		checks["configured_ofed_driver"] = nnoReportStatusNotFound
		checks["driver_image_id"] = nnoReportStatusNotFound
	} else {
		checks["configured_ofed_driver"] = nnoDriverCheckFound
		if ofedDriver.ImageID == "" {
			checks["driver_image_id"] = nnoReportStatusNotFound
		} else {
			checks["driver_image_id"] = nnoDriverCheckFound
		}
	}

	var selection *nnopairreport.PrecompiledSelection
	if precompiled {
		outcome := precompiledSelectionOutcome
		if outcome == "" {
			outcome = nnoReportStatusNotRun
		}
		selection = &nnopairreport.PrecompiledSelection{
			Outcome:       outcome,
			KernelVersion: strings.TrimSpace(precompiledSelectionKernel),
			Architecture:  strings.TrimSpace(precompiledSelectionArchitecture),
			Version:       strings.TrimSpace(precompiledSelectionVersion),
			PullSpec:      strings.TrimSpace(precompiledSelectionPullSpec),
			Error:         strings.TrimSpace(precompiledSelectionError),
		}
	}

	skipReason := strings.TrimSpace(precompiledOFEDSkipReason)
	if skipReason == "" && status == nnoReportStatusSkipped {
		skipReason = strings.TrimSpace(report.Failure.Message)
	}
	buildID := strings.TrimSpace(os.Getenv("BUILD_ID"))
	attempt := strings.TrimSpace(os.Getenv("NNO_STEP_ATTEMPT"))
	if attempt == "" {
		attempt = buildID
	}
	metrics := make(map[string]float64, len(nnoStepMetrics))
	for name, value := range nnoStepMetrics {
		if isStandardRDMA {
			name = "standard_" + name
		}
		metrics[name] = value
	}

	return nnopairreport.WriteStepResult(artifactDir, nnopairreport.StepResult{
		JobName:              strings.TrimSpace(os.Getenv("JOB_NAME")),
		BuildID:              buildID,
		Step:                 step,
		Attempt:              strings.TrimSpace(attempt),
		Status:               status,
		OCPVersion:           strings.TrimSpace(ocpVersion),
		NNOCSVVersion:        strings.TrimSpace(nnoCSVVersion),
		ConfiguredOFEDDriver: ofedDriver,
		Checks:               checks,
		Metrics:              metrics,
		SkipReason:           strings.TrimSpace(skipReason),
		PrecompiledSelection: selection,
	})
}

func nnoStepStatus(specStatus string) string {
	switch specStatus {
	case nnoReportStatusPassed:
		return nnoReportStatusPassed
	case nnoReportStatusSkipped, "pending":
		return nnoReportStatusSkipped
	case nnoReportStatusFailed, "panicked", "aborted", "interrupted", "timedout":
		return nnoReportStatusFailed
	default:
		return "blocked"
	}
}

func observedNNOCSVVersion() string {
	csvs, err := olm.ListClusterServiceVersion(inittools.APIClient, nnoNamespace)
	if err != nil {
		return ""
	}
	for _, csv := range csvs {
		if csv != nil && csv.Object != nil && csv.Definition != nil &&
			strings.HasPrefix(csv.Object.Name, "nvidia-network-operator") {
			return csv.Definition.Spec.Version.String()
		}
	}
	return ""
}

func observedOFEDDriver() *nnopairreport.StepOFEDDriver {
	version := nnoStepOFEDVersion
	repository := nnoStepOFEDRepo
	image := nnoStepOFEDImage
	if version == "" && repository == "" && image == "" {
		policy, err := nvidianetwork.PullNicClusterPolicy(inittools.APIClient, nnoNicClusterPolicyName)
		if err == nil && policy != nil && policy.Definition != nil {
			driver := policy.Definition.Spec.OFEDDriver
			version = driver.Version
			repository = driver.Repository
			image = driver.Image
		}
	}

	imageID := nnoStepOFEDImageID
	if imageID == "" {
		imageID = observedOFEDImageID(image, precompiledSelectionPullSpec)
	}
	if version == "" && repository == "" && image == "" && imageID == "" {
		return nil
	}
	return &nnopairreport.StepOFEDDriver{
		Version:    strings.TrimSpace(version),
		Repository: strings.TrimSpace(repository),
		Image:      strings.TrimSpace(image),
		ImageID:    strings.TrimSpace(imageID),
	}
}

func observedOFEDImageID(configuredImage, selectedPullSpec string) string {
	configuredImage = strings.TrimSpace(configuredImage)
	selectedPullSpec = strings.TrimSpace(selectedPullSpec)
	if configuredImage == "" && selectedPullSpec == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pods, err := inittools.APIClient.Pods(nnoNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ""
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Status.ContainerStatuses {
			if selectedPullSpec != "" && container.Image != selectedPullSpec {
				continue
			}
			if selectedPullSpec == "" && configuredImage != "" && !strings.Contains(container.Image, configuredImage) {
				continue
			}
			if strings.TrimSpace(container.ImageID) != "" {
				return container.ImageID
			}
		}
	}
	return ""
}
