package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SriovOperatorConfigSpec defines the desired state of SriovOperatorConfig.
// Trimmed from upstream SriovOperatorConfigSpec to the fields this repo sets.
type SriovOperatorConfigSpec struct {
	// ConfigDaemonNodeSelector selects the nodes to be configured.
	ConfigDaemonNodeSelector map[string]string `json:"configDaemonNodeSelector,omitempty"`
	// EnableInjector controls whether the network resource injector webhook is deployed.
	EnableInjector bool `json:"enableInjector,omitempty"`
	// EnableOperatorWebhook controls whether the operator admission controller webhook is deployed.
	EnableOperatorWebhook bool `json:"enableOperatorWebhook,omitempty"`
	// LogLevel controls the log verbosity of the operator.
	LogLevel int `json:"logLevel,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovOperatorConfigSpec) DeepCopyInto(out *SriovOperatorConfigSpec) {
	*out = *in

	if in.ConfigDaemonNodeSelector != nil {
		out.ConfigDaemonNodeSelector = make(map[string]string, len(in.ConfigDaemonNodeSelector))
		for k, v := range in.ConfigDaemonNodeSelector {
			out.ConfigDaemonNodeSelector[k] = v
		}
	}
}

// SriovOperatorConfigStatus defines the observed state of SriovOperatorConfig.
type SriovOperatorConfigStatus struct {
	// Injector shows the runtime status of the network resource injector webhook.
	Injector string `json:"injector,omitempty"`
	// OperatorWebhook shows the runtime status of the operator admission controller webhook.
	OperatorWebhook string `json:"operatorWebhook,omitempty"`
}

// SriovOperatorConfig is the Schema for the sriovoperatorconfigs API.
type SriovOperatorConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SriovOperatorConfigSpec   `json:"spec,omitempty"`
	Status SriovOperatorConfigStatus `json:"status,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovOperatorConfig) DeepCopyInto(out *SriovOperatorConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy creates a new SriovOperatorConfig with the same content as the receiver.
func (in *SriovOperatorConfig) DeepCopy() *SriovOperatorConfig {
	if in == nil {
		return nil
	}

	out := new(SriovOperatorConfig)
	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject implements runtime.Object.
func (in *SriovOperatorConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}

	return nil
}

// SriovOperatorConfigList contains a list of SriovOperatorConfig.
type SriovOperatorConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SriovOperatorConfig `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (in *SriovOperatorConfigList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}

	out := new(SriovOperatorConfigList)
	out.TypeMeta = in.TypeMeta
	in.DeepCopyInto(&out.ListMeta)

	if in.Items != nil {
		out.Items = make([]SriovOperatorConfig, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return out
}

// SriovNetworkNicSelector selects the NICs to be configured.
// Trimmed from upstream SriovNetworkNicSelector to the fields this repo sets.
type SriovNetworkNicSelector struct {
	// Vendor is the vendor hex code of the SR-IOV device, e.g. "15b3".
	Vendor string `json:"vendor,omitempty"`
	// PfNames lists the PF interface names to select.
	PfNames []string `json:"pfNames,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovNetworkNicSelector) DeepCopyInto(out *SriovNetworkNicSelector) {
	*out = *in

	if in.PfNames != nil {
		out.PfNames = make([]string, len(in.PfNames))
		copy(out.PfNames, in.PfNames)
	}
}

// SriovNetworkNodePolicySpec defines the desired state of SriovNetworkNodePolicy.
// Trimmed from upstream SriovNetworkNodePolicySpec to the fields this repo sets.
type SriovNetworkNodePolicySpec struct {
	// ResourceName is the SR-IOV network device plugin endpoint resource name.
	ResourceName string `json:"resourceName"`
	// NodeSelector selects the nodes to be configured.
	NodeSelector map[string]string `json:"nodeSelector"`
	// Priority of the policy, higher priority policies can override lower ones.
	Priority int `json:"priority,omitempty"`
	// Mtu of the VF.
	Mtu int `json:"mtu,omitempty"`
	// NumVfs is the number of VFs to create for each PF.
	NumVfs int `json:"numVfs"`
	// NicSelector selects the NICs to be configured.
	NicSelector SriovNetworkNicSelector `json:"nicSelector"`
	// DeviceType is the driver type for configured VFs ("netdevice" or "vfio-pci").
	DeviceType string `json:"deviceType,omitempty"`
	// IsRdma enables RDMA mode.
	IsRdma bool `json:"isRdma,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovNetworkNodePolicySpec) DeepCopyInto(out *SriovNetworkNodePolicySpec) {
	*out = *in

	if in.NodeSelector != nil {
		out.NodeSelector = make(map[string]string, len(in.NodeSelector))
		for k, v := range in.NodeSelector {
			out.NodeSelector[k] = v
		}
	}

	in.NicSelector.DeepCopyInto(&out.NicSelector)
}

// SriovNetworkNodePolicyStatus defines the observed state of SriovNetworkNodePolicy.
// Upstream leaves this empty.
type SriovNetworkNodePolicyStatus struct{}

// SriovNetworkNodePolicy is the Schema for the sriovnetworknodepolicies API.
type SriovNetworkNodePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SriovNetworkNodePolicySpec   `json:"spec,omitempty"`
	Status SriovNetworkNodePolicyStatus `json:"status,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovNetworkNodePolicy) DeepCopyInto(out *SriovNetworkNodePolicy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy creates a new SriovNetworkNodePolicy with the same content as the receiver.
func (in *SriovNetworkNodePolicy) DeepCopy() *SriovNetworkNodePolicy {
	if in == nil {
		return nil
	}

	out := new(SriovNetworkNodePolicy)
	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject implements runtime.Object.
func (in *SriovNetworkNodePolicy) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}

	return nil
}

