package timeslicing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/timeslicing"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TestNamespace             = "test-timeslicing"
	DevicePluginConfigMapName = "plugin-config"
	GPUOperatorNamespace      = "nvidia-gpu-operator"
	TimeSlicingReplicas       = 4
	TimeSlicingImage          = "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0"
	EnsurePodsReadyTimeout    = "25m"
	EnsurePodsReadyInterval   = "30s"
)

var _ = Describe("TimeSlicing", Ordered, Label(tsparams.LabelSuite), func() {
	var (
		nsBuilder     *namespace.Builder
		configMap     *configmap.Builder
		clusterPolicy *nvidiagpu.Builder
	)

	BeforeAll(func() {
		glog.V(gpuparams.GpuLogLevel).Info("Starting TimeSlicing test suite")

		if tmpClusterPolicyBuilder, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName); err == nil {
			if _, err := tmpClusterPolicyBuilder.Get(); err == nil {
				if _, err := tmpClusterPolicyBuilder.Delete(); err != nil {
					glog.Errorf("Error deleting cluster policy: %v", err)
				} else {
					EnsureOnlyOperatorIsRunning()
				}
			} else {
				glog.Error("didn't find cluster policy")
			}
		}

		nsBuilder = namespace.NewBuilder(inittools.APIClient, TestNamespace)
		if !nsBuilder.Exists() {
			createdNsBuilder, err := nsBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating namespace %s: %v", TestNamespace, err)

			labeledNsBuilder := createdNsBuilder.WithMultipleLabels(map[string]string{
				"openshift.io/cluster-monitoring":    "true",
				"pod-security.kubernetes.io/enforce": "privileged",
			})

			_, err = labeledNsBuilder.Update()
			Expect(err).ToNot(HaveOccurred(), "error labeling namespace %s: %v", TestNamespace, err)
		}
	})

	AfterEach(func() {
		if configMap != nil {
			if err := configMap.Delete(); err != nil {
				glog.Errorf("Error deleting ConfigMap %s: %v", configMap.Object.Name, err)
			}
		}

		if clusterPolicy != nil {
			if _, err := clusterPolicy.Delete(); err != nil {
				glog.Errorf("Error deleting cluster policy: %v", err)
			}

			EnsureOnlyOperatorIsRunning()
		}
	})

	AfterAll(func() {
		if err := nsBuilder.Delete(); err != nil {
			glog.Errorf("Error deleting namespace %s: %v", TestNamespace, err)
		}
	})

	Context("Time-slicing GPU workload", Label("timeslicing-workload"), func() {
		It("Should run multiple concurrent CUDA workloads on a time-sliced GPU", Label("timeslicing"), func() {
			var err error

			By("Creating device plugin ConfigMap with time-slicing configuration")
			configMap, err = timeslicing.CreateDevicePluginConfigMap(
				inittools.APIClient,
				TimeSlicingReplicas,
				DevicePluginConfigMapName,
				GPUOperatorNamespace,
				false,
			)
			Expect(err).ToNot(HaveOccurred(), "error creating device plugin ConfigMap: %v", err)

			By("Creating ClusterPolicy from CSV with time-slicing device plugin config")
			clusterPolicy, err = timeslicing.CreateClusterPolicyFromCSV(
				inittools.APIClient, GPUOperatorNamespace, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error creating cluster policy: %v", err)

			By("Waiting for all GPU operator pods to be running")
			EnsureAllGpuPodsAreRunning()

			By("Verifying node labels reflect time-slicing configuration")
			clusterNodes, err := nodes.List(inittools.APIClient)
			Expect(err).ToNot(HaveOccurred(), "error listing nodes: %v", err)

			foundTimeSlicingNode := false

			for _, clusterNode := range clusterNodes {
				strategy, hasStrategy := clusterNode.Object.Labels["nvidia.com/gpu.sharing-strategy"]
				if hasStrategy && strategy == "time-slicing" {
					foundTimeSlicingNode = true
					replicas := clusterNode.Object.Labels["nvidia.com/gpu.replicas"]
					Expect(replicas).To(Equal(fmt.Sprintf("%d", TimeSlicingReplicas)),
						"expected %d replicas on node %s, got %s",
						TimeSlicingReplicas, clusterNode.Object.Name, replicas)

					allocatable := clusterNode.Object.Status.Allocatable["nvidia.com/gpu"]
					allocatableGPUs := allocatable.Value()
					Expect(allocatableGPUs).To(Equal(int64(TimeSlicingReplicas)),
						"expected %d allocatable GPUs on node %s, got %d",
						TimeSlicingReplicas, clusterNode.Object.Name, allocatableGPUs)

					glog.V(gpuparams.GpuLogLevel).Infof(
						"Node %s: sharing-strategy=%s, replicas=%s, allocatable=%d",
						clusterNode.Object.Name, strategy, replicas, allocatableGPUs)
				}
			}

			Expect(foundTimeSlicingNode).To(BeTrue(), "no node found with time-slicing sharing strategy")

			By(fmt.Sprintf("Creating %d CUDA vectoradd pods", TimeSlicingReplicas))

			for i := 1; i <= TimeSlicingReplicas; i++ {
				podName := fmt.Sprintf("timeslice-test-%d", i)
				testPod := timeslicing.CreateTimeSlicingTestPod(podName, TestNamespace, TimeSlicingImage)

				_, err = inittools.APIClient.Pods(TestNamespace).Create(
					context.TODO(), testPod, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred(),
					"error creating pod %s: %v", podName, err)

				DeferCleanup(func() {
					podBuilder, pullErr := pod.Pull(inittools.APIClient, podName, TestNamespace)
					if pullErr != nil {
						glog.Errorf("Error pulling pod %s: %v", podName, pullErr)

						return
					}

					if _, deleteErr := podBuilder.Delete(); deleteErr != nil {
						glog.Errorf("Error deleting pod %s: %v", podName, deleteErr)
					}
				})
			}

			By("Waiting for all pods to reach Succeeded phase")

			for i := 1; i <= TimeSlicingReplicas; i++ {
				podName := fmt.Sprintf("timeslice-test-%d", i)
				podBuilder, err := pod.Pull(inittools.APIClient, podName, TestNamespace)
				Expect(err).ToNot(HaveOccurred(), "error pulling pod %s: %v", podName, err)

				err = podBuilder.WaitUntilInStatus(corev1.PodSucceeded, 3*time.Minute)
				Expect(err).ToNot(HaveOccurred(),
					"timeout waiting for pod %s to succeed: %v", podName, err)
			}

			By("Verifying CUDA test passed in all pod logs")

			for i := 1; i <= TimeSlicingReplicas; i++ {
				podName := fmt.Sprintf("timeslice-test-%d", i)
				podBuilder, err := pod.Pull(inittools.APIClient, podName, TestNamespace)
				Expect(err).ToNot(HaveOccurred(), "error pulling pod %s: %v", podName, err)

				logs, err := podBuilder.GetLog(500*time.Second, "cuda-vectoradd")
				Expect(err).ToNot(HaveOccurred(),
					"error getting logs for pod %s: %v", podName, err)

				Expect(strings.Contains(logs, "Test PASSED")).To(BeTrue(),
					"CUDA vectoradd test did not pass in pod %s, logs:\n%s", podName, logs)

				glog.V(gpuparams.GpuLogLevel).Infof("Pod %s: CUDA vectoradd test PASSED", podName)
			}
		})
	})
})

