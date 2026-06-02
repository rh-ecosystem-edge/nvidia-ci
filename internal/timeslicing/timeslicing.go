package timeslicing

import (
	"fmt"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/configmap"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// CreateDevicePluginConfigMap creates a ConfigMap with the device plugin configuration for time-slicing.
func CreateDevicePluginConfigMap(apiClient *clients.Settings, replicas int,
	configMapName, configMapNamespace string, renameByDefault bool) (*configmap.Builder, error) {
	config := map[string]any{
		"version": "v1",
		"sharing": map[string]any{
			"timeSlicing": map[string]any{
				"renameByDefault":            renameByDefault,
				"failRequestsGreaterThanOne": false,
				"resources": []map[string]any{
					{
						"name":     "nvidia.com/gpu",
						"replicas": replicas,
					},
				},
			},
		},
	}

	yamlData, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal time-slicing config: %w", err)
	}

	devicePluginConfig := map[string]string{
		"plugin-config.yaml": string(yamlData),
	}

	configMapBuilder := configmap.NewBuilder(apiClient, configMapName, configMapNamespace)
	configMapBuilderWithData := configMapBuilder.WithData(devicePluginConfig)

	createdConfigMapBuilder, err := configMapBuilderWithData.Create()
	if err != nil {
		glog.V(gpuparams.GpuLogLevel).Infof(
			"error creating time-slicing ConfigMap %s in namespace %s: %v",
			configMapName, configMapNamespace, err)

		return nil, err
	}

	glog.V(gpuparams.GpuLogLevel).Infof(
		"Created time-slicing ConfigMap %s in namespace %s",
		createdConfigMapBuilder.Object.Name, createdConfigMapBuilder.Object.Namespace)

	return createdConfigMapBuilder, nil
}

// CreateClusterPolicyFromCSV creates a new ClusterPolicy from the CSV ALM example
// with device plugin configuration referencing the time-slicing ConfigMap.
func CreateClusterPolicyFromCSV(apiClient *clients.Settings,
	gpuOperatorNamespace, clusterPolicyName string) (*nvidiagpu.Builder, error) {
	glog.V(gpuparams.GpuLogLevel).Infof("Creating ClusterPolicy %s from CSV ALM example with time-slicing config",
		clusterPolicyName)

	csvList, err := olm.ListClusterServiceVersion(apiClient, gpuOperatorNamespace, metav1.ListOptions{
		LabelSelector: "operators.coreos.com/gpu-operator-certified.nvidia-gpu-operator",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get CSV: %w", err)
	}

	if len(csvList) == 0 {
		return nil, fmt.Errorf("no matching CSV found")
	}

	almExample, ok := csvList[0].Object.Annotations["alm-examples"]
	if !ok {
		return nil, fmt.Errorf("CSV does not contain alm-examples annotation")
	}

	clusterPolicy := nvidiagpu.NewBuilderFromObjectString(apiClient, almExample)
	clusterPolicy.Definition.Name = clusterPolicyName

	enabled := true
	clusterPolicy.Definition.Spec.DevicePlugin.Enabled = &enabled

	if clusterPolicy.Definition.Spec.DevicePlugin.Config == nil {
		clusterPolicy.Definition.Spec.DevicePlugin.Config = &nvidiagpuv1.DevicePluginConfig{}
	}

	clusterPolicy.Definition.Spec.DevicePlugin.Config.Name = "plugin-config"
	clusterPolicy.Definition.Spec.DevicePlugin.Config.Default = "plugin-config.yaml"

	createdPolicy, err := clusterPolicy.Create()
	if err != nil {
		return nil, fmt.Errorf("failed to create ClusterPolicy: %w", err)
	}

	glog.V(gpuparams.GpuLogLevel).Infof("Successfully created ClusterPolicy %s with time-slicing config",
		clusterPolicyName)

	return createdPolicy, nil
}

// CreateTimeSlicingTestPod returns a Pod spec configured for time-slicing validation.
func CreateTimeSlicingTestPod(podName, podNamespace, image string) *corev1.Pod {
	isTrue := true
	isFalse := false

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
			Labels: map[string]string{
				"app": "timeslicing-test-app",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   &isTrue,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "nvidia.com/gpu",
					Effect:   corev1.TaintEffectNoSchedule,
					Operator: corev1.TolerationOpExists,
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "cuda-vectoradd",
					Image: image,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &isFalse,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			NodeSelector: map[string]string{
				"nvidia.com/gpu.present":         "true",
				"node-role.kubernetes.io/worker": "",
			},
		},
	}
}
