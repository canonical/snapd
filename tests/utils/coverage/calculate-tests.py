#!/usr/bin/env python3

import argparse
import json
import sys
import os
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Calculate tests to execute for selected systems by combining core-set tests "
            "and tests relevant to the provided files."
        )
    )
    # parser.add_argument(
    # 	"--files",
    # 	nargs="+",
    # 	help="Repository-relative file paths to evaluate (e.g. arch/arch.go)",
    # )
    # parser.add_argument(
    # 	"--files-list",
    # 	help="Path to a newline-separated file list (alternative to --files)",
    # )
    # parser.add_argument(
    # 	"--systems",
    # 	nargs="+",
    # 	required=True,
    # 	help="Subset of systems (e.g. openstack:ubuntu-core-20-64)",
    # )
    parser.add_argument(
        "--coverage-dir",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/coverages",
        help="Coverage directory containing core-set.json and per-system JSON files",
    )
    parser.add_argument(
        "--core-set-file",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/core-set.json",
        help="Core-set JSON filename inside coverage-dir",
    )
    parser.add_argument(
        "--failure-file",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/failed.json",
        help="Failed JSON filename inside coverage-dir",
    )
    # parser.add_argument(
    # 	"--output",
    # 	help="Optional output file path. If omitted, prints JSON to stdout.",
    # )
    return parser.parse_args()

def remove_inits(systems):
    for results in systems.values():
        for file in list(results):
            entries = results[file]
            filtered_entries = [
                entry
                for entry in entries
                if any(function != 'init' for function in entry.get('functions', []))
            ]
            if filtered_entries:
                entries[:] = filtered_entries
            else:
                del results[file]

def main():
    args = parse_args()
    with open(args.core_set_file) as f:
        core_set = json.load(f)
    systems = {}
    for dirPath, _, files in os.walk(args.coverage_dir):
        for file in files:
            if file.endswith(".json"):
                with open(os.path.join(dirPath, file)) as f:
                    systems[file.split('.json')[0]] = json.load(f)
    with open(args.failure_file) as f:
        failed = json.load(f)
    remove_inits(systems)
    i = 0


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)

