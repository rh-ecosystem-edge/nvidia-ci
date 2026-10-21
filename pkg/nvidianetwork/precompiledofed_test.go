package nvidianetwork

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	operators "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	testKernelX86         = "5.14.0-570.76.1.el9_6.x86_64"
	testKernelArm         = "5.14.0-570.76.1.el9_6.aarch64"
	testKernelLabel       = "5.14.0-570.76.1.el9_6"
	testArchAMD64         = "amd64"
	testArchARM64         = "arm64"
	testCluster422        = "4.22.0"
	testOSTagRHCOS422     = "rhcos4.22"
	testOSTagRHEL98       = "rhel9.8"
	testKernelRHEL98      = "5.14.0-687.48.1.el9_8.x86_64"
	testKernelLabelRHEL98 = "5.14.0-687.48.1.el9_8"
	testOfed2510          = "25.10-OFED.25.10.2.4.1"
	testOfed2601          = "26.01-OFED.26.01.1.0.0.0"
	testOfed2604          = "26.04-0.9.0.0"
	testOfed2607          = "26.07-0.7.7.0"
	testTag2601Rhcos      = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	testTag2604Rhcos      = "26.04-0.9.0.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	testTag2607Rhcos      = "26.07-0.7.7.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	testTag2601Rhel       = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.x86_64-rhel9.6-amd64"
	testTagArm            = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.aarch64-rhcos4.22-arm64"
	testTagArm64k         = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.aarch64_64k-rhcos4.22-arm64"
)

const testNNONamespace = "nvidia-network-operator"

type fakeCatalog struct {
	byRepo map[string][]CatalogImage
	calls  []string
}

type errCatalog struct {
	err error
}

// Embed unused client methods so tests exercise the production typed-reader API
// without introducing new vendored fake-client dependencies.
type subscriptionClient struct {
	operators.OperatorsV1alpha1Interface
	subscriptions []v1alpha1.Subscription
	listError     error
	creates       int
	updates       int
}

type subscriptionStore struct {
	operators.SubscriptionInterface
	client *subscriptionClient
}

func (f *fakeCatalog) ListImages(_ context.Context, repository string) ([]CatalogImage, error) {
	f.calls = append(f.calls, repository)

	return f.byRepo[repository], nil
}

func (e errCatalog) ListImages(context.Context, string) ([]CatalogImage, error) {
	return nil, e.err
}

func TestSelectPrecompiledOFEDMatch(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{
		byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: {
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2601,
					},
					Tags: []string{
						"1782923949",
						testTag2601Rhel,
						"26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.20-amd64",
						testTag2601Rhcos,
					},
				},
			},
		},
	}

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}, "26.1.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a matching precompiled OFED image")
	}
	if got.Repository != "registry.stage.redhat.io/nvidia" {
		t.Errorf("repository = %q, want registry.stage.redhat.io/nvidia", got.Repository)
	}
	if got.Image != "doca-driver-rhel9" {
		t.Errorf("image = %q, want doca-driver-rhel9", got.Image)
	}
	if got.Version != testOfed2601 {
		t.Errorf("version = %q, want 26.01-OFED.26.01.1.0.0.0", got.Version)
	}
	if got.Tag != testTag2601Rhcos {
		t.Errorf("tag = %q, want %q", got.Tag, testTag2601Rhcos)
	}
	wantSpec := "registry.stage.redhat.io/nvidia/doca-driver-rhel9:" + testTag2601Rhcos
	if got.PullSpec() != wantSpec {
		t.Errorf("PullSpec() = %q, want %q", got.PullSpec(), wantSpec)
	}
	if len(catalog.calls) != 1 || catalog.calls[0] != precompiledOFEDRHEL9Repo {
		t.Errorf("catalog calls = %v, want [%s]", catalog.calls, precompiledOFEDRHEL9Repo)
	}
}

