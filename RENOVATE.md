# Renovate Configuration guide for NVIDIA's CI

## Overview
Renovate automates updates in `versions.yaml` by tracking:

1. **GPU Operator Staging Bundle Image Digest** (from `registry.gitlab.com/nvidia/kubernetes/gpu-operator/staging/gpu-operator-bundle:main-latest`)
2. **NVIDIA GPU Operator Versions** (from NVIDIA's `nvcr.io/nvidia/gpu-operator`)
3. **Stable OpenShift Releases** (from `quay.io/openshift-release-dev/ocp-release`)
4. **OpenShift Release Candidates (RCs)** (from `quay.io/openshift-release-dev/ocp-release`)

It checks for updates and creates PRs to keep dependencies current.

---

## What Renovate Tracks

### GPU Operator Staging Bundle
- **Key in `versions.yaml`**: `gpu_operator_staging_digest`
- **Action**: Updates the digest when a new image is published.

### NVIDIA GPU Operator Versions
- **Keys in `versions.yaml`**: `gpu-<major>.<minor>` for previous, current, and next versions (e.g., `gpu-24.6`, `gpu-24.9`, `gpu-24.12`).
- **Action**: Updates versions when a new GPU Operator major, minor, or patch release is available.

### OpenShift Stable Releases
- **Keys in `versions.yaml`**: `ocp-<major>.<minor>` (e.g., `ocp-4.12`).
- **Action**: Updates versions when a new OpenShift stable release is available.

### OpenShift Release Candidates (RCs)
- **Keys in `versions.yaml`**: `ocp-rc-<major>.<minor>` (e.g., `ocp-rc-4.18`).
- **Action**: Tracks OpenShift RC updates in `versions.yaml`.

---

## Maintenance Tasks

- Remove EOL versions as needed from `versions.yaml` and `renovate.json`.
- Add unreleased versions to `versions.yaml` with the older major version upfront to allow Renovate to catch the new major/minor release image (e.g., `ocp-rc-4.23: "3.23.0-rc.0-x86_64"` while `4.23.0-rc.0` is not yet available).
- Update `renovate.json` to track new releases and to comment the correct tests for each pr.

---

## Automated Tests
PRs include test triggers, for example:

```
/test 4.12-stable-nvidia-gpu-operator-e2e-master
```

The Konflux bot is not a trusted user, so we need to label the PR as `/ok-to-test` and also manually trigger the tests included in the PR.
