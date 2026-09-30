package nvidianetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ClusterPullSecretName is the OpenShift cluster pull secret in openshift-config.
	ClusterPullSecretName = "pull-secret"
	// StagingPullSecretName is the operator-namespace secret that holds only
	// registry.stage.redhat.io credentials copied from the cluster pull secret.
	StagingPullSecretName      = "ofed-staging-pull-secret"
	clusterPullSecretNamespace = "openshift-config"
	dockerConfigJSONKey        = ".dockerconfigjson"
)

// EnsureClusterPullSecret copies only registry.stage.redhat.io auths from
// openshift-config/pull-secret into namespace as ofed-staging-pull-secret.
func EnsureClusterPullSecret(ctx context.Context, apiClient *clients.Settings, namespace string) error {
	src, err := apiClient.Secrets(clusterPullSecretNamespace).Get(ctx, ClusterPullSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to read cluster pull-secret %s/%s: %w",
			clusterPullSecretNamespace, ClusterPullSecretName, err)
	}

	scoped, err := stagingDockerConfigJSON(src.Data[dockerConfigJSONKey])
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StagingPullSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			dockerConfigJSONKey: scoped,
		},
	}

	dst, err := apiClient.Secrets(namespace).Get(ctx, StagingPullSecretName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = apiClient.Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create %s in %s: %w", StagingPullSecretName, namespace, err)
		}

		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get %s in %s: %w", StagingPullSecretName, namespace, err)
	}

	dst.Type = corev1.SecretTypeDockerConfigJson
	dst.Data = secret.Data
	_, err = apiClient.Secrets(namespace).Update(ctx, dst, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update %s in %s: %w", StagingPullSecretName, namespace, err)
	}

	return nil
}

func stagingDockerConfigJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("cluster pull-secret has no %s", dockerConfigJSONKey)
	}

	var config dockerConfigJSON
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("failed to parse pull-secret: %w", err)
	}

	scoped := dockerConfigJSON{Auths: make(map[string]dockerAuthEntry, 1)}
	for key, entry := range config.Auths {
		if isStagingRegistryHost(key) {
			scoped.Auths[key] = entry
		}
	}
	if len(scoped.Auths) == 0 {
		return nil, fmt.Errorf("no credentials for %s found in cluster pull-secret", precompiledOFEDRegistry)
	}

	out, err := json.Marshal(scoped)
	if err != nil {
		return nil, fmt.Errorf("failed to encode scoped pull-secret: %w", err)
	}

	return out, nil
}

func isStagingRegistryHost(authKey string) bool {
	host := strings.TrimPrefix(authKey, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.Split(host, "/")[0]

	return strings.EqualFold(host, precompiledOFEDRegistry)
}
