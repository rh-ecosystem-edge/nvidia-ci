package wait

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/dra"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/deployment"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nodes"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidiagpu"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/olm"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

// stateReady and stateNotReady are the common "ready"/"notReady" values used by the
// .status.state field of ClusterPolicy, NVIDIADriver and GPUCluster.
const (
	stateReady    = "ready"
	stateNotReady = "notReady"
)

// ClusterPolicyReady Waits until clusterPolicy is Ready.
func ClusterPolicyReady(apiClient *clients.Settings, clusterPolicyName string, pollInterval, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			clusterPolicy, err := nvidiagpu.Pull(apiClient, clusterPolicyName)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy pull from cluster error: %s\n", err)

				return false, err
			}

			if clusterPolicy.Object != nil && clusterPolicy.Object.Status.State == stateReady {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy %s in now in %s state",
					clusterPolicy.Object.Name, clusterPolicy.Object.Status.State)

				// this exits out of the PollUntilContextTimeout()
				return true, nil
			}
			if clusterPolicy.Object == nil {
				glog.V(gpuparams.GpuLogLevel).Info("ClusterPolicy object is nil")
				return false, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy %s in now in %s state",
				clusterPolicy.Object.Name, clusterPolicy.Object.Status.State)

			return false, nil
		})
}

// ClusterPolicyNotReady Waits until clusterPolicy is NotReady.
func ClusterPolicyNotReady(apiClient *clients.Settings, clusterPolicyName string, pollInterval,
	timeout time.Duration) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("wait.ClusterPolicyNotReady: %s", clusterPolicyName)
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			clusterPolicy, err := nvidiagpu.Pull(apiClient, clusterPolicyName)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy pull from cluster error: %s\n", err)

				return false, err
			}

			if clusterPolicy.Object != nil && clusterPolicy.Object.Status.State == stateNotReady {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy %s is now in %s state",
					clusterPolicy.Object.Name, clusterPolicy.Object.Status.State)

				// this exits out of the PollUntilContextTimeout()
				return true, nil
			}
			if clusterPolicy.Object == nil {
				glog.V(gpuparams.GpuLogLevel).Info("ClusterPolicy object is nil")
				return false, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy %s is currently in %s state",
				clusterPolicy.Object.Name, clusterPolicy.Object.Status.State)

			return false, nil
		})
}

// NVIDIADriverReady waits until the named NVIDIADriver reaches the "ready" state.
func NVIDIADriverReady(apiClient *clients.Settings, nvidiaDriverName string, pollInterval, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			nvidiaDriverBuilder, err := nvidiagpu.PullNVIDIADriver(apiClient, nvidiaDriverName)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("NVIDIADriver pull from cluster error: %s\n", err)

				return false, nil
			}

			if nvidiaDriverBuilder.Object != nil && nvidiaDriverBuilder.Object.Status.State == stateReady {
				glog.V(gpuparams.GpuLogLevel).Infof("NVIDIADriver %s is now in %s state",
					nvidiaDriverBuilder.Object.Name, nvidiaDriverBuilder.Object.Status.State)

				return true, nil
			}

			if nvidiaDriverBuilder.Object == nil {
				glog.V(gpuparams.GpuLogLevel).Info("NVIDIADriver object is nil")

				return false, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("NVIDIADriver %s is currently in %s state",
				nvidiaDriverBuilder.Object.Name, nvidiaDriverBuilder.Object.Status.State)

			return false, nil
		})
}

// GPUClusterReady waits until the named GPUCluster reaches the "ready" state.
func GPUClusterReady(apiClient *clients.Settings, gpuClusterName string, pollInterval, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			gpuClusterBuilder, err := nvidiagpu.PullGPUCluster(apiClient, gpuClusterName)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("GPUCluster pull from cluster error: %s\n", err)

				return false, nil
			}

			state := gpuClusterBuilder.State()
			if state == stateReady {
				glog.V(gpuparams.GpuLogLevel).Infof("GPUCluster %s is now in %s state", gpuClusterName, state)

				return true, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("GPUCluster %s is currently in %q state", gpuClusterName, state)

			return false, nil
		})
}

// CSVSucceeded waits for a defined period of time for CSV to be in Succeeded state.
func CSVSucceeded(apiClient *clients.Settings, csvName, csvNamespace string, pollInterval,
	timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			csvPulled, err := olm.PullClusterServiceVersion(apiClient, csvName, csvNamespace)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterServiceVersion pull from cluster error: %s\n", err)

				return false, err
			}

			if csvPulled.Object.Status.Phase == "Succeeded" {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterServiceVersion %s in now in %s state",
					csvPulled.Object.Name, csvPulled.Object.Status.Phase)

				// this exists out of the wait.PollImmediate().
				return true, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("clusterPolicy %s in now in %s state",
				csvPulled.Object.Name, csvPulled.Object.Status.Phase)

			return false, err
		})
}