func TestSelectPrecompiledOFEDUsesRHEL98TagFromNodeLabels(t *testing.T) {
	t.Parallel()

	const rhelTag = "26.07-0.7.7.0-" + testKernelRHEL98 + "-" + testOSTagRHEL98 + "-amd64"
	const rhcosTag = "26.07-0.7.7.0-5.14.0-687.48.1.el9_8.x86_64-rhcos4.22-amd64"
	osTag, err := NodeOSTag(map[string]string{
		nodeOSReleaseIDLabel:        "rhel",
		nodeOSReleaseVersionIDLabel: "9.8",
	}, testCluster422)
	if err != nil {
		t.Fatalf("NodeOSTag() error = %v", err)
	}

	catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{
		precompiledOFEDRHEL9Repo: catalogImagesFromTags(precompiledOFEDRHEL9Repo, []string{rhcosTag, rhelTag}),
	}}
	selected, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelRHEL98, OSTag: osTag, Architecture: testArchAMD64}}, "26.7.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFEDForNNO() error = %v", err)
	}
	if selected == nil || selected.Tag != rhelTag {
		t.Fatalf("selected tag = %v, want %q", selected, rhelTag)
	}
}

func TestSelectPrecompiledOFEDUsesFallbackOSTag(t *testing.T) {
	t.Parallel()

	const tag = "26.07-0.7.7.0-" + testKernelRHEL98 + "-rhcos4.22-amd64"
	osTag, err := NodeOSTag(nil, testCluster422)
	if err != nil {
		t.Fatalf("NodeOSTag() error = %v", err)
	}
	catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{
		precompiledOFEDRHEL9Repo: {{
			Repository: precompiledOFEDRHEL9Repo,
			Labels: map[string]string{
				kernelVersionLabel: testKernelLabelRHEL98,
				ofedVersionLabel:   testOfed2607,
			},
			Tags: []string{tag},
		}},
	}}
	selected, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelRHEL98, OSTag: osTag, Architecture: testArchAMD64}}, "26.7.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFEDForNNO() error = %v", err)
	}
	if selected == nil || selected.Tag != tag {
		t.Fatalf("selected tag = %v, want %q", selected, tag)
	}
}

func TestSelectPrecompiledOFEDNoMatch(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{
		byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: {
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: "5.14.0-570.76.1.el9_6",
						ofedVersionLabel:   testOfed2601,
					},
					Tags: []string{
						"26.01-OFED.26.01.1.0.0.0-6.12.0-55.el10.x86_64-rhcos4.22-amd64",
					},
				},
			},
		},
	}

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}, "26.1.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no match, got %+v", got)
	}
}

func TestSelectPrecompiledOFEDIgnoresFallbackTags(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{
		byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: {
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2601,
					},
					Tags: []string{
						testTag2601Rhel,
						"26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.20-amd64",
					},
				},
			},
		},
	}

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}, "26.1.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no match without the requested OS tag and architecture, got %+v", got)
	}
}

func TestSelectPrecompiledOFEDAarch64DoesNotMatch64k(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{
		byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: {
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2601,
					},
					Tags: []string{testTagArm64k},
				},
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2601,
					},
					Tags: []string{testTagArm},
				},
			},
		},
	}

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelArm, OSTag: testOSTagRHCOS422, Architecture: testArchARM64}}, "26.1.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected aarch64 image, not an empty result")
	}
	if got.Version != testOfed2601 {
		t.Errorf("version = %q, want 26.01-OFED.26.01.1.0.0.0", got.Version)
	}
}

func TestSelectPrecompiledOFEDPrefersMatchingOlderStream(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{
		byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: {
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2607,
					},
					Tags: []string{
						"26.07-0.7.7.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.20-amd64",
					},
				},
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2604,
					},
					Tags: []string{testTag2604Rhcos},
				},
			},
		},
	}

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}, "26.4.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected the older stream that has the requested OS tag and architecture")
	}
	if got.Version != testOfed2604 {
		t.Errorf("version = %q, want %q", got.Version, testOfed2604)
	}
}

