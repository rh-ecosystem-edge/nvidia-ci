package reporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/openshift-kni/k8sreporter"

	"github.com/golang/glog"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	pathToPodExecLogs = "/tmp/pod_exec_logs.log"
)

func newReporter(
	reportPath string,
	namespacesToDump map[string]string,
	apiScheme func(scheme *runtime.Scheme) error,
	cRDs []k8sreporter.CRData) (*k8sreporter.KubernetesReporter, error) {
	nsToDumpFilter := func(ns string) bool {
		_, found := namespacesToDump[ns]

		return found
	}

	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		err := os.MkdirAll(reportPath, 0755)
		if err != nil {
			return nil, err
		}
	}

	res, err := k8sreporter.New("", apiScheme, nsToDumpFilter, reportPath, cRDs...)

	if err != nil {
		return nil, err
	}

	return res, nil
}

// ReportIfFailed dumps requested cluster CRs if TC is failed to the given directory.
func ReportIfFailed(
	report types.SpecReport,
	testSuite string,
	nSpaces map[string]string,
	cRDs []k8sreporter.CRData,
	apiScheme func(scheme *runtime.Scheme) error) {
	if !types.SpecStateFailureStates.Is(report.State) {
		return
	}

	dumpDir := inittools.GeneralConfig.GetDumpFailedTestReportLocation(testSuite)

	if dumpDir != "" {
		reporter, err := newReporter(dumpDir, nSpaces, apiScheme, cRDs)

		if err != nil {
			glog.Fatalf("Failed to create log reporter due to %s", err)
		}

		tcReportFolderName := strings.ReplaceAll(report.FullText(), " ", "_")
		reporter.Dump(report.RunTime, tcReportFolderName)

		_, podExecLogsFName := path.Split(pathToPodExecLogs)

		err = moveFile(
			pathToPodExecLogs, path.Join(inittools.GeneralConfig.ReportsDirAbsPath, tcReportFolderName, podExecLogsFName))

		if err != nil {
			glog.Fatalf("Failed to move pod exec logs %s to report folder: %s", pathToPodExecLogs, err)
		}
	}

	err := removeFile(pathToPodExecLogs)
	if err != nil {
		glog.Fatalf(err.Error())
	}
}

func moveFile(sourcePath, destPath string) error {
	_, err := os.Stat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	inputFile, err := os.Open(sourcePath)

	if err != nil {
		return fmt.Errorf("couldn't open source file: %w", err)
	}

	defer func() {
		_ = inputFile.Close()
	}()

	outputFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("couldn't open dest file: %w", err)
	}

	defer func() {
		_ = outputFile.Close()
	}()

	_, err = io.Copy(outputFile, inputFile)

	if err != nil {
		return fmt.Errorf("writing to output file failed: %w", err)
	}

	return nil
}

func removeFile(fPath string) error {
	if _, err := os.Stat(fPath); err == nil {
		err := os.Remove(fPath)
		if err != nil {
			return fmt.Errorf("failed to remove pod exec logs from %s: %w", fPath, err)
		}
	}

	return nil
}
func MustGatherIfFailed(specReport types.SpecReport, artifactDir string, timeout time.Duration) error {
	if !types.SpecStateFailureStates.Is(specReport.State) {
		return nil
	}
	if artifactDir == "" {
		return fmt.Errorf("artifact directory cannot be empty")
	}

	mustGatherScriptPath := os.Getenv("PATH_TO_MUST_GATHER_SCRIPT")
	if mustGatherScriptPath == "" {
		return fmt.Errorf("PATH_TO_MUST_GATHER_SCRIPT environment variable is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, mustGatherScriptPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("ARTIFACT_DIR=%s", artifactDir))
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		glog.Errorf("%s script timed out: %v", mustGatherScriptPath, ctx.Err())
		return fmt.Errorf("must gather script timed out: %w", ctx.Err())
	}
	if err != nil {
		glog.Errorf("Error running %s script: %v\nOutput: %s", mustGatherScriptPath, err, output)
		return fmt.Errorf("error running must gather script: %w", err)
	}
	glog.V(100).Infof("must gather script output: %s", output)
	return nil
}
