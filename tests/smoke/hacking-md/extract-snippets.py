#!/usr/bin/env python3
"""Extract tagged code snippets from HACKING.md for testing.

Usage:
    extract-snippets.py MARKDOWN_FILE TAG [TAG...]

Extracts code blocks preceded by <!-- test:TAG --> comments.
Outputs the extracted code to stdout.
"""

import re
import sys
from typing import Dict, List


def extract_snippets(filename: str, tags: List[str]) -> Dict[str, str]:
    """Extract code blocks preceded by <!-- test:TAG --> comments."""
    with open(filename, "r") as f:
        content = f.read()

    snippets: Dict[str, str] = {}

    for tag in tags:
        # Match: <!-- test:TAG -->\n```[optional lang]\nCODE\n```
        # Also match indented code blocks (4 spaces)
        pattern = rf"<!--\s*test:{re.escape(tag)}\s*-->\s*\n```[^\n]*\n(.*?)\n```"
        matches = re.findall(pattern, content, re.DOTALL)

        # Also try to match indented code blocks (HACKING.md uses both styles)
        if not matches:
            # Match: <!-- test:TAG -->\n    code\n    code\n\n
            pattern = rf"<!--\s*test:{re.escape(tag)}\s*-->\s*\n((?:    .*\n)+)"
            matches = re.findall(pattern, content)
            if matches:
                # Remove 4-space indentation
                matches = [
                    "\n".join(
                        line[4:] if line.startswith("    ") else line
                        for line in match.split("\n")
                    )
                    for match in matches
                ]

        if matches:
            snippets[tag] = "\n".join(matches)

    return snippets


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print(
            f"Usage: {sys.argv[0]} MARKDOWN_FILE TAG [TAG...]", file=sys.stderr
        )
        sys.exit(1)

    markdown_file = sys.argv[1]
    tags = sys.argv[2:]

    try:
        snippets = extract_snippets(markdown_file, tags)
    except FileNotFoundError:
        print(f"Error: File '{markdown_file}' not found", file=sys.stderr)
        sys.exit(1)

    for tag in tags:
        if tag in snippets:
            print(snippets[tag])
        else:
            print(
                f"Error: Tag '{tag}' not found in {markdown_file}",
                file=sys.stderr,
            )
            sys.exit(1)