func TestParsePrecompiledTag(t *testing.T) {
	t.Parallel()

	ofed, kernel, ok := parsePrecompiledTag(
		"26.07-0.7.7.0-5.14.0-687.41.1.el9_8.x86_64-rhcos4.22-amd64")
	if !ok {
		t.Fatal("expected to parse staging registry tag")
	}
	if ofed != "26.07-0.7.7.0" {
		t.Errorf("ofed = %q, want 26.07-0.7.7.0", ofed)
	}
	if kernel != "5.14.0-687.41.1.el9_8.x86_64" {
		t.Errorf("kernel = %q, want 5.14.0-687.41.1.el9_8.x86_64", kernel)
	}

	ofed, kernel, ok = parsePrecompiledTag(testTag2601Rhcos)
	if !ok || ofed != testOfed2601 || kernel != testKernelX86 {
		t.Errorf("parsePrecompiledTag(%q) = %q, %q, %v", testTag2601Rhcos, ofed, kernel, ok)
	}

	ofed, kernel, ok = parsePrecompiledTag(testTag2601Rhel)
	if !ok || ofed != testOfed2601 || kernel != testKernelX86 {
		t.Errorf("parsePrecompiledTag(%q) = %q, %q, %v", testTag2601Rhel, ofed, kernel, ok)
	}
}

func TestCatalogImagesFromTagsSelectsStrictMatch(t *testing.T) {
	t.Parallel()

	images := catalogImagesFromTags(precompiledOFEDRHEL9Repo, []string{
		"1782923949",
		testTag2601Rhel,
		testTag2607Rhcos,
	})
	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), &fakeCatalog{byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: images,
		}}, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}, "26.7.0")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got == nil || got.Version != testOfed2607 {
		t.Fatalf("got %+v, want version %s", got, testOfed2607)
	}
}

func TestOfedVersionFromTag(t *testing.T) {
	t.Parallel()

	got := ofedVersionFromTag(
		"26.07-0.7.7.0-5.14.0-687.41.1.el9_8.x86_64-rhcos4.22-amd64",
		"5.14.0-687.41.1.el9_8.x86_64")
	if got != "26.07-0.7.7.0" {
		t.Errorf("ofedVersionFromTag = %q, want 26.07-0.7.7.0", got)
	}
}

func TestCompareDottedVersionYYMMDash(t *testing.T) {
	t.Parallel()

	if compareDottedVersion(testOfed2607, testOfed2604) <= 0 {
		t.Errorf("compareDottedVersion(%q, %q) = %d, want > 0",
			testOfed2607, testOfed2604, compareDottedVersion(testOfed2607, testOfed2604))
	}
	if compareDottedVersion(testOfed2601, testOfed2510) <= 0 {
		t.Errorf("compareDottedVersion(%q, %q) = %d, want > 0",
			testOfed2601, testOfed2510, compareDottedVersion(testOfed2601, testOfed2510))
	}
}

func TestSelectPrecompiledOFEDCatalogError(t *testing.T) {
	t.Parallel()

	catalog := errCatalog{err: errors.New("catalog unavailable")}

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), catalog, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}, "26.1.0")
	if err == nil {
		t.Fatal("expected catalog error")
	}
	if got != nil {
		t.Fatalf("expected nil image on catalog error, got %+v", got)
	}
}

func TestSelectPrecompiledOFEDInvalidOSTag(t *testing.T) {
	t.Parallel()

	got, err := SelectPrecompiledOFEDForNNO(
		context.Background(), &fakeCatalog{}, []OFEDWorker{{KernelVersion: testKernelX86, OSTag: "", Architecture: testArchAMD64}}, "26.1.0")
	if err == nil {
		t.Fatal("expected error for empty node OS tag")
	}
	if got != nil {
		t.Fatalf("expected nil image, got %+v", got)
	}
}

