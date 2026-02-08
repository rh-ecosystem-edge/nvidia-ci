package computedomain

import (
	"context"
	"fmt"
	"strings"
	"time"

	nvidiadrav1beta1 "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
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

	// Naming convention for test objects (stable, no timestamps)
	testObjectPrefix        = "cd-test"
	testNamespaceSuffix     = "-ns"
	testComputeDomainSuffix = "-domain"
	testClaimTemplateSuffix = "-claim-tpl"
	testPodSuffix           = "-pod"
	testClaimSuffix         = "-claim"
)

func createComputeDomain(apiClient *clients.Settings, name, namespace, rctName string) error {
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

	return apiClient.Create(context.TODO(), computeDomain)
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
	var hasClique bool

	BeforeAll(func() {
		err := shared.VerifyDRAPrerequisites(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to verify DRA prerequisites")

		// Create Helm action config once for all operations
		actionConfig, err = shared.NewActionConfig(inittools.APIClient, shared.DRADriverNamespace, gpuparams.GpuLogLevel)
		Expect(err).ToNot(HaveOccurred(), "Failed to create Helm action configuration")

		// For compute domain tests, disable GPU resources
		customValues := shared.NewDRAValues().WithGPUResources(false)

		err = shared.InstallDRADriver(actionConfig, shared.LatestVersion, customValues)
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

	AfterAll(func() {
		By("Cleaning up DRA driver")
		glog.V(gpuparams.GpuLogLevel).Infof("Starting DRA driver cleanup")
		if actionConfig != nil {
			err := shared.UninstallDRADriver(actionConfig)
			Expect(err).ToNot(HaveOccurred(), "Failed to uninstall DRA driver")
		}
		glog.V(gpuparams.GpuLogLevel).Infof("DRA driver cleanup completed successfully")
	})

	Context("Multi-node compute domain with GPU clique", func() {
		BeforeEach(func() {
			if !hasClique {
				Skip(fmt.Sprintf("Skipping multi-node test: requires at least 2 nodes with same %s label. Single-node test will run instead.", gpuCliqueLabel))
			}
		})

		It("Should create IMEX channel, run workload across nodes", func() {
			// Placeholder for multi-node workload with clique
			// TODO: Deploy workload requiring multi-node NVLink, verify cross-node communication
		})
	})

	Context("Single-node compute domain", func() {
		BeforeEach(func() {
			if hasClique {
				Skip("Skipping single-node test: multi-node setup is available. Multi-node test will run instead.")
			}
		})

		It("Should create compute domain, run workload on single node", func() {
			By("Creating temporary test namespace")
			testNamespaceName := testObjectPrefix + testNamespaceSuffix
			testNamespace := namespace.NewBuilder(inittools.APIClient, testNamespaceName)
			_, err := testNamespace.Create()
			Expect(err).ToNot(HaveOccurred(), "Failed to create test namespace")
			defer func() {
				defer GinkgoRecover()
				By("Cleaning up test namespace")
				err := testNamespace.DeleteAndWait(2 * time.Minute)
				if err != nil {
					glog.Warningf("Failed to delete test namespace: %v", err)
				}
			}()
			glog.V(gpuparams.GpuLogLevel).Infof("Created test namespace: %s", testNamespaceName)

			By("Creating ComputeDomain resource")
			computeDomainName := testObjectPrefix + testComputeDomainSuffix
			rctName := testObjectPrefix + testClaimTemplateSuffix
			err = createComputeDomain(inittools.APIClient, computeDomainName, testNamespaceName, rctName)
			Expect(err).ToNot(HaveOccurred(), "Failed to create ComputeDomain")
			glog.V(gpuparams.GpuLogLevel).Infof("Created ComputeDomain: %s", computeDomainName)

			By("Creating VectorAdd pod with resource claims")
			podName := testObjectPrefix + testPodSuffix
			claimName := testObjectPrefix + testClaimSuffix

			rctNamePtr := rctName
			resourceClaims := []corev1.PodResourceClaim{
				{
					Name:                      claimName,
					ResourceClaimTemplateName: &rctNamePtr,
				},
			}

			resources := corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
				Claims: []corev1.ResourceClaim{
					{
						Name: claimName,
					},
				},
			}

			vectorAdd := testworkloads.NewVectorAdd(podName).
				WithResources(resources).
				WithResourceClaims(resourceClaims).
				WithCommand([]string{"/bin/sh", "-c", "/cuda-samples/vectorAdd && sleep 30"})

			workloadBuilder := testworkloads.NewBuilder(inittools.APIClient, testNamespaceName, vectorAdd).
				Create()
			Expect(workloadBuilder.Error()).ToNot(HaveOccurred(), "Failed to create VectorAdd pod")
			glog.V(gpuparams.GpuLogLevel).Infof("Created VectorAdd pod: %s", podName)

			By("Waiting for VectorAdd pod to become Running")
			workloadBuilder.WaitUntilStatus(corev1.PodRunning, 1*time.Minute)
			Expect(workloadBuilder.Error()).ToNot(HaveOccurred(), "Failed to wait for pod Running status")
			glog.V(gpuparams.GpuLogLevel).Infof("VectorAdd pod is Running")

			By("Verifying compute domain pods exist in DRA driver namespace")
			pods, err := pod.List(inittools.APIClient, shared.DRADriverNamespace)
			Expect(err).ToNot(HaveOccurred(), "Failed to list pods in DRA driver namespace")

			expectedPodNamePrefix := testObjectPrefix + testComputeDomainSuffix
			var matchingPods []*pod.Builder
			for _, p := range pods {
				if strings.HasPrefix(p.Object.Name, expectedPodNamePrefix) {
					labelValue, hasLabel := p.Object.Labels[computeDomainLabel]
					Expect(hasLabel).To(BeTrue(),
						"Pod %s has matching name prefix but missing label '%s'", p.Object.Name, computeDomainLabel)
					matchingPods = append(matchingPods, p)
					glog.V(gpuparams.GpuLogLevel).Infof("Found compute domain pod: %s with label %s=%s",
						p.Object.Name, computeDomainLabel, labelValue)
				}
			}
			Expect(len(matchingPods)).To(BeNumerically(">", 0),
				"Expected at least one pod with name starting with '%s' and label '%s' in namespace %s",
				expectedPodNamePrefix, computeDomainLabel, shared.DRADriverNamespace)
			glog.V(gpuparams.GpuLogLevel).Infof("Verified %d compute domain pod(s) in DRA driver namespace", len(matchingPods))
		})
	})
})
