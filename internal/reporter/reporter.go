package reporter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

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