func TestSelectPrecompiledOFEDForNNO(t *testing.T) {
	t.Parallel()
	otherKernel := "5.14.0-570.80.1.el9_6.x86_64"
	worker := OFEDWorker{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}
	otherWorker := worker
	otherWorker.KernelVersion = otherKernel
	tag := func(version, kernel string) string {
		return fmt.Sprintf("%s-%s-%s-%s", version, kernel, worker.OSTag, worker.Architecture)
	}
	for _, test := range []struct {
		name, nno, want string
		tags            []string
		workers         []OFEDWorker
	}{
		{"ignore newer wrong family", "26.4.0-1", testOfed2604,
			[]string{testTag2607Rhcos, testTag2604Rhcos}, []OFEDWorker{worker, worker}},
		{"ignore older wrong family", "26.7.0-1", testOfed2607,
			[]string{testTag2604Rhcos, testTag2607Rhcos}, []OFEDWorker{worker, worker}},
		{"beta family is eligible", "26.7.0-beta.2", testOfed2607,
			[]string{testTag2607Rhcos}, []OFEDWorker{worker, worker}},
		{"rc family is eligible", "26.7.0-rc.1", testOfed2607,
			[]string{testTag2607Rhcos}, []OFEDWorker{worker, worker}},
		{"off-quarter family is eligible", "26.5.0", "26.05-1.0.0.0",
			[]string{tag("26.05-1.0.0.0", testKernelX86)}, []OFEDWorker{worker, worker}},
		{"wrong family only skips", "26.7.0", "",
			[]string{testTag2604Rhcos}, []OFEDWorker{worker, worker}},
		{"missing second worker skips", "26.7.0", "",
			[]string{testTag2607Rhcos}, []OFEDWorker{worker, otherWorker}},
		{"same version on both kernels", "26.7.0", testOfed2607,
			[]string{testTag2607Rhcos, tag(testOfed2607, otherKernel)}, []OFEDWorker{worker, otherWorker}},
		{"rank only versions available on both workers", "26.7.0", testOfed2607,
			[]string{testTag2607Rhcos, tag(testOfed2607, otherKernel), tag("26.07-9.0.0.0", testKernelX86)},
			[]OFEDWorker{worker, otherWorker}},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{
				precompiledOFEDRHEL9Repo: catalogImagesFromTags(precompiledOFEDRHEL9Repo, test.tags),
			}}
			selected, err := SelectPrecompiledOFEDForNNO(context.Background(), catalog, test.workers, test.nno)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if selected != nil {
					t.Fatalf("expected no compatible pair, got %+v", selected)
				}
			} else if selected == nil || selected.Version != test.want {
				t.Fatalf("selected %+v, want %s", selected, test.want)
			}
		})
	}
}

func TestUnsupportedWorkerRepositoriesSkip(t *testing.T) {
	t.Parallel()
	worker := OFEDWorker{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}
	for _, workers := range [][]OFEDWorker{
		{{KernelVersion: "4.18.0-553.el8.x86_64", OSTag: "rhel8.10", Architecture: testArchAMD64}},
		{worker, {KernelVersion: "6.12.0-55.el10.x86_64", OSTag: "rhel10.0", Architecture: testArchAMD64}},
	} {
		catalog := &fakeCatalog{}
		selected, err := SelectPrecompiledOFEDForNNO(context.Background(), catalog, workers, "26.7.0")
		if err != nil || selected != nil || len(catalog.calls) != 0 {
			t.Fatalf("unsupported workers: selected=%v error=%v calls=%v", selected, err, catalog.calls)
		}
	}
}

func TestPairedRankingNormalizesDocAVersionWithoutChangingMetadata(t *testing.T) {
	t.Parallel()
	worker := OFEDWorker{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}
	legacy := "doca3.3.0-26.07-0.7.7.0"
	older := "26.07-0.7.6.0"
	images := []CatalogImage{}
	for _, value := range []string{legacy, older} {
		images = append(images, CatalogImage{
			Repository: precompiledOFEDRHEL9Repo,
			Tags:       []string{fmt.Sprintf("%s-%s-%s-%s", value, worker.KernelVersion, worker.OSTag, worker.Architecture)},
			Labels:     map[string]string{kernelVersionLabel: testKernelLabel, ofedVersionLabel: value},
		})
	}
	catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{precompiledOFEDRHEL9Repo: images}}
	selected, err := SelectPrecompiledOFEDForNNO(context.Background(), catalog, []OFEDWorker{worker}, "26.7.0")
	if err != nil || selected == nil || selected.Version != legacy {
		t.Fatalf("selected=%v error=%v, want %s", selected, err, legacy)
	}
	if images[0].Labels[ofedVersionLabel] != legacy || images[1].Labels[ofedVersionLabel] != older {
		t.Fatal("selection changed the source catalog metadata")
	}
}

