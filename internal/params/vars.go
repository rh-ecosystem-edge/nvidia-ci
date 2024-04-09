package params

// PrivilegedNSLabels represents privileged labels.
var PrivilegedNSLabels = map[string]string{
	"pod-security.kubernetes.io/audit":               "privileged",
	"pod-security.kubernetes.io/enforce":             "privileged",
	"pod-security.kubernetes.io/warn":                "privileged",
	"security.openshift.io/scc.podSecurityLabelSync": "false",
}
