package olm

import (
	"context"

	"github.com/golang/glog"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/internal/check"
	_ "github.com/rh-ecosystem-edge/nvidia-ci/internal/gpuparams"

	_ "github.com/rh-ecosystem-edge/nvidia-ci/internal/inittools"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DeleteOLMPods(apiClient *clients.Settings, logLevel logging.Level) error {
	olmNamespace := "openshift-operator-lifecycle-manager"
	glog.V(glog.Level(logLevel)).Info("Deleting catalog operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=catalog-operator"}); err != nil {
		glog.Errorf("Error deleting catalog operator pods: %v", err)
		return err
	}

	glog.V(glog.Level(logLevel)).Info("Deleting OLM operator pods")
	if err := apiClient.Pods(olmNamespace).DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=olm-operator"}); err != nil {
		glog.Errorf("Error deleting OLM operator pods: %v", err)
		return err
	}

	return nil
}
