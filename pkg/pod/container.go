package pod

import (
	"errors"
	"slices"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
)

var (
	// AllowedSCList list of allowed SecurityCapabilities.
	AllowedSCList          = []string{"NET_RAW", "NET_ADMIN", "SYS_ADMIN", "IPC_LOCK", "ALL"}
	falseVar               = false
	trueVar                = true
	capabilityAll          = []corev1.Capability{"ALL"}
	defaultGroupID         = int64(3000)
	defaultUserID          = int64(2000)
	defaultSecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: &falseVar,
		RunAsNonRoot:             &trueVar,
		SeccompProfile:           &corev1.SeccompProfile{Type: "RuntimeDefault"},
		Capabilities: &corev1.Capabilities{
			Drop: capabilityAll,
		},
		RunAsGroup: &defaultGroupID,
		RunAsUser:  &defaultUserID,
	}
)

// ContainerBuilder provides a struct for container's object definition.
type ContainerBuilder struct {
	// Container definition, used to create the Container object.
	definition *corev1.Container
	// Used to store latest error message upon defining or mutating container definition.
	errorMsg string
}

// NewContainerBuilder creates a new instance of ContainerBuilder.
func NewContainerBuilder(name, image string, cmd []string) *ContainerBuilder {
	glog.V(100).Infof("Initializing new container structure with the following params: "+
		"name: %s, image: %s, cmd: %v", name, image, cmd)

	builder := &ContainerBuilder{
		definition: &corev1.Container{
			Name:            name,
			Image:           image,
			Command:         cmd,
			SecurityContext: defaultSecurityContext,
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the container is empty")

		builder.errorMsg = "container's name is empty"
	}

	if image == "" {
		glog.V(100).Infof("Container's image is empty")

		builder.errorMsg = "container's image is empty"
	}

	if len(cmd) < 1 {
		glog.V(100).Infof("Container's cmd is empty")

		builder.errorMsg = "container's cmd is empty"
	}

	return builder
}

// WithSecurityCapabilities applies SecurityCapabilities to the container definition.
func (builder *ContainerBuilder) WithSecurityCapabilities(sCapabilities []string, redefine bool) *ContainerBuilder {
	glog.V(100).Infof("Applying a list of SecurityCapabilities %v to container %s",
		sCapabilities, builder.definition.Name)

	if builder.definition.SecurityContext != nil {
		if !redefine {
			glog.V(100).Infof("Cannot modify pre-existing SecurityContext")

			builder.errorMsg = "can not modify pre-existing security context"
		}

		builder.definition.SecurityContext = nil
	}

	if !areCapabilitiesValid(sCapabilities) {
		glog.V(100).Infof("Given SecurityCapabilities %v are not valid. Valid list %s",
			sCapabilities, AllowedSCList)

		builder.errorMsg = "one of the give securityCapabilities is invalid. Please extend allowed list or fix parameter"
	}

	if builder.errorMsg != "" {
		return builder
	}

	var sCapabilitiesList []corev1.Capability
	for _, capability := range sCapabilities {
		sCapabilitiesList = append(sCapabilitiesList, corev1.Capability(capability))
	}

	builder.definition.SecurityContext = &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Add: sCapabilitiesList,
		},
	}

	return builder
}

