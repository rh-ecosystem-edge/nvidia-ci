package nvidianetwork

import (
	"context"
	"fmt"
	"strings"
	"time"

	nvidianetworkv1alpha1 "github.com/Mellanox/network-operator/api/v1alpha1"
	networkconsts "github.com/Mellanox/network-operator/pkg/consts"
	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/tsparams"
	. "github.com/rh-ecosystem-edge/nvidia-ci/pkg/global"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// rdmaTestPodNamePrefixes are the workloads created by the RDMA test cases.
var rdmaTestPodNamePrefixes = []string{"rdma-shared-dev-", "rdma-legacy-sriov-"}

var _ = Describe("NNO driver cleanup", Label(tsparams.LabelSuite), func() {
	It("Stop GPU and OFED drivers before switching images", Label("cleanup-nno-drivers"), func(ctx SpecContext) {
		if !nvidiaNetworkConfig.CleanupDriversOnly {
			Skip("driver cleanup requires NVIDIANETWORK_CLEANUP_DRIVERS_ONLY=true")
		}
		workloadNamespace := nvidiaNetworkConfig.RdmaWorkloadNamespace
		Expect(workloadNamespace).NotTo(Or(BeEmpty(), Equal(UndefinedValue)),
			"driver cleanup requires NVIDIANETWORK_RDMA_WORKLOAD_NAMESPACE")

		By("Save the current driver policies before changing them")
		policies := gpuPoliciesForDriverCleanup(ctx)
		writeDriverCleanupSnapshot("gpu-policy-before-signed.yaml", policies)
		policy := &nvidianetworkv1alpha1.NicClusterPolicy{}
		err := inittools.APIClient.Get(ctx, goclient.ObjectKey{Name: nnoNicClusterPolicyName}, policy)
		if !k8serrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "cannot inspect existing NicClusterPolicy")
			writeDriverCleanupSnapshot("nic-policy-before-signed.yaml", policy)
		}

		By("Delete RDMA test workloads before stopping their drivers")
		deleteRdmaTestPods(ctx, workloadNamespace)
		By("Disable the GPU driver and its consumers before removing OFED")
		disableGPUDriverOperands(ctx, policies)
		By("Delete the previous NicClusterPolicy and wait for OFED pods to terminate")
		deleteNicClusterPolicyIfExists(ctx, nnoNicClusterPolicyName, "", 10*time.Minute)
	}, SpecTimeout(30*time.Minute))
})

func gpuPoliciesForDriverCleanup(ctx context.Context) *nvidiagpuv1.ClusterPolicyList {
	policies := &nvidiagpuv1.ClusterPolicyList{}
	err := inittools.APIClient.List(ctx, policies)
	// A network-only cluster may not have the GPU Operator CRD installed.
	if meta.IsNoMatchError(err) || k8serrors.IsNotFound(err) {
		return policies
	}
	Expect(err).NotTo(HaveOccurred(), "cannot list GPU ClusterPolicies")
	return policies
}

func writeDriverCleanupSnapshot(name string, object interface{}) {
	data, err := yaml.Marshal(object)
	Expect(err).NotTo(HaveOccurred(), "cannot marshal driver cleanup snapshot %s", name)
	Expect(inittools.GeneralConfig.WriteReport(name, data)).To(Succeed(),
		"cannot save driver cleanup snapshot %s", name)
}

