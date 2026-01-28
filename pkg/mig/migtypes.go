package mig

import (
	"os"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/pod"
)

// MIGProfileInfo represents information about a MIG profile
type MIGProfileInfo struct {
	GpuID        int      // Physical GPU index
	MigType      string   // always MIG, probably unnecessary
	MigName      string   // e.g., 1g.5gb, 2g.10gb, 3g.20gb
	MigID        int      // Profile identifier used when creating instances
	Available    int      // number of available instances
	Total        int      // total number of instances
	Memory       string   // memory in GB, need to be converted to float64
	P2P          string   // Peer-to-peer support between instances (No = not supported)
	SM           int      // SM: Streaming Multiprocessors per instance (compute units)
	DEC          int      // DEC: Video decode units per instance
	ENC          int      // ENC: Video encode units per instance
	CE           int      // CE: Copy Engine units per instance (second row)
	JPEG         int      // JPEG: JPEG decoder units per instance (second row)
	OFA          int      // OFA: Optical Flow Accelerator units per instance (second row)
	Flavor       string   // single strategy: nvidia.com/gpu or all-balanced: nvidia.com/mig-*
	MixedCnt     int      // number of instances to use for mixed strategy
	SliceUsage   int      // number of slices used per instance
	MemUsage     int      // memory usage in GB per instance
}

type MigPodInfo struct {
	PodName        string         // name of the pod
	Namespace      string         // namespace of the pod
	Pod            *pod.Builder   // pod object
	MigProfileInfo MIGProfileInfo // MIG profile information
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
