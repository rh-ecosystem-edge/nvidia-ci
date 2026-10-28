package wait

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/sriovparams"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork"
	"k8s.io/apimachinery/pkg/util/wait"
)

// sriovSyncStatusSucceeded is the SriovNetworkNodeState.Status.SyncStatus value reported once the
// sriov-network-config-daemon has finished applying the desired SR-IOV configuration on a node
// (including, where required, a drain and reboot of the node).
const sriovSyncStatusSucceeded = "Succeeded"

// SriovNetworkNodeStateSynced waits until every node in nodeNames has a SriovNetworkNodeState in the
// given namespace reporting SyncStatus "Succeeded". This is what actually waits out the node
// drain/reboot that applying a SriovNetworkNodePolicy triggers.
func SriovNetworkNodeStateSynced(apiClient *clients.Settings, nodeNames []string, namespace string,
	pollInterval, timeout time.Duration) error {
	if len(nodeNames) == 0 {
		return fmt.Errorf("sriovNetworkNodeStateSynced: 'nodeNames' cannot be empty")
	}

	return wait.PollUntilContextTimeout(
		context.TODO(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			for _, nodeName := range nodeNames {
				nodeState, err := sriovnetwork.PullSriovNetworkNodeState(apiClient, nodeName, namespace)

				if err != nil {
					glog.V(sriovparams.LogLevel).Infof(
						"SriovNetworkNodeState for node '%s' pull from cluster error: %s\n", nodeName, err)

					return false, nil
				}

				glog.V(sriovparams.LogLevel).Infof("SriovNetworkNodeState for node '%s' is now in '%s' syncStatus",
					nodeState.Name, nodeState.Status.SyncStatus)

				if nodeState.Status.SyncStatus != sriovSyncStatusSucceeded {
					return false, nil
				}
			}

			return true, nil
		})
}
