package nfd

import "time"

func GetAllowedOSLabels() []string {
	return []string{"rhcos", "rhel"}
}

const (
	CustomNFDCatalogSourcePublisherName = "Red Hat"
	CustomCatalogSourceDisplayName      = "Redhat Operators Custom"
	OSLabel                             = "feature.node.kubernetes.io/system-os_release.ID"
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

	DeletionPollInterval    = 30 * time.Second
	DeletionTimeoutDuration = 5 * time.Minute
)
