package nvidianetwork

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestParseExplicitPrecompiledOFEDPullSpec(t *testing.T) {
	t.Parallel()

	const (
		kernel       = "5.14.0-570.76.1.el9_6.x86_64"
		architecture = "amd64"
		pullSpec     = "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-" +
			"5.14.0-570.76.1.el9_6.x86_64-rhel9.8-amd64"
	)

	osTag, err := NodeOSTag(map[string]string{
		nodeOSReleaseIDLabel:        "rhel",
		nodeOSReleaseVersionIDLabel: "9.8",
	}, "4.22.15")
	if err != nil {
		t.Fatalf("NodeOSTag() error = %v", err)
	}
	got, err := ParseExplicitPrecompiledOFEDPullSpec(pullSpec, kernel, osTag, architecture)
	if err != nil {
		t.Fatalf("ParseExplicitPrecompiledOFEDPullSpec() error = %v", err)
	}
	if got.Repository != "registry.stage.redhat.io/nvidia" {
		t.Errorf("Repository = %q, want registry.stage.redhat.io/nvidia", got.Repository)
	}
	if got.Image != "doca-driver-rhel9" {
		t.Errorf("Image = %q, want doca-driver-rhel9", got.Image)
	}
	if got.Version != testOfed2607 {
		t.Errorf("Version = %q, want %q", got.Version, testOfed2607)
	}
	if got.PullSpec() != pullSpec {
		t.Errorf("PullSpec() = %q, want %q", got.PullSpec(), pullSpec)
	}
}

func TestParseExplicitPrecompiledOFEDPullSpecRejectsMismatch(t *testing.T) {
	t.Parallel()

	const (
		kernel       = "5.14.0-570.76.1.el9_6.x86_64"
		osTag        = "rhel9.8"
		architecture = "amd64"
		pullSpec     = "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-" +
			"5.14.0-570.76.1.el9_6.x86_64-rhel9.8-amd64"
	)
	tests := []struct {
		name, pullSpec, kernel, osTag, architecture string
	}{
		{name: "empty image", kernel: kernel, osTag: osTag, architecture: architecture},
		{name: "wrong registry", pullSpec: "registry.example.com/nvidia/doca-driver-rhel9:tag", kernel: kernel, osTag: osTag, architecture: architecture},
		{name: "missing registry host", pullSpec: "nvidia/doca-driver-rhel9:" + testTag2607Rhcos, kernel: testKernelX86, osTag: osTag, architecture: architecture},
		{name: "missing registry host rhel10", pullSpec: "nvidia/doca-driver-rhel10:" + testTag2607Rhcos, kernel: testKernelX86, osTag: osTag, architecture: architecture},
		{name: "wrong kernel", pullSpec: pullSpec, kernel: "5.14.0-570.76.2.el9_6.x86_64", osTag: osTag, architecture: architecture},
		{name: "wrong OS tag", pullSpec: pullSpec, kernel: kernel, osTag: "rhel9.7", architecture: architecture},
		{name: "wrong architecture", pullSpec: pullSpec, kernel: kernel, osTag: osTag, architecture: "arm64"},
		{name: "legacy OCP-derived suffix", pullSpec: strings.Replace(pullSpec, "rhel9.8", "rhcos4.22", 1), kernel: kernel, osTag: osTag, architecture: architecture},
		{name: "empty OS tag", pullSpec: pullSpec, kernel: kernel, architecture: architecture},
		{name: "digest-only reference", pullSpec: "registry.stage.redhat.io/nvidia/doca-driver-rhel9@sha256:deadbeef", kernel: kernel, osTag: osTag, architecture: architecture},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseExplicitPrecompiledOFEDPullSpec(
				test.pullSpec, test.kernel, test.osTag, test.architecture,
			)
			if err == nil {
				t.Fatal("ParseExplicitPrecompiledOFEDPullSpec() error = nil, want mismatch error")
			}
			if test.name == "legacy OCP-derived suffix" &&
				!strings.Contains(err.Error(), "-5.14.0-570.76.1.el9_6.x86_64-rhel9.8-amd64") {
				t.Errorf("error = %q, want expected worker kernel/OS/architecture suffix", err)
			}
		})
	}
}

