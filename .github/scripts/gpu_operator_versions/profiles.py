"""Profile configuration and weekday-based schedule resolution."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import Enum
import json
import os
from pathlib import Path
import re
from typing import Iterable


class Weekday(Enum):
    """Canonical profile schedule weekdays."""

    MONDAY = 'Mon'
    TUESDAY = 'Tue'
    WEDNESDAY = 'Wed'
    THURSDAY = 'Thu'
    FRIDAY = 'Fri'
    SATURDAY = 'Sat'
    SUNDAY = 'Sun'


WEEKDAYS = tuple(Weekday)


@dataclass(frozen=True)
class Profile:
    name: str
    active_weekdays: frozenset[Weekday]
    version_file_path: str
    settings_file_path: str
    test_command_template: str
    support_matrix: dict
    ignored_versions_regex: str
    minimum_gpu_channel: str
    check_catalog_availability: bool
    request_timeout_sec: int


@dataclass(frozen=True)
class ProfileManifest:
    profiles: dict[str, Profile]
    tests_to_trigger_file_path: str
    test_comments_file_path: str
    new_gpu_channels_file: str
    request_timeout_sec: int


COMMAND_PLACEHOLDER_RE = re.compile(r'\{([A-Za-z_][A-Za-z0-9_]*)\}')
SUPPORTED_COMMAND_PLACEHOLDERS = {'ocp_version', 'gpu_version'}

def validate_test_command_template(template: str) -> None:
    """Validate placeholders in a profile's complete /test command template."""
    placeholders = set(COMMAND_PLACEHOLDER_RE.findall(template))
    if placeholders != SUPPORTED_COMMAND_PLACEHOLDERS:
        raise ValueError(
            f'Test command template must contain exactly these placeholders: '
            f'{", ".join(sorted(SUPPORTED_COMMAND_PLACEHOLDERS))}. '
            f'Found: {", ".join(sorted(placeholders)) if placeholders else "none"}'
        )


def _load_json(path: Path) -> dict:
    with path.open('r', encoding='utf-8') as json_file:
        return json.load(json_file)


def _parse_active_weekdays(value: object, profile_name: str) -> frozenset[Weekday]:
    """Parse a nonempty list of canonical weekday abbreviations."""
    if not isinstance(value, list) or not value:
        raise ValueError(
            f'Profile {profile_name!r} must define a nonempty active_weekdays list'
        )

    weekdays: list[Weekday] = []
    for token in value:
        try:
            weekdays.append(Weekday(token))
        except ValueError as exc:
            raise ValueError(
                f'Invalid weekday for profile {profile_name!r}: {token!r}. '
                f'Expected one of: {", ".join(day.value for day in WEEKDAYS)}.'
            ) from exc

    if len(weekdays) != len(set(weekdays)):
        raise ValueError(f'Profile {profile_name!r} contains duplicate active weekdays')
    return frozenset(weekdays)

def load_profiles(manifest_path: str | Path) -> ProfileManifest:
    """Load all version-update profiles from a manifest."""
    manifest_file = Path(manifest_path)
    manifest = _load_json(manifest_file)

    request_timeout_sec = int(
        os.getenv('REQUEST_TIMEOUT_SECONDS', str(manifest.get('request_timeout_sec', 30)))
    )
    profiles: dict[str, Profile] = {}

    for name, config in manifest.get('profiles', {}).items():
        settings_path = Path(config['settings_file_path'])
        settings = _load_json(settings_path)

        test_command_template = config['test_command_template']
        validate_test_command_template(test_command_template)

        active_weekdays = _parse_active_weekdays(config.get('active_weekdays'), name)

        profiles[name] = Profile(
            name=name,
            active_weekdays=active_weekdays,
            version_file_path=config['version_file_path'],
            settings_file_path=config['settings_file_path'],
            test_command_template=test_command_template,
            support_matrix=settings,
            ignored_versions_regex=settings.get(
                'ignored_versions_regex',
                os.getenv('OCP_IGNORED_VERSIONS_REGEX', 'x^'),
            ).rstrip(),
            minimum_gpu_channel=settings.get('minimum_gpu_channel', ''),
            check_catalog_availability=bool(
                config.get(
                    'check_catalog_availability',
                    os.getenv('CHECK_CATALOG_AVAILABILITY', 'false').lower() == 'true',
                )
            ),
            request_timeout_sec=request_timeout_sec,
        )

    if not profiles:
        raise ValueError(f'No profiles defined in {manifest_file}')

    return ProfileManifest(
        profiles=profiles,
        tests_to_trigger_file_path=manifest.get(
            'tests_to_trigger_file_path',
            '.github/scripts/generated-files/tests_to_trigger.txt',
        ),
        test_comments_file_path=manifest.get(
            'test_comments_file_path',
            '.github/scripts/generated-files/test_comments.txt',
        ),
        new_gpu_channels_file=manifest.get(
            'new_gpu_channels_file',
            '.github/scripts/generated-files/new_gpu_channels.txt',
        ),
        request_timeout_sec=request_timeout_sec,
    )


def _current_weekday(now: datetime) -> Weekday:
    """Return the locale-independent weekday for now."""
    return WEEKDAYS[now.weekday()]


def current_weekday_name(now: datetime) -> str:
    """Return the locale-independent full weekday name."""
    return _current_weekday(now).name.capitalize()


def format_weekdays(weekdays: Iterable[Weekday]) -> str:
    """Format weekdays in weekday order."""
    return ', '.join(day.value for day in sorted(weekdays, key=WEEKDAYS.index))


def profile_is_active(profile: Profile, now: datetime) -> bool:
    """Return whether a profile is active on now's UTC weekday."""
    return _current_weekday(now) in profile.active_weekdays


def partition_profiles(
    profiles: dict[str, Profile],
    now: datetime,
) -> tuple[list[Profile], list[Profile]]:
    """Partition configured profiles into active and schedule-skipped profiles."""
    active_profiles = [
        profile for profile in profiles.values() if profile_is_active(profile, now)
    ]
    active_names = {profile.name for profile in active_profiles}
    skipped_profiles = [
        profile
        for profile in profiles.values()
        if profile.name not in active_names
    ]
    return active_profiles, skipped_profiles


def profile_names(profiles: Iterable[Profile]) -> list[str]:
    return [profile.name for profile in profiles]
