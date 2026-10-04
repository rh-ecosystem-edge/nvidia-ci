package nnopairreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stepResultFilePrefix = "nno-step-result-"

// StepOFEDDriver records the configured OFED driver and the image ID observed on a node.
type StepOFEDDriver struct {
	Version    string `json:"version,omitempty"`
	Repository string `json:"repository,omitempty"`
	Image      string `json:"image,omitempty"`
	ImageID    string `json:"image_id,omitempty"`
}

// PrecompiledSelection records the kernel-specific staging image selection attempt.
type PrecompiledSelection struct {
	Outcome       string `json:"outcome"`
	KernelVersion string `json:"kernel_version,omitempty"`
	Architecture  string `json:"architecture,omitempty"`
	Version       string `json:"version,omitempty"`
	PullSpec      string `json:"pull_spec,omitempty"`
	Error         string `json:"error,omitempty"`
}

// StepResult is the durable result record for one NNO Prow test step.
type StepResult struct {
	SchemaVersion        int                   `json:"schema_version"`
	JobName              string                `json:"job_name"`
	BuildID              string                `json:"build_id"`
	Step                 string                `json:"step"`
	Attempt              string                `json:"attempt,omitempty"`
	RecordedAt           string                `json:"recorded_at"`
	Status               string                `json:"status"`
	OCPVersion           string                `json:"ocp_version,omitempty"`
	NNOCSVVersion        string                `json:"nno_csv_version,omitempty"`
	ConfiguredOFEDDriver *StepOFEDDriver       `json:"configured_ofed_driver,omitempty"`
	Checks               map[string]string     `json:"checks"`
	Metrics              map[string]float64    `json:"metrics"`
	SkipReason           string                `json:"skip_reason,omitempty"`
	PrecompiledSelection *PrecompiledSelection `json:"precompiled_selection,omitempty"`
}

// WriteStepResult atomically writes the per-step record into ARTIFACT_DIR.
func WriteStepResult(artifactDir string, result StepResult) error {
	if strings.TrimSpace(artifactDir) == "" {
		return nil
	}
	return writeStepResult(artifactDir, result)
}

func writeStepResult(directory string, result StepResult) error {
	if result.Step == "" {
		return errors.New("step result requires a step name")
	}
	if result.SchemaVersion == 0 {
		result.SchemaVersion = 1
	}
	if result.RecordedAt == "" {
		result.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if result.Checks == nil {
		result.Checks = map[string]string{}
	}
	if result.Metrics == nil {
		result.Metrics = map[string]float64{}
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}

	file, err := os.CreateTemp(directory, ".nno-step-result-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(append(content, '\n')); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	filename := stepResultFilePrefix + strings.TrimPrefix(filepath.Base(file.Name()), ".nno-step-result-") + ".json"
	if err := os.Rename(file.Name(), filepath.Join(directory, filename)); err != nil {
		return fmt.Errorf("publish step result: %w", err)
	}
	return nil
}
