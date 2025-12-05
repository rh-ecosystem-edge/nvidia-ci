#!/usr/bin/env python3
"""
Test script for catalog checker functionality.
Can be run locally to verify catalog checking works.
"""

import sys
from workflows.gpu_operator_versions.catalog_checker import (
    fetch_gpu_operator_catalog_entries,
    is_available_in_catalog_entries
)


def test_single_version_multiple_ocp():
    """Test checking a single GPU operator version across multiple OCP versions."""
    print("=" * 70)
    print("Test 1: Single GPU operator version across multiple OCP versions")
    print("=" * 70)

    ocp_versions = ["4.20", "4.19", "4.18"]
    gpu_version = "24.6.2"
    catalog_entries = fetch_gpu_operator_catalog_entries(
        gpu_versions=[gpu_version],
        ocp_versions=ocp_versions
    )

    print("\nResults:")
    available_count = 0
    for ocp in sorted(ocp_versions, reverse=True):
        available = is_available_in_catalog_entries(catalog_entries, gpu_version, ocp)
        status = "✓" if available else "✗"
        print(f"  OCP {ocp}: {status}")
        if available:
            available_count += 1

    total_count = len(ocp_versions)
    passed = available_count == total_count
    print(f"\nResult: {'PASS ✓' if passed else f'PARTIAL ({available_count}/{total_count})'}")
    return passed


def test_multiple_versions_multiple_ocp():
    """Test checking multiple GPU operator versions across multiple OCP versions."""
    print("\n" + "=" * 70)
    print("Test 2: Multiple GPU operator versions across multiple OCP versions")
    print("=" * 70)

    gpu_versions = ["25.10.1", "24.6.2"]
    ocp_versions = ["4.20", "4.19"]
    catalog_entries = fetch_gpu_operator_catalog_entries(
        gpu_versions=gpu_versions,
        ocp_versions=ocp_versions
    )

    print("\nResults:")
    all_available = True
    for gpu_ver in sorted(gpu_versions):
        print(f"  GPU operator {gpu_ver}:")
        for ocp in sorted(ocp_versions, reverse=True):
            available = is_available_in_catalog_entries(catalog_entries, gpu_ver, ocp)
            status = "✓" if available else "✗"
            print(f"    OCP {ocp}: {status}")
            if not available:
                all_available = False

    print(f"\nResult: {'PASS ✓' if all_available else 'PARTIAL'}")
    return all_available


def test_nonexistent_version():
    """Test with a version that doesn't exist."""
    print("\n" + "=" * 70)
    print("Test 3: Non-existent GPU operator version")
    print("=" * 70)

    gpu_version = "99.99.99"
    ocp_version = "4.20"
    catalog_entries = fetch_gpu_operator_catalog_entries(
        gpu_versions=[gpu_version],
        ocp_versions=[ocp_version]
    )

    found = is_available_in_catalog_entries(catalog_entries, gpu_version, ocp_version)

    print(f"\nResult: {'FAIL ✗ (should not exist)' if found else 'PASS ✓ (correctly not found)'}")
    return not found


def test_mixed_availability():
    """Test with versions that may have different availability across OCP versions."""
    print("\n" + "=" * 70)
    print("Test 4: GPU operator availability across wide OCP range")
    print("=" * 70)

    gpu_version = "25.10.1"
    ocp_versions = ["4.20", "4.19", "4.16", "4.13"]
    catalog_entries = fetch_gpu_operator_catalog_entries(
        gpu_versions=[gpu_version],
        ocp_versions=ocp_versions
    )

    print("\nResults:")
    for ocp in sorted(ocp_versions, reverse=True):
        available = is_available_in_catalog_entries(catalog_entries, gpu_version, ocp)
        status = "✓" if available else "✗"
        print(f"  OCP {ocp}: {status}")

    # This test is informational - we expect it might not be in older versions
    print("\nResult: INFORMATIONAL (showing version distribution)")
    return True


def main():
    print("\n" + "=" * 70)
    print("GPU Operator Catalog Checker - Test Suite")
    print("=" * 70)
    print("\nNOTE: These tests require:")
    print("  1. Internet connection")
    print("  2. Access to catalog.redhat.com API (public, no auth required)")
    print("  3. Tests complete in ~5-10 seconds")
    print()

    tests = [
        ("Single version, multiple OCP", test_single_version_multiple_ocp),
        ("Multiple versions, multiple OCP", test_multiple_versions_multiple_ocp),
        ("Non-existent version", test_nonexistent_version),
        ("Wide OCP range", test_mixed_availability),
    ]

    results = []
    for name, test_func in tests:
        try:
            passed = test_func()
            results.append((name, passed, None))
        except (KeyboardInterrupt, SystemExit):
            raise
        except Exception as e:
            print(f"\n✗ Test failed with exception: {e}")
            import traceback
            traceback.print_exc()
            results.append((name, False, str(e)))

    # Summary
    print("\n" + "=" * 70)
    print("Test Summary")
    print("=" * 70)

    passed_count = sum(1 for _, passed, _ in results if passed)
    total_count = len(results)

    for name, passed, error in results:
        status = "✓ PASS" if passed else "✗ FAIL"
        print(f"{status}: {name}")
        if error:
            print(f"  Error: {error}")

    print(f"\nTotal: {passed_count}/{total_count} tests passed")

    return 0 if passed_count == total_count else 1


if __name__ == "__main__":
    sys.exit(main())