// DeploymentCreated waits for a defined period of time for deployment to be created.
func DeploymentCreated(apiClient *clients.Settings, deploymentName, deploymentNamespace string, pollInterval,
	timeout time.Duration) bool {
	err := wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			deploymentPulled, err := deployment.Pull(apiClient, deploymentName, deploymentNamespace)
			if err != nil {
				// Missing Deployment is expected until OLM creates it; keep polling only for NotFound.
				if apierrors.IsNotFound(err) {
					glog.V(gpuparams.GpuLogLevel).Infof(
						"Deployment '%s' not yet present in namespace '%s': %v",
						deploymentName, deploymentNamespace, err)
					return false, nil
				}
				glog.V(gpuparams.GpuLogLevel).Infof(
					"Deployment '%s' pull from cluster namespace '%s' error: %v",
					deploymentName, deploymentNamespace, err)
				return false, err
			}

			if deploymentPulled.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("Deployment '%s' in namespace '%s' has been created",
					deploymentPulled.Object.Name, deploymentNamespace)
				return true, nil
			}

			return false, nil
		})

	return err == nil
}

// NodeLabelExists waits for at least one node with the specified label selector to have a label with the given key and value.
func NodeLabelExists(apiClient *clients.Settings, labelKey, labelValue string, nodeSelector labels.Set, pollInterval,
	timeout time.Duration) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("Waiting for node label '%s'='%s' on nodes with selector: %v", labelKey, labelValue, nodeSelector)
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			nodeBuilders, err := nodes.List(apiClient, metav1.ListOptions{LabelSelector: nodeSelector.String()})

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("Error listing nodes: %v", err)

				return false, err
			}

			for _, node := range nodeBuilders {
				glog.V(gpuparams.Gpu10LogLevel).Infof("Checking node '%s' for label '%s'", node.Object.Name, labelKey)
				if value, ok := node.Object.Labels[labelKey]; ok && value == labelValue {
					glog.V(gpuparams.Gpu100LogLevel).Infof("Found label '%s' with value '%s' on node '%s'", labelKey, labelValue, node.Object.Name)

					// this exits out of the PollUntilContextTimeout()
					return true, nil
				} else {
					glog.V(gpuparams.Gpu10LogLevel).Infof("Label '%s'='%s' not found on node '%s'", labelKey, labelValue, node.Object.Name)
					return false, nil
				}
			}

			glog.V(gpuparams.Gpu10LogLevel).Infof("Label '%s'='%s' not found yet, retrying...", labelKey, labelValue)

			return false, nil
		})
}

// WaitForNodes waits for nodes matching the selector to satisfy the condition function.
func WaitForNodes(apiClient *clients.Settings, nodeSelector labels.Set, condition func(*corev1.Node) (bool, error), pollInterval, timeout time.Duration) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("Waiting for nodes with selector: %v", nodeSelector)

	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			nodeBuilders, err := nodes.List(apiClient, metav1.ListOptions{
				LabelSelector: nodeSelector.String(),
			})

			if err != nil {
				return false, fmt.Errorf("error listing nodes: %w", err)
			}

			if len(nodeBuilders) == 0 {
				return false, fmt.Errorf("no nodes found matching selector %v", nodeSelector)
			}

			for _, nodeBuilder := range nodeBuilders {
				satisfied, err := condition(nodeBuilder.Object)
				if err != nil {
					return false, fmt.Errorf("failed to check node %s: %w", nodeBuilder.Object.Name, err)
				}

				if !satisfied {
					return false, nil
				}
				glog.V(gpuparams.GpuLogLevel).Infof("Node %s satisfies the required condition", nodeBuilder.Object.Name)
			}

			glog.V(gpuparams.GpuLogLevel).Info("All nodes satisfy the required condition")
			return true, nil
		})
}

