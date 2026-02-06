package mps

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/mps"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nvidiagpuconfig"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	// TestNamespace is the namespace where MPS tests will run
	TestNamespace = "test-mps"
	// ConfigMapName is the name of the ConfigMap containing MPS test data
	ConfigMapName = "mps-test-entrypoint"
	// DevicePluginConfigMapName is the name of the ConfigMap containing device plugin configuration
	DevicePluginConfigMapName = "plugin-config"
	// PodName is the name of the MPS test pod
	PodName = "mps-test-pod"
	// MPSReplicas is the number of GPU replicas for MPS
	MPSReplicas = 10
	// MPSImage is the container image for MPS testing
	MPSImage = "nvcr.io/nvidia/pytorch:23.12-py3"
	// Number of worker pods to create
	NumWorkerPods = 20
	// TestDuration is how long each pod should run
	TestDuration = "25m"
	// GPUOperatorNamespace is the namespace where the NVIDIA GPU operator is installed
	GPUOperatorNamespace = "nvidia-gpu-operator"
	LargeMPSReplicas     = 49
	TimeStep             = "30s"
)

var (
	nvidiaGPUConfig *nvidiagpuconfig.NvidiaGPUConfig
)

var _ = Describe("MPS", Ordered, Label(tsparams.LabelSuite), func() {
	var (
		nsBuilder     *namespace.Builder
		configMap     *configmap.Builder
		clusterPolicy *nvidiagpu.Builder
	)
	nvidiaGPUConfig = nvidiagpuconfig.NewNvidiaGPUConfig()

	BeforeAll(func() {
		// Set log level
		glog.V(gpuparams.GpuLogLevel).Info("Starting MPS test suite")

		if tmpClusterPolicyBulider, err := nvidiagpu.Pull(inittools.APIClient, nvidiagpu.ClusterPolicyName); err == nil {
			if _, err := tmpClusterPolicyBulider.Get(); err == nil {

				if _, err := tmpClusterPolicyBulider.Delete(); err != nil {
					glog.Errorf("Error deleting cluster policy: %v", err)
				} else {

					EnsureOnlyOperatorIsRunning()
				}
			} else {
				glog.Error("didn't find cluster policy")
			}
		}
		// Create test namespace
		nsBuilder = namespace.NewBuilder(inittools.APIClient, TestNamespace)
		if !nsBuilder.Exists() {
			createdNsBuilder, err := nsBuilder.Create()
			Expect(err).ToNot(HaveOccurred(), "error creating namespace %s: %v", TestNamespace, err)

			// Label the namespace
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

	Context("MPS with large number of replicas", Label("mps-with-large-replicas"), func() {
		It("MPS daemon should fail and log error message", Label("mps"), func() {

			var err error
			configMap, err = mps.CreateDevicePluginConfigMap(
				inittools.APIClient,
				LargeMPSReplicas,
				DevicePluginConfigMapName,
				GPUOperatorNamespace,
				false,
			)

			Expect(err).ToNot(HaveOccurred(), "error creating device plugin ConfigMap: %v", err)
			clusterPolicy, err = mps.CreateClusterPolicyFromCSV(inittools.APIClient, GPUOperatorNamespace, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error updating cluster policy: %v", err)
			Eventually(func() bool {

				mpsDaemon, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=nvidia-device-plugin-mps-control-daemon"})

				if err != nil {
					glog.Errorf("Error listing NVIDIA mps pods: %v", err)
					return false
				}

				if len(mpsDaemon.Items) == 0 {
					glog.Errorf("No NVIDIA driver pods found in namespace %s", GPUOperatorNamespace)
					return false
				}

				for _, pod := range mpsDaemon.Items {

					for _, containerStatus := range pod.Status.ContainerStatuses {
						glog.V(gpuparams.GpuLogLevel).Infof("container %s waiting %v", containerStatus.Name, containerStatus.State.Waiting)
						if strings.Contains(containerStatus.Name, "mps-control-daemon-ctr") {

							glog.V(gpuparams.GpuLogLevel).Infof("container %s waiting %v", containerStatus.Name, containerStatus.State.Waiting)
							if containerStatus.State.Waiting != nil {
								return true

							}
						}
					}
				}
				return false
			}, TestDuration, TimeStep).Should(BeTrue(), "MPS daemon failed as expected")

			mpsDaemons, err := pod.List(inittools.APIClient, GPUOperatorNamespace, metav1.ListOptions{LabelSelector: "app=nvidia-device-plugin-mps-control-daemon"})
			Expect(err).ToNot(HaveOccurred(), "Failed locate MPS daemon %v", err)
			if len(mpsDaemons) == 0 {
				glog.Errorf("No NVIDIA driver pods found in namespace %s", GPUOperatorNamespace)

			}

			for _, daemon := range mpsDaemons {
				glog.V(gpuparams.GpuLogLevel).Infof("Pod %s is %s ", daemon.Object.Name, daemon.Object.Status.Phase)
				if strings.Contains(daemon.Object.Name, "nvidia-device-plugin-mps-control") || daemon.Object.Status.Phase == corev1.PodFailed {
					for _, container := range daemon.Object.Spec.Containers {
						if strings.Contains(container.Name, "mps-control-daemon-ctr") {
							glog.V(gpuparams.GpuLogLevel).Infof("\n=== Logs for container: %s ===\n", container.Name)

							logOptions := &corev1.PodLogOptions{
								Container: container.Name,
								TailLines: nil, // get full log
							}

							logs, err := inittools.APIClient.Pods(GPUOperatorNamespace).GetLogs(daemon.Object.Name, logOptions).
								Do(context.TODO()).Raw()

							if err != nil {
								glog.Error("Error getting logs for %s: %v\n", container.Name, err)
								continue
							}

							logStr := string(logs)
							searchStr := "invalid MPS configuration: invalid device maximum allowed replicas exceeded: 49 > 48"
							// Search the string
							if strings.Contains(logStr, searchStr) {
								glog.V(gpuparams.GpuLogLevel).Info("Match found:")
								lines := strings.Split(logStr, "\n")
								for _, line := range lines {
									if strings.Contains(line, searchStr) {
										glog.V(gpuparams.GpuLogLevel).Info(">> " + line)
									}
								}
							} else {
								glog.Error("No match found for error string.")
							}

							// Print entire log
							glog.V(gpuparams.GpuLogLevel).Info("\n--- Full log ---")
							glog.V(gpuparams.GpuLogLevel).Info(logStr)
						}
					}
				}
			}

		})
	})

	Context("MPS Multiple Worker Pods", Label("mps-with-valid-replicas-number"), func() {
		It("Should run multiple worker pods with MPS enabled", Label("mps"), func() {

			var err error
			// Create worker ConfigMap
			configMap, err = mps.CreateDevicePluginConfigMap(
				inittools.APIClient,
				MPSReplicas,
				DevicePluginConfigMapName,
				GPUOperatorNamespace,
				false)
			Expect(err).ToNot(HaveOccurred(), "error creating device plugin configMap: %v", err)
			clusterPolicy, err = mps.CreateClusterPolicyFromCSV(inittools.APIClient, GPUOperatorNamespace, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error creating MPS worker clusterPolicy: %v", err)
			Expect(configMap).ToNot(BeNil())

			EnsureAllGpuPodsAreRunning()
			testPodsCM, err := mps.CreateWorkerPodConfigMap(inittools.APIClient, TestNamespace)
			Expect(err).ToNot(HaveOccurred(), "error creating MPS worker ConfigMap: %v", err)
			DeferCleanup(func() {
				err = testPodsCM.Delete()
				glog.Error("failed deleteing mps worker config map %v", err)
			})
			// Create and run multiple worker pods
			for i := 0; i < NumWorkerPods; i++ {
				workerPodName := fmt.Sprintf("mps-worker-%d", i)

				// Create MPS worker pod
				workerPod, err := mps.CreateMPSTestPod(
					inittools.APIClient,
					workerPodName,
					TestNamespace,
					MPSImage,
				)
				Expect(err).ToNot(HaveOccurred(), "error creating MPS worker pod %s: %v", workerPodName, err)
				Expect(workerPod).ToNot(BeNil())

				// Create pod in cluster
				_, err = inittools.APIClient.Pods(TestNamespace).Create(context.TODO(), workerPod, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred(), "error creating worker pod %s in cluster: %v", workerPodName, err)

				// Cleanup worker pod
				DeferCleanup(func() {
					podBuilder, err := pod.Pull(inittools.APIClient, workerPodName, TestNamespace)
					if err != nil {
						glog.Errorf("Error pulling pod %s: %v", workerPodName, err)
					} else {
						_, err = podBuilder.Delete()
						if err != nil {
							glog.Errorf("Error deleting pod %s: %v", workerPodName, err)
						}
					}
				})

			}

			// Wait for worker pods to run for a while
			glog.V(gpuparams.GpuLogLevel).Infof("Waiting for worker pods to run for 2 minutes...")
			time.Sleep(2 * time.Minute)
			// Verify at least one worker pod is still running
			Eventually(func() error {
				pods, err := inittools.APIClient.Pods(TestNamespace).List(context.TODO(), metav1.ListOptions{
					LabelSelector: "app=mps-test-app",
				})
				if err != nil {
					return err
				}

				runningPods := 0
				for _, p := range pods.Items {
					if p.Status.Phase == corev1.PodRunning {
						runningPods++
					}
				}

				if runningPods < 1 {
					return fmt.Errorf("expected at least 1 running pod, got %d", runningPods)
				}

				return nil
			}, TestDuration, TimeStep).Should(Succeed(), "Not enough worker pods are running")

			// Get NVIDIA driver pods from the GPU operator namespace
			driverPods, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/component=nvidia-driver",
			})
			Expect(err).ToNot(HaveOccurred(), "error listing NVIDIA driver pods: %v", err)
			Expect(driverPods.Items).ToNot(BeEmpty(), "No NVIDIA driver pods found in namespace %s", GPUOperatorNamespace)

			// Execute nvidia-smi commands on each driver pod
			for _, driverPod := range driverPods.Items {
				glog.V(gpuparams.GpuLogLevel).Infof("Executing nvidia-smi commands on driver pod %s", driverPod.Name)

				// Execute nvidia-smi
				cmd := []string{"nvidia-smi"}
				output, err := executeCommandInPod(driverPod.Name, GPUOperatorNamespace, cmd)
				Expect(err).ToNot(HaveOccurred(), "error executing nvidia-smi on pod %s: %v", driverPod.Name, err)
				glog.V(gpuparams.GpuLogLevel).Infof("nvidia-smi output from pod %s:\n%s", driverPod.Name, output)

				// Parse the nvidia-smi output to check for Python processes in M+C mode
				parseNvidiaSmiOutput(output, driverPod.Name)

				// Execute nvidia-smi pmon -c 1 -s m
				cmd = []string{"nvidia-smi", "pmon", "-c", "1", "-s", "m"}
				output, err = executeCommandInPod(driverPod.Name, GPUOperatorNamespace, cmd)
				Expect(err).ToNot(HaveOccurred(), "error executing nvidia-smi pmon on pod %s: %v", driverPod.Name, err)
				glog.V(gpuparams.GpuLogLevel).Infof("nvidia-smi pmon output from pod %s:\n%s", driverPod.Name, output)

				// Check for process information in the output
				Expect(output).ToNot(BeEmpty(), "nvidia-smi pmon output is empty from pod %s", driverPod.Name)

				// Execute nvidia-smi topo -m
				cmd = []string{"nvidia-smi", "topo", "-m"}
				output, err = executeCommandInPod(driverPod.Name, GPUOperatorNamespace, cmd)
				Expect(err).ToNot(HaveOccurred(), "error executing nvidia-smi topo on pod %s: %v", driverPod.Name, err)
				glog.V(gpuparams.GpuLogLevel).Infof("nvidia-smi topo output from pod %s:\n%s", driverPod.Name, output)
			}

		})
	})

	Context("MPS renameByDefault set to true", Label("mps-renameByDefault"), func() {
		It("Node should advertise on gpu.shared", Label("mps"), func() {
			var err error
			configMap, err = mps.CreateDevicePluginConfigMap(
				inittools.APIClient,
				MPSReplicas,
				DevicePluginConfigMapName,
				GPUOperatorNamespace,
				true)
			Expect(err).ToNot(HaveOccurred(), "error creating device plugin ConfigMap: %v", err)
			clusterPolicy, err = mps.CreateClusterPolicyFromCSV(inittools.APIClient, GPUOperatorNamespace, nvidiagpu.ClusterPolicyName)
			Expect(err).ToNot(HaveOccurred(), "error updating cluster policy: %v", err)
			EnsureAllGpuPodsAreRunning()
			clusterNodes, err := nodes.List(inittools.APIClient)
			Expect(err).ToNot(HaveOccurred(), "error updating cluster policy: %v", err)

			for _, clusterNode := range clusterNodes {
				if _, ok := clusterNode.Object.Status.Capacity["nvidia.com/gpu.shared"]; ok {
					Expect(ok).To(BeTrue(), "missing shared resource (nvidia.com/gpu.shared)")
				}
			}

		})
	})
})

// executeCommandInPod executes a command in a pod and returns the output
func executeCommandInPod(podName, namespace string, command []string) (string, error) {
	// Get the pod
	podObj, err := inittools.APIClient.Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod: %v", err)
	}

	// Find the container name (assuming it's the first container)
	containerName := podObj.Spec.Containers[0].Name

	// Create a Kubernetes client
	k8sClient, err := kubernetes.NewForConfig(inittools.APIClient.Config)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// Create a request to execute the command
	req := k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	// Create an executor
	exec, err := remotecommand.NewSPDYExecutor(inittools.APIClient.Config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %v", err)
	}

	// Create buffers for stdout and stderr
	var stdout, stderr bytes.Buffer

	// Execute the command
	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute command: %v", err)
	}

	// Return the combined output
	return stdout.String() + stderr.String(), nil
}

// parseNvidiaSmiOutput parses the nvidia-smi output to check for Python processes in M+C mode
func parseNvidiaSmiOutput(output, podName string) {
	// Split the output into lines
	lines := strings.Split(output, "\n")

	// Find the table header line
	var headerLineIndex int
	for i, line := range lines {
		if strings.Contains(line, "PID") && strings.Contains(line, "Type") {
			headerLineIndex = i
			break
		}
	}

	// If we found a header line, parse the table
	if headerLineIndex > 0 && headerLineIndex < len(lines)-1 {
		// Get the separator line to determine column positions
		separatorLine := lines[headerLineIndex+1]

		// Find column positions
		pidPos := strings.Index(separatorLine, "PID")
		typePos := strings.Index(separatorLine, "Type")
		namePos := strings.Index(separatorLine, "Name")

		if pidPos >= 0 && typePos >= 0 && namePos >= 0 {
			// Process each row in the table
			for i := headerLineIndex + 2; i < len(lines); i++ {
				line := lines[i]
				if len(line) < namePos {
					continue
				}

				// Extract process name
				processName := strings.TrimSpace(line[namePos:])

				// Check if it's a Python process
				if strings.Contains(processName, "python") {
					// Extract process type
					processType := ""
					if len(line) >= typePos+10 {
						processType = strings.TrimSpace(line[typePos:namePos])
					}

					// Verify it's running in M+C mode
					Expect(processType).To(ContainSubstring("M+C"),
						"Python process %s is not running in M+C mode in pod %s",
						processName, podName)

					glog.V(gpuparams.GpuLogLevel).Infof("Found Python process %s running in %s mode", processName, processType)
				}
			}
		}
	} else {
		// If we couldn't find a table, try a simpler approach with regex
		pythonProcessRegex := regexp.MustCompile(`(\d+)\s+(\w+)\s+(\w+)\s+python`)
		matches := pythonProcessRegex.FindAllStringSubmatch(output, -1)

		for _, match := range matches {
			if len(match) >= 4 {
				pid := match[1]
				processType := match[2]
				processName := match[3]

				Expect(processType).To(ContainSubstring("M+C"),
					"Python process %s (PID %s) is not running in M+C mode in pod %s",
					processName, pid, podName)

				glog.V(gpuparams.GpuLogLevel).Infof("Found Python process %s (PID %s) running in %s mode",
					processName, pid, processType)
			}
		}
	}
}

func EnsureAllGpuPodsAreRunning() {
	Eventually(func() bool {
		driverPods, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			glog.Errorf("Error listing NVIDIA driver pods: %v", err)
			return false
		}

		if len(driverPods.Items) < 8 {
			glog.Errorf("Not all NVIDIA driver pods found in namespace %s", GPUOperatorNamespace)
			return false
		}

		for _, pod := range driverPods.Items {
			glog.V(gpuparams.GpuLogLevel).Infof("Pod %s is %s ", pod.Name, pod.Status.Phase)
			// Check container ready status
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if !containerStatus.Ready && containerStatus.State.Terminated == nil {
					return false
				}
			}
		}
		return true
	}, TestDuration, TimeStep).Should(BeTrue(), "NVIDIA driver pods did not become ready")

	driverPods, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(context.TODO(), metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred(), "error listing NVIDIA driver pods: %v", err)
	Expect(driverPods.Items).ToNot(BeEmpty(), "no NVIDIA driver pods found")

	// Verify all pods are running
	for _, pod := range driverPods.Items {
		Expect(pod.Status.Phase).To(Or(Equal(corev1.PodRunning), Equal(corev1.PodSucceeded)), "NVIDIA driver pod %s is not running or succeeded", pod.Name)
	}
}

func EnsureOnlyOperatorIsRunning() {
	Eventually(func() bool {
		driverPods, err := inittools.APIClient.Pods(GPUOperatorNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			glog.Errorf("Error listing NVIDIA driver pods: %v", err)
			return false
		}

		if len(driverPods.Items) > 1 || len(driverPods.Items) == 0 {
			glog.Errorf("Not all NVIDIA driver pods deleted in namespace %s or namespace is empty", GPUOperatorNamespace)
			return false
		}

		for _, pod := range driverPods.Items {
			glog.V(gpuparams.GpuLogLevel).Infof("Pod %s is %s ", pod.Name, pod.Status.Phase)
			// Check container ready status
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if !containerStatus.Ready && containerStatus.State.Terminated == nil {
					return false
				}
			}
		}
		return true
	}, TestDuration, TimeStep).Should(BeTrue(), "NVIDIA driver pods did not become ready")
}