// SriovNetworkNodePolicyList contains a list of SriovNetworkNodePolicy.
type SriovNetworkNodePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SriovNetworkNodePolicy `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (in *SriovNetworkNodePolicyList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}

	out := new(SriovNetworkNodePolicyList)
	out.TypeMeta = in.TypeMeta
	in.DeepCopyInto(&out.ListMeta)

	if in.Items != nil {
		out.Items = make([]SriovNetworkNodePolicy, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return out
}

// SriovNetworkSpec defines the desired state of SriovNetwork.
// Trimmed from upstream SriovNetworkSpec to the fields this repo sets.
type SriovNetworkSpec struct {
	// NetworkNamespace is the namespace of the generated NetworkAttachmentDefinition.
	NetworkNamespace string `json:"networkNamespace,omitempty"`
	// ResourceName is the SR-IOV network device plugin endpoint resource name.
	ResourceName string `json:"resourceName"`
	// IPAM configuration to be used for this network.
	IPAM string `json:"ipam,omitempty"`
	// Vlan ID to assign for the VF. Defaults to 0.
	Vlan int `json:"vlan,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovNetworkSpec) DeepCopyInto(out *SriovNetworkSpec) {
	*out = *in
}

// SriovNetworkStatus defines the observed state of SriovNetwork. Upstream leaves this empty.
type SriovNetworkStatus struct{}

// SriovNetwork is the Schema for the sriovnetworks API.
type SriovNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SriovNetworkSpec   `json:"spec,omitempty"`
	Status SriovNetworkStatus `json:"status,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovNetwork) DeepCopyInto(out *SriovNetwork) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy creates a new SriovNetwork with the same content as the receiver.
func (in *SriovNetwork) DeepCopy() *SriovNetwork {
	if in == nil {
		return nil
	}

	out := new(SriovNetwork)
	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject implements runtime.Object.
func (in *SriovNetwork) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}

	return nil
}

// SriovNetworkList contains a list of SriovNetwork.
type SriovNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SriovNetwork `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (in *SriovNetworkList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}

	out := new(SriovNetworkList)
	out.TypeMeta = in.TypeMeta
	in.DeepCopyInto(&out.ListMeta)

	if in.Items != nil {
		out.Items = make([]SriovNetwork, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return out
}

// SriovNetworkNodeStateStatus defines the observed state of SriovNetworkNodeState.
// Trimmed from upstream SriovNetworkNodeStateStatus to the fields this repo reads.
type SriovNetworkNodeStateStatus struct {
	// SyncStatus reports the sync state of the SR-IOV configuration on the node,
	// e.g. "InProgress", "Succeeded", "Failed".
	SyncStatus string `json:"syncStatus,omitempty"`
	// LastSyncError holds the last error encountered while syncing, if any.
	LastSyncError string `json:"lastSyncError,omitempty"`
}

// SriovNetworkNodeState is the Schema for the sriovnetworknodestates API.
// It is read-only from this repo's perspective: the operator creates one per
// node (named identically to the Node) in its own namespace to report config
// sync status.
type SriovNetworkNodeState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status SriovNetworkNodeStateStatus `json:"status,omitempty"`
}

// DeepCopyInto copies the receiver into out.
func (in *SriovNetworkNodeState) DeepCopyInto(out *SriovNetworkNodeState) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Status = in.Status
}

// DeepCopy creates a new SriovNetworkNodeState with the same content as the receiver.
func (in *SriovNetworkNodeState) DeepCopy() *SriovNetworkNodeState {
	if in == nil {
		return nil
	}

	out := new(SriovNetworkNodeState)
	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject implements runtime.Object.
func (in *SriovNetworkNodeState) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}

	return nil
}

// SriovNetworkNodeStateList contains a list of SriovNetworkNodeState.
type SriovNetworkNodeStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SriovNetworkNodeState `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (in *SriovNetworkNodeStateList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}

	out := new(SriovNetworkNodeStateList)
	out.TypeMeta = in.TypeMeta
	in.DeepCopyInto(&out.ListMeta)

	if in.Items != nil {
		out.Items = make([]SriovNetworkNodeState, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return out
}
