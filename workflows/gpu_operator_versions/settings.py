import os
import json
from pathlib import Path

class Settings:
    ignored_versions: str
    version_file_path: str
    tests_to_trigger_file_path: str
    settings_file_path: str
    request_timeout_sec: int
    support_matrix: dict

    def __init__(self):
        self.version_file_path = os.getenv("VERSION_FILE_PATH")
        self.tests_to_trigger_file_path = os.getenv("TEST_TO_TRIGGER_FILE_PATH")
        self.request_timeout_sec = int(os.getenv("REQUEST_TIMEOUT_SECONDS", 30))

        # Settings file can be specified via env var or defaults to settings.json in same directory
        self.settings_file_path = os.getenv(
            "SETTINGS_FILE_PATH",
            str(Path(__file__).parent / "settings.json")
        )

        if not self.version_file_path:
            raise ValueError("VERSION_FILE_PATH must be specified")
        if not self.tests_to_trigger_file_path:
            raise ValueError("TEST_TO_TRIGGER_FILE_PATH must be specified")

        # Load support matrix
        self.support_matrix = self._load_support_matrix()

        # Get ignored_versions from support matrix or fall back to env var
        self.ignored_versions = self.support_matrix.get(
            "ignored_versions_regex",
            os.getenv("OCP_IGNORED_VERSIONS_REGEX", "x^")
        ).rstrip()

    def _load_support_matrix(self) -> dict:
        """Load the OpenShift support matrix configuration."""
        try:
            with open(self.settings_file_path, 'r') as f:
                return json.load(f)
        except FileNotFoundError as e:
            raise FileNotFoundError(
                f"Settings file not found: {self.settings_file_path}. "
                f"This file is required to determine which OpenShift versions are in maintenance mode "
                f"and which tests should be generated. Please ensure the file exists."
            ) from e