func EnsureAllGpuPodsAreRunning() {
	Eventually(func() bool {
		gpuPods, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(
			context.TODO(), metav1.ListOptions{})
		if err != nil {
			glog.Errorf("Error listing GPU operator pods: %v", err)

			return false
		}

		if len(gpuPods.Items) < 8 {
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting for GPU operator pods: %d/8+",
				len(gpuPods.Items))

			return false
		}

		for _, p := range gpuPods.Items {
			glog.V(gpuparams.GpuLogLevel).Infof("Pod %s is %s", p.Name, p.Status.Phase)

			for _, containerStatus := range p.Status.ContainerStatuses {
				if !containerStatus.Ready && containerStatus.State.Terminated == nil {
					return false
				}
			}
		}

		return true
	}, EnsurePodsReadyTimeout, EnsurePodsReadyInterval).Should(BeTrue(),
		"GPU operator pods did not become ready")
}

func EnsureOnlyOperatorIsRunning() {
	Eventually(func() bool {
		gpuPods, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(
			context.TODO(), metav1.ListOptions{})
		if err != nil {
			glog.Errorf("Error listing GPU operator pods: %v", err)

			return false
		}

		if len(gpuPods.Items) > 1 || len(gpuPods.Items) == 0 {
			glog.V(gpuparams.GpuLogLevel).Infof(
				"Waiting for cleanup: %d pods remaining", len(gpuPods.Items))

			return false
		}

		for _, p := range gpuPods.Items {
			for _, containerStatus := range p.Status.ContainerStatuses {
				if !containerStatus.Ready && containerStatus.State.Terminated == nil {
					return false
				}
			}
		}

		return true
	}, EnsurePodsReadyTimeout, EnsurePodsReadyInterval).Should(BeTrue(),
		"GPU operator pods did not reach expected state after cleanup")
}