func TestPairedDiscoveryUsesTagsAndIgnoresMalformedCandidates(t *testing.T) {
	t.Parallel()
	worker := OFEDWorker{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}
	images := []CatalogImage{{Repository: precompiledOFEDRHEL9Repo,
		Tags:   []string{"invalid:-" + testKernelX86 + "-" + testOSTagRHCOS422 + "-amd64", testTag2607Rhcos},
		Labels: map[string]string{ofedVersionLabel: "unexpected", kernelVersionLabel: "wrong"}}}
	catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{precompiledOFEDRHEL9Repo: images}}
	got, err := SelectPrecompiledOFEDForNNO(context.Background(), catalog, []OFEDWorker{worker}, "26.7.0")
	if err != nil || got == nil || got.Tag != testTag2607Rhcos {
		t.Fatalf("selected=%v error=%v, want valid tag despite unrelated metadata", got, err)
	}
}

func TestPairedRankingPrefersNewestExplicitDOCAVariant(t *testing.T) {
	t.Parallel()
	worker := OFEDWorker{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}
	versions := []string{testOfed2607, "doca3.2.0-" + testOfed2607, "doca3.3.0-" + testOfed2607}
	for _, order := range [][]int{{0, 1, 2}, {2, 1, 0}, {1, 0, 2}} {
		images := []CatalogImage{}
		for _, i := range order {
			images = append(images, CatalogImage{Repository: precompiledOFEDRHEL9Repo,
				Tags:   []string{versions[i] + "-" + testKernelX86 + "-" + testOSTagRHCOS422 + "-amd64"},
				Labels: map[string]string{kernelVersionLabel: testKernelLabel, ofedVersionLabel: testOfed2607}})
		}
		catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{precompiledOFEDRHEL9Repo: images}}
		got, err := SelectPrecompiledOFEDForNNO(context.Background(), catalog, []OFEDWorker{worker}, "26.7.0")
		if err != nil || got == nil || got.Version != versions[2] {
			t.Fatalf("order=%v selected=%v error=%v, want %s", order, got, err, versions[2])
		}
	}
}

func TestPairedDiscoveryErrorsAreNotSkips(t *testing.T) {
	t.Parallel()
	workers := []OFEDWorker{{KernelVersion: testKernelX86, OSTag: testOSTagRHCOS422, Architecture: testArchAMD64}}
	lookupErr := errors.New("registry authentication failed")
	_, err := SelectPrecompiledOFEDForNNO(context.Background(), errCatalog{err: lookupErr}, workers, "26.7.0")
	if !errors.Is(err, lookupErr) {
		t.Fatalf("lookup error = %v, want %v", err, lookupErr)
	}
	if _, err := SelectPrecompiledOFEDForNNO(context.Background(), &fakeCatalog{}, workers, ""); err == nil {
		t.Fatal("missing installed NNO must fail validation")
	}
	if _, err := SelectPrecompiledOFEDForNNO(context.Background(), &fakeCatalog{}, nil, "26.7.0"); err == nil {
		t.Fatal("missing workers must fail validation")
	}
}

