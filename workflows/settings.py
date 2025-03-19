import os

class Settings:
    ignored_versions: str
    version_file_path: str
    tests_to_trigger_file_path: str
    request_timeout_sec: int

    def __init__(self):
        self.ignored_versions = os.getenv("OCP_IGNORED_VERSIONS_REGEX", "x^").rstrip()
        self.version_file_path = os.getenv("VERSION_FILE_PATH")
        self.tests_to_trigger_file_path = os.getenv("TEST_TO_TRIGGER_FILE_PATH")
        self.request_timeout_sec = int(os.getenv("REQUEST_TIMEOUT_SECONDS", 30))

settings = Settings()
