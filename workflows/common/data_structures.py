"""
Shared data structures and constants for CI dashboards.
"""

from dataclasses import dataclass
from typing import Any, Dict, Optional

# Constants for version field names (shared across dashboards)
OCP_FULL_VERSION = "ocp_full_version"
OPERATOR_VERSION = "operator_version"  # Generic operator version field

# GPU Operator specific (for backward compatibility)
GPU_OPERATOR_VERSION = "gpu_operator_version"

# Constants for job statuses
STATUS_SUCCESS = "SUCCESS"
STATUS_FAILURE = "FAILURE"
STATUS_ABORTED = "ABORTED"


@dataclass(frozen=True)
class TestResult:
    """Represents a single test run result (shared data structure)."""
    ocp_full_version: str
    operator_version: str  # Can be GPU or Network operator version
    test_status: str
    prow_job_url: str
    job_timestamp: str
    test_flavor: Optional[str] = None  # Optional: for dashboards with test flavors (NNO)

    def to_dict(self) -> Dict[str, Any]:
        """Convert TestResult to dictionary format for JSON serialization."""
        result = {
            OCP_FULL_VERSION: self.ocp_full_version,
            "operator_version": self.operator_version,
            "test_status": self.test_status,
            "prow_job_url": self.prow_job_url,
            "job_timestamp": self.job_timestamp,
        }
        # Include test_flavor only if it's set
        if self.test_flavor is not None:
            result["test_flavor"] = self.test_flavor
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TestResult":
        """Create TestResult from dictionary."""
        # Handle backward compatibility with GPU operator data
        operator_version = data.get("operator_version") or data.get(GPU_OPERATOR_VERSION)
        
        return cls(
            ocp_full_version=data[OCP_FULL_VERSION],
            operator_version=operator_version,
            test_status=data["test_status"],
            prow_job_url=data["prow_job_url"],
            job_timestamp=data["job_timestamp"],
            test_flavor=data.get("test_flavor"),
        )

