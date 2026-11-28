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

	glog.V(100).Infof("Reboot pod %s created on node %s, waiting briefly for command to execute", podName, nodeName)
	time.Sleep(10 * time.Second)

	// Best-effort cleanup: the pod may already be gone if the node rebooted quickly.
	_ = apiClient.Pods(rebootPodNamespace).Delete(
		context.TODO(), podName, metav1.DeleteOptions{})

	return nil
}
