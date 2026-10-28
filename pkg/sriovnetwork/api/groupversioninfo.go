// Package api contains a hand-maintained, trimmed copy of the Custom Resource
// schema exposed by the upstream OpenShift SRIOV Network Operator
// (github.com/k8snetworkplumbingwg/sriov-network-operator, api/v1, Apache-2.0).
//
// It is not generated or vendored from the upstream module: the real api/v1
// package transitively imports github.com/openshift/machine-config-operator,
// which pulls in coreos/ignition, coreos/fcct, aws-sdk-go and other unrelated
// modules. This package only contains the fields this repo's test suites
// actually set or read.
package api

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "sriovnetwork.openshift.io", Version: "v1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(
		&SriovOperatorConfig{},
		&SriovOperatorConfigList{},
		&SriovNetworkNodePolicy{},
		&SriovNetworkNodePolicyList{},
		&SriovNetwork{},
		&SriovNetworkList{},
		&SriovNetworkNodeState{},
		&SriovNetworkNodeStateList{},
	)
}
