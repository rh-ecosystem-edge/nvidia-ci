package nvidiagpu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

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

	precompiledRepository  = precompiledNamespace + "/" + precompiledImage
	registryRequestTimeout = 30 * time.Second
)

type dockerConfigJSON struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Auth string `json:"auth"`
}

// DiscoverPrecompiledDriverVersion queries registry.redhat.io for precompiled
// driver images matching the given kernel version. It returns all unique driver
// branch versions deduplicated by major version prefix (e.g., "580.178.04" and
// "580" are treated as the same branch, keeping the long form). The returned
// list is sorted lexicographically.
func DiscoverPrecompiledDriverVersion(apiClient *clients.Settings, kernelVersion string) ([]string, error) {
	glog.V(100).Infof("Discovering precompiled driver version for kernel %s", kernelVersion)

	secret := &corev1.Secret{}
	err := apiClient.Get(context.TODO(), goclient.ObjectKey{
		Namespace: "openshift-config",
		Name:      "pull-secret",
	}, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to read cluster pull-secret: %w", err)
	}

	var config dockerConfigJSON
	if err := json.Unmarshal(secret.Data[".dockerconfigjson"], &config); err != nil {
		return nil, fmt.Errorf("failed to parse pull-secret: %w", err)
	}

	auth, ok := config.Auths[precompiledRegistry]
	if !ok {
		return nil, fmt.Errorf("no credentials for %s found in cluster pull-secret", precompiledRegistry)
	}

	tags, err := listRegistryTags(auth.Auth)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags from %s/%s: %w", precompiledRegistry, precompiledRepository, err)
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
		return nil, fmt.Errorf("no precompiled driver images found for kernel %s in %s/%s",
			kernelVersion, precompiledRegistry, precompiledRepository)
	}

	glog.V(100).Infof("Found precompiled driver versions for kernel %s: %v", kernelVersion, driverVersions)

	deduplicated := deduplicateByMajorVersion(driverVersions)

	glog.V(100).Infof("Deduplicated precompiled driver versions: %v", deduplicated)

	return deduplicated, nil
}

// deduplicateByMajorVersion groups versions by their major prefix (the part
// before the first dot) and keeps the longest (most specific) version for each
// group. Results are sorted lexicographically ascending. For example, given
// ["580.178.04", "580", "595", "595.91.07"], it returns ["580.178.04",
// "595.91.07"].
func deduplicateByMajorVersion(versions []string) []string {
	best := make(map[string]string)

	for _, v := range versions {
		major := v
		if idx := strings.Index(v, "."); idx > 0 {
			major = v[:idx]
		}

		existing, ok := best[major]
		if !ok || len(v) > len(existing) {
			best[major] = v
		}
	}

	result := make([]string, 0, len(best))
	for _, v := range best {
		result = append(result, v)
	}

	sort.Strings(result)

	return result
}

func listRegistryTags(authBase64 string) ([]string, error) {
	tagsURL := fmt.Sprintf("https://%s/v2/%s/tags/list", precompiledRegistry, precompiledRepository)

	client := &http.Client{Timeout: registryRequestTimeout}

	ctx, cancel := context.WithTimeout(context.Background(), registryRequestTimeout)
	defer cancel()

	challengeReq, err := http.NewRequestWithContext(ctx, "GET", tagsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry request: %w", err)
	}

	resp, err := client.Do(challengeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to contact registry: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("expected 401 from registry, got %d", resp.StatusCode)
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	token, err := obtainRegistryToken(client, wwwAuth, authBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain registry token: %w", err)
	}

	var allTags []string
	nextURL := tagsURL

	for nextURL != "" {
		reqCtx, reqCancel := context.WithTimeout(context.Background(), registryRequestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, "GET", nextURL, nil)
		if err != nil {
			reqCancel()
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			reqCancel()
			return nil, fmt.Errorf("failed to list tags: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			reqCancel()
			return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Tags []string `json:"tags"`
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		reqCancel()
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

func obtainRegistryToken(client *http.Client, wwwAuth, authBase64 string) (string, error) {
	params := parseWWWAuthenticate(wwwAuth)
	realm, ok := params["realm"]
	if !ok {
		return "", fmt.Errorf("no realm in WWW-Authenticate header: %s", wwwAuth)
	}

	if err := validateRegistryURL(realm); err != nil {
		return "", fmt.Errorf("untrusted token realm %q: %w", realm, err)
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

	ctx, cancel := context.WithTimeout(context.Background(), registryRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
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

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

var allowedRegistryHosts = map[string]bool{
	"registry.redhat.io": true,
}

func validateRegistryURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("scheme %q is not https", parsed.Scheme)
	}
	if !allowedRegistryHosts[parsed.Hostname()] {
		return fmt.Errorf("host %q is not an allowed registry host", parsed.Hostname())
	}
	return nil
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
		if validateRegistryURL(urlPart) != nil {
			return ""
		}
		return urlPart
	}
	return ""
}