// WithDropSecurityCapabilities drops SecurityCapabilities from the container definition.
func (builder *ContainerBuilder) WithDropSecurityCapabilities(sCapabilities []string, redefine bool) *ContainerBuilder {
	glog.V(100).Infof("Dropping a list of SecurityCapabilities %v from container %s",
		sCapabilities, builder.definition.Name)

	if !areCapabilitiesValid(sCapabilities) {
		glog.V(100).Infof("Given SecurityCapabilities %v are not valid. Valid list %s",
			sCapabilities, AllowedSCList)

		builder.errorMsg = "one of the provided securityCapabilities is invalid. " +
			"Please extend the allowed list or fix parameter"

		return builder
	}

	var sCapabilitiesList []corev1.Capability
	for _, capability := range sCapabilities {
		sCapabilitiesList = append(sCapabilitiesList, corev1.Capability(capability))
	}

	// filter possible duplicated capabilities from user's input
	sCapabilitiesList = uniqueCapabilities(sCapabilitiesList)
	glog.V(100).Infof("Filtered user input: %v", sCapabilitiesList)

	// filter conflicting capabilities between ADD and DROP
	if builder.definition.SecurityContext != nil &&
		builder.definition.SecurityContext.Capabilities != nil &&
		builder.definition.SecurityContext.Capabilities.Add != nil {
		glog.V(100).Infof("Filtering conflicting options between ADD and DROP capabilities")

		confCapabilitiesList := capabilitiesIntersection(
			builder.definition.SecurityContext.Capabilities.Add,
			sCapabilitiesList)

		if len(confCapabilitiesList) > 0 {
			glog.V(100).Infof("Conflicting ADD and DROP capabilities")

			for _, mcap := range confCapabilitiesList {
				glog.V(100).Infof("SecurityCapability %q already present in the Capabilities.Add list", mcap)
			}

			builder.errorMsg = "Conflicting ADD and DROP SecurityCapabilities"

			return builder
		}
	}

	if builder.definition.SecurityContext == nil {
		glog.V(100).Infof("SecurityContext is nil. Initializing one")

		builder.definition.SecurityContext = new(corev1.SecurityContext)
	}

	if builder.definition.SecurityContext.Capabilities == nil {
		glog.V(100).Infof("Capabilities are nil. Initializing one")

		builder.definition.SecurityContext.Capabilities = new(corev1.Capabilities)
	}

	if !redefine {
		glog.V(100).Infof("SecurityContext.Capabilities will not be redefined")
		glog.V(100).Infof("Filtering duplicated DROP capabilities - %v", sCapabilitiesList)
		sCapabilitiesList = capabilitiesDifference(builder.definition.SecurityContext.Capabilities.Drop,
			sCapabilitiesList)

		glog.V(100).Infof("Updating existing SecurityContext.Capabilities.Drop list with %v", sCapabilitiesList)
		builder.definition.SecurityContext.Capabilities.Drop = append(builder.definition.SecurityContext.Capabilities.Drop,
			sCapabilitiesList...)
	} else {
		glog.V(100).Infof("Redefining existing SecurityContext.Capabilities.Drop list with %v", sCapabilitiesList)
		builder.definition.SecurityContext.Capabilities.Drop = sCapabilitiesList
	}

	return builder
}

