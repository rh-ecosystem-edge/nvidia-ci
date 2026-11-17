package main

import (
	"testing"
	"time"
)

func TestOptionsComeFromFlags(t *testing.T) {
	options, err := parseOptions([]string{
		"--kubeconfig=/explicit", "--manifest-dir=custom", "--wait-for-worker-mcp=true",
		"--batch-worker-mcp=true", "--worker-mcp-rollout-start-timeout=4m",
	}, []string{"KUBECONFIG=/ambient", "MANIFEST_DIR=wrong", "VALUE=one=two"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Kubeconfig != "/explicit" || options.Directory != "custom" ||
		!options.WaitForMCP || !options.BatchMCP || options.StartTimeout != 4*time.Minute ||
		options.Variables["VALUE"] != "one=two" {
		t.Fatalf("options did not preserve explicit arguments: %+v", options)
	}
	defaults, err := parseOptions(nil, []string{"KUBECONFIG=/ambient", "BATCH_WORKER_MCP=true"})
	if err != nil || defaults.Kubeconfig != "" || defaults.BatchMCP {
		t.Fatalf("configuration leaked from environment: %+v %v", defaults, err)
	}
}
