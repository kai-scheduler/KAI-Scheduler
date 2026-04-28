#!/usr/bin/env python3
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

"""Merge CHANGELOG additions from a git unified diff into [Unreleased] for backport PRs.

Backports cherry-pick changelog entries under release headers (e.g. ## [v0.9.0]) that do
not exist on the target branch, causing merge conflicts. This script extracts only the
added entries from a diff and re-inserts them under [Unreleased] on the target branch,
preserving their ### subsection (Added/Fixed/Changed/etc.).
"""

import argparse
import re
import sys


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("diff_path", help="path to unified diff (e.g. CHANGELOG.md from git show)")
    args = parser.parse_args()

    with open(args.diff_path) as f:
        diff_lines = f.read().splitlines()

    # Walk the diff and collect every '+' line, tagged with the ### subsection it belongs to.
    # Section is tracked from both context (' ') and addition ('+') lines so an entry added
    # under a pre-existing subsection still gets the right tag.
    additions = []  # list of (section_header, entry_line)
    current_section = None
    for line in diff_lines:
        # Skip diff metadata lines (file headers and hunk markers).
        if line.startswith(("+++", "---", "@@")):
            continue
        raw = line[1:] if line else ""
        if line.startswith((" ", "+")):
            if raw.startswith("### "):
                current_section = raw
        # Collect actual added entries; skip blank additions and the section header itself
        # (the header is re-emitted later only if missing in [Unreleased]).
        if line.startswith("+") and not line.startswith("+++"):
            if not raw.startswith("### ") and raw.strip():
                additions.append((current_section, raw))

    if not additions:
        sys.exit(0)

    with open("CHANGELOG.md") as f:
        content = f.read()

    # Locate the [Unreleased] header; create one above the first version header if absent.
    unreleased = re.search(r"## \[Unreleased\]\n", content)
    if not unreleased:
        m = re.search(r"\n## \[v", content)
        insert_at = m.start() if m else len(content)
        content = content[:insert_at] + "\n\n## [Unreleased]\n" + content[insert_at:]
        unreleased = re.search(r"## \[Unreleased\]\n", content)

    # Bound the [Unreleased] block by the next ## header (or EOF) so edits stay scoped to it.
    unreleased_end = re.search(r"\n## \[", content[unreleased.end() :])
    section_end = (unreleased.end() + unreleased_end.start()) if unreleased_end else len(content)
    unreleased_block = content[unreleased.end() : section_end]

    # Group by section, preserving order of first appearance
    groups: dict[str, list[str]] = {}
    order: list[str] = []
    for section, entry in additions:
        key = section or "__top__"
        if key not in groups:
            groups[key] = []
            order.append(key)
        groups[key].append(entry)

    for key in order:
        entries = groups[key]
        entry_text = "\n".join(entries)
        if key == "__top__":
            # Entries that appeared before any ### header go to the top of [Unreleased].
            unreleased_block = "\n" + entry_text + "\n" + unreleased_block
        else:
            # If the subsection already exists in [Unreleased], append entries right after
            # its header; otherwise create the subsection at the end of the block.
            m = re.search(re.escape(key) + r"\n", unreleased_block)
            if m:
                pos = m.end()
                unreleased_block = unreleased_block[:pos] + entry_text + "\n" + unreleased_block[pos:]
            else:
                unreleased_block += "\n" + key + "\n" + entry_text + "\n"

    # Splice the modified [Unreleased] block back into the original file content.
    content = content[: unreleased.end()] + unreleased_block + content[section_end:]
    with open("CHANGELOG.md", "w") as f:
        f.write(content)


if __name__ == "__main__":
    main()
