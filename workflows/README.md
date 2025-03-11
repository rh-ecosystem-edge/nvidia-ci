# Automation for updating versions and triggering CI jobs

## Fetching OpenShift release versions

Registry URL: [https://quay.io/repository/openshift-release-dev/ocp-release?tab=tags](https://quay.io/repository/openshift-release-dev/ocp-release?tab=tags)

The registry is public and doesn't require authentication. However, it must be used with pagination.

Example using [Red Hat Quay API](https://docs.redhat.com/en/documentation/red_hat_quay/latest/html-single/red_hat_quay_api_guide/index):

```console
curl -SsL "https://quay.io/api/v1/repository/openshift-release-dev/ocp-release/tag/?limit=100&page=1&onlyActiveTags=true&filter_tag_name=like:4.1%.%-multi-x86_64" -H "Content-Type: application/json" | jq
```

output:

```json
{
  "tags": [
    {
      "name": "4.15.47-multi-x86_64",
      "reversion": false,
      "start_ts": 1741359805,
      "manifest_digest": "sha256:12192f1c49ad70603b19f9ce8f886be14fdd4cf275dfdc2c9c867edf7f2f792d",
      "is_manifest_list": false,
      "size": 168953417,
      "last_modified": "Fri, 07 Mar 2025 15:03:25 -0000"
    },
    {
      ...
    }
    },
    {
      "name": "4.18.3-multi-x86_64",
      "reversion": false,
      "start_ts": 1741102000,
      "manifest_digest": "sha256:cda3ea1ebc84b5586cb45c61ff7c3dc6ac80a734adee9fb0c0a7d170029058da",
      "is_manifest_list": false,
      "size": 182092611,
      "last_modified": "Tue, 04 Mar 2025 15:26:40 -0000"
    }
  ],
  "page": 1,
  "has_additional": true
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

## Useful links

* [Workflow syntax for GitHub Actions](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions)
* [How do I simply run a python script from github repo with actions](https://stackoverflow.com/questions/70458458/how-do-i-simply-run-a-python-script-from-github-repo-with-actions)
* [Create Pull Request GitHub Action](https://github.com/marketplace/actions/create-pull-request)
