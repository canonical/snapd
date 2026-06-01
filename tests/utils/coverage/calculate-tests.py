#!/usr/bin/env python3

import argparse
import json
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Calculate tests to execute for selected systems by combining core-set tests "
            "and tests relevant to the provided files."
        )
    )
    parser.add_argument(
        "--group",
        required=True,
        help="Group name (e.g. ubuntu-core-20 or nested-ubuntu-24.04)",
    )
    parser.add_argument(
        "--files",
        nargs="+",
        help="Repository-relative changed file paths (e.g. arch/arch.go)",
    )
    parser.add_argument(
        "--files-list",
        help="Path to a newline-separated changed-file list (alternative to --files)",
    )
    parser.add_argument(
        "--spread-list",
        required=True,
        help="A file that contains the list of spread tests that would be executed in theory",
    )
    parser.add_argument(
        "--always-run-suites",
        nargs="+",
        help="list of suites that should always be run",
    )
    parser.add_argument(
        "--coverage-dir",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/coverages",
        help="Coverage directory containing per-group JSON files",
    )
    parser.add_argument(
        "--core-set-file",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/core-set.json",
        help="Path to core-set JSON file",
    )
    parser.add_argument(
        "--failure-file",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/failed.json",
        help="Path to failed-tests JSON file",
    )
    parser.add_argument(
        "--fundamental-data",
        default=".github/workflows/data-fundamental-systems.json",
        help="Path to workflow group mapping for fundamental systems",
    )
    parser.add_argument(
        "--non-fundamental-data",
        default=".github/workflows/data-non-fundamental-systems.json",
        help="Path to workflow group mapping for non-fundamental systems",
    )
    parser.add_argument(
        "--nested-data",
        default=".github/workflows/data-nested-systems.json",
        help="Path to workflow group mapping for nested systems",
    )
    return parser.parse_args()


def read_files_input(files: list[str] | None, files_list: str | None) -> list[str]:
    selected_files: set[str] = set()

    for file_path in files or []:
        stripped = file_path.strip()
        if stripped:
            selected_files.add(stripped)

    if files_list:
        for line in Path(files_list).expanduser().read_text().splitlines():
            stripped = line.strip()
            if stripped:
                selected_files.add(stripped)

    return sorted(selected_files)


def load_json_file(path: str) -> dict | list:
    with Path(path).expanduser().open() as f:
        return json.load(f)


def read_spread_list(path: str) -> list[str]:
    tests: list[str] = []
    for line in Path(path).expanduser().read_text().splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        tests.append(stripped)
    return tests


def load_group_mapping(files: list[str]) -> dict[str, tuple[str, list[str]]]:
    mapping: dict[str, tuple[str, list[str]]] = {}

    for file_path in files:
        data = load_json_file(file_path)
        include = data.get("include", []) if isinstance(data, dict) else []
        for item in include:
            if not isinstance(item, dict):
                continue
            group = item.get("group")
            backend = item.get("backend")
            systems_field = item.get("systems", "")

            if not isinstance(group, str) or not isinstance(backend, str):
                continue
            if not isinstance(systems_field, str):
                continue

            systems = [system for system in systems_field.split() if system]
            mapping[group] = (backend, systems)

    return mapping


def group_coverage_file_path(coverage_dir: str, group: str) -> Path:
    return Path(coverage_dir).expanduser() / f"{group}.json"


def test_system_prefix(test_name: str) -> str:
    parsed = parse_full_test_name(test_name)
    if not parsed:
        return ""
    backend, system, _ = parsed
    return f"{backend}:{system}"


def parse_full_test_name(test_name: str) -> tuple[str, str, str] | None:
    parts = test_name.split(":", 2)
    if len(parts) != 3:
        return None

    backend, system, test_part = parts
    # Fully qualified IDs are backend:system:test; short test names usually begin with
    # paths like tests/... and should not be parsed as backend/system prefixes.
    if not backend or not system or not test_part:
        return None
    if "/" in backend or "/" in system:
        return None

    return backend, system, test_part


def full_test_name(test_name: str, system_prefix: str) -> str:
    if parse_full_test_name(test_name):
        return test_name
    return f"{system_prefix}:{test_name}"


def short_test_name(test_name: str) -> str:
    parsed = parse_full_test_name(test_name)
    if parsed:
        _, _, test_part = parsed
        return test_part
    return test_name


def expand_test_names(test_name: str, selected_systems: set[str]) -> set[str]:
    if not test_name:
        return set()

    if parse_full_test_name(test_name):
        if test_system_prefix(test_name) in selected_systems:
            return {test_name}
        return set()

    return {full_test_name(test_name, system) for system in selected_systems}


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


def tests_for_changed_files(
    system_data: dict, changed_files: list[str], selected_systems: set[str]
) -> set[str]:
    selected: set[str] = set()
    for file_path in changed_files:
        for entry in system_data.get(file_path, []):
            if isinstance(entry, dict):
                test_name = entry.get("test")
                if isinstance(test_name, str):
                    selected |= expand_test_names(test_name, selected_systems)
    return selected


def should_keep_test(
    test_name: str, selected_tests: set[str], always_run_suites: list[str]
) -> bool:
    if test_name in selected_tests:
        return True

    short_name = short_test_name(test_name)
    for suite_prefix in always_run_suites:
        if test_name.startswith(suite_prefix) or short_name.startswith(suite_prefix):
            return True

    return False


def main():
    args = parse_args()


    changed_files = read_files_input(args.files, args.files_list)
    spread_tests = read_spread_list(args.spread_list)
    always_run_suites = args.always_run_suites or []
    group_mapping = load_group_mapping(
        [args.fundamental_data, args.non_fundamental_data, args.nested_data]
    )

    if args.group not in group_mapping:
        raise KeyError(f"group not found in workflow data: {args.group}")

    backend, systems = group_mapping[args.group]
    selected_systems = {f"{backend}:{system}" for system in systems}

    core_set = load_json_file(args.core_set_file)
    if not isinstance(core_set, dict):
        raise RuntimeError("unexpected core set shape; expected JSON object")

    coverage_file = group_coverage_file_path(args.coverage_dir, args.group)
    if not coverage_file.exists():
        raise FileNotFoundError(f"cannot find group coverage file: {coverage_file}")

    systems = {args.group: json.loads(coverage_file.read_text())}
    if not isinstance(systems[args.group], dict):
        raise RuntimeError(
            f"unexpected coverage shape in {coverage_file}; expected JSON object"
        )

    failed = load_json_file(args.failure_file)
    if not isinstance(failed, list):
        raise RuntimeError("unexpected failure list shape; expected JSON array")

    remove_inits(systems)

    selected_tests: set[str] = set()

    for system in selected_systems:
        core_tests = core_set.get(system, [])
        if isinstance(core_tests, list):
            for test_name in core_tests:
                if isinstance(test_name, str) and test_name:
                    selected_tests |= expand_test_names(test_name, {system})

    if changed_files:
        selected_tests |= tests_for_changed_files(
            systems[args.group], changed_files, selected_systems
        )

    for test_name in failed:
        if isinstance(test_name, str):
            selected_tests |= expand_test_names(test_name, selected_systems)

    final_tests = [
        test_name
        for test_name in spread_tests
        if should_keep_test(test_name, selected_tests, always_run_suites)
    ]

    print("\n".join(final_tests))


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)

