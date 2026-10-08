package nvidianetwork

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestParseExplicitPrecompiledOFEDPullSpec(t *testing.T) {
	t.Parallel()

	const (
		kernel       = "5.14.0-570.76.1.el9_6.x86_64"
		cluster      = "4.22.0"
		architecture = "amd64"
		pullSpec     = "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-" +
			"5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	)

	got, err := ParseExplicitPrecompiledOFEDPullSpec(pullSpec, kernel, cluster, architecture)
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

	const pullSpec = "registry.stage.redhat.io/nvidia/doca-driver-rhel9:26.07-0.7.7.0-" +
		"5.14.0-570.76.1.el9_6.x86_64-rhcos4.22-amd64"
	tests := []struct {
		name, pullSpec, kernel, cluster, architecture string
	}{
		{name: "empty image", kernel: "5.14.0-570.76.1.el9_6.x86_64", cluster: "4.22.0", architecture: "amd64"},
		{name: "wrong registry", pullSpec: "registry.example.com/nvidia/doca-driver-rhel9:tag", kernel: "5.14.0-570.76.1.el9_6.x86_64", cluster: "4.22.0", architecture: "amd64"},
		{name: "missing registry host", pullSpec: "nvidia/doca-driver-rhel9:" + testTag2607Rhcos, kernel: testKernelX86, cluster: testCluster422, architecture: testArchAMD64},
		{name: "missing registry host rhel10", pullSpec: "nvidia/doca-driver-rhel10:" + testTag2607Rhcos, kernel: testKernelX86, cluster: testCluster422, architecture: testArchAMD64},
		{name: "wrong kernel", pullSpec: pullSpec, kernel: "5.14.0-570.76.2.el9_6.x86_64", cluster: "4.22.0", architecture: "amd64"},
		{name: "wrong OCP minor", pullSpec: pullSpec, kernel: "5.14.0-570.76.1.el9_6.x86_64", cluster: "4.23.0", architecture: "amd64"},
		{name: "wrong architecture", pullSpec: pullSpec, kernel: "5.14.0-570.76.1.el9_6.x86_64", cluster: "4.22.0", architecture: "arm64"},
		{name: "digest-only reference", pullSpec: "registry.stage.redhat.io/nvidia/doca-driver-rhel9@sha256:deadbeef", kernel: "5.14.0-570.76.1.el9_6.x86_64", cluster: "4.22.0", architecture: "amd64"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseExplicitPrecompiledOFEDPullSpec(
				test.pullSpec, test.kernel, test.cluster, test.architecture,
			); err == nil {
				t.Fatal("ParseExplicitPrecompiledOFEDPullSpec() error = nil, want mismatch error")
			}
		})
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
