"""Tests for profile configuration and shared-snapshot orchestration."""

from datetime import datetime, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from gpu_operator_versions.profiles import (
    Profile,
    ProfileManifest,
    cron_day_matches,
    load_profiles,
    resolve_profiles,
    validate_test_command_template,
)
from gpu_operator_versions.update_versions import process_profile, run


SUPPORT_MATRIX = {
    "openshift_support": {},
    "defaults": {"unlisted_versions": {"status": "active"}},
}

REGULAR_TEST_COMMAND_TEMPLATE = (
    "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}"
)
SIGNED_TEST_COMMAND_TEMPLATE = (
    "/test {ocp_version}-stable-nvidia-gpu-operator-e2e-{gpu_version}-signed"
)


def make_profile(name: str, schedule: str, **overrides) -> Profile:
    values = {
        "name": name,
        "schedule": schedule,
        "version_file_path": f"/tmp/{name}-versions.json",
        "settings_file_path": f"/tmp/{name}-settings.json",
        "tests_to_trigger_file_path": f"/tmp/{name}-tests.txt",
        "test_command_template": REGULAR_TEST_COMMAND_TEMPLATE,
        "support_matrix": SUPPORT_MATRIX,
        "ignored_versions_regex": "x^",
        "minimum_gpu_channel": "",
        "check_catalog_availability": False,
        "request_timeout_sec": 30,
    }
    values.update(overrides)
    return Profile(**values)


class TestCronSchedules(unittest.TestCase):
    def test_regular_schedule_matches_sunday_through_friday(self):
        schedule = "0 0 * * 0-5"
        for day in range(7, 20):
            now = datetime(2026, 11, day, tzinfo=timezone.utc)
            # November 7 and 14 are Saturdays; November 8 is Sunday.
            expected = day not in (7, 14)
            self.assertEqual(cron_day_matches(schedule, now), expected, now.date())

    def test_signed_schedule_matches_tuesday_through_thursday(self):
        schedule = "0 0 * * 2-4"
        # November 10, 11, and 12 are Tuesday, Wednesday, and Thursday.
        for day in (10, 11, 12):
            now = datetime(2026, 11, day, tzinfo=timezone.utc)
            self.assertTrue(cron_day_matches(schedule, now))
        for day in (8, 9, 13, 14):
            now = datetime(2026, 11, day, tzinfo=timezone.utc)
            self.assertFalse(cron_day_matches(schedule, now))

    def test_sunday_can_be_expressed_as_zero_or_seven(self):
        sunday = datetime(2026, 11, 8, tzinfo=timezone.utc)
        self.assertTrue(cron_day_matches("0 0 * * 0", sunday))
        self.assertTrue(cron_day_matches("0 0 * * 7", sunday))

    def test_invalid_cron_expression(self):
        with self.assertRaises(ValueError):
            cron_day_matches("0 0 * *", datetime(2026, 11, 10, tzinfo=timezone.utc))


class TestTestCommandTemplateValidation(unittest.TestCase):
    def test_accepts_supported_placeholders(self):
        validate_test_command_template(REGULAR_TEST_COMMAND_TEMPLATE)
        validate_test_command_template(SIGNED_TEST_COMMAND_TEMPLATE)

    def test_rejects_unknown_placeholder(self):
        template = "/test {ocp}-stable-nvidia-gpu-operator-e2e-{gpu_version}"
        with self.assertRaises(ValueError):
            validate_test_command_template(template)

    def test_rejects_missing_placeholder(self):
        template = "/test {ocp_version}-stable-nvidia-gpu-operator-e2e"
        with self.assertRaises(ValueError):
            validate_test_command_template(template)


