#!/usr/bin/env python3
"""
Parse spread test results from results.json files to identify failing tests.

Also analyzes PR diff to correlate failures with code changes.
"""

import json
import os
import re
import sys
import zipfile
from collections import defaultdict
from pathlib import Path
from fnmatch import fnmatch


def extract_results_from_zip(zip_path: Path) -> list:
    """Extract and parse results.json from a spread-results zip file."""
    entries = []
    with zipfile.ZipFile(zip_path, "r") as z:
        for name in z.namelist():
            if name.endswith("results.json"):
                with z.open(name) as f:
                    data = json.load(f)
                entries.append({
                    "source": zip_path.name,
                    "data": data,
                })
    return entries


def parse_results_json(results_path: Path) -> dict:
    """Parse a results.json file directly."""
    with open(results_path, "r") as f:
        return json.load(f)


def find_all_results(analysis_dir: Path) -> list:
    """Find all results.json files (zipped or unzipped) in the analysis directory."""
    entries = []
    # Look for already extracted JSON files
    for json_file in analysis_dir.rglob("results.json"):
        try:
            with open(json_file, "r") as f:
                data = json.load(f)
            entries.append({
                "source": str(json_file.relative_to(analysis_dir)),
                "data": data,
            })
        except (json.JSONDecodeError, OSError) as e:
            print(f"warning: could not parse {json_file}: {e}", file=sys.stderr)

    # Look for zip files that haven't been extracted
    for zip_file in analysis_dir.glob("spread-results-*.zip"):
        # Only process if we haven't already found extracted results for this zip
        if not any(e["source"].startswith(zip_file.name) for e in entries):
            try:
                zip_entries = extract_results_from_zip(zip_file)
                entries.extend(zip_entries)
            except (zipfile.BadZipFile, OSError) as e:
                print(f"warning: could not extract {zip_file}: {e}", file=sys.stderr)

    return entries


def get_failed_tests(results_data: dict) -> list:
    """Extract failed non-skipped tasks from results data.

    Returns list of dicts with keys: verb, backend, system, name, variant, log_id, source
    """
    failed = []
    items = results_data.get("items", [])
    for item in items:
        if (
            item.get("level") == "task"
            and item.get("success") is False
            and item.get("skipped") is False
            and item.get("aborted") is not True
            and item.get("verb") != "checking"
        ):
            failed.append({
                "verb": item.get("verb", "unknown"),
                "backend": item.get("backend", ""),
                "system": item.get("system", ""),
                "name": item.get("name", ""),
                "variant": item.get("variant", ""),
                "log_id": item.get("log-id", ""),
                "duration": item.get("duration", ""),
                "detail": item,
            })
    return failed


def get_aborted_tests(results_data: dict) -> list:
    """Extract aborted tests (aborted true)."""
    aborted = []
    items = results_data.get("items", [])
    for item in items:
        if item.get("aborted") is True:
            aborted.append({
                "verb": item.get("verb", ""),
                "backend": item.get("backend", ""),
                "system": item.get("system", ""),
                "name": item.get("name", ""),
                "variant": item.get("variant", ""),
                "detail": item,
            })
    return aborted


def get_preparing_failures(results_data: dict) -> list:
    """Extract preparing phase failures."""
    failed = []
    items = results_data.get("items", [])
    for item in items:
        if item.get("verb") == "preparing" and item.get("success") is False:
            failed.append({
                "backend": item.get("backend", ""),
                "system": item.get("system", ""),
                "name": item.get("name", ""),
                "variant": item.get("variant", ""),
                "detail": item,
            })
    return failed


def get_restoring_failures(results_data: dict) -> list:
    """Extract restoring phase failures."""
    failed = []
    items = results_data.get("items", [])
    for item in items:
        if item.get("verb") == "restoring" and item.get("success") is False:
            failed.append({
                "backend": item.get("backend", ""),
                "system": item.get("system", ""),
                "name": item.get("name", ""),
                "variant": item.get("variant", ""),
                "detail": item,
            })
    return failed


def build_test_identifier(test: dict) -> str:
    """Build a standard test identifier: backend:system:name or with variant."""
    parts = [test.get("backend", ""), test.get("system", ""), test.get("name", "")]
    variant = test.get("variant", "")
    if variant:
        parts.append(variant)
    return ":".join(parts)


