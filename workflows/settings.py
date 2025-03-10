import json
import os

class Settings:
    ignored_versions: list[str]
    version_file_path: str
    tests_to_trigger_file_path: str

    def __init__(self):
        self.ignored_versions = json.loads(os.getenv("OCP_IGNORED_VERSIONS", "[]"))
        self.version_file_path = os.getenv("VERSION_FILE_PATH")
        self.tests_to_trigger_file_path = os.getenv("TEST_TO_TRIGGER_FILE_PATH")

settings = Settings()
