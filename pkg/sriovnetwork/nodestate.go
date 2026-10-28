package sriovnetwork

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	sriovnetworkapi "github.com/rh-ecosystem-edge/nvidia-ci/pkg/sriovnetwork/api"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// PullSriovNetworkNodeState fetches the SriovNetworkNodeState for the given node from the cluster.
// SriovNetworkNodeState is read-only from this repo's perspective: the operator creates and reconciles
// one per node (named identically to the Node) in its own namespace to report SR-IOV config sync status.
func PullSriovNetworkNodeState(apiClient *clients.Settings, nodeName, nsname string) (
	*sriovnetworkapi.SriovNetworkNodeState, error) {
	glog.V(100).Infof("Pulling SriovNetworkNodeState for node %s in namespace %s", nodeName, nsname)

	if nodeName == "" {
		return nil, fmt.Errorf("sriovNetworkNodeState 'nodeName' cannot be empty")
	}

	if nsname == "" {
		return nil, fmt.Errorf("sriovNetworkNodeState 'nsname' cannot be empty")
	}

	nodeState := &sriovnetworkapi.SriovNetworkNodeState{}
	err := apiClient.Get(context.TODO(), goclient.ObjectKey{
		Name:      nodeName,
		Namespace: nsname,
	}, nodeState)

	if err != nil {
		glog.V(100).Infof("SriovNetworkNodeState object %s doesn't exist in namespace %s", nodeName, nsname)

		return nil, err
	}

	return nodeState, nil
}
