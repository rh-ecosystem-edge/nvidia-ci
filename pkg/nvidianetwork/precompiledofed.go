package nvidianetwork

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
)

const (
	precompiledOFEDRegistry   = "registry.stage.redhat.io"
	precompiledOFEDRHEL9Repo  = "nvidia/doca-driver-rhel9"
	precompiledOFEDRHEL10Repo = "nvidia/doca-driver-rhel10"

	kernelVersionLabel = "kernel_version"
	ofedVersionLabel   = "ofed_version"
)

var (
	elMajorRegexp = regexp.MustCompile(`\.el(\d+)`)

	// Longer arch suffixes must be stripped first so aarch64_64k is not
	// reduced to aarch64.
	kernelArchSuffixes = []string{
		".aarch64_64k",
		".aarch64",
		".x86_64",
		".ppc64le",
		".s390x",
	}
)

// CatalogClient lists container images from a catalog repository.
type CatalogClient interface {
	// ListImages returns catalog images for repository
	// (for example nvidia/doca-driver-rhel9).
	ListImages(ctx context.Context, repository string) ([]CatalogImage, error)
}

// CatalogImage is one catalog record with tags and labels.
type CatalogImage struct {
	Registry   string
	Repository string
	Tags       []string
	Labels     map[string]string
}

// OFEDImage is the NicClusterPolicy ofedDriver repository, image, and version,
// plus the exact staging registry tag that matched.
type OFEDImage struct {
	Repository string
	Image      string
	Version    string
	Tag        string
}

// PullSpec is repository/image:full-kernel-tag on registry.stage.redhat.io.
func (o *OFEDImage) PullSpec() string {
	if o == nil || o.Tag == "" {
		return ""
	}

	return o.Repository + "/" + o.Image + ":" + o.Tag
}

// SelectPrecompiledOFED finds a staging-registry DOCA/OFED driver image whose
// tag matches kernelVersion, the worker's NFD os_release-based OS tag, and
// architecture. If ofedVersion or ofedRepository is already set, it returns
// (nil, nil) and does not query the registry. No strict tag match also returns
// (nil, nil). Registry fetch errors and an unusable OS tag or architecture are
// returned to the caller.
func SelectPrecompiledOFED(
	ctx context.Context,
	client CatalogClient,
	kernelVersion, osTag, architecture, ofedVersion, ofedRepository string,
) (*OFEDImage, error) {
	if ofedVersion != "" || ofedRepository != "" {
		glog.V(100).Infof("Skipping precompiled OFED catalog lookup because "+
			"ofedDriver version %q or repository %q is already set", ofedVersion, ofedRepository)

		return nil, nil
	}

	repo, ok := catalogRepositoryForKernel(kernelVersion)
	if !ok {
		glog.V(100).Infof("No precompiled DOCA/OFED catalog repository for kernel %s", kernelVersion)

		return nil, nil
	}

	osTag = strings.TrimSpace(osTag)
	if osTag == "" {
		return nil, fmt.Errorf("empty node OS tag for precompiled OFED tag matching")
	}
	if architecture == "" {
		return nil, fmt.Errorf("empty node architecture for precompiled OFED tag matching")
	}

	images, err := client.ListImages(ctx, repo)
	if err != nil {
		return nil, err
	}

	kernelLabel := kernelVersionWithoutArch(kernelVersion)
	var best *CatalogImage
	bestOfed := ""
	bestTag := ""

	for i := range images {
		img := &images[i]
		if !imageMatchesKernel(img, kernelVersion, kernelLabel) {
			continue
		}

		tag := chooseDriverTag(img.Tags, kernelVersion, osTag, architecture)
		if tag == "" {
			glog.V(100).Infof("Skipping precompiled OFED image without %s-%s tag for kernel %s",
				osTag, architecture, kernelVersion)

			continue
		}

		ofed := img.Labels[ofedVersionLabel]
		if best == nil || compareDottedVersion(ofed, bestOfed) > 0 {
			if best != nil {
				glog.V(100).Infof("Skipping precompiled OFED stream %s in favor of %s for kernel %s",
					bestOfed, ofed, kernelVersion)
			}

			best = img
			bestOfed = ofed
			bestTag = tag

			continue
		}

		glog.V(100).Infof("Skipping precompiled OFED stream %s in favor of %s for kernel %s",
			ofed, bestOfed, kernelVersion)
	}

	if best == nil {
		logNoPrecompiledOFED(kernelVersion)

		return nil, nil
	}

	selected := mapCatalogImage(*best, bestTag, kernelVersion)
	if selected == nil {
		logNoPrecompiledOFED(kernelVersion)

		return nil, nil
	}

	glog.V(100).Infof("Selected precompiled DOCA/OFED image %s for kernel %s",
		selected.PullSpec(), kernelVersion)

	return selected, nil
}

