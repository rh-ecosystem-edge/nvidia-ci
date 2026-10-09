# Manual NNO DOCA2 signed pair request

The optional signed presubmit reads `nno_doca2_signed_matrix.json` from the
`nvidia-ci` PR checkout. The support branch contains no active request. Add a
request deliberately when validating a confirmed OCP/staging-image pair.

Use integer `schema_version: 1` and exactly one pair with `status: planned`.
The pair requires an `id`, exact `openshift_version` z-stream (including an
installer suffix when needed), and `driver_requested.image` containing the
complete tagged `registry.stage.redhat.io/nvidia/doca-driver-rhel9` or
`doca-driver-rhel10` pullspec. Its tag must encode the DOCA base version, worker
kernel, RHCOS minor and architecture. Optional `kernel` and `architecture`
assertions must agree with the tag. Optional `digest` is the platform image
digest observed on the workers, not a multi-architecture OCI index digest.

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

## Automatic discovery

`discover-signed-target.py` scans both staging RHEL9/RHEL10 driver repositories,
all DOCA streams and RHCOS minors for the configured architecture (default amd64).
Only OCI creation dates in the rolling seven-day window are eligible; missing,
invalid and future dates fail discovery. Old images repushed recently are excluded.
Aliases share repository/architecture/platform-digest identity; rebuilt tags are new.
The oldest eligible image wins, paired with the newest accepted release whose
default DTK kernel and RHEL family match and whose exact installer identifier
Assisted supports for that architecture.

Attempts come directly from signed Prow artifacts over eight days (one day margin):
an automatic selection consumes the image even if provisioning fails; manual
worker evidence counts when both runtime IDs map to its manifest/config/index.
There are no automatic retries; a manual request can test a skipped image.
An unfinished signed build other than the current JOB_NAME/BUILD_ID returns
`no_work` for at most 26 hours, covering starts during the scan and normal upload
lag after lease release. This exceeds Prow's 24-hour outer timeout, one-hour
termination grace and an upload allowance. Older builds missing `finished.json`
still have their published attempts scanned. Keep the cutoff above the full
lifetime of all configured signed jobs. Reads must succeed.
Prow can finish despite an artifact upload failure, so deduplication depends on
published attempt records. No ledger, report history update or receipt is needed.

Run under the exclusive lab lease with fresh handoff/output directories:

```sh
python3 scripts/nno/discover-signed-target.py \
  --registry-auth-file /path/to/staging-docker-auth.json \
  --release-auth-file /path/to/openshift-pull-secret \
  --installer-token-file /path/to/assisted-token \
  --output-dir "$ARTIFACT_DIR" --shared-dir "$SHARED_DIR"
```

Endpoints, architecture and signed job names are CLI options; repeat
`--signed-job` for all manual/automatic jobs if their deployed names differ.
Both registry credentials use Docker auth-file JSON. Outcomes in
`nno-discovery-result.json` are `selected`, `no_work` (exit zero), or `failed`
(nonzero with redacted diagnostics). Only selection passes one request to the
existing validator. Set `NNO_DISCOVERY_REQUIRED=true` and
`NNO_REGISTRY_AUTH_FILE` for automatic execution: preflight checks tag content
before each stage, and result checks bind both workers to the selected digests.
Manual Phase 1 runs need no registry binding or runtime credential.

The release follow-up must wire the lease, discovery, credentials and outcome
gates before lab mutations. Actual lab Assisted identifiers, DTK metadata and
CRI-O IDs still require a live run; these scripts alone enable no lab job.
