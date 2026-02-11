package shared

// TestNames provides consistent naming for DRA test objects.
type TestNames struct {
	prefix string
}

// NewTestNames creates a TestNames helper with the given prefix.
func NewTestNames(prefix string) *TestNames {
	return &TestNames{prefix: prefix}
}

// Namespace returns the namespace name.
func (n *TestNames) Namespace() string {
	return n.prefix + "-ns"
}

// ClaimTemplate returns the ResourceClaimTemplate name.
func (n *TestNames) ClaimTemplate() string {
	return n.prefix + "-claim-tpl"
}

// Pod returns the pod name.
func (n *TestNames) Pod() string {
	return n.prefix + "-pod"
}

// Claim returns the resource claim name.
func (n *TestNames) Claim() string {
	return n.prefix + "-claim"
}

// ComputeDomain returns the compute domain name.
func (n *TestNames) ComputeDomain() string {
	return n.prefix + "-domain"
}
