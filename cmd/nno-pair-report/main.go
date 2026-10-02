package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/nnopairreport"
)

func required(value, name string) error {
	if value == "" {
		return fmt.Errorf("--%s is required", name)
	}
	return nil
}

func readJSON(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(content, target)
}

func execute(args []string) error {
	if len(args) == 0 {
		return errors.New("expected init, update, set-run, or publish")
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	stateFile := flags.String("state-file", "", "manifest state in SHARED_DIR")
	var run func() error
	switch command {
	case "init":
		planFile := flags.String("plan-file", "", "JSON array of planned pairs")
		kind := flags.String("kind", "", "presubmit or periodic")
		jobName := flags.String("job-name", "", "Prow job name")
		buildID := flags.String("build-id", "", "Prow build ID")
		startedAt := flags.String("started-at", "", "optional UTC start time")
		ocpVersion := flags.String("ocp-version", "", "observed OpenShift version")
		gpuVersion := flags.String("gpu-operator-version", "", "observed GPU Operator version")
		environment := flags.String("environment", "", "test environment")
		run = func() error {
			if err := required(*planFile, "plan-file"); err != nil {
				return err
			}
			var plans []nnopairreport.Pair
			if err := readJSON(*planFile, &plans); err != nil {
				return err
			}
			return nnopairreport.Init(*stateFile, nnopairreport.Run{
				Kind: *kind, JobName: *jobName, BuildID: *buildID, StartedAt: *startedAt,
				OCPVersion: *ocpVersion, GPUOperatorVersion: *gpuVersion, Environment: *environment,
			}, plans)
		}
	case "update":
		pairFile := flags.String("pair-file", "", "complete pair result JSON")
		run = func() error {
			if err := required(*pairFile, "pair-file"); err != nil {
				return err
			}
			var pair nnopairreport.Pair
			if err := readJSON(*pairFile, &pair); err != nil {
				return err
			}
			return nnopairreport.UpdatePair(*stateFile, pair)
		}
	case "set-run":
		ocpVersion := flags.String("ocp-version", "", "observed OpenShift version")
		gpuVersion := flags.String("gpu-operator-version", "", "observed GPU Operator version")
		environment := flags.String("environment", "", "test environment")
		run = func() error {
			if *ocpVersion == "" && *gpuVersion == "" && *environment == "" {
				return errors.New("provide at least one run field")
			}
			return nnopairreport.SetRun(*stateFile, *ocpVersion, *gpuVersion, *environment)
		}
	case "publish":
		artifactDir := flags.String("artifact-dir", "", "Prow ARTIFACT_DIR")
		run = func() error {
			if err := required(*artifactDir, "artifact-dir"); err != nil {
				return err
			}
			output, err := nnopairreport.Publish(*stateFile, *artifactDir)
			if err == nil {
				fmt.Println(output)
			}
			return err
		}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if err := required(*stateFile, "state-file"); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	return run()
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
