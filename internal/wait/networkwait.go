package wait

import (
	"context"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidianetwork"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"k8s.io/apimachinery/pkg/util/wait"

	networkoperator "github.com/Mellanox/network-operator/api/v1alpha1"
)

// NicClusterPolicyReady Waits until nicClusterPolicy is Ready.
func NicClusterPolicyReady(apiClient *clients.Settings, nicClusterPolicyName string, pollInterval,
	timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			nicClusterPolicy, err := nvidianetwork.Pull(apiClient, nicClusterPolicyName)

			if err != nil {
				glog.V(networkparams.LogLevel).Infof("NicClusterPolicy pull from cluster error: %s\n", err)

				return false, err
			}

			glog.V(networkparams.LogLevel).Infof("NicClusterPolicy %s in now in %s state",
				nicClusterPolicy.Object.Name, nicClusterPolicy.Object.Status.State)

			// returns true, nil when NicClusterPolicy is ready, this exits out of the PollUntilContextTimeout()
			return nicClusterPolicy.Object.Status.State == networkoperator.StateReady, nil
		})
}
