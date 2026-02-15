package gpuallocation

import (
	"context"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/dra"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/helm"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/testworkloads"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/wait"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/namespace"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/tests/dra/shared"
	"helm.sh/helm/v3/pkg/action"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func createGPUResourceClaimTemplate(namespace, name string) error {
	rct := &resourcev1.ResourceClaimTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: resourcev1.ResourceClaimTemplateSpec{
			Spec: resourcev1.ResourceClaimSpec{
				Devices: resourcev1.DeviceClaim{
					Requests: []resourcev1.DeviceRequest{
						{
							Name: "gpu",
							Exactly: &resourcev1.ExactDeviceRequest{
								DeviceClassName: "gpu.nvidia.com",
							},
						},
					},
				},
			},
		},
	}

	_, err := inittools.APIClient.K8sClient.ResourceV1().
		ResourceClaimTemplates(namespace).
		Create(context.TODO(), rct, metav1.CreateOptions{})
	return err
}

var _ = Describe("DRA Driver Installation", Ordered, Label("dra", "dra-gpu"), func() {
	var actionConfig *action.Configuration
	var driver *dra.Driver
	var originalDevicePluginEnabled bool

	BeforeAll(func() {
		By("Verifying DRA prerequisites")
		err := shared.VerifyDRAPrerequisites(inittools.APIClient)
		Expect(err).ToNot(HaveOccurred(), "Failed to verify DRA prerequisites")

		By("Disabling device plugin for GPU allocation tests")
		devicePluginEnabled, err := shared.SetDevicePluginEnabled(inittools.APIClient, false)
		Expect(err).ToNot(HaveOccurred(), "Failed to disable device plugin")
		originalDevicePluginEnabled = devicePluginEnabled
		glog.V(gpuparams.GpuLogLevel).Infof("Device plugin originally enabled: %v", originalDevicePluginEnabled)

		if originalDevicePluginEnabled {
			DeferCleanup(func() error {
				By("Restoring original device plugin state")
				_, err := shared.SetDevicePluginEnabled(inittools.APIClient, originalDevicePluginEnabled)
				return err
			})
		}

		By("Waiting for GPU capacity on all nodes with GPU present to become 0")
		noGPUCapacityCondition := func(node *corev1.Node) (bool, error) {
			gpuCount, ok := node.Status.Capacity[corev1.ResourceName(nvidiagpu.GPUCapacityKey)]
			if ok {
				glog.V(gpuparams.GpuLogLevel).Infof("Node's %s GPU capacity: %v", node.Name, gpuCount.String())
				return gpuCount.IsZero(), nil
			}
			glog.V(gpuparams.GpuLogLevel).Infof("Node %s does not have GPU capacity", node.Name)
			return true, nil
		}

		err = wait.WaitForNodes(inittools.APIClient, labels.Set{nvidiagpu.GPUPresentLabel: "true"}, noGPUCapacityCondition, 20*time.Second, 10*time.Minute)
		Expect(err).ToNot(HaveOccurred(), "Failed to wait for GPU capacity on GPU nodes to become 0")

		By("Installing DRA Driver's Helm chart")
		actionConfig, err = helm.NewActionConfig(inittools.APIClient, dra.DriverNamespace, gpuparams.GpuLogLevel)
		Expect(err).ToNot(HaveOccurred(), "Failed to create Helm action configuration")

		// For GPU allocation tests, explicitly enable GPU resources
		driver, err = dra.NewDriver()
		Expect(err).ToNot(HaveOccurred(), "Failed to create DRA driver")
		driver.WithGPUResources(true).WithGPUResourcesOverride(true)

		DeferCleanup(func() error {
			By("Uninstalling DRA driver")
			return driver.Uninstall(actionConfig, shared.DriverInstallationTimeout)
		})

		err = driver.Install(actionConfig, shared.DriverInstallationTimeout)
		Expect(err).ToNot(HaveOccurred(), "Failed to install DRA driver")
	})

	Context("When DRA driver is installed", func() {
		It("Should allocate a single GPU using ResourceClaimTemplate", func() {
			names := shared.NewTestNames("gpu-test")

			By("Creating test namespace")
			testNs := namespace.NewBuilder(inittools.APIClient, names.Namespace())
			testNs, err := testNs.Create()
			Expect(err).ToNot(HaveOccurred(), "Failed to create test namespace")
			DeferCleanup(func() error {
				By("Cleaning up test namespace")
				return testNs.DeleteAndWait(2 * time.Minute)
			})
			glog.V(gpuparams.GpuLogLevel).Infof("Created test namespace: %s", names.Namespace())

			By("Creating ResourceClaimTemplate for single GPU")
			err = createGPUResourceClaimTemplate(names.Namespace(), names.ClaimTemplate())
			Expect(err).ToNot(HaveOccurred(), "Failed to create ResourceClaimTemplate")
			glog.V(gpuparams.GpuLogLevel).Infof("Created ResourceClaimTemplate: %s", names.ClaimTemplate())

			By("Creating VectorAdd pod with resource claim")
			rctNamePtr := names.ClaimTemplate()
			resourceClaims := []corev1.PodResourceClaim{
				{
					Name:                      names.Claim(),
					ResourceClaimTemplateName: &rctNamePtr,
				},
			}

			resources := corev1.ResourceRequirements{
				Claims: []corev1.ResourceClaim{
					{
						Name: names.Claim(),
					},
				},
			}

			vectorAdd := testworkloads.NewVectorAdd(names.Pod()).
				WithResources(resources).
				WithResourceClaims(resourceClaims)

			workloadBuilder := testworkloads.NewBuilder(inittools.APIClient, names.Namespace(), vectorAdd).
				Create()
			Expect(workloadBuilder.Error()).ToNot(HaveOccurred(), "Failed to create VectorAdd pod")
			glog.V(gpuparams.GpuLogLevel).Infof("Created VectorAdd pod: %s", names.Pod())

			By("Waiting for VectorAdd pod to succeed")
			workloadBuilder.WaitUntilSuccess(1 * time.Minute)
			Expect(workloadBuilder.Error()).ToNot(HaveOccurred(), "VectorAdd pod did not succeed")
			glog.V(gpuparams.GpuLogLevel).Infof("VectorAdd pod succeeded: %s", names.Pod())
		})
	})
})
