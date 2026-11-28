package nodes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	rebootPodNamespace = "default"
	rebootPodImage     = "registry.access.redhat.com/ubi9/ubi-minimal:latest"
)

// RebootNode triggers a reboot of the specified node by creating a privileged
// pod that runs nsenter to execute "systemctl reboot" on the host. The pod is
// cleaned up after the reboot command is issued. The caller is responsible for
// waiting until the node goes NotReady and comes back Ready.
func RebootNode(apiClient *clients.Settings, nodeName string) error {
	glog.V(100).Infof("Triggering reboot on node %s", nodeName)

	podName := fmt.Sprintf("reboot-%s", strings.ReplaceAll(nodeName, ".", "-"))
	isTrue := true

	rebootPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: rebootPodNamespace,
		},
		Spec: corev1.PodSpec{
			HostPID:       true,
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "reboot",
					Image: rebootPodImage,
					Command: []string{
						"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "--",
						"sh", "-c", "systemctl reboot",
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &isTrue,
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
		},
	}

	_, err := apiClient.Pods(rebootPodNamespace).Create(
		context.TODO(), rebootPod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create reboot pod on node %s: %w", nodeName, err)
	}

	glog.V(100).Infof("Reboot pod %s created on node %s, waiting for command to execute", podName, nodeName)

	// Poll until the pod leaves Pending (container started, so the reboot
	// command was issued) or a timeout is reached. The pod will typically
	// transition to Running briefly before the host goes down, or straight
	// to Failed/Unknown once the node reboots underneath it.
	const (
		rebootPodTimeout  = 5 * time.Minute
		rebootPodInterval = 3 * time.Second
	)

	deadline := time.Now().Add(rebootPodTimeout)
	var lastPhase corev1.PodPhase

	for time.Now().Before(deadline) {
		p, getErr := apiClient.Pods(rebootPodNamespace).Get(
			context.TODO(), podName, metav1.GetOptions{})
		if getErr != nil {
			// Pod disappeared (node rebooted and pod was lost): reboot confirmed.
			glog.V(100).Infof("Reboot pod %s is no longer reachable, reboot likely in progress", podName)

			return nil
		}

		lastPhase = p.Status.Phase
		if lastPhase != corev1.PodPending {
			glog.V(100).Infof("Reboot pod %s reached phase %s, reboot command was issued", podName, lastPhase)

			break
		}

		time.Sleep(rebootPodInterval)
	}

	if lastPhase == corev1.PodPending {
		// Best-effort cleanup before returning the error.
		_ = apiClient.Pods(rebootPodNamespace).Delete(
			context.TODO(), podName, metav1.DeleteOptions{})

		return fmt.Errorf("reboot pod %s on node %s did not start within %s",
			podName, nodeName, rebootPodTimeout)
	}

	// Best-effort cleanup: the pod may already be gone if the node rebooted quickly.
	_ = apiClient.Pods(rebootPodNamespace).Delete(
		context.TODO(), podName, metav1.DeleteOptions{})

	return nil
}

// WaitForNodeReboot polls until the node's BootID differs from originalBootID
// and the node is Ready. This is the definitive way to confirm a reboot
// completed: BootID (from /proc/sys/kernel/random/boot_id) changes on every
// kernel boot.
func WaitForNodeReboot(apiClient *clients.Settings, nodeName, originalBootID string, timeout time.Duration) error {
	glog.V(100).Infof("Waiting for node %s to reboot (original BootID: %s)", nodeName, originalBootID)

	const pollInterval = 5 * time.Second

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		node, err := apiClient.CoreV1Interface.Nodes().Get(
			context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			glog.V(100).Infof("Node %s not reachable (expected during reboot): %v", nodeName, err)
			time.Sleep(pollInterval)

			continue
		}

		currentBootID := node.Status.NodeInfo.BootID
		if currentBootID == originalBootID {
			glog.V(100).Infof("Node %s BootID unchanged, reboot not yet complete", nodeName)
			time.Sleep(pollInterval)

			continue
		}

		glog.V(100).Infof("Node %s BootID changed: %s -> %s", nodeName, originalBootID, currentBootID)

		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				glog.V(100).Infof("Node %s is Ready after reboot", nodeName)

				return nil
			}
		}

		glog.V(100).Infof("Node %s has new BootID but is not yet Ready", nodeName)
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("node %s did not complete reboot within %s", nodeName, timeout)
}
