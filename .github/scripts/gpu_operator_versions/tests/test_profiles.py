"""Tests for profile configuration, weekday resolution, and atomic updates."""

from datetime import datetime, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from gpu_operator_versions.profiles import (
    Profile,
    ProfileManifest,
    load_profiles,
    partition_profiles,
    profile_is_active,
    validate_test_command_template,
)
from gpu_operator_versions.update_versions import (
    ProfileUpdate,
    prepare_profile,
    render_test_comments,
    run,
    write_profile_update,
)


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

ALL_WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
SIGNED_WEEKDAYS = ["Tue", "Wed", "Thu"]


def make_profile(name: str, weekdays: list[str], **overrides) -> Profile:
    values = {
        "name": name,
        "active_weekdays": frozenset(weekdays),
        "version_file_path": f"/tmp/{name}-versions.json",
        "settings_file_path": f"/tmp/{name}-settings.json",
        "test_command_template": REGULAR_TEST_COMMAND_TEMPLATE,
        "support_matrix": SUPPORT_MATRIX,
        "ignored_versions_regex": "x^",
        "minimum_gpu_channel": "",
        "check_catalog_availability": False,
        "request_timeout_sec": 30,
    }
    values.update(overrides)
    return Profile(**values)


class TestWeekdayResolution(unittest.TestCase):
    def setUp(self):
        self.profiles = {
            "regular": make_profile("regular", ALL_WEEKDAYS),
            "signed": make_profile(
                "signed",
                SIGNED_WEEKDAYS,
                test_command_template=SIGNED_TEST_COMMAND_TEMPLATE,
            ),
        }

    def test_regular_profile_is_active_every_day(self):
        for day in range(7, 20):
            now = datetime(2026, 11, day, tzinfo=timezone.utc)
            self.assertTrue(profile_is_active(self.profiles["regular"], now), now.date())

    def test_signed_profile_is_active_tuesday_through_thursday(self):
        for day in (10, 11, 12):
            now = datetime(2026, 11, day, tzinfo=timezone.utc)
            self.assertTrue(profile_is_active(self.profiles["signed"], now), now.date())
        for day in (8, 9, 13, 14):
            now = datetime(2026, 11, day, tzinfo=timezone.utc)
            self.assertFalse(profile_is_active(self.profiles["signed"], now), now.date())

    def test_partition_resolves_both_profiles_on_tuesday(self):
        active, skipped = partition_profiles(
            self.profiles,
            datetime(2026, 11, 10, tzinfo=timezone.utc),
        )
        self.assertEqual([profile.name for profile in active], ["regular", "signed"])
        self.assertEqual(skipped, [])

    def test_partition_resolves_regular_and_skips_signed_on_saturday(self):
        active, skipped = partition_profiles(
            self.profiles,
            datetime(2026, 11, 14, tzinfo=timezone.utc),
        )
        self.assertEqual([profile.name for profile in active], ["regular"])
        self.assertEqual([profile.name for profile in skipped], ["signed"])


