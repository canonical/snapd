#!/usr/bin/env python3

import argparse
import json
import sys
from pathlib import Path, PurePosixPath


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
        "--files-list",
        required=True,
        help=(
            "Path to JSON changed-files list. Format: "
            "[{\"path\": \"arch/arch.go\", \"changeType\": \"MODIFIED\"}]"
        ),
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
        "--hook-feature",
        default="/home/katie.may@canonical.com/Desktop/coverage/2026-05-31/tmp/run-hook",
        help=(
            "Path to hook-feature JSON mapping with keys as <backend>:<system> and "
            "values as test name lists"
        ),
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


def read_files_input(files_list: str) -> list[tuple[str, str]]:
    changed_files_data = load_json_file(files_list)
    if not isinstance(changed_files_data, list):
        raise RuntimeError("unexpected changed-files shape; expected JSON array")

    changed_files: list[tuple[str, str]] = []
    valid_change_types = {"ADDED", "MODIFIED", "DELETED"}

    for item in changed_files_data:
        if not isinstance(item, dict):
            raise RuntimeError("invalid changed-files entry; expected JSON object")

        path = item.get("path")
        change_type = item.get("changeType")
        if not isinstance(path, str) or not path.strip():
            raise RuntimeError("invalid changed-files entry; missing non-empty path")
        if not isinstance(change_type, str):
            raise RuntimeError("invalid changed-files entry; missing changeType")

        normalized_change_type = change_type.strip().upper()
        if normalized_change_type not in valid_change_types:
            raise RuntimeError(
                f"invalid changeType '{change_type}'; expected one of {sorted(valid_change_types)}"
            )

        changed_files.append((path.strip(), normalized_change_type))

    return changed_files


def immediate_directory(file_path: str) -> str:
    return str(PurePosixPath(file_path).parent)


def changed_files_touch_hookstate(changed_files: list[tuple[str, str]]) -> bool:
    for file_path, _ in changed_files:
        if "hookstate" in PurePosixPath(file_path).parts:
            return True
    return False


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
    system_data: dict, changed_files: list[tuple[str, str]], selected_systems: set[str]
) -> set[str]:
    selected: set[str] = set()

    coverage_paths_by_dir: dict[str, list[str]] = {}
    for covered_path in system_data:
        covered_dir = immediate_directory(covered_path)
        coverage_paths_by_dir.setdefault(covered_dir, []).append(covered_path)

    for file_path, change_type in changed_files:
        candidate_paths: list[str]
        if change_type == "MODIFIED":
            candidate_paths = [file_path]
        else:
            changed_dir = immediate_directory(file_path)
            candidate_paths = coverage_paths_by_dir.get(changed_dir, [])

        for candidate_path in candidate_paths:
            for entry in system_data.get(candidate_path, []):
                if isinstance(entry, dict):
                    test_name = entry.get("test")
                    if isinstance(test_name, str):
                        selected |= expand_test_names(test_name, selected_systems)

    return selected


def coverage_tests(system_data: dict, selected_systems: set[str]) -> set[str]:
    selected: set[str] = set()
    for entries in system_data.values():
        for entry in entries:
            if isinstance(entry, dict):
                test_name = entry.get("test")
                if isinstance(test_name, str):
                    selected |= expand_test_names(test_name, selected_systems)
    return selected


def hook_feature_tests(hook_feature_file: str, selected_systems: set[str]) -> set[str]:
    data = load_json_file(hook_feature_file)
    if not isinstance(data, dict):
        raise RuntimeError("unexpected hook-feature shape; expected JSON object")

    selected: set[str] = set()
    for system in selected_systems:
        tests = data.get(system, [])
        if not isinstance(tests, list):
            raise RuntimeError(
                f"unexpected hook-feature tests shape for {system}; expected JSON array"
            )
        for test_name in tests:
            if isinstance(test_name, str) and test_name:
                selected |= expand_test_names(test_name, {system})

    return selected


def tests_missing_from_coverage(
    spread_tests: list[str], system_data: dict, selected_systems: set[str]
) -> set[str]:
    represented_in_coverage = coverage_tests(system_data, selected_systems)
    missing: set[str] = set()

    for test_name in spread_tests:
        if test_name not in represented_in_coverage:
            missing.add(test_name)

    return missing


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


    changed_files = read_files_input(args.files_list)
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

    if changed_files_touch_hookstate(changed_files):
        selected_tests |= hook_feature_tests(args.hook_feature, selected_systems)

    selected_tests |= tests_missing_from_coverage(
        spread_tests, systems[args.group], selected_systems
    )

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

