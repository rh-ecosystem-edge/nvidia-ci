"""
Validation utilities for CI dashboards.
Shared validation logic for GPU Operator and Network Operator dashboards.
"""

from typing import Dict, Any
import semver


def is_valid_ocp_version(version: str) -> bool:
    """
    Check if a version string is a valid OpenShift version (not an infrastructure type).
    
    Args:
        version: Version string to validate
        
    Returns:
        True if valid OCP version, False otherwise
        
    Examples:
        >>> is_valid_ocp_version("4.17.16")
        True
        >>> is_valid_ocp_version("doca4")
        False
        >>> is_valid_ocp_version("bare-metal")
        False
    """
    invalid_keys = ["doca4", "bare-metal", "hosted", "unknown"]
    if version.lower() in invalid_keys:
        return False
    if not version or not version[0].isdigit():
        return False
    if '.' not in version:
        return False
    parts = version.split('.')
    if len(parts) < 2:
        return False
    try:
        int(parts[0])
        int(parts[1])
        return True
    except (ValueError, IndexError):
        return False


def has_valid_semantic_versions(result: Dict[str, Any], ocp_key: str = "ocp_full_version", operator_key: str = "operator_version") -> bool:
    """
    Check if both OCP and operator versions contain valid semantic versions.
    
    Args:
        result: Test result dictionary containing version fields
        ocp_key: Key name for OCP version in result dict
        operator_key: Key name for operator version in result dict
        
    Returns:
        True if both versions are valid semantic versions, False otherwise
    """
    try:
        ocp_version = result.get(ocp_key, "")
        operator_version = result.get(operator_key, "")
        
        if not ocp_version or not operator_version:
            return False
        
        # Parse OCP version (should be like "4.14.1")
        semver.VersionInfo.parse(ocp_version)
        
        # Parse operator version (may have suffix like "23.9.0(bundle)" - extract version part)
        operator_version_clean = operator_version.split("(")[0].strip()
        semver.VersionInfo.parse(operator_version_clean)
        
    except (ValueError, TypeError):
        return False
    else:
        return True


def is_infrastructure_type(value: str) -> bool:
    """
    Check if a string is an infrastructure type rather than a version.
    
    Args:
        value: String to check
        
    Returns:
        True if it's an infrastructure type, False otherwise
    """
    infrastructure_types = ["doca4", "bare-metal", "hosted", "unknown"]
    return value.lower() in infrastructure_types

