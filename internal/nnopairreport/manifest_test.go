package nnopairreport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestKeepsPlannedPairsAfterFailure(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "shared", "nno-pairs.json")
	plans := []Pair{
		{
			ID: "standard-a", Mode: "standard", NNORequested: "25.10.1",
			DriverRequested: Image{Version: "doca3.5.0-26.07-0.7.7.0-0", DOCAVersion: "3.5.0"},
			Checks:          map[string]string{"nno_csv": "planned", "gpudirect_rdma_write": "planned"},
		},
		{
			ID: "signed-b", Mode: "signed", NNORequested: "26.1.0",
			DriverRequested: Image{Version: "26.04-0.7.1.0-0"},
			Checks:          map[string]string{"signature_verification": "planned"},
		},
	}
	if err := Init(state, Run{Kind: "presubmit", JobName: "pull-ci-example", BuildID: "123", OCPVersion: "4.22.14"}, plans); err != nil {
		t.Fatal(err)
	}
	if err := SetRun(state, "", "25.10.1", "DOCA2, 2 workers"); err != nil {
		t.Fatal(err)
	}
	result := plans[0]
	result.Status = "failed"
	result.NNOObserved = "25.10.1"
	result.DriverObserved = &Image{Version: result.DriverRequested.Version, OFEDVersion: "OFED 26.07"}
	result.Checks = map[string]string{"nno_csv": "passed", "gpudirect_rdma_write": "failed"}
	result.Metrics = map[string]float64{"bandwidth_gbps": 8.4}
	if err := UpdatePair(state, result); err != nil {
		t.Fatal(err)
	}
	output, err := Publish(state, filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Pairs[0].Status != "failed" || manifest.Pairs[1].Status != "planned" {
		t.Fatalf("unexpected pair results: %+v", manifest.Pairs)
	}
	if manifest.Run.GPUOperatorVersion != "25.10.1" || manifest.Pairs[0].DriverRequested.DOCAVersion != "3.5.0" {
		t.Fatalf("missing observed context: %+v", manifest)
	}
	if len(manifest.Run.PlannedModes) != 2 || manifest.Run.PlannedModes[0] != "signed" {
		t.Fatalf("unexpected planned modes: %v", manifest.Run.PlannedModes)
	}
}

func TestRejectsUnplannedPairAndUnsignedPass(t *testing.T) {
	state := filepath.Join(t.TempDir(), "nno-pairs.json")
	plan := Pair{ID: "a", Mode: "signed", NNORequested: "26.1.0", DriverRequested: Image{Version: "26.04-0.7.1.0-0"}}
	if err := Init(state, Run{Kind: "periodic", JobName: "periodic-ci-example", BuildID: "123"}, []Pair{plan}); err != nil {
		t.Fatal(err)
	}
	unplanned := plan
	unplanned.ID = "other"
	unplanned.Status = "failed"
	if err := UpdatePair(state, unplanned); err == nil || !strings.Contains(err.Error(), "not in the build plan") {
		t.Fatalf("expected unplanned pair error, got %v", err)
	}
	unsigned := plan
	unsigned.Status = "passed"
	unsigned.NNOObserved = plan.NNORequested
	unsigned.DriverObserved = &Image{Version: plan.DriverRequested.Version}
	unsigned.Checks = map[string]string{"nno_csv": "passed"}
	if err := UpdatePair(state, unsigned); err == nil || !strings.Contains(err.Error(), "signature verification") {
		t.Fatalf("expected signature check error, got %v", err)
	}
	manifest, err := read(state)
	if err != nil || manifest.Pairs[0].Status != "planned" {
		t.Fatalf("state changed after rejected update: %+v, %v", manifest, err)
	}
}