func TestReleaseFamilies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		nno, driver string
		valid       bool
	}{
		{"26.7.0-1", testOfed2607, true},
		{"26.7.0", "26.07", false},
		{"26.7.0", "26.7-0.7.7.0", true},
		{"25.10.0", "25.07", false},
		{"nvidia-network-operator.v26.4.1-0.123.p", testOfed2604, true},
		{"v26.04", "doca3.3.0-26.04-1.1.0.0", true},
		{"26.10.0", "26.10-1.0.0.0", true},
		{"27.1.0", "27.01-1.0.0.0", true},
		{"26.7.0-1", testOfed2604, false},
		{"26.4.0", testOfed2607, false},
		{"26.7.0-beta.1", testOfed2607, true},
		{"26.7.0-rc.1", testOfed2607, true},
		{"26.5.0-beta.2", "26.05-1.0.0.0", true},
		{"26.7.0-beta.1", testOfed2604, false},
		{"", testOfed2607, false},
		{"26.7.0", "invalid", false},
	} {
		t.Run(test.nno+"/"+test.driver, func(t *testing.T) {
			if err := VerifyOFEDReleaseFamily(test.nno, test.driver); (err == nil) != test.valid {
				t.Fatalf("VerifyOFEDReleaseFamily() = %v, valid = %t", err, test.valid)
			}
		})
	}
}

func observedCSV(csvVersion string, phase v1alpha1.ClusterServiceVersionPhase) v1alpha1.ClusterServiceVersion {
	return v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{Name: nnoPackageName + ".v" + csvVersion, Namespace: testNNONamespace},
		Spec: v1alpha1.ClusterServiceVersionSpec{
			Version: version.OperatorVersion{Version: semver.MustParse(csvVersion)},
		},
		Status: v1alpha1.ClusterServiceVersionStatus{Phase: phase},
	}
}

func observedSubscription(name, installed, current string) v1alpha1.Subscription {
	return v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNNONamespace},
		Spec:       &v1alpha1.SubscriptionSpec{Package: nnoPackageName, Channel: "v26.7"},
		Status:     v1alpha1.SubscriptionStatus{InstalledCSV: installed, CurrentCSV: current},
	}
}

func TestFindNNOCSVKeepsFailureEvidenceAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	ready := observedCSV("26.4.0", v1alpha1.CSVPhaseSucceeded)
	failed := observedCSV("26.7.0-beta.2", v1alpha1.CSVPhaseFailed)
	for _, phase := range []v1alpha1.ClusterServiceVersionPhase{v1alpha1.CSVPhaseFailed, v1alpha1.CSVPhaseInstalling} {
		failed.Status.Phase = phase
		csv, err := FindNNOCSV([]v1alpha1.ClusterServiceVersion{ready, failed}, false)
		if err != nil || csv == nil || csv.Name != failed.Name {
			t.Fatalf("lost failed CSV evidence: %v, %v", csv, err)
		}
	}
	if csv, err := FindNNOCSV([]v1alpha1.ClusterServiceVersion{ready, ready}, true); csv != nil || err == nil {
		t.Fatal("ambiguous ready CSVs must not authorize execution")
	}
	if csv, err := FindNNOCSV(nil, true); csv != nil || err != nil {
		t.Fatalf("absent NNO: %v, %v", csv, err)
	}
	if csv, err := FindNNOCSV([]v1alpha1.ClusterServiceVersion{failed}, true); csv != nil || err == nil {
		t.Fatal("unhealthy NNO must not authorize execution")
	}
}

func TestEnsureNNOSubscriptionPreservesOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, channel string
		reconcile     bool
		wantChannel   string
		wantUpdates   int
		wantError     bool
	}{
		{"nno-subscription", "v26.4", true, "v26.7", 1, false},
		{"nno-subscription", "stable", true, "v26.7", 1, false},
		{"rhoai-network", "v26.4", true, "v26.4", 0, true},
		{"rhoai-network", "stable", false, "stable", 0, true},
		{"nno-subscription", "v26.7", true, "v26.7", 0, false},
		{"rhoai-network", "v26.7", true, "v26.7", 0, true},
	} {
		t.Run(test.name+"/"+test.channel+"/"+test.wantChannel, func(t *testing.T) {
			existing := observedSubscription(test.name, "old-csv", "old-csv")
			existing.Spec.Channel = test.channel
			existing.ResourceVersion = "42"
			desired := observedSubscription("nno-subscription", "", "")
			client := &subscriptionClient{subscriptions: []v1alpha1.Subscription{existing}}
			got, created, err := EnsureNNOSubscription(context.Background(), client, &desired, test.reconcile)
			if test.wantError {
				if err == nil || created || client.creates != 0 || client.updates != 0 {
					t.Fatalf("foreign subscription was modified: created=%t error=%v", created, err)
				}
				return
			}
			if err != nil || created || got == nil || got.Name != test.name || got.Spec.Channel != test.wantChannel ||
				got.ResourceVersion != "42" || got.Status.InstalledCSV != "old-csv" ||
				client.creates != 0 || client.updates != test.wantUpdates {
				t.Fatalf("subscription=%+v error=%v creates=%d updates=%d", got, err, client.creates, client.updates)
			}
		})
	}
}

