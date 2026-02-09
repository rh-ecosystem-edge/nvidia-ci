package testworkloads

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// NewUnprivilegedPod creates a pod with security best practices.
// Accepts a slice of containers to support both single and multi-container workloads.
func NewUnprivilegedPod(
	podName string,
	containers []corev1.Container,
	nodeSelector map[string]string,
	tolerations []corev1.Toleration,
	labels map[string]string,
) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   ptr.To(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Tolerations:  tolerations,
			Containers:   containers,
			NodeSelector: nodeSelector,
		},
	}
}

// NewUnprivilegedContainer creates a container with security best practices.
func NewUnprivilegedContainer(
	name string,
	image string,
	resources corev1.ResourceRequirements,
) corev1.Container {
	return corev1.Container{
		Name:            name,
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		Resources: resources,
	}
}
