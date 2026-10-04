"""Add the NNO pair reports to an existing GitHub Pages index."""

import argparse
import re
from pathlib import Path

REPORT_LINKS = (
    ("nno_doca2_matrix.html", "Test Matrix: NNO and DOCA/OFED on Red Hat OpenShift"),
    ("nno_doca2_signed_matrix.html", "Test Matrix: NNO and signed DOCA/OFED on Red Hat OpenShift"),
    ("nno_doca2_explorer.html", "Compatibility Explorer: NNO and DOCA/OFED on Red Hat OpenShift"),
    ("nno_doca2_signed_explorer.html", "Compatibility Explorer: NNO and signed DOCA/OFED on Red Hat OpenShift"),
)


def add_report_links(index_html: str) -> str:
    """Keep the existing site links and add each report at most once."""
    closing_list = re.search(r"(?m)^([ \t]*)</ul>", index_html)
    if closing_list is None:
        raise ValueError("GitHub Pages index has no report list")
    indent = closing_list.group(1) + "    "
    missing = [
        f'{indent}<li><a href="{filename}">{label}</a></li>'
        for filename, label in REPORT_LINKS
        if f'href="{filename}"' not in index_html
    ]
    if not missing:
        return index_html
    return index_html[:closing_list.start()] + "\n".join(missing) + "\n" + index_html[closing_list.start():]


def main() -> None:
    parser = argparse.ArgumentParser(description="Add NNO report links to the Pages index")
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(add_report_links(args.source.read_text()))


if __name__ == "__main__":
    main()
