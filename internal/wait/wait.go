package wait

import (
	"context"
	"time"

	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/nvidiagpu"
	"github.com/openshift-kni/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ClusterPolicyReady Waits until clusterPolicy is Ready.
func ClusterPolicyReady(apiClient *clients.Settings, clusterPolicyName string, pollInterval,
	timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			clusterPolicy, err := nvidiagpu.Pull(apiClient, clusterPolicyName)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy pull from cluster error: %s\n", err)

				return false, err
			}

			if clusterPolicy.Object.Status.State == "ready" {
				glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy %s in now in %s state",
					clusterPolicy.Object.Name, clusterPolicy.Object.Status.State)

				// this exists out of the wait.PollImmediate()
				return true, nil
			}

			glog.V(gpuparams.GpuLogLevel).Infof("ClusterPolicy %s in now in %s state",
				clusterPolicy.Object.Name, clusterPolicy.Object.Status.State)

			return false, err
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
	// Note: the value for boolean variable "immediate" is false here, meaning check AFTER polling interval
	//       on the very first try.  Otherwise the first check was causing an error and failing testcase.
	err := wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, false, func(ctx context.Context) (bool, error) {
			var err error
			deploymentPulled, err := deployment.Pull(apiClient, deploymentName, deploymentNamespace)

			if err != nil {
				glog.V(gpuparams.GpuLogLevel).Infof("Deployment '%s' pull from cluster namespace '%s' error:"+
					" %v", deploymentName, deploymentNamespace, err)

				return false, err
			}

			if deploymentPulled.Exists() {
				glog.V(gpuparams.GpuLogLevel).Infof("Deployment '%s' in namespace '%s' has been created",
					deploymentPulled.Object.Name, deploymentNamespace)

				// this exists out of the wait.PollImmediate().
				return true, nil
			}

			return false, nil
		})

	return err == nil
}
