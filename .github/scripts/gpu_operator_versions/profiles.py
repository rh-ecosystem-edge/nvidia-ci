"""Profile configuration and schedule resolution for version updates."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import json
import os
import re
from pathlib import Path
from typing import Iterable


@dataclass(frozen=True)
class Profile:
    name: str
    schedule: str
    version_file_path: str
    settings_file_path: str
    tests_to_trigger_file_path: str
    test_command_template: str
    support_matrix: dict
    ignored_versions_regex: str
    minimum_gpu_channel: str
    check_catalog_availability: bool
    request_timeout_sec: int


@dataclass(frozen=True)
class ProfileManifest:
    profiles: dict[str, Profile]
    combined_output_file: str
    active_profiles_file: str
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

        profiles[name] = Profile(
            name=name,
            schedule=config['schedule'],
            version_file_path=config['version_file_path'],
            settings_file_path=config['settings_file_path'],
            tests_to_trigger_file_path=config['tests_to_trigger_file_path'],
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
        combined_output_file=manifest.get(
            'combined_output_file',
            '.github/scripts/generated-files/tests_to_trigger.txt',
        ),
        active_profiles_file=manifest.get(
            'active_profiles_file',
            '.github/scripts/generated-files/active_profiles.txt',
        ),
        request_timeout_sec=request_timeout_sec,
    )


def _expand_cron_dow_part(part: str) -> set[int]:
    """Expand one comma-separated cron day-of-week component to ISO weekdays."""
    if part == '*':
        return set(range(1, 8))

    step = 1
    if '/' in part:
        part, step_text = part.split('/', 1)
        step = int(step_text)
        if step <= 0:
            raise ValueError(f'Invalid cron day-of-week step: {step}')

    if '-' in part:
        start_text, end_text = part.split('-', 1)
        start = int(start_text)
        end = int(end_text)
    else:
        start = end = int(part)

    if not (0 <= start <= 7 and 0 <= end <= 7):
        raise ValueError(f'Invalid cron day-of-week range: {part}')

    days: set[int] = set()
    for day in range(start, end + 1, step):
        # Cron uses 0 and 7 for Sunday; Python's ISO weekday uses 7.
        days.add(7 if day % 7 == 0 else day)
    return days


def cron_day_matches(schedule: str, now: datetime) -> bool:
    """Return whether a five-field cron schedule is active on now's UTC weekday."""
    fields = schedule.split()
    if len(fields) != 5:
        raise ValueError(f'Expected a five-field cron expression: {schedule}')

    allowed_days = set[int]()
    for part in fields[4].split(','):
        allowed_days.update(_expand_cron_dow_part(part))

    return now.isoweekday() in allowed_days


def resolve_profiles(
    profiles: dict[str, Profile],
    selection: str,
    now: datetime,
) -> list[Profile]:
    """Resolve the profiles selected by auto, all, or an explicit list."""
    selection = (selection or 'auto').strip()

    if selection == 'auto':
        return [profile for profile in profiles.values() if cron_day_matches(profile.schedule, now)]

    if selection == 'all':
        return list(profiles.values())

    selected_names = [name.strip() for name in selection.split(',') if name.strip()]
    unknown = [name for name in selected_names if name not in profiles]
    if unknown:
        raise ValueError(f'Unknown profile(s): {", ".join(unknown)}')
    if not selected_names:
        raise ValueError('No profiles selected')

    return [profiles[name] for name in selected_names]


def profile_names(profiles: Iterable[Profile]) -> list[str]:
    return [profile.name for profile in profiles]
