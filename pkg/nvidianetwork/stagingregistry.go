package nvidianetwork

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rh-ecosystem-edge/nvidia-ci/pkg/clients"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	registryRequestTimeout = 30 * time.Second
	catalogUserAgent       = "nvidia-ci-precompiled-ofed"
)

var (
	kernelInTagRegexp = regexp.MustCompile(`(\d+\.\d+\.\d+-\d+(?:\.\d+)*\.el\d[\w.]*)`)

	stagingRegistryAuthKeys = []string{
		precompiledOFEDRegistry,
		"https://" + precompiledOFEDRegistry,
		"https://" + precompiledOFEDRegistry + "/v1",
		"https://" + precompiledOFEDRegistry + "/v2/",
	}
)

type dockerConfigJSON struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type registryTagClient struct {
	apiClient *clients.Settings
	client    *http.Client
}

// NewStagingCatalogClient lists precompiled tags from registry.stage.redhat.io
// using credentials in openshift-config/pull-secret.
func NewStagingCatalogClient(apiClient *clients.Settings) CatalogClient {
	return &registryTagClient{
		apiClient: apiClient,
		client:    &http.Client{Timeout: registryRequestTimeout},
	}
}

func (c *registryTagClient) ListImages(ctx context.Context, repository string) ([]CatalogImage, error) {
	if c.apiClient == nil {
		return nil, fmt.Errorf("cluster client is required to read pull-secret for %s", precompiledOFEDRegistry)
	}

	glog.V(100).Infof("Querying staging registry %s for repository %s", precompiledOFEDRegistry, repository)

	auth, err := stagingRegistryAuth(ctx, c.apiClient)
	if err != nil {
		return nil, err
	}

	tags, err := listStagingRegistryTags(ctx, c.client, auth, repository)
	if err != nil {
		return nil, err
	}

	return catalogImagesFromTags(repository, tags), nil
}

func stagingRegistryAuth(ctx context.Context, apiClient *clients.Settings) (string, error) {
	secret, err := apiClient.Secrets(clusterPullSecretNamespace).Get(ctx, ClusterPullSecretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to read cluster pull-secret: %w", err)
	}

	auth, err := dockerAuthFromSecret(secret, stagingRegistryAuthKeys)
	if err != nil {
		return "", err
	}

	return auth, nil
}

func dockerAuthFromSecret(secret *corev1.Secret, keys []string) (string, error) {
	raw, ok := secret.Data[dockerConfigJSONKey]
	if !ok {
		return "", fmt.Errorf("cluster pull-secret has no %s", dockerConfigJSONKey)
	}

	scoped, err := stagingDockerConfigJSON(raw)
	if err != nil {
		return "", err
	}

	var config dockerConfigJSON
	if err := json.Unmarshal(scoped, &config); err != nil {
		return "", fmt.Errorf("failed to parse scoped pull-secret: %w", err)
	}

	for _, key := range keys {
		entry, ok := config.Auths[key]
		if !ok {
			continue
		}

		auth, err := entry.basicAuth()
		if err != nil {
			return "", fmt.Errorf("invalid credentials for %s in cluster pull-secret: %w", key, err)
		}

		return auth, nil
	}

	for key, entry := range config.Auths {
		auth, err := entry.basicAuth()
		if err != nil {
			return "", fmt.Errorf("invalid credentials for %s in cluster pull-secret: %w", key, err)
		}

		return auth, nil
	}

	return "", fmt.Errorf("no credentials for %s found in cluster pull-secret", precompiledOFEDRegistry)
}

func (e dockerAuthEntry) basicAuth() (string, error) {
	if e.Auth != "" {
		return e.Auth, nil
	}
	if e.Username == "" {
		return "", fmt.Errorf("missing auth and username")
	}

	return base64.StdEncoding.EncodeToString([]byte(e.Username + ":" + e.Password)), nil
}

func listStagingRegistryTags(
	ctx context.Context, client *http.Client, authBase64, repository string,
) ([]string, error) {
	tagsURL := fmt.Sprintf("https://%s/v2/%s/tags/list", precompiledOFEDRegistry, repository)

	challengeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry request: %w", err)
	}
	challengeReq.Header.Set("User-Agent", catalogUserAgent)

	resp, err := client.Do(challengeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to contact registry: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("expected 401 from %s, got %d", precompiledOFEDRegistry, resp.StatusCode)
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	token, err := obtainStagingRegistryToken(ctx, client, wwwAuth, authBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain registry token: %w", err)
	}

	var allTags []string
	nextURL := tagsURL

	for nextURL != "" {
		pageTags, next, err := listStagingRegistryTagsPage(ctx, client, nextURL, token)
		if err != nil {
			return nil, err
		}

		allTags = append(allTags, pageTags...)
		nextURL = next
	}

	return allTags, nil
}

func listStagingRegistryTagsPage(
	ctx context.Context, client *http.Client, pageURL, token string,
) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", catalogUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read registry response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", fmt.Errorf("failed to parse tags response: %w", err)
	}

	return result.Tags, nextRegistryPageURL(resp.Header.Get("Link")), nil
}

func obtainStagingRegistryToken(
	ctx context.Context, client *http.Client, wwwAuth, authBase64 string,
) (string, error) {
	params := parseWWWAuthenticate(wwwAuth)
	realm, ok := params["realm"]
	if !ok {
		return "", fmt.Errorf("no realm in WWW-Authenticate header: %s", wwwAuth)
	}

	if err := validateStagingAuthURL(realm); err != nil {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
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
	req.Header.Set("User-Agent", catalogUserAgent)

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

func validateStagingAuthURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("scheme %q is not https", parsed.Scheme)
	}

	switch parsed.Hostname() {
	case precompiledOFEDRegistry, "sso.redhat.com":
		return nil
	default:
		return fmt.Errorf("host %q is not an allowed staging registry auth host", parsed.Hostname())
	}
}

func validateStagingRegistryURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("scheme %q is not https", parsed.Scheme)
	}
	if parsed.Hostname() != precompiledOFEDRegistry {
		return fmt.Errorf("host %q is not %s", parsed.Hostname(), precompiledOFEDRegistry)
	}

	return nil
}

func nextRegistryPageURL(linkHeader string) string {
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
			return "https://" + precompiledOFEDRegistry + urlPart
		}
		if validateStagingRegistryURL(urlPart) != nil {
			return ""
		}

		return urlPart
	}

	return ""
}

func catalogImagesFromTags(repository string, tags []string) []CatalogImage {
	images := make([]CatalogImage, 0, len(tags))
	for _, tag := range tags {
		ofed, kernel, ok := parsePrecompiledTag(tag)
		if !ok {
			continue
		}

		images = append(images, CatalogImage{
			Registry:   precompiledOFEDRegistry,
			Repository: repository,
			Tags:       []string{tag},
			Labels: map[string]string{
				kernelVersionLabel: kernelVersionWithoutArch(kernel),
				ofedVersionLabel:   ofed,
			},
		})
	}

	return images
}

func parsePrecompiledTag(tag string) (ofed, kernel string, ok bool) {
	rhcosIdx := strings.Index(tag, rhcosTagPrefix)
	if rhcosIdx <= 0 {
		return "", "", false
	}

	left := tag[:rhcosIdx]
	matches := kernelInTagRegexp.FindAllString(left, -1)
	if len(matches) == 0 {
		return "", "", false
	}

	kernel = matches[len(matches)-1]
	if !strings.HasSuffix(left, kernel) {
		return "", "", false
	}

	ofed = strings.TrimSuffix(left, "-"+kernel)
	if ofed == "" {
		return "", "", false
	}

	return ofed, kernel, true
}