class TestProfileResolution(unittest.TestCase):
    def setUp(self):
        self.profiles = {
            "regular": make_profile("regular", "0 0 * * 0-5"),
            "signed": make_profile("signed", "0 0 * * 2-4", test_command_template=SIGNED_TEST_COMMAND_TEMPLATE),
        }

    def test_auto_resolves_regular_only_on_sunday(self):
        now = datetime(2026, 11, 8, tzinfo=timezone.utc)
        self.assertEqual(
            [profile.name for profile in resolve_profiles(self.profiles, "auto", now)],
            ["regular"],
        )

    def test_auto_resolves_both_profiles_on_tuesday(self):
        now = datetime(2026, 11, 10, tzinfo=timezone.utc)
        self.assertEqual(
            [profile.name for profile in resolve_profiles(self.profiles, "auto", now)],
            ["regular", "signed"],
        )

    def test_auto_resolves_no_profiles_on_saturday(self):
        now = datetime(2026, 11, 14, tzinfo=timezone.utc)
        self.assertEqual(resolve_profiles(self.profiles, "auto", now), [])

    def test_all_overrides_schedule(self):
        now = datetime(2026, 11, 14, tzinfo=timezone.utc)
        self.assertEqual(
            [profile.name for profile in resolve_profiles(self.profiles, "all", now)],
            ["regular", "signed"],
        )

    def test_explicit_profile_selection(self):
        now = datetime(2026, 11, 14, tzinfo=timezone.utc)
        self.assertEqual(
            [profile.name for profile in resolve_profiles(self.profiles, "signed", now)],
            ["signed"],
        )

    def test_unknown_profile_is_rejected(self):
        now = datetime(2026, 11, 10, tzinfo=timezone.utc)
        with self.assertRaises(ValueError):
            resolve_profiles(self.profiles, "unknown", now)