// DaemonSetReady waits for a specific DaemonSet to have all pods ready.
func DaemonSetReady(apiClient *clients.Settings, daemonSetName, namespace string, pollInterval, timeout time.Duration) error {
	glog.V(gpuparams.Gpu10LogLevel).Infof("Waiting for DaemonSet '%s' in namespace '%s' to be ready", daemonSetName, namespace)
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			ds, err := apiClient.DaemonSets(namespace).Get(ctx, daemonSetName, metav1.GetOptions{})

			if err != nil {
				return false, fmt.Errorf("error getting DaemonSet '%s' in namespace '%s': %w", daemonSetName, namespace, err)
			}

			// Verify the generation observed by the DaemonSet controller matches the spec generation
			if ds.Status.ObservedGeneration != ds.Generation {
				glog.V(gpuparams.GpuLogLevel).Infof("DaemonSet '%s' in namespace '%s': ObservedGeneration %d != Generation %d",
					daemonSetName, namespace, ds.Status.ObservedGeneration, ds.Generation)
				return false, nil
			}

			// Make sure all the updated pods have been scheduled
			if ds.Status.UpdatedNumberScheduled != ds.Status.DesiredNumberScheduled {
				glog.V(gpuparams.GpuLogLevel).Infof("DaemonSet '%s' in namespace '%s': %d/%d pods updated",
					daemonSetName, namespace, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled)
				return false, nil
			}

			// Verify all nodes have available pods (ready for at least minReadySeconds)
			// NumberAvailable only counts nodes with the current revision's pods that are available,
			// unlike NumberReady which can include old revision pods during rolling updates
			available := ds.Status.NumberAvailable
			desired := ds.Status.DesiredNumberScheduled

			glog.V(gpuparams.GpuLogLevel).Infof("DaemonSet '%s' in namespace '%s': %d/%d pods available",
				daemonSetName, namespace, available, desired)

			if desired > 0 && available == desired {
				glog.V(gpuparams.GpuLogLevel).Infof("DaemonSet '%s' in namespace '%s' is now ready",
					daemonSetName, namespace)
				return true, nil
			}

			return false, nil
		})
}

// GPUResourceSliceUUIDsMatch waits until the gpu.nvidia.com ResourceSlice for the given
// node reports exactly the given set of GPU UUIDs. ResourceSlice publication lags
// DaemonSet readiness (NVML discovery + building attributes + the Create call all take
// non-zero time after the kubelet-plugin pod flips Ready), so this polls rather than
// doing a one-shot comparison.
func GPUResourceSliceUUIDsMatch(apiClient *clients.Settings, nodeName string, expectedUUIDs map[string]bool,
	pollInterval, timeout time.Duration) error {
	var lastErr error

	pollErr := wait.PollUntilContextTimeout(context.TODO(), pollInterval, timeout, true,
		func(ctx context.Context) (bool, error) {
			reported, err := gpuResourceSliceUUIDs(apiClient, nodeName)
			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("ResourceSlice not ready yet on node '%s': %v", nodeName, err)
				lastErr = err

				return false, nil
			}

			if !maps.Equal(reported, expectedUUIDs) {
				lastErr = fmt.Errorf("ResourceSlice UUIDs %v do not match expected UUIDs %v on node '%s'",
					slices.Sorted(maps.Keys(reported)), slices.Sorted(maps.Keys(expectedUUIDs)), nodeName)

				return false, nil
			}

			return true, nil
		})
	if pollErr != nil {
		return fmt.Errorf("ResourceSlice inventory for node '%s' never matched expected UUIDs: %w", nodeName, lastErr)
	}

	return nil
}

// gpuResourceSliceUUIDs lists ResourceSlices published by the gpu.nvidia.com driver for
// the given node and returns the set of UUIDs published in each Device's "uuid"
// attribute.
func gpuResourceSliceUUIDs(apiClient *clients.Settings, nodeName string) (map[string]bool, error) {
	fieldSelector := fmt.Sprintf("%s=%s,%s=%s",
		resourcev1.ResourceSliceSelectorNodeName, nodeName,
		resourcev1.ResourceSliceSelectorDriver, dra.GPUDriverName)

	slicesList, err := apiClient.K8sClient.ResourceV1().ResourceSlices().List(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ResourceSlices for node '%s': %w", nodeName, err)
	}

	if len(slicesList.Items) == 0 {
		return nil, fmt.Errorf("no ResourceSlice found for driver '%s' on node '%s'", dra.GPUDriverName, nodeName)
	}

	uuids := make(map[string]bool)

	for _, slice := range slicesList.Items {
		for _, device := range slice.Spec.Devices {
			attr, ok := device.Attributes[resourcev1.QualifiedName(dra.UUIDAttributeName)]
			if !ok || attr.StringValue == nil {
				return nil, fmt.Errorf("device '%s' in ResourceSlice '%s' has no '%s' string attribute",
					device.Name, slice.Name, dra.UUIDAttributeName)
			}

			uuids[*attr.StringValue] = true
		}
	}

	return uuids, nil
}