class TestProfileManifestLoading(unittest.TestCase):
    def _write_manifest(self, root: Path, weekdays: object) -> Path:
        settings = root / "settings.json"
        settings.write_text(json.dumps({
            "ignored_versions_regex": "^4.10$",
            "openshift_support": {},
        }), encoding="utf-8")
        manifest = {
            "request_timeout_sec": 7,
            "tests_to_trigger_file_path": str(root / "tests_to_trigger.txt"),
            "test_comments_file_path": str(root / "test_comments.txt"),
            "new_gpu_channels_file": str(root / "new_gpu_channels.txt"),
            "profiles": {
                "profile": {
                    "active_weekdays": weekdays,
                    "version_file_path": str(root / "versions.json"),
                    "settings_file_path": str(settings),
                    "test_command_template": REGULAR_TEST_COMMAND_TEMPLATE,
                    "check_catalog_availability": False,
                },
            },
        }
        manifest_path = root / "profiles.json"
        manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
        return manifest_path

    def test_load_profiles_accepts_common_weekday_spellings(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            manifest_path = self._write_manifest(
                Path(temp_dir), ["Su", "TUES", "wednesday", "Thurs"]
            )
            loaded = load_profiles(manifest_path)
            profile = loaded.profiles["profile"]
            self.assertEqual(profile.active_weekdays, {"Sun", "Tue", "Wed", "Thu"})
            self.assertEqual(
                loaded.tests_to_trigger_file_path,
                str(Path(temp_dir) / "tests_to_trigger.txt"),
            )

    def test_load_profiles_accepts_all_weekdays_value(self):
        for weekdays in ("all", "ALL", ["all"]):
            with tempfile.TemporaryDirectory() as temp_dir:
                manifest_path = self._write_manifest(Path(temp_dir), weekdays)
                loaded = load_profiles(manifest_path)
                profile = loaded.profiles["profile"]
                self.assertEqual(profile.active_weekdays, set(ALL_WEEKDAYS))

    def test_load_profiles_rejects_all_combined_with_weekdays(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            manifest_path = self._write_manifest(Path(temp_dir), ["all", "Mon"])
            with self.assertRaises(ValueError):
                load_profiles(manifest_path)

    def test_load_profiles_rejects_missing_weekdays(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            manifest_path = self._write_manifest(Path(temp_dir), ["Mon"])
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
            del manifest["profiles"]["profile"]["active_weekdays"]
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")

            with self.assertRaises(ValueError):
                load_profiles(manifest_path)

    def test_load_profiles_rejects_invalid_weekday(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            manifest_path = self._write_manifest(Path(temp_dir), ["Notaday"])
            with self.assertRaises(ValueError):
                load_profiles(manifest_path)

    def test_load_profiles_rejects_empty_weekdays(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            manifest_path = self._write_manifest(Path(temp_dir), [])
            with self.assertRaises(ValueError):
                load_profiles(manifest_path)

    def test_load_profiles_rejects_duplicate_weekdays(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            manifest_path = self._write_manifest(Path(temp_dir), ["Sun", "Sunday"])
            with self.assertRaises(ValueError):
                load_profiles(manifest_path)


class TestCommandTemplateValidation(unittest.TestCase):
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


class TestAtomicProfileUpdates(unittest.TestCase):
    def _make_manifest(self, root: Path, regular: Profile, signed: Profile) -> ProfileManifest:
        return ProfileManifest(
            profiles={"regular": regular, "signed": signed},
            tests_to_trigger_file_path=str(root / "tests_to_trigger.txt"),
            test_comments_file_path=str(root / "test_comments.txt"),
            new_gpu_channels_file=str(root / "new_gpu_channels.txt"),
            request_timeout_sec=30,
        )

    def _make_profiles(self, root: Path) -> tuple[Profile, Profile]:
        regular = make_profile(
            "regular",
            ALL_WEEKDAYS,
            version_file_path=str(root / "regular-versions.json"),
        )
        signed = make_profile(
            "signed",
            SIGNED_WEEKDAYS,
            version_file_path=str(root / "signed-versions.json"),
            test_command_template=SIGNED_TEST_COMMAND_TEMPLATE,
        )
        return regular, signed

    def test_run_with_no_active_profiles_writes_schedule_information(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            profile = make_profile(
                "signed",
                SIGNED_WEEKDAYS,
                version_file_path=str(root / "versions.json"),
            )
            manifest = ProfileManifest(
                profiles={"signed": profile},
                tests_to_trigger_file_path=str(root / "tests_to_trigger.txt"),
                test_comments_file_path=str(root / "test_comments.txt"),
                new_gpu_channels_file=str(root / "new_gpu_channels.txt"),
                request_timeout_sec=30,
            )

            with patch("gpu_operator_versions.update_versions.discover_versions") as discover:
                active_names = run(
                    manifest,
                    datetime(2026, 11, 14, tzinfo=timezone.utc),
                )

            self.assertEqual(active_names, [])
            discover.assert_not_called()
            self.assertEqual(
                (root / "tests_to_trigger.txt").read_text(encoding="utf-8"),
                "",
            )
            self.assertEqual(
                (root / "new_gpu_channels.txt").read_text(encoding="utf-8"),
                "",
            )
            comments = (root / "test_comments.txt").read_text(encoding="utf-8")
            self.assertIn(
                "INFO: **signed** was not run because Saturday is outside "
                "its configured weekdays (Tue, Wed, Thu).",
                comments,
            )

    def test_run_calculates_all_profiles_before_writing_any(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            regular, signed = self._make_profiles(root)
            manifest = self._make_manifest(root, regular, signed)
            regular_update = ProfileUpdate(
                profile=regular,
                final_versions={"gpu-main-latest": "sha"},
                commands={"/test regular"},
                comments={"**regular:** WARNING: example"},
                new_gpu_channels={"26.7"},
            )
            signed_update = ProfileUpdate(
                profile=signed,
                final_versions={"gpu-main-latest": "sha"},
                commands={"/test signed"},
                comments=set(),
                new_gpu_channels=set(),
            )
            snapshot = {"gpu-main-latest": "sha", "gpu-operator": {}, "ocp": []}

            with patch(
                "gpu_operator_versions.update_versions.discover_versions",
                return_value=snapshot,
            ) as discover, patch(
                "gpu_operator_versions.update_versions.prepare_profile",
                side_effect=[regular_update, signed_update],
            ) as prepare, patch(
                "gpu_operator_versions.update_versions.write_profile_update",
            ) as write:
                active_names = run(
                    manifest,
                    datetime(2026, 11, 10, tzinfo=timezone.utc),
                )

            self.assertEqual(active_names, ["regular", "signed"])
            self.assertEqual(discover.call_count, 1)
            self.assertIs(prepare.call_args_list[0].args[0], snapshot)
            self.assertIs(prepare.call_args_list[1].args[0], snapshot)
            self.assertEqual(write.call_count, 2)
            self.assertEqual(
                (root / "tests_to_trigger.txt").read_text(encoding="utf-8"),
                "/test regular\n/test signed\n",
            )
            self.assertIn(
                "**regular:** WARNING: example",
                (root / "test_comments.txt").read_text(encoding="utf-8"),
            )
            self.assertEqual(
                (root / "new_gpu_channels.txt").read_text(encoding="utf-8"),
                "26.7\n",
            )

    def test_run_separates_commands_from_schedule_information(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            regular, signed = self._make_profiles(root)
            manifest = self._make_manifest(root, regular, signed)
            regular_update = ProfileUpdate(
                profile=regular,
                final_versions={"gpu-main-latest": "sha"},
                commands={"/test regular"},
                comments=set(),
                new_gpu_channels=set(),
            )
            snapshot = {"gpu-main-latest": "sha", "gpu-operator": {}, "ocp": []}

            with patch(
                "gpu_operator_versions.update_versions.discover_versions",
                return_value=snapshot,
            ), patch(
                "gpu_operator_versions.update_versions.prepare_profile",
                return_value=regular_update,
            ), patch(
                "gpu_operator_versions.update_versions.write_profile_update",
            ):
                active_names = run(
                    manifest,
                    datetime(2026, 11, 14, tzinfo=timezone.utc),
                )

            self.assertEqual(active_names, ["regular"])
            self.assertEqual(
                (root / "tests_to_trigger.txt").read_text(encoding="utf-8"),
                "/test regular\n",
            )
            comments = (root / "test_comments.txt").read_text(encoding="utf-8")
            self.assertNotIn("/test regular", comments)
            self.assertIn(
                "INFO: **signed** was not run because Saturday is outside "
                "its configured weekdays (Tue, Wed, Thu).",
                comments,
            )

    def test_run_writes_nothing_when_any_profile_calculation_fails(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            regular, signed = self._make_profiles(root)
            manifest = self._make_manifest(root, regular, signed)
            (root / "regular-versions.json").write_text("original regular", encoding="utf-8")
            (root / "signed-versions.json").write_text("original signed", encoding="utf-8")
            regular_update = ProfileUpdate(
                profile=regular,
                final_versions={"gpu-main-latest": "new"},
                commands={"/test regular"},
                comments=set(),
                new_gpu_channels={"26.7"},
            )
            snapshot = {"gpu-main-latest": "sha", "gpu-operator": {}, "ocp": []}

            with patch(
                "gpu_operator_versions.update_versions.discover_versions",
                return_value=snapshot,
            ), patch(
                "gpu_operator_versions.update_versions.prepare_profile",
                side_effect=[regular_update, RuntimeError("signed profile failed")],
            ):
                with self.assertRaises(RuntimeError):
                    run(manifest, datetime(2026, 11, 10, tzinfo=timezone.utc))

            self.assertEqual(
                (root / "regular-versions.json").read_text(encoding="utf-8"),
                "original regular",
            )
            self.assertEqual(
                (root / "signed-versions.json").read_text(encoding="utf-8"),
                "original signed",
            )
            for output in (
                "tests_to_trigger.txt",
                "test_comments.txt",
                "new_gpu_channels.txt",
            ):
                self.assertFalse((root / output).exists())

    def test_prepare_profile_separates_commands_comments_and_channels(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            version_file = root / "versions.json"
            version_file.write_text(json.dumps({
                "gpu-main-latest": "old-sha",
                "gpu-operator": {"26.3": "26.3.0"},
                "ocp": {"4.20": "4.20.0"},
            }), encoding="utf-8")

            profile = make_profile(
                "signed",
                SIGNED_WEEKDAYS,
                version_file_path=str(version_file),
                test_command_template=SIGNED_TEST_COMMAND_TEMPLATE,
                ignored_versions_regex="^4.19$",
                minimum_gpu_channel="26.3",
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

            update = prepare_profile(snapshot, profile)
            self.assertTrue(update.commands)
            self.assertTrue(all(command.startswith("/test ") for command in update.commands))
            self.assertEqual(update.new_gpu_channels, {"26.7"})

            write_profile_update(update)
            self.assertEqual(
                json.loads(version_file.read_text(encoding="utf-8"))["gpu-operator"],
                {"26.3": "26.3.1", "26.7": "26.7.0"},
            )

    def test_render_test_comments_uses_github_friendly_profile_prefixes(self):
        regular = make_profile("regular", ALL_WEEKDAYS)
        signed = make_profile(
            "signed",
            SIGNED_WEEKDAYS,
            test_command_template=SIGNED_TEST_COMMAND_TEMPLATE,
        )
        comments = render_test_comments(
            {"**signed:** WARNING: example"},
            [signed],
            datetime(2026, 11, 14, tzinfo=timezone.utc),
        )

        rendered = "\n".join(comments)
        self.assertIn("- **signed:** WARNING: example", rendered)
        self.assertIn("INFO: **signed** was not run", rendered)
        self.assertIsNotNone(regular)


if __name__ == "__main__":
    unittest.main()
