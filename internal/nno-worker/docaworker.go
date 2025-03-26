package nnoworker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MinBandwidth   = 10.0 // Minimum BW in Gbps
	MinMsgRate     = 0.1  // Minimum MsgRate in Mpps
	ValidLinkTypes = "Ethernet,InfiniBand"
)

func CreateDocaWorkerPod(clientset *clients.Settings, mode, name, hostname, serverIP string) (*v1.Pod, error) {
	command := ""
	if mode == "server" {
		command = "ib_write_bw -R -T 41 -s 65536 -F -x 3 -m 4096 --report_gbits -q 16 -D 60 -d mlx5_1 -p 10000"
	} else {
		command = fmt.Sprintf("ib_write_bw -R -T 41 -s 65536 -F -x 3 -m 4096 --report_gbits -q 16 -D 60 -d mlx5_1 -p 10000 --source_ip %s --use_cuda=0", serverIP)
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app":  "ib-test",
				"role": mode, // 'server' or 'client'
			},
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": "hostdev-net",
			},
		},
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": hostname,
			},
			ServiceAccountName: "rdma",
			Containers: []v1.Container{
				{
					Name:  "hostdev-32-workload",
					Image: "quay.io/redhat_emp1/ecosys-nvidia/gpu-operator:tools",
					Command: []string{
						"sh",
						"-c",
						command,
					},
					SecurityContext: &v1.SecurityContext{
						Privileged: boolPtr(true),
						Capabilities: &v1.Capabilities{
							Add: []v1.Capability{"IPC_LOCK"},
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							"nvidia.com/gpu":     resource.MustParse("1"),
							"nvidia.com/hostdev": resource.MustParse("1"),
						},
						Requests: v1.ResourceList{
							"nvidia.com/gpu":     resource.MustParse("1"),
							"nvidia.com/hostdev": resource.MustParse("1"),
						},
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	return clientset.Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
}

func GetWorkerIP(clientset *clients.Settings, podName string, podinterface string) (string, error) {
	pod, err := clientset.Pods("default").Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod: %v", err)
	}

	// Extract network-status annotation
	networkStatus, ok := pod.Annotations["k8s.v1.cni.cncf.io/network-status"]
	if !ok {
		return "", fmt.Errorf("network status annotation not found")
	}

	// Parse JSON from annotation
	var networkData []map[string]interface{}
	err = json.Unmarshal([]byte(networkStatus), &networkData)
	if err != nil {
		return "", fmt.Errorf("failed to parse network-status annotation: %v", err)
	}

	// Search for the `ipoib` network entry
	for _, net := range networkData {
		if interFace, exists := net["interface"]; exists && interFace == podinterface {
			if ips, exists := net["ips"].([]interface{}); exists && len(ips) > 0 {
				return ips[0].(string), nil // Return the first IP found
			}
		}
	}

	return "", fmt.Errorf("network IP not found")
}
func boolPtr(b bool) *bool {
	return &b
}

func GetPodLogs(clientset *clients.Settings, podName string) (string, error) {
	req := clientset.Pods("default").GetLogs(podName, &v1.PodLogOptions{})
	logStream, err := req.Stream(context.TODO())
	if err != nil {
		return "", fmt.Errorf("error opening log stream: %v", err)
	}
	defer logStream.Close()

	var logs strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := logStream.Read(buf)
		if n > 0 {
			logs.WriteString(string(buf[:n]))
		}
		if err != nil {
			break
		}
	}

	return logs.String(), nil
}

func ParseIBWriteBWOutput(output string) (map[string]string, error) {
	results := make(map[string]string)

	// Regex patterns
	configRegex := regexp.MustCompile(`([\w\s\*]+):\s+([\w\[\]\/.]+)`)
	//configRegex := regexp.MustCompile(`(?P<key>[\w\s\*]+):\s+(?P<value>[\w\[\]\/.]+)`)
	bwTableRegex := regexp.MustCompile(`\s*(\d+)\s+(\d+)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)`)

	scanner := bufio.NewScanner(strings.NewReader(output))
	isParsingConfig := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.Contains(line, "RDMA_Write BW Test") {
			isParsingConfig = true
			results["Test_Type"] = "RDMA_Write BW Test"
			continue
		}

		if strings.Contains(line, "---------------------------------------------------------------------------------------") {
			isParsingConfig = false
		}

		// Parse RDMA configuration key-value pairs
		if isParsingConfig {
			matches := configRegex.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) > 2 {
					key := strings.TrimSpace(match[1])
					value := strings.TrimSpace(match[2])
					results[key] = value
				}
			}
		}

		// Match the performance table
		if matches := bwTableRegex.FindStringSubmatch(line); len(matches) > 4 {
			glog.Infof("Matched Bandwidth Table:%v", matches)
			results["Bytes"] = matches[1]
			results["Iterations"] = matches[2]
			results["BW_Peak_Gbps"] = matches[3]
			results["BW_Avg_Gbps"] = matches[4]
			results["MsgRate_Mpps"] = matches[5]
			break // Stop after finding the first occurrence
		}
	}

	// Check for errors
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func ValidateRDMAResults(results map[string]string) error {
	// Check Test Type
	testType, exists := results["Test_Type"]
	if !exists || testType != "RDMA_Write BW Test" {
		return fmt.Errorf("Invalid Test Type: %s", testType)
	}

	// Check Link Type
	linkType, exists := results["Link type"]
	if !exists || !(linkType == "Ethernet" || linkType == "InfiniBand") {
		return fmt.Errorf("Invalid Link Type: %s (Expected: Ethernet or InfiniBand)", linkType)
	}

	// Check Bandwidth
	bwAvg, err := strconv.ParseFloat(results["BW_Avg_Gbps"], 64)
	if err != nil || bwAvg < MinBandwidth {
		return fmt.Errorf("Bandwidth too low: %.2f Gbps (Min: %.2f Gbps)", bwAvg, MinBandwidth)
	}

	// Check Message Rate
	msgRate, err := strconv.ParseFloat(results["MsgRate_Mpps"], 64)
	if err != nil || msgRate < MinMsgRate {
		return fmt.Errorf("MsgRate too low: %.3f Mpps (Min: %.1f Mpps)", msgRate, MinMsgRate)
	}

	// If everything is valid
	return nil
}
