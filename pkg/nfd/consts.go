package nfd

const (
	NfdCustomNFDCatalogSourcePublisherName = "Red Hat"
	NfdCustomCatalogSourceDisplayName      = "Redhat Operators Custom"
	NfdRhcosLabel                          = "feature.node.kubernetes.io/system-os_release.ID"
	NfdRhcosLabelValue                     = "rhcos"
	NfdOperatorNamespace                   = "openshift-nfd"
	NfdCatalogSourceDefault                = "redhat-operators"
	NfdCatalogSourceNamespace              = "openshift-marketplace"
	NfdOperatorDeploymentName              = "nfd-controller-manager"
	NfdPackage                             = "nfd"
	NfdCRName                              = "nfd-instance"

	//not related to NFD but common consts between gpu and nno
	UndefinedValue       = "undefined"
	OperatorVersionFile  = "operator.version"
	OpenShiftVersionFile = "ocp.version"
)
