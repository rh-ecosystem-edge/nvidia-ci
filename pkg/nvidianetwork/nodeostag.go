package nvidianetwork

import (
	"fmt"
	"strings"
)

const (
	nodeOSReleaseIDLabel        = "feature.node.kubernetes.io/system-os_release.ID"
	nodeOSReleaseVersionIDLabel = "feature.node.kubernetes.io/system-os_release.VERSION_ID"
)

// NodeOSTag returns the NFD os_release ID and VERSION_ID concatenated for use
// in a precompiled DOCA/OFED image tag. Older nodes without both labels use the
// historical rhcos plus OpenShift minor suffix.
func NodeOSTag(labels map[string]string, clusterVersion string) (string, error) {
	osID := strings.TrimSpace(labels[nodeOSReleaseIDLabel])
	versionID := strings.TrimSpace(labels[nodeOSReleaseVersionIDLabel])
	if osID != "" && versionID != "" {
		return osID + versionID, nil
	}

	minor := clusterMinor(clusterVersion)
	if minor == "" {
		return "", fmt.Errorf("NFD os_release.ID and VERSION_ID labels are incomplete and OpenShift version %q has no usable minor for fallback", clusterVersion)
	}

	return "rhcos" + minor, nil
}
