#!/usr/bin/env python3
"""Validate the manual request before any lab action; no discovery or defaults."""

import argparse
import json
import os
import re
import sys
from pathlib import Path


def fail(message):
    print("signed DOCA2 request error: " + message, file=sys.stderr)
    raise SystemExit(1)


version_re = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9._-]*)?$")
image_re = re.compile(
    r"^registry\.stage\.redhat\.io/nvidia/"
    r"(?P<image>doca-driver-rhel9|doca-driver-rhel10):(?P<tag>[A-Za-z0-9][A-Za-z0-9._-]*)$"
)
tag_re = re.compile(
    r"^(?P<version>[A-Za-z0-9][A-Za-z0-9._-]*)-"
    r"(?P<kernel>[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z0-9._+-]*\.el[0-9]+_[0-9]+\."
    r"(?:x86_64|aarch64(?:_64k)?|ppc64le|s390x))-"
    r"rhcos(?P<rhcos>[0-9]+\.[0-9]+)-"
    r"(?P<architecture>amd64|arm64|ppc64le|s390x)$"
)
pair_id_re = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
architecture_re = re.compile(r"^(?:amd64|arm64|ppc64le|s390x)$")
digest_re = re.compile(r"^sha256:[a-f0-9]{64}$")

def main():
    parser = argparse.ArgumentParser(description="Validate and publish one manual NNO DOCA2 signed request")
    parser.add_argument("--matrix", default=os.environ.get("NNO_DOCA2_SIGNED_MATRIX") or "nno_doca2_signed_matrix.json")
    parser.add_argument("--shared-dir", default=os.environ.get("SHARED_DIR", ""))
    parser.add_argument("--artifact-dir", default=os.environ.get("ARTIFACT_DIR", ""))
    args = parser.parse_args()
    matrix_path = Path(args.matrix)
    if not matrix_path.is_file():
        fail("required file %s is missing; no default or discovery fallback is allowed" % matrix_path)
    try:
        matrix = json.loads(matrix_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        fail("cannot read valid JSON from %s: %s" % (matrix_path, error))

    if not isinstance(matrix, dict):
        fail("top-level JSON value must be an object")
    if type(matrix.get("schema_version")) is not int or matrix["schema_version"] != 1:
        fail("schema_version must be integer 1")
    pairs = matrix.get("pairs")
    if not isinstance(pairs, list):
        fail("pairs must be an array")
    planned = [pair for pair in pairs if isinstance(pair, dict) and pair.get("status") == "planned"]
    if len(planned) != 1:
        fail("expected exactly one pair with status planned, found %d" % len(planned))

    pair = planned[0]
    pair_id = pair.get("id")
    if not isinstance(pair_id, str) or pair_id_re.fullmatch(pair_id.strip()) is None:
        fail("the planned pair must have a non-empty id containing only letters, digits, '.', '_' or '-'")
    pair_id = pair_id.strip()

    version = pair.get("openshift_version")
    if not isinstance(version, str):
        fail("the planned pair must have openshift_version as a string")
    version = version.strip()
    if version_re.fullmatch(version) is None:
        fail("openshift_version %r is not an exact z-stream version (expected major.minor.patch with an optional installer suffix)" % version)
    ocp_minor = ".".join(version.split(".")[:2])

    driver = pair.get("driver_requested")
    if not isinstance(driver, dict):
        fail("the planned pair must have driver_requested as an object")
    image = driver.get("image")
    if not isinstance(image, str):
        fail("driver_requested.image must be a complete staging pull specification")
    image = image.strip()
    image_match = image_re.fullmatch(image)
    if image_match is None:
        fail("driver_requested.image must be a tagged registry.stage.redhat.io/nvidia/doca-driver-rhel9 or doca-driver-rhel10 pull specification")
    tag_match = tag_re.fullmatch(image_match.group("tag"))
    if tag_match is None:
        fail("driver_requested.image tag is incomplete; it must include the OFED version, kernel, RHCOS minor and architecture")
    if tag_match.group("rhcos") != ocp_minor:
        fail("requested image targets RHCOS %s but openshift_version %s targets RHCOS %s" % (
            tag_match.group("rhcos"), version, ocp_minor
        ))

    expected_kernel = driver.get("kernel", "")
    if expected_kernel is not None and not isinstance(expected_kernel, str):
        fail("driver_requested.kernel must be a string when provided")
    expected_kernel = (expected_kernel or "").strip()
    if expected_kernel and expected_kernel != tag_match.group("kernel"):
        fail("driver_requested.kernel %r does not match image tag kernel %r" % (
            expected_kernel, tag_match.group("kernel")
        ))

    expected_architecture = driver.get("architecture", "")
    if expected_architecture is not None and not isinstance(expected_architecture, str):
        fail("driver_requested.architecture must be a string when provided")
    expected_architecture = (expected_architecture or "").strip()
    if expected_architecture and architecture_re.fullmatch(expected_architecture) is None:
        fail("driver_requested.architecture must be one of amd64, arm64, ppc64le or s390x")
    if expected_architecture and expected_architecture != tag_match.group("architecture"):
        fail("driver_requested.architecture %r does not match image tag architecture %r" % (
            expected_architecture, tag_match.group("architecture")
        ))

    expected_digest = driver.get("digest", "")
    if expected_digest is not None and not isinstance(expected_digest, str):
        fail("driver_requested.digest must be a string when provided")
    expected_digest = (expected_digest or "").strip()
    if expected_digest and digest_re.fullmatch(expected_digest) is None:
        fail("driver_requested.digest must be a sha256 digest")

    normalized_pair = {
        "id": pair_id,
        "status": "planned",
        "openshift_version": version,
        "driver_requested": {"image": image},
    }
    if expected_kernel:
        normalized_pair["driver_requested"]["kernel"] = expected_kernel
    if expected_architecture:
        normalized_pair["driver_requested"]["architecture"] = expected_architecture
    if expected_digest:
        normalized_pair["driver_requested"]["digest"] = expected_digest

    shared_dir = args.shared_dir.strip()
    artifact_dir = args.artifact_dir.strip()
    if not shared_dir:
        fail("SHARED_DIR is required")
    if not artifact_dir:
        fail("ARTIFACT_DIR is required to publish the normalized request")

    provenance = {}
    for source, key in (
        ("JOB_NAME", "job_name"),
        ("BUILD_ID", "build_id"),
        ("PULL_NUMBER", "pull_number"),
        ("PULL_PULL_SHA", "pull_sha"),
        ("PULL_REFS", "pull_refs"),
    ):
        value = os.environ.get(source, "").strip()
        if value:
            provenance[key] = value
    normalized = {
        "schema_version": 1,
        "pairs": [normalized_pair],
        "provenance": provenance,
    }

    shared = Path(shared_dir)
    artifacts = Path(artifact_dir)
    shared.mkdir(parents=True, exist_ok=True)
    artifacts.mkdir(parents=True, exist_ok=True)
    for name, value in (
        ("dpf-openshift-version", version),
        ("ofed-pullspec", image),
    ):
        (shared / name).write_text(value + "\n", encoding="utf-8")

    request_json = json.dumps(normalized, indent=2, sort_keys=True) + "\n"
    (shared / "nno-doca2-signed-request.json").write_text(request_json, encoding="utf-8")
    (artifacts / "nno-doca2-signed-request.json").write_text(request_json, encoding="utf-8")
    print("signed target pair=%s openshift_version=%s image=%s" % (pair_id, version, image))


if __name__ == "__main__":
    main()