func logNoPrecompiledOFED(kernelVersion string) {
	glog.V(100).Infof("No precompiled DOCA/OFED image found for kernel %s", kernelVersion)
}

func catalogRepositoryForKernel(kernelVersion string) (string, bool) {
	match := elMajorRegexp.FindStringSubmatch(kernelVersion)
	if len(match) != 2 {
		return "", false
	}

	switch match[1] {
	case "9":
		return precompiledOFEDRHEL9Repo, true
	case "10":
		return precompiledOFEDRHEL10Repo, true
	default:
		return "", false
	}
}

func kernelVersionWithoutArch(kernelVersion string) string {
	for _, suffix := range kernelArchSuffixes {
		if strings.HasSuffix(kernelVersion, suffix) {
			return strings.TrimSuffix(kernelVersion, suffix)
		}
	}

	return kernelVersion
}

func imageMatchesKernel(img *CatalogImage, kernelVersion, kernelLabel string) bool {
	if img.Labels[kernelVersionLabel] != kernelLabel {
		return false
	}

	needle := "-" + kernelVersion + "-"
	for _, tag := range img.Tags {
		if strings.Contains(tag, needle) {
			return true
		}
	}

	return false
}

func chooseDriverTag(tags []string, kernelVersion, osTag, architecture string) string {
	needle := "-" + kernelVersion + "-"
	suffix := "-" + osTag + "-" + architecture

	for _, tag := range tags {
		if strings.Contains(tag, needle) && strings.HasSuffix(tag, suffix) {
			return tag
		}
	}

	return ""
}

func clusterMinor(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return ""
	}

	minor := strings.Split(parts[1], "-")[0]

	return parts[0] + "." + minor
}

func mapCatalogImage(img CatalogImage, tag, kernelVersion string) *OFEDImage {
	ns, name, ok := strings.Cut(img.Repository, "/")
	if !ok || ns == "" || name == "" {
		return nil
	}

	version := ofedVersionFromTag(tag, kernelVersion)
	if version == "" {
		return nil
	}

	return &OFEDImage{
		Repository: precompiledOFEDRegistry + "/" + ns,
		Image:      name,
		Version:    version,
		Tag:        tag,
	}
}

func ofedVersionFromTag(tag, kernelVersion string) string {
	needle := "-" + kernelVersion + "-"
	idx := strings.Index(tag, needle)
	if idx <= 0 {
		return ""
	}

	return tag[:idx]
}

func compareDottedVersion(a, b string) int {
	aParts := versionNumericParts(a)
	bParts := versionNumericParts(b)
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}

	for i := 0; i < n; i++ {
		ai := numericPart(aParts, i)
		bi := numericPart(bParts, i)
		if ai != bi {
			return ai - bi
		}
	}

	return 0
}

func versionNumericParts(version string) []int {
	raw := strings.FieldsFunc(version, func(r rune) bool {
		return r == '.' || r == '-'
	})
	parts := make([]int, 0, len(raw))

	for _, p := range raw {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}

		parts = append(parts, n)
	}

	return parts
}

func numericPart(parts []int, i int) int {
	if i >= len(parts) {
		return 0
	}

	return parts[i]
}
