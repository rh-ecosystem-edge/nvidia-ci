import json
import os
import re
from typing import Pattern, AnyStr


class Settings:
    quay_url_api: str
    tag_regex: Pattern[AnyStr]
    ignored_versions: list[str]
    version_file_path: str
    tests_to_trigger_file_path: str
    test_commands: set

    def __init__(self):
        self.quay_url_api = os.getenv("OCP_TAGS_URL")
        self.ignored_versions = json.loads(os.getenv("OCP_IGNORED_VERSIONS", "[]"))
        self.version_file_path = os.getenv("VERSION_FILE_PATH")
        self.tests_to_trigger_file_path = os.getenv("TEST_TO_TRIGGER_FILE_PATH")
        self.tag_regex = re.compile(r"^(?P<minor>\d+\.\d+)\.(?P<patch>\d+(?:-rc\.\d+)?)\-multi\-x86_64$")

settings = Settings()