// WithSecurityContext applies security Context on container.
func (builder *ContainerBuilder) WithSecurityContext(securityContext *corev1.SecurityContext) *ContainerBuilder {
	glog.V(100).Infof("Applying custom securityContext %v", securityContext)

	if securityContext == nil {
		glog.V(100).Infof("Cannot add empty securityContext to container structure")

		builder.errorMsg = "can not modify container config with empty securityContext"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.definition.SecurityContext = securityContext

	return builder
}

// WithResourceLimit applies resource limit on container.
func (builder *ContainerBuilder) WithResourceLimit(hugePages, memory string, cpu int64) *ContainerBuilder {
	glog.V(100).Infof("Applying custom resource limit to container: hugePages: %s memory: %s cpu: %d",
		hugePages, memory, cpu)

	if hugePages == "" {
		glog.V(100).Infof("Container's resource limit hugePages is empty")

		builder.errorMsg = "container's resource limit 'hugePages' is empty"
	}

	if memory == "" {
		glog.V(100).Infof("Container's resource limit memory is empty")

		builder.errorMsg = "container's resource limit 'memory' is empty"
	}

	if cpu <= 0 {
		glog.V(100).Infof("Container's resource limit cpu can not be zero or negative number.")

		builder.errorMsg = "container's resource limit 'cpu' is invalid"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.definition.Resources.Limits = corev1.ResourceList{
		"hugepages-1Gi": resource.MustParse(hugePages),
		"memory":        resource.MustParse(memory),
		"cpu":           *resource.NewQuantity(cpu, resource.DecimalSI),
	}

	return builder
}

// WithResourceRequest applies resource request on container.
func (builder *ContainerBuilder) WithResourceRequest(hugePages, memory string, cpu int64) *ContainerBuilder {
	glog.V(100).Infof("Applying custom resource request to container: hugePages: %s memory: %s cpu: %d",
		hugePages, memory, cpu)

	if hugePages == "" {
		glog.V(100).Infof("Container's resource request hugePages is empty")

		builder.errorMsg = "container's resource request 'hugePages' is empty"
	}

	if memory == "" {
		glog.V(100).Infof("Container's resource request memory is empty")

		builder.errorMsg = "container's resource request 'memory' is empty"
	}

	if cpu <= 0 {
		glog.V(100).Infof("Container's resource request cpu can not be zero or negative number.")

		builder.errorMsg = "container's resource request 'cpu' is invalid"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.definition.Resources.Requests = corev1.ResourceList{
		"hugepages-1Gi": resource.MustParse(hugePages),
		"memory":        resource.MustParse(memory),
		"cpu":           *resource.NewQuantity(cpu, resource.DecimalSI),
	}

	return builder
}

// WithCustomResourcesLimits applies custom resource limit struct on container.
func (builder *ContainerBuilder) WithCustomResourcesLimits(resourceList corev1.ResourceList) *ContainerBuilder {
	glog.V(100).Infof("Applying custom resource limit to container: %v", resourceList)

	if len(resourceList) == 0 {
		glog.V(100).Infof("Container's resource limit var 'resourceList' is empty")

		builder.errorMsg = "container's resource limit var 'resourceList' is empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.definition.Resources.Limits = resourceList

	return builder
}

// WithImagePullPolicy applies specific image pull policy on container.
func (builder *ContainerBuilder) WithImagePullPolicy(pullPolicy corev1.PullPolicy) *ContainerBuilder {
	glog.V(100).Infof("Applying image pull policy to container: %s", pullPolicy)

	if len(pullPolicy) == 0 {
		glog.V(100).Infof("Container's image pull policy 'pullPolicy' is empty")

		builder.errorMsg = "container's pull policy var 'pullPolicy' is empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.definition.ImagePullPolicy = pullPolicy

	return builder
}

// WithEnvVar adds environment variables to container.
func (builder *ContainerBuilder) WithEnvVar(name, value string) *ContainerBuilder {
	glog.V(100).Infof("Applying custom environment variables to container: name %s, value: %s", name, value)

	if name == "" {
		glog.V(100).Infof("Container's environment var 'name' is empty")

		builder.errorMsg = "container's environment var 'name' is empty"
	}

	if value == "" {
		glog.V(100).Infof("Container's environment var 'value' is empty")

		builder.errorMsg = "container's environment var 'value' is empty"
	}

	if builder.errorMsg != name {
		return builder
	}

	if builder.definition.Env != nil {
		builder.definition.Env = append(builder.definition.Env, corev1.EnvVar{Name: name, Value: value})

		return builder
	}

	builder.definition.Env = []corev1.EnvVar{{Name: name, Value: value}}

	return builder
}

// WithVolumeMount adds a pod volume mount inside the container.
func (builder *ContainerBuilder) WithVolumeMount(volMount corev1.VolumeMount) *ContainerBuilder {
	glog.V(100).Infof("Adding VolumeMount to the %s container's definition", builder.definition.Name)

	if volMount.Name == "" {
		glog.V(100).Infof("Container's VolumeMount name cannot be empty")

		builder.errorMsg = "container's volume mount name is empty"
	}

	if volMount.MountPath == "" {
		glog.V(100).Infof("Container's VolumeMount mount path cannot be empty")

		builder.errorMsg = "container's volume mount path is empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	glog.V(100).Infof("VolumeMount %s will be mounted at %s", volMount.Name, volMount.MountPath)
	builder.definition.VolumeMounts = append(builder.definition.VolumeMounts, volMount)

	return builder
}

// GetContainerCfg returns Container struct.
func (builder *ContainerBuilder) GetContainerCfg() (*corev1.Container, error) {
	glog.V(100).Infof("Returning configuration for container %s", builder.definition.Name)

	if builder.errorMsg != "" {
		glog.V(100).Infof("Failed to build container configuration due to %s", builder.errorMsg)

		return nil, errors.New(builder.errorMsg)
	}

	return builder.definition, nil
}

func areCapabilitiesValid(capabilities []string) bool {
	valid := true

	for _, capability := range capabilities {
		if !slices.Contains(AllowedSCList, capability) {
			valid = false
		}
	}

	return valid
}

// uniqueCapabilities filters duplicated Security Capabilities from the provided list.
func uniqueCapabilities(sCap []corev1.Capability) []corev1.Capability {
	uniqMap := make(map[corev1.Capability]bool)
	uniqSlice := []corev1.Capability{}

	for _, val := range sCap {
		uniqMap[val] = true
	}

	for key := range uniqMap {
		uniqSlice = append(uniqSlice, key)
	}

	return uniqSlice
}

// capabilitiesIntersection returns an intersection of 2 Security Capabilities lists.
func capabilitiesIntersection(aCaps, bCaps []corev1.Capability) []corev1.Capability {
	var resultCaps []corev1.Capability

	for _, bVal := range bCaps {
		found := false

		for _, aVal := range aCaps {
			if bVal == aVal {
				found = true

				break
			}
		}

		if found {
			resultCaps = append(resultCaps, bVal)
		}
	}

	return resultCaps
}

// capabilitiesDifference returns a difference of 2 Security Capabilities lists.
func capabilitiesDifference(aCaps, bCaps []corev1.Capability) []corev1.Capability {
	var resultCaps []corev1.Capability

	for _, bVal := range bCaps {
		found := false

		for _, aVal := range aCaps {
			if bVal == aVal {
				found = true

				break
			}
		}

		if !found {
			resultCaps = append(resultCaps, bVal)
		}
	}

	return resultCaps
}