func TestParseExplicitPrecompiledOFEDPullSpecAcceptsLegacyOSTag(t *testing.T) {
	t.Parallel()

	pullSpec := "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-" +
		"5.14.0-687.48.1.el9_8.x86_64-rhcos4.16-amd64"
	osTag, err := NodeOSTag(map[string]string{
		nodeOSReleaseIDLabel:        "rhcos",
		nodeOSReleaseVersionIDLabel: "4.16",
	}, "4.22.15")
	if err != nil {
		t.Fatalf("NodeOSTag() error = %v", err)
	}
	if _, err := ParseExplicitPrecompiledOFEDPullSpec(
		pullSpec, "5.14.0-687.48.1.el9_8.x86_64", osTag, "amd64",
	); err != nil {
		t.Fatalf("ParseExplicitPrecompiledOFEDPullSpec() error = %v", err)
	}
}

func TestParseExplicitPrecompiledOFEDPullSpecUsesFallbackOSTag(t *testing.T) {
	t.Parallel()

	osTag, err := NodeOSTag(nil, "4.22.15")
	if err != nil {
		t.Fatalf("NodeOSTag() error = %v", err)
	}
	pullSpec := "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-" +
		"5.14.0-687.48.1.el9_8.x86_64-rhcos4.22-amd64"
	if _, err := ParseExplicitPrecompiledOFEDPullSpec(
		pullSpec, "5.14.0-687.48.1.el9_8.x86_64", osTag, "amd64",
	); err != nil {
		t.Fatalf("ParseExplicitPrecompiledOFEDPullSpec() error = %v", err)
	}
}

func TestCheckPrecompiledOFEDTag(t *testing.T) {
	selected := &OFEDImage{Repository: precompiledOFEDRegistry + "/nvidia", Image: "doca-driver-rhel9", Tag: testTag2607Rhcos}
	catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{
		precompiledOFEDRHEL9Repo: {{Tags: []string{testTag2601Rhcos, selected.Tag}}},
	}}
	if err := CheckPrecompiledOFEDTag(context.Background(), catalog, selected); err != nil {
		t.Fatal(err)
	}
	selected.Tag = "missing-requested-tag"
	if err := CheckPrecompiledOFEDTag(context.Background(), catalog, selected); err == nil {
		t.Fatal("missing exact tag must fail even if other tags exist")
	}
	if err := CheckPrecompiledOFEDTag(context.Background(), errCatalog{err: errors.New("authentication failed")}, selected); err == nil {
		t.Fatal("registry errors must fail")
	}
}

func TestCheckPrecompiledOFEDWorkerTag(t *testing.T) {
	t.Parallel()

	selected := OFEDImage{
		Repository: precompiledOFEDRegistry + "/nvidia", Image: "doca-driver-rhel9",
		Version: testOfed2607, Tag: testTag2607Rhcos,
	}
	const serverTag = testOfed2607 + "-" + testKernelRHEL98 + "-rhel9.8-amd64"
	const armTag = testOfed2607 + "-" + testKernelArm + "-rhcos4.22-arm64"
	tests := []struct {
		name, kernel, osTag, architecture string
		tags                              []string
		wantError                         bool
	}{
		{name: "same suffix", kernel: testKernelX86, osTag: testOSTagRHCOS422, architecture: testArchAMD64,
			tags: []string{selected.Tag}},
		{name: "different kernel and OS", kernel: testKernelRHEL98, osTag: testOSTagRHEL98, architecture: testArchAMD64,
			tags: []string{selected.Tag, serverTag}},
		{name: "different architecture", kernel: testKernelArm, osTag: testOSTagRHCOS422, architecture: testArchARM64,
			tags: []string{selected.Tag, armTag}},
		{name: "missing second worker tag", kernel: testKernelRHEL98, osTag: testOSTagRHEL98, architecture: testArchAMD64,
			tags: []string{selected.Tag}, wantError: true},
		{name: "only another version exists", kernel: testKernelRHEL98, osTag: testOSTagRHEL98, architecture: testArchAMD64,
			tags: []string{selected.Tag, testOfed2604 + "-" + testKernelRHEL98 + "-rhel9.8-amd64"}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			catalog := &fakeCatalog{byRepo: map[string][]CatalogImage{
				precompiledOFEDRHEL9Repo: {{Tags: test.tags}},
			}}
			got := selected
			err := CheckPrecompiledOFEDWorkerTag(context.Background(), catalog, &got,
				test.kernel, test.osTag, test.architecture)
			if (err != nil) != test.wantError {
				t.Fatalf("CheckPrecompiledOFEDWorkerTag() error = %v, wantError %t", err, test.wantError)
			}
			if test.wantError && !strings.Contains(err.Error(), serverTag) {
				t.Errorf("error = %q, want exact selected version and worker suffix %q", err, serverTag)
			}
			if got != selected {
				t.Errorf("selected image changed: got %+v, want %+v", got, selected)
			}
			if len(catalog.calls) != 1 || catalog.calls[0] != precompiledOFEDRHEL9Repo {
				t.Errorf("catalog calls = %v, want [%s]", catalog.calls, precompiledOFEDRHEL9Repo)
			}
		})
	}
}

