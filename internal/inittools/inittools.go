package inittools

import (
	"context"
	"flag"
	"fmt"

	"github.com/golang/glog"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/rh-ecosystem-edge/nvidia-ci/internal/config"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	// APIClient provides access to cluster.
	APIClient *clients.Settings
	// GeneralConfig provides access to general configuration parameters.
	GeneralConfig *config.GeneralConfig
)

// init loads all variables automatically when this package is imported. Once package is imported a user has full
// access to all vars within init function. It is recommended to import this package using dot import.
func init() {
	// Work around bug in glog lib
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	if GeneralConfig = config.NewConfig(); GeneralConfig == nil {
		glog.Fatalf("error to load general config")
	}

	_ = flag.Lookup("logtostderr").Value.Set("true")
	_ = flag.Lookup("v").Value.Set(GeneralConfig.VerboseLevel)

	if APIClient = clients.New(""); APIClient == nil {
		if GeneralConfig.DryRun {
			return
		}

		glog.Fatalf("can not load ApiClient. Please check your KUBECONFIG env var")
	}
}

func GetOpenShiftVersion() (string, error) {
	clusterVersion, err := APIClient.ClusterVersions().Get(context.TODO(), "version", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, condition := range clusterVersion.Status.History {
		if condition.State != "Completed" {
			continue
		}

		// Parse as semantic version to preserve prerelease identifiers
		parsedVersion, err := utilversion.ParseSemantic(condition.Version)
		if err != nil {
			return "", fmt.Errorf("invalid semantic version format '%s': %w", condition.Version, err)
		}

		// Return the parsed version string which preserves prerelease identifiers like "-rc.0"
		return parsedVersion.String(), nil
	}

	return "", fmt.Errorf("no completed version found in cluster version history")
}
