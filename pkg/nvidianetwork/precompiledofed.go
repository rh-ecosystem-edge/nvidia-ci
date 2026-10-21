package nvidianetwork

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	operators "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	precompiledOFEDRegistry   = "registry.stage.redhat.io"
	precompiledOFEDRHEL9Repo  = "nvidia/doca-driver-rhel9"
	precompiledOFEDRHEL10Repo = "nvidia/doca-driver-rhel10"

	kernelVersionLabel = "kernel_version"
	ofedVersionLabel   = "ofed_version"
)

const nnoPackageName = "nvidia-network-operator"

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

var (
	nnoReleaseRegexp    = regexp.MustCompile(`^(?:nvidia-network-operator\.)?v?([0-9]{2})\.(0?[1-9]|1[0-2])(?:\.[0-9]+(?:-[a-zA-Z0-9.-]+)?(?:\+[a-zA-Z0-9.-]+)?)?$`)
	driverVersionRegexp = regexp.MustCompile(`^(?:doca[0-9]+\.[0-9]+\.[0-9]+-)?[0-9]{2}\.(?:0?[1-9]|1[0-2])-(?:OFED\.)?[0-9]+(?:\.[0-9]+)+(?:[-.][a-zA-Z0-9.]+)*$`)
	driverReleaseRegexp = regexp.MustCompile(`^(?:doca[0-9]+\.[0-9]+\.[0-9]+-)?([0-9]{2})\.(0?[1-9]|1[0-2])(?:-.+)?$`)
	docaPrefixRegexp    = regexp.MustCompile(`^doca[0-9]+\.[0-9]+\.[0-9]+-`)
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

// OFEDWorker describes the exact runtime tag required by one RDMA worker.
type OFEDWorker struct {
	KernelVersion string
	OSTag         string
	Architecture  string
}

// PullSpec is repository/image:full-kernel-tag on registry.stage.redhat.io.
func (o *OFEDImage) PullSpec() string {
	if o == nil || o.Tag == "" {
		return ""
	}

	return o.Repository + "/" + o.Image + ":" + o.Tag
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

// SelectPrecompiledOFEDForNNO matches the requested NNO family and every worker before ranking tags.
// Unsupported repositories and an empty compatible set return nil without a lookup error.
func SelectPrecompiledOFEDForNNO(ctx context.Context, client CatalogClient, workers []OFEDWorker, nnoVersion string) (*OFEDImage, error) {
	family, err := NNOReleaseFamily(nnoVersion)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return nil, fmt.Errorf("at least one RDMA worker is required")
	}
	repository, supported := catalogRepositoryForKernel(workers[0].KernelVersion)
	for _, worker := range workers {
		if worker.KernelVersion == "" || strings.TrimSpace(worker.OSTag) == "" || worker.Architecture == "" {
			return nil, fmt.Errorf("worker kernel, OS tag, and architecture are required")
		}
		workerRepository, ok := catalogRepositoryForKernel(worker.KernelVersion)
		if !supported || !ok || workerRepository != repository {
			return nil, nil
		}
	}
	images, err := client.ListImages(ctx, repository)
	if err != nil {
		return nil, err
	}
	available := make(map[string]bool)
	for _, image := range images {
		if image.Repository == repository {
			for _, tag := range image.Tags {
				available[tag] = true
			}
		}
	}
	var best *OFEDImage
	for tag := range available {
		if !strings.HasSuffix(tag, workers[0].tag("")) {
			continue
		}
		candidate := mapCatalogImage(CatalogImage{Repository: repository}, tag, workers[0].KernelVersion)
		if candidate == nil || !driverVersionRegexp.MatchString(candidate.Version) {
			continue
		}
		candidateFamily, err := DriverReleaseFamily(candidate.Version)
		if err != nil || candidateFamily != family {
			continue
		}
		matches := true
		for _, worker := range workers {
			if !available[worker.tag(candidate.Version)] {
				matches = false
				break
			}
		}
		if matches && (best == nil || newerOFED(candidate, best)) {
			best = candidate
		}
	}
	return best, nil
}

func (w OFEDWorker) tag(version string) string {
	return fmt.Sprintf("%s-%s-%s-%s", version, w.KernelVersion, strings.TrimSpace(w.OSTag), w.Architecture)
}

func newerOFED(candidate, current *OFEDImage) bool {
	a, b := candidate.Version, current.Version
	if order := compareDottedVersion(docaPrefixRegexp.ReplaceAllString(a, ""), docaPrefixRegexp.ReplaceAllString(b, "")); order != 0 {
		return order > 0
	}
	aDOCA, bDOCA := docaPrefixRegexp.FindString(a), docaPrefixRegexp.FindString(b)
	if (aDOCA == "") != (bDOCA == "") {
		return aDOCA != ""
	}
	if order := compareDottedVersion(strings.TrimPrefix(aDOCA, "doca"), strings.TrimPrefix(bDOCA, "doca")); order != 0 {
		return order > 0
	}
	return candidate.Tag < current.Tag
}

// NNOReleaseFamily normalizes the year/month of a CSV or channel, including prereleases.
// GA channel discovery belongs to the step script, not driver pairing.
func NNOReleaseFamily(version string) (string, error) {
	return releaseFamily(nnoReleaseRegexp, version)
}

// DriverReleaseFamily normalizes the quarterly release in a DOCA driver version.
func DriverReleaseFamily(version string) (string, error) {
	return releaseFamily(driverReleaseRegexp, version)
}

func releaseFamily(pattern *regexp.Regexp, version string) (string, error) {
	match := pattern.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) != 3 {
		return "", fmt.Errorf("cannot determine release family from %q", version)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	return fmt.Sprintf("%d.%d", major, minor), nil
}

// VerifyOFEDReleaseFamily enforces the CI policy that the driver and installed NNO use the same release family.
func VerifyOFEDReleaseFamily(nnoVersion, driverVersion string) error {
	if !driverVersionRegexp.MatchString(driverVersion) {
		return fmt.Errorf("a complete DOCA/OFED driver version is required, got %q", driverVersion)
	}
	nnoFamily, err := NNOReleaseFamily(nnoVersion)
	if err != nil {
		return err
	}
	driverFamily, err := DriverReleaseFamily(driverVersion)
	if err != nil {
		return err
	}
	if nnoFamily != driverFamily {
		return fmt.Errorf("driver %s belongs to NNO family %s, but installed NNO %s belongs to %s",
			driverVersion, driverFamily, nnoVersion, nnoFamily)
	}
	return nil
}

// FindNNOCSV returns one ready CSV for execution, or the newest observed CSV for reporting.
// Reporting retains failed/installing evidence; ambiguous ready installations cannot authorize tests.
func FindNNOCSV(csvs []v1alpha1.ClusterServiceVersion, ready bool) (*v1alpha1.ClusterServiceVersion, error) {
	var selected *v1alpha1.ClusterServiceVersion
	found := false
	for i := range csvs {
		csv := &csvs[i]
		if !strings.HasPrefix(csv.Name, nnoPackageName+".") {
			continue
		}
		found = true
		if ready && csv.Status.Phase != v1alpha1.CSVPhaseSucceeded {
			continue
		}
		if ready && selected != nil {
			return nil, fmt.Errorf("multiple ready NVIDIA Network Operator CSVs")
		}
		if selected == nil || csv.Spec.Version.GT(selected.Spec.Version.Version) {
			selected = csv
		}
	}
	if ready && found && selected == nil {
		return nil, fmt.Errorf("NVIDIA Network Operator CSV is not ready")
	}
	return selected, nil
}

// EnsureNNOSubscription creates or reconciles the conventional test subscription without adopting others.
// Only the conventional test subscription may be reconciled. The bool reports creation for cleanup.
func EnsureNNOSubscription(ctx context.Context, client operators.OperatorsV1alpha1Interface,
	desired *v1alpha1.Subscription, reconcileChannel bool) (*v1alpha1.Subscription, bool, error) {
	if desired == nil || desired.Spec == nil {
		return nil, false, fmt.Errorf("NNO subscription specification is required")
	}
	subscriptions, err := client.Subscriptions(desired.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("list NNO subscriptions: %w", err)
	}
	var existing *v1alpha1.Subscription
	for i := range subscriptions.Items {
		candidate := &subscriptions.Items[i]
		if candidate.Spec != nil && candidate.Spec.Package == desired.Spec.Package {
			if candidate.Name != desired.Name {
				return nil, false, fmt.Errorf("NNO package already belongs to subscription %s; refusing to adopt it", candidate.Name)
			}
			existing = candidate
		}
	}
	if existing == nil {
		created, err := client.Subscriptions(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return created, err == nil, err
	}
	if !reconcileChannel || (existing.Spec.Channel == desired.Spec.Channel &&
		existing.Spec.CatalogSource == desired.Spec.CatalogSource &&
		existing.Spec.CatalogSourceNamespace == desired.Spec.CatalogSourceNamespace &&
		existing.Spec.InstallPlanApproval == desired.Spec.InstallPlanApproval) {
		return existing, false, nil
	}
	updated := existing.DeepCopy()
	updated.Spec.Channel = desired.Spec.Channel
	updated.Spec.CatalogSource = desired.Spec.CatalogSource
	updated.Spec.CatalogSourceNamespace = desired.Spec.CatalogSourceNamespace
	updated.Spec.InstallPlanApproval = desired.Spec.InstallPlanApproval
	result, err := client.Subscriptions(desired.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	return result, false, err
}
