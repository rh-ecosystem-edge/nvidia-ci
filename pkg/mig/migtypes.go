package mig

import (
	"flag"
	"fmt"
	"os"

	nvidiagpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
)

// MIGProfileInfo represents information about a MIG profile
type MIGProfileInfo struct {
	GpuID      int    // Physical GPU index
	MigType    string // always MIG, probably unnecessary
	MigName    string // e.g., 1g.5gb, 2g.10gb, 3g.20gb
	MigID      int    // Profile identifier used when creating instances
	Available  int    // number of available instances
	Total      int    // total number of instances
	Memory     string // memory in GB, need to be converted to float64
	P2P        string // Peer-to-peer support between instances (No = not supported)
	SM         int    // SM: Streaming Multiprocessors per instance (compute units)
	DEC        int    // DEC: Video decode units per instance
	ENC        int    // ENC: Video encode units per instance
	CE         int    // CE: Copy Engine units per instance (second row)
	JPEG       int    // JPEG: JPEG decoder units per instance (second row)
	OFA        int    // OFA: Optical Flow Accelerator units per instance (second row)
	Flavor     string // single strategy: nvidia.com/gpu or all-balanced: nvidia.com/mig-*
	MixedCnt   int    // number of instances to use for mixed strategy
	SliceUsage int    // number of slices used per instance
	MemUsage   int    // memory usage in GB per instance
}

type MigPodInfo struct {
	PodName        string         // name of the pod
	Namespace      string         // namespace of the pod
	Pod            *pod.Builder   // pod object
	MigProfileInfo MIGProfileInfo // MIG profile information
}

type TsPodInfo struct {
	PodName   string       // name of the pod
	Namespace string       // namespace of the pod
	Pod       *pod.Builder // pod object
	Checked   bool         // whether the pod has been checked
}

// PMonRow is one data row from "nvidia-smi pmon -c 1" output.
// Pid is 0 when the GPU slot has no process (pid column is "-").
type PMonRow struct {
	GpuIdx  int    // GPU index (0-based)
	Pid     int    // Process ID; 0 if no process
	Type    string // C/G or -
	Command string // Process name, e.g. gpu_burn
}

// ANSI color constants for console output highlighting
// colors are \033[31m - red through \033[37m - white
const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorCyan  = "\033[36m"
	colorGreen = "\033[32m"
	colorBold  = "\033[1m"
)

var useColors = os.Getenv("NO_COLOR") != "true"

// colorLog returns the message with the color if coloring is enabled (currently checking both env and CLI parameters)
func colorLog(color, message string) string {
	if !useColors || NoColor {
		return message
	}
	return fmt.Sprintf("%s%s%s", color, message, colorReset)
}

// Global variables for ginkgo CLI parameters and values derived from them
var (
	PodDelay          int
	SingleMigProfile  int
	MigInstances      string
	NoColor           bool
	MixedMigInstances []int
	TsPodCount        int
	TsInstancesCSV    string
	TsInstances       []int // first TsPodCount entries after validation; slices per pod
	TsMonAfterPod     int   // minimum number of pods running before monitoring starts
)

const (
	defaultMigInstances     int = -1 // parameter not provided
	defaultSingleMigProfile int = -2 // parameter not provided
)

const (
	MIGStrategySingle     = "single"
	MIGStrategyMixed      = "mixed"
	MaxTsSlices       int = 8 // max time-slice replicas per GPU (device plugin)
	// MaxTsInstance is the maximum sum of per-pod time-slice counts for one GPU.
	MaxTsInstance int = 100
	// defaultTsInstancesCSV has MaxTsSlices entries; the first TsPodCount values are used.
	defaultTsInstancesCSV = "8" // e.g. "8,1,3,2,6,5,8,8"
	//defaultTsMonAfterPod   int = 1 // start monitoring pods after 1 pod is running
)

func init() {
	// Register flags before Ginkgo parses them
	flag.IntVar(&PodDelay, "mixed.mig.pod-delay", 0, "delay in seconds between pod creation on mixed-mig testcase")
	flag.IntVar(&SingleMigProfile, "single.mig.profile", -2, "index of the MIG profile to be used for single-mig testcase")
	flag.StringVar(&MigInstances, "mixed.mig.instances", "-1", "comma-separated number of instances for mixed-mig testcase, defaults are for A100 GPU [2,0,1,1,0,0]")
	// flag.IntVar(&TsPodCount, "time.slicing.pod-count", 1, "number of gpu-burn pods for time-slicing testcase (1-8)")
	flag.StringVar(&TsInstancesCSV, "time.slicing.instances", defaultTsInstancesCSV, "comma-separated time-slice counts per pod; sum must not exceed MaxTsInstance (100)")
	flag.IntVar(&TsMonAfterPod, "time.slicing.mon-after-pod", 0, "minimum number of pods running before monitoring starts")
	flag.BoolVar(&NoColor, "no-color", false, "disable color output")
}

const (
	migConfigLabel      = "nvidia.com/mig.config"
	migConfigStateLabel = "nvidia.com/mig.config.state"
	migConfigDisabled   = "all-disabled"
)

const timeSlicingDevicePluginConfigMapName = "device-plugin-config"

const gpuProductSharedSuffix = "-SHARED"

const gpuProductLabelKey = "nvidia.com/gpu.product"

type timeSlicingConfigSnapshot struct {
	hadDevicePluginConfig bool
	devicePluginConfig    *nvidiagpuv1.DevicePluginConfig
	gfdEnabled            *bool
	configMapExisted      bool
	configMapData         map[string]string
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
