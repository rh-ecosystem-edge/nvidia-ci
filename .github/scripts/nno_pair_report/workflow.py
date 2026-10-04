"""Write the GitHub Actions outputs for the NNO report workflow."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def summarize_collection(
    history: dict[str, Any], collection: dict[str, Any], baseline: dict[str, Any],
) -> tuple[bool, str]:
    """Return whether any builds were fetched and the job-summary text."""
    old_paths = {build["build_path"] for build in baseline.get("builds", [])}
    new_paths = {build["build_path"] for build in history["builds"]}
    fetched_builds = collection["fetched_builds"]
    has_data = fetched_builds > 0
    text = (
        f"NNO report: {fetched_builds} builds fetched from Prow, "
        f"{len(new_paths - old_paths)} new builds, "
        f"{len(history['builds'])} builds in history.\n"
    )
    if not has_data:
        text += "No NNO report artifacts were found; nothing was published.\n"
    return has_data, text


def has_new_or_modified_files(output_dir: Path, baseline_dir: Path) -> bool:
    """Report whether a rendered file is new or differs from the baseline."""
    return any(
        not (baseline_dir / path.name).is_file() or path.read_bytes() != (baseline_dir / path.name).read_bytes()
        for path in output_dir.iterdir()
        if path.is_file()
    )


def _append(path: Path, text: str) -> None:
    with path.open("a") as handle:
        handle.write(text)


def _load_json(path: Path) -> dict[str, Any]:
    if not path.is_file():
        return {"builds": []}
    return json.loads(path.read_text())


def main() -> None:
    parser = argparse.ArgumentParser(description="Write NNO report workflow outputs")
    commands = parser.add_subparsers(dest="command", required=True)

    summarize = commands.add_parser("summarize", help="Set has_data and write the job summary")
    summarize.add_argument("--history", type=Path, required=True)
    summarize.add_argument("--collection", type=Path, required=True)
    summarize.add_argument("--baseline", type=Path, required=True)
    summarize.add_argument("--github-output", type=Path, required=True)
    summarize.add_argument("--step-summary", type=Path, required=True)

    diff = commands.add_parser("diff", help="Set changed by comparing rendered files to the baseline")
    diff.add_argument("--output-dir", type=Path, required=True)
    diff.add_argument("--baseline-dir", type=Path, required=True)
    diff.add_argument("--github-output", type=Path, required=True)

    args = parser.parse_args()
    if args.command == "summarize":
        has_data, text = summarize_collection(
            json.loads(args.history.read_text()),
            json.loads(args.collection.read_text()),
            _load_json(args.baseline),
        )
        _append(args.github_output, f"has_data={str(has_data).lower()}\n")
        _append(args.step_summary, text)
        return
    changed = has_new_or_modified_files(args.output_dir, args.baseline_dir)
    _append(args.github_output, f"changed={str(changed).lower()}\n")


if __name__ == "__main__":
    main()
