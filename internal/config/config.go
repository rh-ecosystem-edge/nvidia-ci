package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	yaml "sigs.k8s.io/yaml/goyaml.v2"

	"github.com/kelseyhightower/envconfig"
)

const (
	// PathToDefaultParamsFile path to config file with default parameters.
	PathToDefaultParamsFile = "./default.yaml"
)

// GeneralConfig type keeps general configuration.
type GeneralConfig struct {
	ReportsDirAbsPath    string `yaml:"reports_dump_dir" envconfig:"REPORTS_DUMP_DIR"`
	VerboseLevel         string `yaml:"verbose_level" envconfig:"VERBOSE_LEVEL"`
	DumpFailedTests      bool   `yaml:"dump_failed_tests" envconfig:"DUMP_FAILED_TESTS"`
	DryRun               bool   `yaml:"dry_run" envconfig:"DRY_RUN"`
	KubernetesRolePrefix string `yaml:"kubernetes_role_prefix" envconfig:"KUBERNETES_ROLE_PREFIX"`
	WorkerLabelEnvVar    string `yaml:"worker_label" envconfig:"WORKER_LABEL"`
	WorkerLabel          string
	ControlPlaneLabel    string `yaml:"control_plane_label" envconfig:"CONTROL_PLANE_LABEL"`
	WorkerLabelMap       map[string]string
	ControlPlaneLabelMap map[string]string
}

// NewConfig returns instance of GeneralConfig config type.
func NewConfig() *GeneralConfig {
	log.Print("Creating new GeneralConfig struct")

	var conf GeneralConfig

	_, filename, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(filename)
	confFile := filepath.Join(baseDir, PathToDefaultParamsFile)
	err := readFile(&conf, confFile)

	if err != nil {
		log.Printf("Error to read config file %s", confFile)

		return nil
	}

	err = readEnv(&conf)

	if err != nil {
		log.Print("Error to read environment variables")

		return nil
	}

	err = deployReportDir(conf.ReportsDirAbsPath)

	if err != nil {
		log.Printf("Error to deploy report directory %s due to %s", conf.ReportsDirAbsPath, err.Error())

		return nil
	}

	return &conf
}

// GetJunitReportPath returns full path to the junit report file.
func (cfg *GeneralConfig) GetJunitReportPath(file string) string {
	reportFileName := strings.TrimSuffix(filepath.Base(file), filepath.Ext(filepath.Base(file)))
	return fmt.Sprintf("%s_junit.xml", filepath.Join(cfg.ReportsDirAbsPath, reportFileName))
}

// GetArtifactPath return full path to a file in the report directory.
func (cfg *GeneralConfig) GetReportPath(file string) string {
	fileName := filepath.Base(file)
	return filepath.Join(cfg.ReportsDirAbsPath, fileName)
}

// WriteReport writes contents into a file in the report directory.
func (cfg *GeneralConfig) WriteReport(fileName string, content []byte) error {
	file := cfg.GetReportPath(fileName)
	return os.WriteFile(file, content, 0666)
}

// GetDumpFailedTestReportLocation returns destination file for failed tests logs.
func (cfg *GeneralConfig) GetDumpFailedTestReportLocation(file string) string {
	if cfg.DumpFailedTests {
		if _, err := os.Stat(cfg.ReportsDirAbsPath); os.IsNotExist(err) {
			err := os.MkdirAll(cfg.ReportsDirAbsPath, 0744)
			if err != nil {
				log.Fatalf("panic: Failed to create report dir due to %s", err)
			}
		}

		dumpFileName := strings.TrimSuffix(filepath.Base(file), filepath.Ext(filepath.Base(file)))

		return filepath.Join(cfg.ReportsDirAbsPath, fmt.Sprintf("failed_%s", dumpFileName))
	}
	return ""
}

// GetDumpTestReportLocation returns destination file for failed tests logs.
func (cfg *GeneralConfig) GetDumpTestReportLocation(file string) string {
	if _, err := os.Stat(cfg.ReportsDirAbsPath); os.IsNotExist(err) {
		err := os.MkdirAll(cfg.ReportsDirAbsPath, 0744)
		if err != nil {
			log.Fatalf("panic: Failed to create report dir due to %s", err)
		}
	}
	dumpFileName := strings.TrimSuffix(filepath.Base(file), filepath.Ext(filepath.Base(file)))
	return filepath.Join(cfg.ReportsDirAbsPath, dumpFileName)
}

func readFile(cfg *GeneralConfig, cfgFile string) error {
	openedCfgFile, err := os.Open(cfgFile)
	if err != nil {
		return err
	}

	defer func() {
		_ = openedCfgFile.Close()
	}()

	decoder := yaml.NewDecoder(openedCfgFile)
	err = decoder.Decode(&cfg)

	if err != nil {
		return err
	}

	return nil
}

func readEnv(cfg *GeneralConfig) error {
	err := envconfig.Process("", cfg)
	if err != nil {
		return err
	}

	cfg.WorkerLabel = fmt.Sprintf("%s/%s", cfg.KubernetesRolePrefix, cfg.WorkerLabelEnvVar)
	cfg.ControlPlaneLabel = fmt.Sprintf("%s/%s", cfg.KubernetesRolePrefix, cfg.ControlPlaneLabel)
	cfg.WorkerLabelMap = map[string]string{cfg.WorkerLabel: ""}
	cfg.ControlPlaneLabelMap = map[string]string{cfg.ControlPlaneLabel: ""}

	return nil
}

func deployReportDir(dirName string) error {
	_, err := os.Stat(dirName)

	if os.IsNotExist(err) {
		return os.MkdirAll(dirName, 0777)
	}

	return err
}
