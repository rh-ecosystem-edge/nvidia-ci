package nfd

import "time"

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

	NFDOperatorCheckInterval = 30 * time.Second
	NFDOperatorTimeout       = 5 * time.Minute
	resourceCRD              = "NodeFeatureDiscovery"
	LogLevel                 = 100
)
