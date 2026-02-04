#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2025 Canonical Ltd
# SPDX-License-Identifier: GPL-3.0-only

"""
This script injects a block of HTML into a file just before a specified closing tag.

It is used to insert custom content, such as CSS or a footer, into the
statically generated Redoc documentation.
"""

import argparse
import sys

def inject_html(file_path: str, html_content: str, html_to_inject: str, target_tag: str) -> str:
    """
    Injects a string of HTML into a file before a specified closing tag.

    Args:
        file_path: The path to the HTML file.
        html_content: The full HTML content of the page.
        html_to_inject: The HTML string to inject.
        target_tag: The closing tag to inject before (e.g., '</head>').
    
    Returns:
        The modified HTML content.
    """
    if target_tag not in html_content:
        print(
            f"Error: Target tag '{target_tag}' not found in '{file_path}'.",
            file=sys.stderr,
        )
        sys.exit(1)

    # Inject the HTML before the target tag
    modified_content = html_content.replace(target_tag, f"{html_to_inject}{target_tag}")
    return modified_content


def set_dark_theme(html_content: str) -> str:
    """
    Sets the dark theme by adding data-theme="dark" to the <body> tag.

    Args:
        html_content: The HTML content.

    Returns:
        The modified HTML content with the dark theme enabled.
    """
    return html_content.replace("<body>", '<body data-theme="dark">')


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Inject HTML into a file and set the theme."
    )
    parser.add_argument(
        "file_path",
        help="The path to the HTML file.",
    )
    parser.add_argument(
        "html_file",
        nargs="?",
        default=None,
        help="The path to the file containing the HTML to inject (optional).",
    )
    parser.add_argument(
        "target_tag",
        nargs="?",
        default=None,
        help="The closing tag to inject before (e.g., '</head>') (optional).",
    )

    args = parser.parse_args()

    try:
        with open(args.file_path, "r", encoding="utf-8") as f:
            html_content = f.read()
    except FileNotFoundError:
        print(f"Error: File not found at '{args.file_path}'.", file=sys.stderr)
        sys.exit(1)

    if args.html_file and args.target_tag:
        try:
            with open(args.html_file, "r", encoding="utf-8") as f:
                html_to_inject = f.read()
        except FileNotFoundError:
            print(
                f"Error: HTML file not found at '{args.html_file}'.",
                file=sys.stderr,
            )
            sys.exit(1)
        # Set the dark theme first
        modified_content = set_dark_theme(html_content)
        # Then inject the CSS
        modified_content = inject_html(
            args.file_path, modified_content, html_to_inject, args.target_tag
        )
    else:
        modified_content = html_content

    try:
        with open(args.file_path, "w", encoding="utf-8") as f:
            f.write(modified_content)
        print(f"Successfully processed and wrote to '{args.file_path}'.")
    except Exception as e:
        print(f"An error occurred while writing to file: {e}", file=sys.stderr)
        sys.exit(1)
