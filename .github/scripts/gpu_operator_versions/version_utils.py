"""Version comparison utilities shared across GPU operator workflows."""

from semver import Version


def max_version(a: str, b: str) -> str:
    """
    Parse and compare two semver versions.
    Return the higher of them.
    """
    return str(max(map(Version.parse, (a, b))))

def get_latest_versions(versions: list, count: int) -> list:
    if count <= 0:
        raise ValueError("count must be positive")
    sorted_versions = get_sorted_versions(versions)
    return sorted_versions[-count:] if len(sorted_versions) > count else sorted_versions


def get_earliest_versions(versions: list, count: int) -> list:
    if count <= 0:
        raise ValueError("count must be positive")
    sorted_versions = get_sorted_versions(versions)
    return sorted_versions[:count] if len(sorted_versions) > count else sorted_versions


def get_sorted_versions(versions: list) -> list:
    return sorted(versions, key=lambda v: tuple(map(int, v.split('.'))))