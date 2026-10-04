package nnopairreport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStepResultEmptyDirectoryIsNoop(t *testing.T) {
	if err := WriteStepResult("  ", StepResult{Step: "network-operator-e2e-shared-device"}); err != nil {
		t.Fatal(err)
	}
}

func TestWriteStepResultWritesJSONFile(t *testing.T) {
	directory := t.TempDir()
	const (
		stepName = "network-operator-e2e-shared-device"
		status   = "passed"
	)
	result := StepResult{
		JobName: "periodic-ci-example",
		BuildID: "100",
		Step:    stepName,
		Attempt: "100",
		Status:  status,
		Checks:  map[string]string{"nno_deployment": status},
	}
	if err := WriteStepResult(directory, result); err != nil {
		t.Fatal(err)
	}

	matches, err := filepath.Glob(filepath.Join(directory, stepResultFilePrefix+"*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one step result file, got %v", matches)
	}
	if !strings.HasPrefix(filepath.Base(matches[0]), stepResultFilePrefix) || !strings.HasSuffix(matches[0], ".json") {
		t.Fatalf("unexpected step result filename %s", matches[0])
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]any
	if err := json.Unmarshal(content, &written); err != nil {
		t.Fatal(err)
	}
	if written["schema_version"] != float64(1) || written["step"] != stepName || written["status"] != status {
		t.Fatalf("unexpected step result: %s", content)
	}
	if written["attempt"] != "100" {
		t.Fatalf("attempt was not recorded: %s", content)
	}
	for _, field := range []string{"ocp_version", "nno_csv_version", "skip_reason", "configured_ofed_driver", "precompiled_selection"} {
		if _, present := written[field]; present {
			t.Fatalf("empty optional field %s was written: %s", field, content)
		}
	}
}
