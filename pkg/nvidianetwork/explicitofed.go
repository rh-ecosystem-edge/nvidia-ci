package nvidianetwork

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ParseExplicitPrecompiledOFEDPullSpec maps a complete staging image reference
// to NicClusterPolicy fields and confirms that its constructed tag matches the
// running worker kernel, OCP RHCOS minor, and architecture. The operator builds
// its final image tag from ImageSpec.Version plus these runtime suffixes.
func ParseExplicitPrecompiledOFEDPullSpec(
	pullSpec, kernelVersion, clusterVersion, architecture string,
) (*OFEDImage, error) {
	pullSpec = strings.TrimSpace(pullSpec)
	if kernelVersion == "" || architecture == "" {
		return nil, fmt.Errorf("worker kernel and architecture are required")
	}
	minor := clusterMinor(clusterVersion)
	if minor == "" {
		return nil, fmt.Errorf("invalid OpenShift version %q", clusterVersion)
	}
	path, tag, found := strings.Cut(pullSpec, ":")
	repository := strings.TrimPrefix(path, precompiledOFEDRegistry+"/")
	if !strings.HasPrefix(path, precompiledOFEDRegistry+"/") || !found || tag == "" || strings.ContainsAny(tag, ":@/") ||
		(repository != precompiledOFEDRHEL9Repo && repository != precompiledOFEDRHEL10Repo) {
		return nil, fmt.Errorf("unsupported tagged staging pull specification %q", pullSpec)
	}
	suffix := fmt.Sprintf("-%s%s%s-%s", kernelVersion, rhcosTagPrefix, minor, architecture)
	if !strings.HasSuffix(tag, suffix) {
		return nil, fmt.Errorf("requested image tag %q must end with worker kernel/OCP/architecture suffix %q", tag, suffix)
	}
	// Reuse the catalog path's repository/image/base-version mapping.
	selected := mapCatalogImage(CatalogImage{Repository: repository}, tag, kernelVersion)
	if selected != nil {
		selected.Version = strings.TrimSuffix(tag, suffix)
	}
	if selected == nil || strings.HasSuffix(selected.Version, "-") {
		return nil, fmt.Errorf("requested image tag %q has no valid OFED base version", tag)
	}
	return selected, nil
}

// CheckPrecompiledOFEDTag confirms the requested tag with the existing registry
// client. It never selects a replacement image or compares manifest digests.
func CheckPrecompiledOFEDTag(ctx context.Context, client CatalogClient, selected *OFEDImage) error {
	if selected == nil {
		return fmt.Errorf("explicit precompiled OFED image is required")
	}
	images, err := client.ListImages(ctx, "nvidia/"+selected.Image)
	if err != nil {
		return err
	}
	for _, image := range images {
		for _, tag := range image.Tags {
			if tag == selected.Tag {
				return nil
			}
		}
	}
	return fmt.Errorf("requested staging image %q is unavailable", selected.PullSpec())
}

// VerifyOFEDDriverPods requires a ready container using the exact requested image
// on each RDMA worker, and returns their runtime image IDs for evidence.
func VerifyOFEDDriverPods(pods []corev1.Pod, requestedImage string, workers []string) (map[string]string, error) {
	if len(workers) != 2 || workers[0] == "" || workers[1] == "" || workers[0] == workers[1] {
		return nil, fmt.Errorf("two distinct RDMA worker names are required")
	}
	imageIDs := make(map[string]string, len(workers))
	for _, worker := range workers {
		for _, pod := range pods {
			if pod.Spec.NodeName != worker || pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
				continue
			}
			for _, container := range pod.Status.ContainerStatuses {
				if container.Image == requestedImage && container.Ready && container.State.Running != nil &&
					strings.TrimSpace(container.ImageID) != "" {
					imageIDs[worker] = container.ImageID
				}
			}
		}
		if imageIDs[worker] == "" {
			return nil, fmt.Errorf("no ready, running OFED driver uses %q on RDMA worker %s", requestedImage, worker)
		}
	}
	return imageIDs, nil
}