def summarize_failures(entries: list) -> dict:
    """Summarize all failures across all result files.

    Returns dict with:
    - failed_tasks: list of unique failed task identifiers
    - by_system: dict mapping system -> list of failed tests
    - by_test: dict mapping test name -> list of (system, backend) tuples
    - preparing_failures: list of preparing failures
    - restoring_failures: list of restoring failures
    - aborted: list of aborted tests
    """
    failed_tasks = []
    by_system = defaultdict(list)
    by_test = defaultdict(list)
    preparing_failures = []
    restoring_failures = []
    aborted_tests = []

    seen = set()

    for entry in entries:
        source = entry["source"]
        data = entry["data"]

        # Task failures
        for test in get_failed_tests(data):
            tid = build_test_identifier(test)
            key = f"{source}:{tid}"
            if key not in seen:
                seen.add(key)
                test["source"] = source
                failed_tasks.append(test)
                by_system[test["system"]].append(test)
                by_test[test["name"]].append({
                    "system": test["system"],
                    "backend": test["backend"],
                    "variant": test["variant"],
                    "verb": test["verb"],
                })

        # Preparing failures
        for test in get_preparing_failures(data):
            test["source"] = source
            preparing_failures.append(test)

        # Restoring failures
        for test in get_restoring_failures(data):
            test["source"] = source
            restoring_failures.append(test)

        # Aborted tests
        for test in get_aborted_tests(data):
            test["source"] = source
            aborted_tests.append(test)

    return {
        "failed_tasks": failed_tasks,
        "by_system": dict(by_system),
        "by_test": dict(by_test),
        "preparing_failures": preparing_failures,
        "restoring_failures": restoring_failures,
        "aborted": aborted_tests,
    }


def parse_diff(diff_path: Path) -> dict:
    """Parse a PR diff file to extract changed files.

    Returns dict with:
    - files: list of changed file paths
    - by_prefix: dict mapping directory prefix -> list of files
    """
    files = []
    by_prefix = defaultdict(list)

    with open(diff_path, "r") as f:
        for line in f:
            if line.startswith("diff --git "):
                # Format: diff --git a/path b/path
                parts = line.strip().split()
                if len(parts) >= 4:
                    file_path = parts[2][2:]  # Remove 'a/' prefix
                    files.append(file_path)
                    # Extract top-level prefix
                    prefix = file_path.split("/")[0] if "/" in file_path else file_path
                    by_prefix[prefix].append(file_path)

    return {
        "files": files,
        "by_prefix": dict(by_prefix),
    }


def correlate_failures_with_changes(summary: dict, diff_data: dict) -> dict:
    """Correlate failing tests with PR changes.

    Returns dict with:
    - direct_correlations: tests whose paths match changed files
    - possible_correlations: tests in directories that were changed
    - unrelated: tests with no obvious correlation
    """
    changed_files = set(diff_data["files"])
    changed_prefixes = set(diff_data["by_prefix"].keys())

    direct = []
    possible = []
    unrelated = []

    for test_name, occurrences in summary["by_test"].items():
        # Check if test name or its directory appears in changed files
        test_path = test_name.replace(":", "/")

        correlated = False
        for changed in changed_files:
            # Direct match: changed file is in the test directory
            if test_path in changed or changed in test_path:
                direct.append({
                    "test": test_name,
                    "occurrences": occurrences,
                    "matching_file": changed,
                })
                correlated = True
                break

        if not correlated:
            # Check prefix correlation
            test_prefix = test_path.split("/")[0] if "/" in test_path else test_path
            if test_prefix in changed_prefixes:
                possible.append({
                    "test": test_name,
                    "occurrences": occurrences,
                    "changed_prefix": test_prefix,
                })
                correlated = True

        if not correlated:
            unrelated.append({
                "test": test_name,
                "occurrences": occurrences,
            })

    return {
        "direct_correlations": direct,
        "possible_correlations": possible,
        "unrelated": unrelated,
    }


def analyze_failure_patterns(summary: dict) -> dict:
    """Analyze patterns in failures to detect flakiness indicators.

    Returns dict with analysis results:
    - same_test_multiple_systems: tests that failed on multiple systems
    - single_system_failures: tests that only failed on one system
    - system_wide_failures: systems with many failures (possible infra issues)
    """
    same_test_multi = []
    single_system = []

    for test_name, occurrences in summary["by_test"].items():
        systems = set(o["system"] for o in occurrences)
        if len(systems) > 1:
            same_test_multi.append({
                "test": test_name,
                "systems": sorted(systems),
                "count": len(occurrences),
            })
        else:
            single_system.append({
                "test": test_name,
                "system": list(systems)[0],
                "count": len(occurrences),
            })

    # System-wide failure analysis
    system_counts = {}
    for system, tests in summary["by_system"].items():
        system_counts[system] = len(tests)

    return {
        "same_test_multiple_systems": same_test_multi,
        "single_system_failures": single_system,
        "system_failure_counts": system_counts,
    }


