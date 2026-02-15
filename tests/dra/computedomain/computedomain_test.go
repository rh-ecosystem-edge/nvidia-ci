package computedomain

import (
	"context"
	"fmt"
	"time"

	nvidiadrav1beta1 "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/dra"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/helm"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/testworkloads"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	"github.com/rh-ecosystem-edge/nvidia-ci/tests/dra/shared"
	"helm.sh/helm/v3/pkg/action"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	gpuCliqueLabel     = "nvidia.com/gpu.clique"
	computeDomainLabel = "resource.nvidia.com/computeDomain"
)

func createComputeDomain(apiClient *clients.Settings, name, namespace, rctName string) (*nvidiadrav1beta1.ComputeDomain, error) {
	computeDomain := &nvidiadrav1beta1.ComputeDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: nvidiadrav1beta1.ComputeDomainSpec{
			NumNodes: 0,
			Channel: &nvidiadrav1beta1.ComputeDomainChannelSpec{
				ResourceClaimTemplate: nvidiadrav1beta1.ComputeDomainResourceClaimTemplate{
					Name: rctName,
				},
			},
		},
	}

	err := apiClient.Create(context.TODO(), computeDomain)
	if err != nil {
		return nil, err
	}

	// computeDomain is updated in place by Create() with server response
	return computeDomain, nil
}

func hasMultiNodeClique(apiClient *clients.Settings) (bool, error) {
	nodeList, err := nodes.List(apiClient)
	if err != nil {
		return false, err
	}

	cliqueGroups := make(map[string]int)
	for _, node := range nodeList {
		if cliqueValue, ok := node.Object.Labels[gpuCliqueLabel]; ok {
			cliqueGroups[cliqueValue]++
			if cliqueGroups[cliqueValue] >= 2 {
				return true, nil
			}
		}
	}

	return false, nil
}

var _ = Describe("DRA Driver Installation", Ordered, Label("dra", "dra-imex"), func() {
	var actionConfig *action.Configuration
	var driver *dra.Driver
	var hasClique bool

	BeforeAll(func() {
		By("Verifying DRA prerequisites")
		err := shared.VerifyDRAPrerequisites(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to verify DRA prerequisites")

		By("Installing DRA Driver's Helm chart")
		actionConfig, err = helm.NewActionConfig(inittools.APIClient, dra.DriverNamespace, gpuparams.GpuLogLevel)
		Expect(err).ToNot(HaveOccurred(), "Failed to create Helm action configuration")

		// For compute domain tests, disable GPU resources
		driver, err = dra.NewDriver()
		Expect(err).ToNot(HaveOccurred(), "Failed to create DRA driver")
		driver.WithGPUResources(false)

		DeferCleanup(func() error {
			By("Uninstalling DRA driver")
			return driver.Uninstall(actionConfig, shared.DriverInstallationTimeout)
		})

		err = driver.Install(actionConfig, shared.DriverInstallationTimeout)
		Expect(err).ToNot(HaveOccurred(), "Failed to install DRA driver")

		By("Verifying compute domain DeviceClass resources")
		deviceClasses := []string{
			"compute-domain-daemon.nvidia.com",
			"compute-domain-default-channel.nvidia.com",
		}
		err = shared.VerifyDeviceClasses(inittools.APIClient, deviceClasses)
		Expect(err).ToNot(HaveOccurred(), "Failed to verify compute domain DeviceClasses")
		glog.V(gpuparams.GpuLogLevel).Infof("Compute domain DeviceClasses verified successfully")

		By("Verifying device plugin is enabled in ClusterPolicy")
		isEnabled, err := shared.IsDevicePluginEnabled(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to check device plugin state")
		Expect(isEnabled).To(BeTrue(), "Device plugin must be enabled in ClusterPolicy for compute domain tests")
		glog.V(gpuparams.GpuLogLevel).Infof("Device plugin is enabled in ClusterPolicy")

		By("Detecting multi-node GPU clique configuration")
		hasClique, err = hasMultiNodeClique(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to check for multi-node GPU clique")
		glog.V(gpuparams.GpuLogLevel).Infof("Multi-node GPU clique available: %v", hasClique)
	})

	Context("Multi-node compute domain with GPU clique", func() {
		BeforeEach(func() {
			if !hasClique {
				Skip(fmt.Sprintf("Skipping multi-node test: requires at least 2 nodes with same %s label. Single-node test will run instead.", gpuCliqueLabel))
			}
		})

		It("Should create IMEX channel, run workload across nodes", func() {
			Skip("not yet implemented")
		})
	})

	Context("Single-node compute domain", func() {
		BeforeEach(func() {
			if hasClique {
				Skip("Skipping single-node test: multi-node setup is available. Multi-node test will run instead.")
			}
		})

		It("Should create compute domain, run workload on single node", func() {
			names := shared.NewTestNames("cd-test")

			By("Creating temporary test namespace")
			testNamespace := namespace.NewBuilder(inittools.APIClient, names.Namespace())
			_, err := testNamespace.Create()
			Expect(err).ToNot(HaveOccurred(), "Failed to create test namespace")
			glog.V(gpuparams.GpuLogLevel).Infof("Created test namespace: %s", names.Namespace())

			DeferCleanup(func() error {
				By("Cleaning up test namespace")
				return testNamespace.DeleteAndWait(2 * time.Minute)
			})

			By("Creating ComputeDomain resource")
			computeDomain, err := createComputeDomain(inittools.APIClient, names.ComputeDomain(), names.Namespace(), names.ClaimTemplate())
			Expect(err).ToNot(HaveOccurred(), "Failed to create ComputeDomain")
			computeDomainUID := string(computeDomain.UID)
			glog.V(gpuparams.GpuLogLevel).Infof("Created ComputeDomain: %s with UID: %s", names.ComputeDomain(), computeDomainUID)

			By("Creating VectorAdd pod with resource claims")
			rctNamePtr := names.ClaimTemplate()
			resourceClaims := []corev1.PodResourceClaim{
				{
					Name:                      names.Claim(),
					ResourceClaimTemplateName: &rctNamePtr,
				},
			}

			resources := corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
				Claims: []corev1.ResourceClaim{
					{
						Name: names.Claim(),
					},
				},
			}

			vectorAdd := testworkloads.NewVectorAdd(names.Pod()).
				WithResources(resources).
				WithResourceClaims(resourceClaims).
				WithCommand([]string{"/bin/sh", "-c", "/cuda-samples/vectorAdd && sleep 30"})

			workloadBuilder := testworkloads.NewBuilder(inittools.APIClient, names.Namespace(), vectorAdd).
				Create()
			Expect(workloadBuilder.Error()).ToNot(HaveOccurred(), "Failed to create VectorAdd pod")
			glog.V(gpuparams.GpuLogLevel).Infof("Created VectorAdd pod: %s", names.Pod())

			By("Waiting for VectorAdd pod to become Running")
			workloadBuilder.WaitUntilStatus(corev1.PodRunning, 1*time.Minute)
			Expect(workloadBuilder.Error()).ToNot(HaveOccurred(), "Failed to wait for pod Running status")
			glog.V(gpuparams.GpuLogLevel).Infof("VectorAdd pod is Running")

			By("Verifying compute domain pods exist in DRA driver namespace")
			labelSelector := fmt.Sprintf("%s=%s", computeDomainLabel, computeDomainUID)
			pods, err := pod.List(inittools.APIClient, dra.DriverNamespace, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list pods in DRA driver namespace")
			Expect(pods).NotTo(BeEmpty(),
				"Expected at least one pod with label selector '%s' in namespace %s",
				labelSelector, dra.DriverNamespace)
		})
	})
})
