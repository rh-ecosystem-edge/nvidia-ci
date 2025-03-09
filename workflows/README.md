# Scripts and other files for automated workflows

## TODO

* Calculate the right tests to run for PR messages. First, collect changes and calculate the result version dictionary.
  Then write the versions to file, use the dictionary to calculate the tests to run. For example, use the last two GPU
  operator versions instead of hard-coded ones (as they might have change on the same day).
  Use a set for storing tests - this way each test will appear only once even if it should be triggered by multiple
  changes (e.g. a GPU operator update and an OpenShift update on the same day).

## Useful links

* https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions
* https://stackoverflow.com/questions/70458458/how-do-i-simply-run-a-python-script-from-github-repo-with-actions
* https://github.com/marketplace/actions/create-pull-request

## Updating container images

* See https://docker-docs.uclv.cu/registry/spec/api/

### OpenShift releases

Public, doesn't require authentication, but requires pagination.

Also see https://docs.redhat.com/en/documentation/red_hat_quay/latest/html-single/red_hat_quay_api_guide/index

```console
curl -SsL "https://quay.io/api/v1/repository/openshift-release-dev/ocp-release/tag/?limit=100&page=1&onlyActiveTags=true&filter_tag_name=like:4.1%.%-multi-x86_64" -H "Content-Type: application/json" | jq
```

### NVIDIA GPU operator (releases)

Public, but requires proxy authentication.

```console
token=$(curl -SsL "https://nvcr.io/proxy_auth?scope=repository:nvidia/gpu-operator:pull"  | jq -r .token)

# listing tags
curl -SsL -X GET "https://nvcr.io/v2/nvidia/gpu-operator/tags/list" -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" | jq
```

### NVIDIA GPU operator OLM bundle from main branch

Public, requires authentication. When running in a GitHub action, `secret.GITHUB_TOKEN` can be used to authentication
(see https://github.com/orgs/community/discussions/26279 and https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication).

```console
token=$(curl -SsL "https://ghcr.io/token?scope=repository:nvidia/gpu-operator:pull" | jq -r .token)

# listing tags
curl -SsL -X GET "https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/tags/list" -H "Content-Type: application/json" -H "Authorization: Bearer ${token}" | jq

# getting the digest of `main-latest`
curl -SsL -X GET "https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/manifests/main-latest" -H "Content-Type: application/json" -H "Authorization: Bearer ${token}" | jq -r .config.digest
```