func TestCheckPrecompiledOFEDWorkerTagRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	selected := &OFEDImage{
		Repository: precompiledOFEDRegistry + "/nvidia", Image: "doca-driver-rhel9",
		Version: testOfed2607, Tag: testTag2607Rhcos,
	}
	tests := []struct {
		name, kernel, osTag, architecture string
		selected                          *OFEDImage
	}{
		{name: "missing selection", kernel: testKernelX86, osTag: testOSTagRHCOS422, architecture: testArchAMD64},
		{name: "empty kernel", selected: selected, osTag: testOSTagRHCOS422, architecture: testArchAMD64},
		{name: "empty OS tag", selected: selected, kernel: testKernelX86, architecture: testArchAMD64},
		{name: "empty architecture", selected: selected, kernel: testKernelX86, osTag: testOSTagRHCOS422},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			catalog := &fakeCatalog{}
			if err := CheckPrecompiledOFEDWorkerTag(context.Background(), catalog, test.selected,
				test.kernel, test.osTag, test.architecture); err == nil {
				t.Fatal("invalid worker or selection must fail")
			}
			if len(catalog.calls) != 0 {
				t.Errorf("invalid input queried catalog: %v", catalog.calls)
			}
		})
	}
	registryErr := errors.New("authentication failed")
	err := CheckPrecompiledOFEDWorkerTag(context.Background(), errCatalog{err: registryErr}, selected,
		testKernelX86, testOSTagRHCOS422, testArchAMD64)
	if !errors.Is(err, registryErr) {
		t.Errorf("registry error = %v, want %v", err, registryErr)
	}
}

func TestVerifyOFEDDriverPodsRequiresBothRDMAWorkers(t *testing.T) {
	const image = "registry.stage.redhat.io/nvidia/doca-driver-rhel9:" + testTag2607Rhcos
	readyPod := func(worker string) corev1.Pod {
		return corev1.Pod{
			Spec: corev1.PodSpec{NodeName: worker},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
				Image: image, ImageID: "sha256:runtime-digest", Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}},
		}
	}
	workers := []string{"rdma-client", "rdma-server"}
	pods := []corev1.Pod{readyPod(workers[0]), readyPod(workers[1])}
	ids, err := VerifyOFEDDriverPods(pods, image, workers)
	if err != nil || len(ids) != 2 {
		t.Fatalf("both ready workers: ids=%v error=%v", ids, err)
	}
	for _, failure := range []string{"missing-server", "unrelated-worker", "wrong-image", "not-ready", "not-running", "missing-image-id"} {
		t.Run(failure, func(t *testing.T) {
			pods := []corev1.Pod{readyPod(workers[0]), readyPod(workers[1])}
			switch failure {
			case "missing-server":
				pods = pods[:1]
			case "unrelated-worker":
				pods[1].Spec.NodeName = "another-worker"
			case "wrong-image":
				pods[1].Status.ContainerStatuses[0].Image = "another-image"
			case "not-ready":
				pods[1].Status.ContainerStatuses[0].Ready = false
			case "not-running":
				pods[1].Status.ContainerStatuses[0].State.Running = nil
			case "missing-image-id":
				pods[1].Status.ContainerStatuses[0].ImageID = ""
			}
			if _, err := VerifyOFEDDriverPods(pods, image, workers); err == nil {
				t.Fatal("incomplete requested-driver coverage must fail")
			}
		})
	}
	for _, workers := range [][]string{nil, {"rdma-client"}, {"rdma-client", "rdma-client"}, {"", "rdma-server"}} {
		if _, err := VerifyOFEDDriverPods(pods, image, workers); err == nil {
			t.Fatal("missing or duplicate RDMA worker configuration must fail")
		}
	}
}
