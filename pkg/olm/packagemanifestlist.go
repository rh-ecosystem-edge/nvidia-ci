package olm

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	v1 "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/operators/v1"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ListPackageManifest returns PackageManifest inventory in the given namespace.
func ListPackageManifest(
	apiClient *clients.Settings,
	nsname string,
	options metav1.ListOptions) ([]*PackageManifestBuilder, error) {
	if nsname == "" {
		glog.V(100).Infof("packagemanifest 'nsname' parameter can not be empty")
		return nil, fmt.Errorf("failed to list packagemanifests, 'nsname' parameter is empty")
	}

	glog.V(100).Infof("Listing PackageManifests in the namespace %s", nsname)

	pkgManifestList, err := apiClient.PackageManifestInterface.PackageManifests(nsname).List(context.TODO(), options)
	if err != nil {
		glog.V(100).Infof("Failed to list PackageManifests in the namespace %s due to %s",
			nsname, err.Error())
		return nil, err
	}

	var pkgManifestObjects []*PackageManifestBuilder
	for _, runningPkgManifest := range pkgManifestList.Items {
		copiedPkgManifest := runningPkgManifest
		pkgManifestBuilder := &PackageManifestBuilder{
			apiClient:  apiClient,
			Object:     &copiedPkgManifest,
			Definition: &copiedPkgManifest,
		}
		pkgManifestObjects = append(pkgManifestObjects, pkgManifestBuilder)
	}
	return pkgManifestObjects, nil
}

// ListPackageManifestWithTimeout returns PackageManifest inventory in the given namespace and timeout.
func ListPackageManifestWithTimeout(
	apiClient *clients.Settings,
	nsname string,
	backoff time.Duration,
	timeout time.Duration,
	options metav1.ListOptions) ([]*PackageManifestBuilder, error) {
	if nsname == "" {
		glog.V(100).Infof("packagemanifest 'nsname' parameter can not be empty")
		return nil, fmt.Errorf("failed to list packagemanifests, 'nsname' parameter is empty")
	}

	glog.V(100).Infof("Listing PackageManifests in the namespace %s", nsname)
	var pkgManifestList *v1.PackageManifestList
	err := wait.PollUntilContextTimeout(
		context.TODO(), backoff, timeout, true, func(ctx context.Context) (bool, error) {
			var err error
			pkgManifestList, err = apiClient.PackageManifestInterface.PackageManifests(nsname).List(context.TODO(), options)
			if err != nil {
				return false, err
			}
			if len(pkgManifestList.Items) != 0 {
				return true, nil
			}
			return false, nil
		})
	if err != nil {
		glog.V(100).Infof("Failed to list PackageManifests in the namespace %s due to %s",
			nsname, err.Error())
		return nil, err
	}

	var pkgManifestObjects []*PackageManifestBuilder
	for _, runningPkgManifest := range pkgManifestList.Items {
		copiedPkgManifest := runningPkgManifest
		pkgManifestBuilder := &PackageManifestBuilder{
			apiClient:  apiClient,
			Object:     &copiedPkgManifest,
			Definition: &copiedPkgManifest,
		}
		pkgManifestObjects = append(pkgManifestObjects, pkgManifestBuilder)
	}
	return pkgManifestObjects, nil
}