func TestEnsureNNOSubscriptionCreatesOnlyWhenAbsent(t *testing.T) {
	t.Parallel()
	desired := observedSubscription("nno-subscription", "", "")
	unrelated := observedSubscription("other-package", "", "")
	unrelated.Spec.Package = "other-package"
	client := &subscriptionClient{subscriptions: []v1alpha1.Subscription{unrelated}}
	got, created, err := EnsureNNOSubscription(context.Background(), client, &desired, true)
	if err != nil || !created || got == nil || got.Name != desired.Name || client.creates != 1 || client.updates != 0 {
		t.Fatalf("subscription=%+v error=%v creates=%d updates=%d", got, err, client.creates, client.updates)
	}
	client.subscriptions = append(client.subscriptions, observedSubscription("rhoai-network", "", ""))
	if _, _, err := EnsureNNOSubscription(context.Background(), client, &desired, true); err == nil {
		t.Fatal("duplicate package subscriptions must fail rather than create another")
	}
	if client.creates != 1 || client.updates != 0 {
		t.Fatal("ambiguous subscription lookup changed the cluster")
	}
}

func TestEnsureNNOSubscriptionDoesNotTreatLookupErrorsAsAbsence(t *testing.T) {
	t.Parallel()
	resource := schema.GroupResource{Group: "operators.coreos.com", Resource: "subscriptions"}
	desired := observedSubscription("nno-subscription", "", "")
	for _, lookupErr := range []error{
		k8serrors.NewForbidden(resource, desired.Name, errors.New("access denied")),
		k8serrors.NewNotFound(resource, "subscriptions"),
	} {
		client := &subscriptionClient{listError: lookupErr}
		got, created, err := EnsureNNOSubscription(context.Background(), client, &desired, true)
		if got != nil || created || !errors.Is(err, lookupErr) || client.creates != 0 {
			t.Fatalf("subscription=%v created=%t error=%v, want original lookup error", got, created, err)
		}
	}
}

func (c *subscriptionClient) Subscriptions(string) operators.SubscriptionInterface {
	return subscriptionStore{client: c}
}

func (s subscriptionStore) List(context.Context, metav1.ListOptions) (*v1alpha1.SubscriptionList, error) {
	return &v1alpha1.SubscriptionList{Items: s.client.subscriptions}, s.client.listError
}

func (s subscriptionStore) Create(_ context.Context, desired *v1alpha1.Subscription, _ metav1.CreateOptions) (*v1alpha1.Subscription, error) {
	s.client.creates++
	s.client.subscriptions = append(s.client.subscriptions, *desired.DeepCopy())
	return desired.DeepCopy(), nil
}

func (s subscriptionStore) Update(_ context.Context, desired *v1alpha1.Subscription, _ metav1.UpdateOptions) (*v1alpha1.Subscription, error) {
	s.client.updates++
	for i := range s.client.subscriptions {
		if s.client.subscriptions[i].Name == desired.Name {
			s.client.subscriptions[i] = *desired.DeepCopy()
			return desired.DeepCopy(), nil
		}
	}
	return nil, fmt.Errorf("unexpected update of absent subscription %s", desired.Name)
}
