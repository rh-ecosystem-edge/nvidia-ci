package nvidianetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidianetwork"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// verifyExplicitPrecompiledOFEDDriver proves that the requested NicClusterPolicy
// configuration rendered to ready driver pods on both RDMA workers.
func verifyExplicitPrecompiledOFEDDriver(selected *nvidianetwork.OFEDImage) error {
	if selected == nil {
		return fmt.Errorf("no explicit precompiled OFED image was resolved")
	}

	policy, err := nvidianetwork.PullNicClusterPolicy(inittools.APIClient, nnoNicClusterPolicyName)
	if err != nil {
		return fmt.Errorf("failed to read NicClusterPolicy %s: %w", nnoNicClusterPolicyName, err)
	}
	if policy.Definition.Spec.OFEDDriver == nil {
		return fmt.Errorf("NicClusterPolicy %s has no ofedDriver spec", nnoNicClusterPolicyName)
	}
	driver := policy.Definition.Spec.OFEDDriver
	if driver.Repository != selected.Repository || driver.Image != selected.Image || driver.Version != selected.Version {
		return fmt.Errorf(
			"NicClusterPolicy ofedDriver rendered repository=%q image=%q version=%q, requested repository=%q image=%q version=%q",
			driver.Repository, driver.Image, driver.Version,
			selected.Repository, selected.Image, selected.Version,
		)
	}
	if !driver.ForcePrecompiled {
		return fmt.Errorf("NicClusterPolicy ofedDriver.forcePrecompiled is not true")
	}
	if !slices.Contains(driver.ImagePullSecrets, nvidianetwork.StagingPullSecretName) {
		return fmt.Errorf("NicClusterPolicy ofedDriver.imagePullSecrets does not include %q",
			nvidianetwork.StagingPullSecretName)
	}

	expectedPullSpec := strings.TrimSpace(nvidiaNetworkConfig.OfedDriverPullSpec)
	if selected.PullSpec() != expectedPullSpec {
		return fmt.Errorf("NicClusterPolicy image fields render %q, not the requested pull specification %q",
			selected.PullSpec(), expectedPullSpec)
	}

	var imageIDs map[string]string
	var lastErr error
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for {
		pods, err := inittools.APIClient.Pods(nnoNamespace).List(ctx, metav1.ListOptions{})
		lastErr = err
		if err == nil {
			imageIDs, lastErr = nvidianetwork.VerifyOFEDDriverPods(pods.Items, expectedPullSpec,
				[]string{rdmaClientHostname, rdmaServerHostname})
		}
		if lastErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("requested OFED driver image %q did not become running: %w", expectedPullSpec, lastErr)
		case <-time.After(15 * time.Second):
		}
	}

	if artifactDir := os.Getenv("ARTIFACT_DIR"); artifactDir != "" {
		// Keep both workers' evidence separate from the unchanged report schema.
		evidence, err := json.MarshalIndent(map[string]interface{}{
			"step": os.Getenv("NNO_STEP_NAME"), "attempt": os.Getenv("NNO_STEP_ATTEMPT"),
			"pull_spec": expectedPullSpec, "workers": imageIDs,
		}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(artifactDir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(artifactDir, "nno-signed-driver-images.json"), append(evidence, '\n'), 0644); err != nil {
			return err
		}
	}

	nnoStepOFEDVersion = driver.Version
	nnoStepOFEDRepo = driver.Repository
	nnoStepOFEDImage = driver.Image
	nnoStepOFEDImageID = imageIDs[rdmaClientHostname]
	nnoStepChecks["requested_driver_policy"] = nnoReportStatusPassed
	nnoStepChecks["requested_driver_running"] = nnoReportStatusPassed

	return nil
}