def print_summary(summary: dict, correlation: dict = None, patterns: dict = None) -> None:
    """Print a human-readable summary of the analysis."""
    print("=" * 60)
    print("SPREAD TEST FAILURE ANALYSIS")
    print("=" * 60)

    failed = summary["failed_tasks"]
    if not failed:
        print("no failed spread tasks found")
        return

    print(f"\ntotal failed tasks: {len(failed)}")

    # Group by verb
    verbs = defaultdict(list)
    for t in failed:
        verbs[t["verb"]].append(t)

    print("\n--- by phase ---")
    for verb, tests in sorted(verbs.items()):
        print(f"  {verb}: {len(tests)} failure(s)")

    print("\n--- by system ---")
    for system, tests in sorted(summary["by_system"].items()):
        print(f"  {system}: {len(tests)} failure(s)")

    print("\n--- failed tests ---")
    for test_name, occurrences in sorted(summary["by_test"].items()):
        systems = sorted(set(o["system"] for o in occurrences))
        print(f"  {test_name}")
        print(f"    systems: {', '.join(systems)}")
        if len(occurrences) > len(systems):
            print(f"    note: {len(occurrences) - len(systems)} duplicate(s) (rerun?)")

    if summary["preparing_failures"]:
        print(f"\n--- preparing failures ({len(summary['preparing_failures'])}) ---")
        for t in summary["preparing_failures"]:
            print(f"  {build_test_identifier(t)}")

    if summary["restoring_failures"]:
        print(f"\n--- restoring failures ({len(summary['restoring_failures'])}) ---")
        for t in summary["restoring_failures"]:
            print(f"  {build_test_identifier(t)}")

    if summary["aborted"]:
        print(f"\n--- aborted tests ({len(summary['aborted'])}) ---")
        for t in summary["aborted"]:
            print(f"  {build_test_identifier(t)}")

    if patterns:
        print("\n--- pattern analysis ---")
        multi = patterns["same_test_multiple_systems"]
        if multi:
            print(f"\n  tests failing on multiple systems ({len(multi)}):")
            for item in multi:
                # These are more likely to be real regressions than flaky
                likelihood = "high" if len(item["systems"]) > 2 else "medium"
                print(f"    {item['test']} on {', '.join(item['systems'])} (likelihood of PR regression: {likelihood})")
        else:
            print("\n  no tests failed on multiple systems")

        single = patterns["single_system_failures"]
        if single:
            print(f"\n  single-system failures ({len(single)}):")
            for item in single:
                print(f"    {item['test']} on {item['system']} (could be flaky or system-specific)")

    if correlation:
        print("\n--- correlation with PR changes ---")
        direct = correlation["direct_correlations"]
        if direct:
            print(f"\n  direct correlations ({len(direct)}):")
            for item in direct:
                print(f"    {item['test']} -> changed file: {item['matching_file']}")

        possible = correlation["possible_correlations"]
        if possible:
            print(f"\n  possible correlations ({len(possible)}):")
            for item in possible:
                print(f"    {item['test']} -> changes in: {item['changed_prefix']}")

        unrelated = correlation["unrelated"]
        if unrelated:
            print(f"\n  no obvious correlation ({len(unrelated)}):")
            for item in unrelated:
                print(f"    {item['test']}")

    print("=" * 60)


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Parse spread test results")
    parser.add_argument("analysis_dir", type=Path, help="Directory containing spread results")
    parser.add_argument("--diff", type=Path, help="PR diff file to correlate with failures")
    parser.add_argument("--json", type=Path, help="Output results as JSON to this file")
    args = parser.parse_args()

    if not args.analysis_dir.exists():
        print(f"error: directory not found: {args.analysis_dir}", file=sys.stderr)
        sys.exit(1)

    entries = find_all_results(args.analysis_dir)
    if not entries:
        print("error: no results.json files found in the analysis directory", file=sys.stderr)
        sys.exit(1)

    print(f"found {len(entries)} result file(s)")

    summary = summarize_failures(entries)
    correlation = None
    patterns = None

    if args.diff and args.diff.exists():
        diff_data = parse_diff(args.diff)
        print(f"parsed diff: {len(diff_data['files'])} changed file(s)")
        correlation = correlate_failures_with_changes(summary, diff_data)

    patterns = analyze_failure_patterns(summary)

    print_summary(summary, correlation, patterns)

    if args.json:
        output = {
            "summary": summary,
        }
        if correlation:
            output["correlation"] = correlation
        if patterns:
            output["patterns"] = patterns

        with open(args.json, "w") as f:
            json.dump(output, f, indent=2, default=str)
        print(f"\njson output saved to: {args.json}")


if __name__ == "__main__":
    main()
