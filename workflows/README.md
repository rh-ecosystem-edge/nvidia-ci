# Automation for updating versions and triggering CI jobs

## Fetching OpenShift release versions

Release URL: [https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted](https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted)

```console
curl -SsL "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted" -H "Content-Type: application/json" | jq -r '."4-stable"'
```

output:

```json
{
  "4.18.2",
  "4.18.1",
  "4.18.0-rc.10",
  ...
}
```

## NVIDIA GPU operator releases

Registry URL: [https://catalog.ngc.nvidia.com/orgs/nvidia/containers/gpu-operator/tags](https://catalog.ngc.nvidia.com/orgs/nvidia/containers/gpu-operator/tags)

The registry is public, but requires proxy authentication.

Example using [Docker Registry HTTP API V2](https://docker-docs.uclv.cu/registry/spec/api/):

```console
token=$(curl -SsL "https://nvcr.io/proxy_auth?scope=repository:nvidia/gpu-operator:pull"  | jq -r .token)
curl -SsL -X GET "https://nvcr.io/v2/nvidia/gpu-operator/tags/list" -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" | jq
```

output:

```json
{
  "name": "nvidia/gpu-operator",
  "tags": [
    "1.0.0-techpreview.1",
    "1.2.0",
    ...
    "b96b4d08",
    "devel-ubi8",
    "devel",
    "latest",
    "sha256-041e75a3df84039c2dbbd4b9d67763bd212138822dbb6dbc0008858c1c6eff8d.sig",
    "sha256-083661eba44beceedbf2d3e99a229a994c77a4b4c36a6d552a1b50db2022f12a.sbom",
    "sha256-083661eba44beceedbf2d3e99a229a994c77a4b4c36a6d552a1b50db2022f12a.vex",
    ...
    "v24.6.2-ubi8",
    "v24.6.2",
    "v24.9.0",
    "v24.9.1",
    "v24.9.2"
  ]
}
```

## NVIDIA GPU operator OLM bundle from main branch

Registry URL: [https://github.com/NVIDIA/gpu-operator/pkgs/container/gpu-operator%2Fgpu-operator-bundle](https://github.com/NVIDIA/gpu-operator/pkgs/container/gpu-operator%2Fgpu-operator-bundle)

The image is public, but ghrc.io requires authentication. When calling the API in the context of a GitHub action, `secret.GITHUB_TOKEN` can be used to authentication
(see [How to check if a container image exists on GHCR?](https://github.com/orgs/community/discussions/26279) and [Automatic token authentication](https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication)).

Example using [Docker Registry HTTP API V2](https://docker-docs.uclv.cu/registry/spec/api/):

```console
token=$(curl -SsL "https://ghcr.io/token?scope=repository:nvidia/gpu-operator:pull" | jq -r .token)
curl -SsL -X GET "https://ghcr.io/v2/nvidia/gpu-operator/gpu-operator-bundle/manifests/main-latest" -H "Content-Type: application/json" -H "Authorization: Bearer ${token}" | jq -r .config.digest
```

output:

```console
sha256:e8576f598bfa189085921ea6bf6d8335d78cb5302de51bcd117cfac0428e7665
```

## How to run unit tests

```console
for f in workflows/*_test.py; do echo "File: $f"; python "$f"; done
```

## Useful links

* [Workflow syntax for GitHub Actions](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions)
* [How do I simply run a python script from github repo with actions](https://stackoverflow.com/questions/70458458/how-do-i-simply-run-a-python-script-from-github-repo-with-actions)
* [Create Pull Request GitHub Action](https://github.com/marketplace/actions/create-pull-request)
