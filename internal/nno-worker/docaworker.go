package nnoworker

import (
	"context"
	"fmt"
	"time"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateDocaWorkerPod(clientset *clients.Settings, mode, name, hostname, serverIP string) (*v1.Pod, error) {
	command := ""
	if mode == "server" {
		command = "ib_write_bw -R -T 41 -s 65536 -F -x 3 -m 4096 --report_gbits -q 16 -D 60 -d mlx5_1 -p 10000"
	} else {
		command = fmt.Sprintf("ib_write_bw -R -T 41 -s 65536 -F -x 3 -m 4096 --report_gbits -q 16 -D 60 -d mlx5_1 -p 10000 --source_ip %s --use_cuda=0", serverIP)
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app":  "ib-test",
				"role": mode, // 'server' or 'client'
			},
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": "hostdev-net",
			},
		},
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": hostname,
			},
			ServiceAccountName: "rdma",
			Containers: []v1.Container{
				{
					Name:  "hostdev-32-workload",
					Image: "quay.io/redhat_emp1/ecosys-nvidia/gpu-operator:tools",
					Command: []string{
						"sh",
						"-c",
						command,
					},
					SecurityContext: &v1.SecurityContext{
						Privileged: boolPtr(true),
						Capabilities: &v1.Capabilities{
							Add: []v1.Capability{"IPC_LOCK"},
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							"nvidia.com/gpu":     resource.MustParse("1"),
							"nvidia.com/hostdev": resource.MustParse("1"),
						},
						Requests: v1.ResourceList{
							"nvidia.com/gpu":     resource.MustParse("1"),
							"nvidia.com/hostdev": resource.MustParse("1"),
						},
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	return clientset.Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
}

// getServerIP fetches the IP of the server pod.
func GetServerIP(clientset *clients.Settings) (string, error) {
	for {
		pods, err := clientset.Pods("default").List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=ib-test,role=server",
		})
		if err != nil {
			return "", err
		}

		if len(pods.Items) > 0 && pods.Items[0].Status.PodIP != "" {
			return pods.Items[0].Status.PodIP, nil
		}

		fmt.Println("Waiting for server pod IP...")
		time.Sleep(2 * time.Second) // Wait and retry
	}
}

func boolPtr(b bool) *bool {
	return &b
}