func disableGPUDriverOperands(ctx context.Context, policies *nvidiagpuv1.ClusterPolicyList) {
	patch := []byte(`{"spec":{"driver":{"enabled":false},"devicePlugin":{"enabled":false},"dcgm":{"enabled":false},"dcgmExporter":{"enabled":false},"gfd":{"enabled":false}}}`)
	for i := range policies.Items {
		policy := &policies.Items[i]
		Expect(inittools.APIClient.Patch(ctx, policy, goclient.RawPatch(types.MergePatchType, patch))).To(Succeed(),
			"cannot disable GPU driver operands in ClusterPolicy %s", policy.Name)
	}
	Eventually(ctx, func(ctx context.Context) error {
		pods, err := inittools.APIClient.Pods("").List(ctx, metav1.ListOptions{LabelSelector: "app=nvidia-driver-daemonset"})
		if err != nil {
			return err
		}
		if len(pods.Items) != 0 {
			return fmt.Errorf("waiting for %d GPU driver pods to terminate", len(pods.Items))
		}
		return nil
	}, 10*time.Minute, 5*time.Second).Should(Succeed(), "previous GPU driver pods did not terminate")
}

func deleteRdmaTestPods(ctx context.Context, workloadNamespace string) {
	if workloadNamespace == UndefinedValue || workloadNamespace == "" {
		glog.V(networkparams.LogLevel).Info("RDMA workload namespace is not set, no RDMA test pods to delete")
		return
	}
	pods, err := inittools.APIClient.Pods(workloadNamespace).List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "error listing pods in namespace %s", workloadNamespace)
	var deleted []string
	for _, pod := range pods.Items {
		if !hasAnyPrefix(pod.Name, rdmaTestPodNamePrefixes) {
			continue
		}
		glog.V(networkparams.LogLevel).Infof("Deleting RDMA test pod %s in namespace %s", pod.Name, workloadNamespace)
		err := inittools.APIClient.Pods(workloadNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if !k8serrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "error deleting RDMA test pod %s", pod.Name)
		}
		deleted = append(deleted, pod.Name)
	}
	for _, name := range deleted {
		Eventually(ctx, func(ctx context.Context) bool {
			_, err := inittools.APIClient.Pods(workloadNamespace).Get(ctx, name, metav1.GetOptions{})
			return k8serrors.IsNotFound(err)
		}, 5*time.Minute, 5*time.Second).Should(BeTrue(), "RDMA test pod %s was not deleted", name)
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func deleteNicClusterPolicyIfExists(ctx context.Context, name, driverNamespace string, timeout time.Duration) {
	existing := &nvidianetworkv1alpha1.NicClusterPolicy{}
	err := inittools.APIClient.Get(ctx, goclient.ObjectKey{Name: name}, existing)
	if !k8serrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred(), "cannot inspect existing NicClusterPolicy")
		glog.V(networkparams.LogLevel).Infof("Deleting existing NicClusterPolicy %s before recreate", name)
		err = inittools.APIClient.Delete(ctx, existing)
		if !k8serrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "error deleting existing NicClusterPolicy %s", name)
		}
	}
	Eventually(ctx, func(ctx context.Context) error {
		policy := &nvidianetworkv1alpha1.NicClusterPolicy{}
		err := inittools.APIClient.Get(ctx, goclient.ObjectKey{Name: name}, policy)
		if !k8serrors.IsNotFound(err) {
			if err != nil {
				return err
			}
			return fmt.Errorf("NicClusterPolicy %s still exists", name)
		}
		pods, err := inittools.APIClient.Pods(driverNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, pod := range pods.Items {
			// Labels also identify lingering pods when a previous cleanup already
			// deleted the policy and its image is no longer available.
			if _, ofedDriver := pod.Labels[networkconsts.OfedDriverLabel]; ofedDriver {
				return fmt.Errorf("waiting for previous driver pod %s/%s to stop", pod.Namespace, pod.Name)
			}
			if driver := existing.Spec.OFEDDriver; driver != nil && driver.Image != "" {
				for _, container := range pod.Spec.Containers {
					if strings.Contains(container.Image, "/"+driver.Image+":") ||
						strings.Contains(container.Image, "/"+driver.Image+"@") {
						return fmt.Errorf("waiting for previous driver pod %s/%s to stop", pod.Namespace, pod.Name)
					}
				}
			}
		}
		return nil
	}, timeout, 5*time.Second).Should(Succeed(), "previous driver must stop before inventory cleanup")
}
