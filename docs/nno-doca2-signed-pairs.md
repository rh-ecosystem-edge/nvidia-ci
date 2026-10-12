# Manual NNO DOCA2 signed pair request

The optional signed presubmit reads `nno_doca2_signed_matrix.json` from the
`nvidia-ci` PR checkout. The support branch contains no active request. Add a
request deliberately when validating a confirmed OCP/staging-image pair.

Use integer `schema_version: 1` and exactly one pair with `status: planned`.
The pair requires an `id`, exact `openshift_version` z-stream (including an
installer suffix when needed), and `driver_requested.image` containing the
complete tagged `registry.stage.redhat.io/nvidia/doca-driver-rhel9` or
`doca-driver-rhel10` pullspec. Its tag must encode the DOCA base version, worker
kernel, the NFD `os_release.ID` and `VERSION_ID` OS tag, and architecture.
Optional `kernel` and `architecture` assertions must agree with the tag. Optional
`digest` is the platform image digest observed on the workers, not a
multi-architecture OCI index digest.

For the final OCP comparison, installer architecture suffixes such as `-x86_64`
or `-multi` are removed. The exact patch and release prerelease identifiers such
as `-ec.1` must still match the cluster. The original installer version remains
in the deployment handoff and request artifact.

`scripts/nno/validate-signed-request.py` validates the contract before lab
provisioning. It writes `dpf-openshift-version`, `ofed-pullspec`, and the
normalized `nno-doca2-signed-request.json` into `SHARED_DIR`, and publishes the
normalized request with PR/build identity in `ARTIFACT_DIR`. Missing or invalid
input fails; there are no defaults, discovery, or replacement-image selection.

## Execution

The release workflow uses the existing DPF deployment and credential-merge
steps, and the existing day-2 and RDMA-enabled GPU installation commands.
`scripts/nno/run-signed-nno.sh deploy|gpudirect` loads the lab profile, applies
the exact image handoff, and invokes the existing NNO tests with the deployment
or shared-device GPUDirect label. It only handles signed NNO execution.

The signed flow uses DMA-BUF for GPUDirect. When the GPU suite sees the normalized
signed request in `SHARED_DIR`, `scripts/test-runner.sh` appends ClusterPolicy
patches setting `driver.kernelModuleType=open` and `driver.rdma.enabled=false`.
These override the release step's legacy peer-memory setting before the policy
is created. Other supplied policy patches are retained. GPU jobs without the
signed handoff keep their existing policy configuration.

The default amd64 workload image `quay.io/wabouham/ecosys-nvidia/rdma-tools:0.0.3`
was inspected at digest
`sha256:2752921f228f00b63016c001ed9482b9963125e07e61fbd47b2fa428b9e7d1b6`.
It installs CUDA Toolkit 12.8, builds perftest with CUDA headers, and already
passes `--use_cuda=0 --use_cuda_dmabuf` for both server and client when `-c yes`
is selected. The signed GPUDirect step keeps this CUDA selection enabled.
A lab-profile override of `NVIDIANETWORK_RDMA_TEST_IMAGE` must also support and
select DMA-BUF; inspect its entrypoint and CUDA/perftest versions before use.
Live GPUDirect validation still requires compatible GPU/NIC hardware in the lab.

`NVIDIANETWORK_OFED_DRIVER_PULLSPEC` is the sole new Go configuration input.
The runner maps it to the existing NicClusterPolicy repository/image/base-version
fields and reuses precompiled-driver and namespace pull-secret setup. The
existing staging client checks that the exact tag is accessible; it selects no
alternative. Both configured RDMA workers must match the image's kernel and
architecture and have a ready driver container using that exact image.

Existing NNO step reports remain unchanged. Additional per-worker image IDs are
published in `nno-signed-driver-images.json`. The signed result checker compares
the request with this invocation's reports and both workers' image IDs. Exact
OCP/image, digest evidence, and passing deployment or GPUDirect checks are
required. Missing, stale, failed, and skipped results fail the signed step.
The regular PR and nightly retain their original commands and reporting.

## Validation and merge order

Run the cluster-free tests with:

```sh
python3 -m unittest discover -s scripts/nno -p 'test_*.py' -v
```

Set `DOCA2_RELEASE_DIR` to a release checkout to exercise its signed callers.
These tests mock `make`; live driver and GPUDirect validation require the lab.

Merge release to deploy both optional jobs, then validate the nvidia-ci support
PR with `/test doca2-nno-e2e-pr`. For `/test doca2-nno-signed`, deliberately add
one verified request to the validation PR. Later signed validation PRs can change
only that JSON. The support branch itself does not choose a pair.
