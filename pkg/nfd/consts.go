package nfd

const (
	CustomNFDCatalogSourcePublisherName = "Red Hat"
	CustomCatalogSourceDisplayName      = "Redhat Operators Custom"
	RhcosLabel                          = "feature.node.kubernetes.io/system-os_release.ID"
	RhcosLabelValue                     = "rhcos"
	OperatorNamespace                   = "openshift-nfd"
	CatalogSourceDefault                = "redhat-operators"
	CatalogSourceNamespace              = "openshift-marketplace"
	OperatorDeploymentName              = "nfd-controller-manager"
	Package                             = "nfd"
	CRName                              = "nfd-instance"

	resourceCRD = "NodeFeatureDiscovery"
	LogLevel    = 100
)
