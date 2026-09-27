// Package nnopairreport maintains one result manifest across a Prow build.
package nnopairreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Image struct {
	Version     string `json:"version"`
	Image       string `json:"image,omitempty"`
	Digest      string `json:"digest,omitempty"`
	DOCAVersion string `json:"doca_version,omitempty"`
	OFEDVersion string `json:"ofed_version,omitempty"`
}

type Pair struct {
	ID              string             `json:"id"`
	Mode            string             `json:"mode"`
	Status          string             `json:"status"`
	NNORequested    string             `json:"nno_requested"`
	NNOObserved     string             `json:"nno_observed,omitempty"`
	DriverRequested Image              `json:"driver_requested"`
	DriverObserved  *Image             `json:"driver_observed,omitempty"`
	Checks          map[string]string  `json:"checks,omitempty"`
	Metrics         map[string]float64 `json:"metrics,omitempty"`
	Artifacts       map[string]string  `json:"artifacts,omitempty"`
	CompletedAt     string             `json:"completed_at,omitempty"`
	Note            string             `json:"note,omitempty"`
}

type Run struct {
	Kind               string   `json:"kind"`
	JobName            string   `json:"job_name"`
	BuildID            string   `json:"build_id"`
	StartedAt          string   `json:"started_at"`
	OCPVersion         string   `json:"ocp_version,omitempty"`
	GPUOperatorVersion string   `json:"gpu_operator_version,omitempty"`
	Environment        string   `json:"environment,omitempty"`
	Status             string   `json:"status"`
	PlannedModes       []string `json:"planned_modes"`
}

type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Run           Run    `json:"run"`
	Pairs         []Pair `json:"pairs"`
}

func validStatus(value string) bool {
	switch value {
	case "planned", "running", "passed", "failed", "skipped", "blocked":
		return true
	default:
		return false
	}
}

func validatePair(pair Pair) error {
	if strings.TrimSpace(pair.ID) == "" || strings.TrimSpace(pair.NNORequested) == "" || strings.TrimSpace(pair.DriverRequested.Version) == "" {
		return errors.New("pair requires id, nno_requested, and driver_requested.version")
	}
	if pair.Mode != "standard" && pair.Mode != "signed" {
		return fmt.Errorf("pair %s has invalid mode %q", pair.ID, pair.Mode)
	}
	if !validStatus(pair.Status) {
		return fmt.Errorf("pair %s has invalid status %q", pair.ID, pair.Status)
	}
	for name, status := range pair.Checks {
		if !validStatus(status) {
			return fmt.Errorf("pair %s check %s has invalid status %q", pair.ID, name, status)
		}
	}
	if pair.Status == "passed" {
		if pair.NNOObserved != pair.NNORequested || pair.DriverObserved == nil || pair.DriverObserved.Version != pair.DriverRequested.Version {
			return fmt.Errorf("passed pair %s requires matching observed versions", pair.ID)
		}
		if len(pair.Checks) == 0 {
			return fmt.Errorf("passed pair %s requires passed checks", pair.ID)
		}
		for _, status := range pair.Checks {
			if status != "passed" {
				return fmt.Errorf("passed pair %s requires passed checks", pair.ID)
			}
		}
		if pair.Mode == "signed" && pair.Checks["signature_verification"] != "passed" {
			return fmt.Errorf("passed signed pair %s requires signature verification", pair.ID)
		}
	}
	return nil
}

func validate(manifest Manifest) error {
	if manifest.SchemaVersion != 1 {
		return errors.New("manifest requires schema_version: 1")
	}
	if manifest.Run.Kind != "presubmit" && manifest.Run.Kind != "periodic" {
		return errors.New("run.kind must be presubmit or periodic")
	}
	if manifest.Run.JobName == "" || manifest.Run.BuildID == "" || manifest.Run.StartedAt == "" {
		return errors.New("run requires job_name, build_id, and started_at")
	}
	seen := make(map[string]bool)
	for _, pair := range manifest.Pairs {
		if err := validatePair(pair); err != nil {
			return err
		}
		if seen[pair.ID] {
			return fmt.Errorf("duplicate pair id %s", pair.ID)
		}
		seen[pair.ID] = true
	}
	return nil
}

func read(path string) (Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, validate(manifest)
}

func write(path string, manifest Manifest) error {
	if err := validate(manifest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".nno-pairs-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(content, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// Init records all planned pairs before the tests begin.
func Init(path string, run Run, plans []Pair) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("state file already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if run.StartedAt == "" {
		run.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	run.Status = "unknown"
	modes := make(map[string]bool)
	for index := range plans {
		if plans[index].Status != "" {
			return fmt.Errorf("planned pair %s must not have a status", plans[index].ID)
		}
		plans[index].Status = "planned"
		modes[plans[index].Mode] = true
	}
	run.PlannedModes = make([]string, 0, len(modes))
	for mode := range modes {
		run.PlannedModes = append(run.PlannedModes, mode)
	}
	sort.Strings(run.PlannedModes)
	return write(path, Manifest{SchemaVersion: 1, Run: run, Pairs: plans})
}

// UpdatePair replaces one planned or running attempt with its observed result.
func UpdatePair(path string, incoming Pair) error {
	manifest, err := read(path)
	if err != nil {
		return err
	}
	if err := validatePair(incoming); err != nil {
		return err
	}
	for index, existing := range manifest.Pairs {
		if existing.ID != incoming.ID {
			continue
		}
		if existing.Mode != incoming.Mode || existing.NNORequested != incoming.NNORequested || existing.DriverRequested != incoming.DriverRequested {
			return fmt.Errorf("pair %s changed its planned versions or mode", incoming.ID)
		}
		if existing.Status != "planned" && existing.Status != "running" {
			return fmt.Errorf("pair %s already has a final result", incoming.ID)
		}
		for name := range existing.Checks {
			if _, present := incoming.Checks[name]; !present {
				return fmt.Errorf("pair %s omitted planned check %s", incoming.ID, name)
			}
		}
		manifest.Pairs[index] = incoming
		return write(path, manifest)
	}
	return fmt.Errorf("pair %s was not in the build plan", incoming.ID)
}

// SetRun adds cluster context that may only be known after setup.
func SetRun(path, ocpVersion, gpuOperatorVersion, environment string) error {
	manifest, err := read(path)
	if err != nil {
		return err
	}
	if ocpVersion != "" {
		manifest.Run.OCPVersion = ocpVersion
	}
	if gpuOperatorVersion != "" {
		manifest.Run.GPUOperatorVersion = gpuOperatorVersion
	}
	if environment != "" {
		manifest.Run.Environment = environment
	}
	return write(path, manifest)
}

// Publish writes the single build manifest into a Prow post step's ARTIFACT_DIR.
func Publish(path, artifactDir string) (string, error) {
	manifest, err := read(path)
	if err != nil {
		return "", err
	}
	output := filepath.Join(artifactDir, "nno-pairs.json")
	return output, write(output, manifest)
}