class TestLoadProfiles(unittest.TestCase):
    def test_load_profiles_and_settings(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            regular_settings = root / "regular-settings.json"
            signed_settings = root / "signed-settings.json"
            regular_versions = root / "regular-versions.json"
            signed_versions = root / "signed-versions.json"
            regular_output = root / "regular-output.txt"
            signed_output = root / "signed-output.txt"
            combined_output = root / "combined-output.txt"
            active_output = root / "active-output.txt"
            manifest_path = root / "profiles.json"

            regular_settings.write_text(json.dumps({
                "ignored_versions_regex": "^4.10$",
                "openshift_support": {},
            }), encoding="utf-8")
            signed_settings.write_text(json.dumps({
                "ignored_versions_regex": "^4.1[0-9]$",
                "minimum_gpu_channel": "26.3",
                "openshift_support": {},
            }), encoding="utf-8")
            manifest = {
                "request_timeout_sec": 7,
                "combined_output_file": str(combined_output),
                "active_profiles_file": str(active_output),
                "profiles": {
                    "regular": {
                        "schedule": "0 0 * * 0-5",
                        "version_file_path": str(regular_versions),
                        "settings_file_path": str(regular_settings),
                        "tests_to_trigger_file_path": str(regular_output),
                        "test_command_template": REGULAR_TEST_COMMAND_TEMPLATE,
                        "check_catalog_availability": False,
                    },
                    "signed": {
                        "schedule": "0 0 * * 2-4",
                        "version_file_path": str(signed_versions),
                        "settings_file_path": str(signed_settings),
                        "tests_to_trigger_file_path": str(signed_output),
                        "test_command_template": SIGNED_TEST_COMMAND_TEMPLATE,
                        "check_catalog_availability": False,
                    },
                },
            }
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")

            loaded = load_profiles(manifest_path)

            self.assertEqual(loaded.request_timeout_sec, 7)
            self.assertEqual(set(loaded.profiles), {"regular", "signed"})
            self.assertEqual(loaded.profiles["regular"].ignored_versions_regex, "^4.10$")
            self.assertEqual(loaded.profiles["signed"].minimum_gpu_channel, "26.3")
            self.assertEqual(loaded.profiles["signed"].test_command_template, SIGNED_TEST_COMMAND_TEMPLATE)


class TestSharedSnapshotOrchestration(unittest.TestCase):
    def test_run_discovers_once_and_dispatches_same_snapshot(self):
        regular = make_profile(
            "regular",
            "0 0 * * 0-5",
            version_file_path="/tmp/regular-versions.json",
            tests_to_trigger_file_path="/tmp/regular-output.txt",
        )
        signed = make_profile(
            "signed",
            "0 0 * * 2-4",
            version_file_path="/tmp/signed-versions.json",
            tests_to_trigger_file_path="/tmp/signed-output.txt",
            test_command_template=SIGNED_TEST_COMMAND_TEMPLATE,
        )

        with tempfile.TemporaryDirectory() as temp_dir:
            combined = Path(temp_dir) / "combined.txt"
            active = Path(temp_dir) / "active.txt"
            manifest = ProfileManifest(
                profiles={"regular": regular, "signed": signed},
                combined_output_file=str(combined),
                active_profiles_file=str(active),
                request_timeout_sec=30,
            )

            snapshot = {"gpu-main-latest": "sha", "gpu-operator": {}, "ocp": []}

            with patch(
                "gpu_operator_versions.update_versions.discover_versions",
                return_value=snapshot,
            ) as discover, patch(
                "gpu_operator_versions.update_versions.process_profile",
                side_effect=[{"/test regular"}, {"/test signed"}],
            ) as process:
                active_names = run(manifest, "all", datetime(2026, 11, 10, tzinfo=timezone.utc))

            self.assertEqual(active_names, ["regular", "signed"])
            self.assertEqual(discover.call_count, 1)
            self.assertEqual(process.call_count, 2)
            self.assertIs(process.call_args_list[0].args[0], snapshot)
            self.assertIs(process.call_args_list[1].args[0], snapshot)
            self.assertEqual(combined.read_text(encoding="utf-8"), "/test regular\n/test signed\n")
            self.assertEqual(active.read_text(encoding="utf-8"), "regular\nsigned\n")

    def test_run_with_no_active_profiles_writes_empty_outputs(self):
        regular = make_profile("regular", "0 0 * * 1")

        with tempfile.TemporaryDirectory() as temp_dir:
            combined = Path(temp_dir) / "combined.txt"
            active = Path(temp_dir) / "active.txt"
            manifest = ProfileManifest(
                profiles={"regular": regular},
                combined_output_file=str(combined),
                active_profiles_file=str(active),
                request_timeout_sec=30,
            )

            with patch("gpu_operator_versions.update_versions.discover_versions") as discover:
                active_names = run(manifest, "auto", datetime(2026, 11, 14, tzinfo=timezone.utc))

            self.assertEqual(active_names, [])
            discover.assert_not_called()
            self.assertEqual(combined.read_text(encoding="utf-8"), "")
            self.assertEqual(active.read_text(encoding="utf-8"), "")


class TestProcessProfile(unittest.TestCase):
    def test_signed_profile_uses_minimum_channel_and_command_template(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            version_file = root / "signed-versions.json"
            output_file = root / "signed-tests.txt"
            settings_file = root / "signed-settings.json"
            settings_file.write_text(json.dumps({
                "ignored_versions_regex": "^4.19$",
                "minimum_gpu_channel": "26.3",
                "openshift_support": {},
                "defaults": {"unlisted_versions": {"status": "active"}},
            }), encoding="utf-8")
            version_file.write_text(json.dumps({
                "gpu-main-latest": "old-sha",
                "gpu-operator": {"26.3": "26.3.0"},
                "ocp": {"4.20": "4.20.0"},
            }), encoding="utf-8")

            profile = Profile(
                name="signed",
                schedule="0 0 * * 2-4",
                version_file_path=str(version_file),
                settings_file_path=str(settings_file),
                tests_to_trigger_file_path=str(output_file),
                test_command_template=SIGNED_TEST_COMMAND_TEMPLATE,
                support_matrix=json.loads(settings_file.read_text(encoding="utf-8")),
                ignored_versions_regex="^4.19$",
                minimum_gpu_channel="26.3",
                check_catalog_availability=False,
                request_timeout_sec=30,
            )
            snapshot = {
                "gpu-main-latest": "new-sha",
                "gpu-operator": {
                    "25.3": "25.3.0",
                    "26.3": "26.3.1",
                    "26.7": "26.7.0",
                },
                "ocp": ["4.19.9", "4.20.1", "4.20.2", "4.21.0"],
            }

            commands = process_profile(snapshot, profile)
            updated = json.loads(version_file.read_text(encoding="utf-8"))

            self.assertEqual(updated["gpu-operator"], {"26.3": "26.3.1", "26.7": "26.7.0"})
            self.assertEqual(updated["ocp"], {"4.20": "4.20.2", "4.21": "4.21.0"})
            self.assertTrue(any(command.endswith("-signed") for command in commands))
            self.assertTrue(all("-signed" in command for command in commands if command.startswith("/test")))


if __name__ == "__main__":
    unittest.main()
