package nvidiagpu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	corev1 "k8s.io/api/core/v1"
	goclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	precompiledRegistry  = "registry.redhat.io"
	precompiledNamespace = "nvidia"
	precompiledImage     = "gpu-driver-rhel9"

	PrecompiledDriverRepoField  = precompiledRegistry + "/" + precompiledNamespace
	PrecompiledDriverImageField = precompiledImage

	precompiledRepository = precompiledNamespace + "/" + precompiledImage
)

type dockerConfigJSON struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Auth string `json:"auth"`
}

// DiscoverPrecompiledDriverVersion queries registry.redhat.io for precompiled
// driver images matching the given kernel version. It returns the driver branch
// version (e.g., "580") and serves as validation that precompiled images are
// available for the given kernel.
func DiscoverPrecompiledDriverVersion(apiClient *clients.Settings, kernelVersion string) (string, error) {
	glog.V(100).Infof("Discovering precompiled driver version for kernel %s", kernelVersion)

	secret := &corev1.Secret{}
	err := apiClient.Get(context.TODO(), goclient.ObjectKey{
		Namespace: "openshift-config",
		Name:      "pull-secret",
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to read cluster pull-secret: %w", err)
	}

	var config dockerConfigJSON
	if err := json.Unmarshal(secret.Data[".dockerconfigjson"], &config); err != nil {
		return "", fmt.Errorf("failed to parse pull-secret: %w", err)
	}

	auth, ok := config.Auths[precompiledRegistry]
	if !ok {
		return "", fmt.Errorf("no credentials for %s found in cluster pull-secret", precompiledRegistry)
	}

	tags, err := listRegistryTags(auth.Auth)
	if err != nil {
		return "", fmt.Errorf("failed to list tags from %s/%s: %w", precompiledRegistry, precompiledRepository, err)
	}

	glog.V(100).Infof("Found %d total tags in %s/%s", len(tags), precompiledRegistry, precompiledRepository)

	var driverVersions []string
	seen := make(map[string]bool)

	for _, tag := range tags {
		if !strings.Contains(tag, kernelVersion) {
			continue
		}
		if strings.HasSuffix(tag, "-source") {
			continue
		}
		idx := strings.Index(tag, "-"+kernelVersion)
		if idx <= 0 {
			continue
		}
		version := tag[:idx]
		if !seen[version] {
			seen[version] = true
			driverVersions = append(driverVersions, version)
		}
	}

	if len(driverVersions) == 0 {
		return "", fmt.Errorf("no precompiled driver images found for kernel %s in %s/%s",
			kernelVersion, precompiledRegistry, precompiledRepository)
	}

	glog.V(100).Infof("Found precompiled driver versions for kernel %s: %v", kernelVersion, driverVersions)

	for _, v := range driverVersions {
		if !strings.Contains(v, ".") {
			glog.V(100).Infof("Selected precompiled driver branch version: %s", v)
			return v, nil
		}
	}

	glog.V(100).Infof("No short-form driver version found, using: %s", driverVersions[0])
	return driverVersions[0], nil
}

func listRegistryTags(authBase64 string) ([]string, error) {
	tagsURL := fmt.Sprintf("https://%s/v2/%s/tags/list", precompiledRegistry, precompiledRepository)

	resp, err := http.Get(tagsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to contact registry: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("expected 401 from registry, got %d", resp.StatusCode)
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	token, err := obtainRegistryToken(wwwAuth, authBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain registry token: %w", err)
	}

	var allTags []string
	nextURL := tagsURL

	for nextURL != "" {
		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to list tags: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Tags []string `json:"tags"`
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse tags response: %w", err)
		}

		allTags = append(allTags, result.Tags...)
		nextURL = getNextPageURL(resp.Header.Get("Link"))
	}

	return allTags, nil
}

func obtainRegistryToken(wwwAuth, authBase64 string) (string, error) {
	params := parseWWWAuthenticate(wwwAuth)
	realm, ok := params["realm"]
	if !ok {
		return "", fmt.Errorf("no realm in WWW-Authenticate header: %s", wwwAuth)
	}

	tokenURL := realm
	sep := "?"
	if service, ok := params["service"]; ok {
		tokenURL += sep + "service=" + url.QueryEscape(service)
		sep = "&"
	}
	if scope, ok := params["scope"]; ok {
		tokenURL += sep + "scope=" + url.QueryEscape(scope)
	}

	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}

	decoded, err := base64.StdEncoding.DecodeString(authBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode auth credentials: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid auth format in pull secret")
	}
	req.SetBasicAuth(parts[0], parts[1])

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.Token != "" {
		return tokenResp.Token, nil
	}
	if tokenResp.AccessToken != "" {
		return tokenResp.AccessToken, nil
	}

	return "", fmt.Errorf("no token in response")
}

func parseWWWAuthenticate(header string) map[string]string {
	params := make(map[string]string)
	header = strings.TrimPrefix(header, "Bearer ")
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = strings.Trim(kv[1], "\"")
		}
	}
	return params
}

func getNextPageURL(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		urlPart := strings.SplitN(part, ";", 2)[0]
		urlPart = strings.TrimSpace(urlPart)
		urlPart = strings.TrimPrefix(urlPart, "<")
		urlPart = strings.TrimSuffix(urlPart, ">")
		if strings.HasPrefix(urlPart, "/") {
			return "https://" + precompiledRegistry + urlPart
		}
		return urlPart
	}
	return ""
}
