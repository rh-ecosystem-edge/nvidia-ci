package nvidianetwork

import (
	"context"
	"errors"
	"testing"
)

const (
	testKernelX86    = "5.14.0-570.76.1.el9_6.x86_64"
	testKernelArm    = "5.14.0-570.76.1.el9_6.aarch64"
	testKernelLabel  = "5.14.0-570.76.1.el9_6"
	testArchAMD64    = "amd64"
	testArchARM64    = "arm64"
	testCluster422   = "4.22.0"
	testOfed2510     = "25.10-OFED.25.10.2.4.1"
	testOfed2601     = "26.01-OFED.26.01.1.0.0.0"
	testOfed2604     = "26.04-0.9.0.0"
	testOfed2607     = "26.07-0.7.7.0"
	testTag2601Rhcos = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	testTag2604Rhcos = "26.04-0.9.0.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	testTag2607Rhcos = "26.07-0.7.7.0-5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	testTag2601Rhel  = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.x86_64-rhel9.6"
	testTagArm       = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.aarch64-rhcos4.22-arm64"
	testTagArm64k    = "26.01-OFED.26.01.1.0.0.0-5.14.0-570.76.1.el9_6.aarch64_64k-rhcos4.22-arm64"
)

type fakeCatalog struct {
	byRepo map[string][]CatalogImage
	calls  []string
}

type errCatalog struct {
	err error
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

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "", "")
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

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "", "")
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

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "", "")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no match without rhcos4.22-amd64 tag, got %+v", got)
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

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelArm, testCluster422, testArchARM64, "", "")
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

func TestSelectPrecompiledOFEDHighestOfedVersion(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{
		byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: {
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2604,
					},
					Tags: []string{testTag2604Rhcos},
				},
				{
					Registry:   precompiledOFEDRegistry,
					Repository: precompiledOFEDRHEL9Repo,
					Labels: map[string]string{
						kernelVersionLabel: testKernelLabel,
						ofedVersionLabel:   testOfed2607,
					},
					Tags: []string{testTag2607Rhcos},
				},
			},
		},
	}

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "", "")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a matching precompiled OFED image")
	}
	if got.Version != testOfed2607 {
		t.Errorf("version = %q, want higher OFED stream %q", got.Version, testOfed2607)
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

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "", "")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected the older stream that has a rhcos4.22-amd64 tag")
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

	if _, _, ok = parsePrecompiledTag(testTag2601Rhel); ok {
		t.Fatal("rhel tags should not parse as precompiled rhcos tags")
	}
}

func TestCatalogImagesFromTagsSelectsStrictMatch(t *testing.T) {
	t.Parallel()

	images := catalogImagesFromTags(precompiledOFEDRHEL9Repo, []string{
		"1782923949",
		testTag2601Rhel,
		testTag2607Rhcos,
	})
	got, err := SelectPrecompiledOFED(
		context.Background(), &fakeCatalog{byRepo: map[string][]CatalogImage{
			precompiledOFEDRHEL9Repo: images,
		}}, testKernelX86, testCluster422, testArchAMD64, "", "")
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

func TestSelectPrecompiledOFEDExplicitEnvSkipsCatalog(t *testing.T) {
	t.Parallel()

	catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{}}

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "25.01-0.6.0.0-0", "")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when version is set, got %+v", got)
	}
	if len(catalog.calls) != 0 {
		t.Fatalf("catalog was queried on explicit version: %v", catalog.calls)
	}

	got, err = SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "",
		"registry.stage.redhat.io/nvidia")
	if err != nil {
		t.Fatalf("SelectPrecompiledOFED returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when repository is set, got %+v", got)
	}
	if len(catalog.calls) != 0 {
		t.Fatalf("catalog was queried on explicit repository: %v", catalog.calls)
	}
}

func TestSelectPrecompiledOFEDCatalogError(t *testing.T) {
	t.Parallel()

	catalog := errCatalog{err: errors.New("catalog unavailable")}

	got, err := SelectPrecompiledOFED(
		context.Background(), catalog, testKernelX86, testCluster422, testArchAMD64, "", "")
	if err == nil {
		t.Fatal("expected catalog error")
	}
	if got != nil {
		t.Fatalf("expected nil image on catalog error, got %+v", got)
	}
}

func TestSelectPrecompiledOFEDInvalidClusterVersion(t *testing.T) {
	t.Parallel()

	got, err := SelectPrecompiledOFED(
		context.Background(), &fakeCatalog{}, testKernelX86, "", testArchAMD64, "", "")
	if err == nil {
		t.Fatal("expected error for empty OpenShift version")
	}
	if got != nil {
		t.Fatalf("expected nil image, got %+v", got)
	}
}
