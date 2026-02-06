package wait

import (
	"context"
	"time"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/nvidianetwork"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/networkparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"k8s.io/apimachinery/pkg/util/wait"

	networkoperator "github.com/Mellanox/network-operator/api/v1alpha1"
)

// NicClusterPolicyReady Waits until nicClusterPolicy is Ready.
func NicClusterPolicyReady(apiClient *clients.Settings, nicClusterPolicyName string, pollInterval, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			nicClusterPolicy, err := nvidianetwork.PullNicClusterPolicy(apiClient, nicClusterPolicyName)

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

// MacvlanNetworkReady Waits until macvlanNetwork is Ready.
func MacvlanNetworkReady(apiClient *clients.Settings, macvlanNetworkName string, pollInterval,
	timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			macVlanNetwork, err := nvidianetwork.PullMacvlanNetwork(apiClient, macvlanNetworkName)

			if err != nil {
				glog.V(networkparams.LogLevel).Infof("MacvlanNetwork pull from cluster error: %s\n", err)

				return false, err
			}

			glog.V(networkparams.LogLevel).Infof("MacvlanNetwork %s in now in %s state",
				macVlanNetwork.Object.Name, macVlanNetwork.Object.Status.State)

			// returns true, nil when MacvlanNetwork is ready, this exits out of the PollUntilContextTimeout()
			return macVlanNetwork.Object.Status.State == networkoperator.StateReady, nil
		})
}

// IPoIBNetworkReady Waits until ipoibNetwork is Ready.
func IPoIBNetworkReady(apiClient *clients.Settings, ipoibNetworkName string, pollInterval,
	timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			ipoIBNetwork, err := nvidianetwork.PullIPoIBNetwork(apiClient, ipoibNetworkName)

			if err != nil {
				glog.V(networkparams.LogLevel).Infof("IPoIBNetwork pull from cluster error: %s\n", err)

				return false, err
			}

			glog.V(networkparams.LogLevel).Infof("IPoIBNetwork %s in now in %s state",
				ipoIBNetwork.Object.Name, ipoIBNetwork.Object.Status.State)

			// returns true, nil when IPoIBNetwork is ready, this exits out of the PollUntilContextTimeout()
			return ipoIBNetwork.Object.Status.State == networkoperator.StateReady, nil
		})
}
