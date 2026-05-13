#!/usr/bin/env python3
"""
Extract and search spread log files for specific test failures.

Spread logs are typically stored as log files per system/backend in the
spread-logs artifacts. This script can extract logs for specific failing
tests and search for error patterns.
"""

import argparse
import json
import os
import re
import sys
import zipfile
from pathlib import Path


ERROR_PATTERNS = [
    re.compile(r"^\s*error[:\s]", re.IGNORECASE),
    re.compile(r"^\s*fail[:\s]", re.IGNORECASE),
    re.compile(r"^\s*panic:"),
    re.compile(r"^\s*FATAL"),
    re.compile(r"^\s*CRITICAL"),
    re.compile(r"(?i)segmentation fault"),
    re.compile(r"(?i)assertion failed"),
    re.compile(r"(?i)cannot\s+"),
    re.compile(r"(?i)timed out"),
    re.compile(r"(?i)connection refused"),
    re.compile(r"(?i)no such file or directory"),
    re.compile(r"(?i)permission denied"),
]

PHASE_MARKERS = {
    "prepare": re.compile(r"^\s*[-=]+\s*Prepare\s*"),
    "execute": re.compile(r"^\s*[-=]+\s*Execute\s*"),
    "restore": re.compile(r"^\s*[-=]+\s*Restore\s*"),
    "debug": re.compile(r"^\s*[-=]+\s*Debug\s*"),
}


def extract_logs_from_zip(zip_path: Path, output_dir: Path) -> list:
    """Extract all log files from a spread-logs zip."""
    extracted = []
    output_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(zip_path, "r") as z:
        for name in z.namelist():
            if name.endswith(".log"):
                dest = output_dir / Path(name).name
                with z.open(name) as src, open(dest, "wb") as dst:
                    dst.write(src.read())
                extracted.append(dest)
    return extracted


def find_log_files(analysis_dir: Path) -> list:
    """Find all spread log files in the analysis directory.

    Finds both plain .log files and logs inside spread-logs-*.zip artifacts.
    Log files may be debug logs (named like backend_system_test.debug.log)
    or spread transcript logs.
    """
    logs = []

    # Already extracted logs — accept any .log or .debug.log in the
    # analysis tree (including inside _extracted_* directories).
    for log_file in analysis_dir.rglob("*.log"):
        logs.append(log_file)

    # Logs inside zip files that have not been extracted yet
    for zip_file in analysis_dir.glob("spread-logs-*.zip"):
        extract_dir = analysis_dir / f"_extracted_{zip_file.stem}"
        if not extract_dir.exists():
            try:
                extracted = extract_logs_from_zip(zip_file, extract_dir)
                logs.extend(extracted)
            except (zipfile.BadZipFile, OSError) as e:
                print(f"warning: could not extract {zip_file}: {e}", file=sys.stderr)
        else:
            # Extraction already happened; pick up any logs we may have
            # missed in the rglob above (e.g. when zip was extracted in a
            # previous run but the directory is not under analysis_dir root).
            for log_file in extract_dir.rglob("*.log"):
                if log_file not in logs:
                    logs.append(log_file)

    return logs


def log_filename_matches(log_path: Path, test_name: str, backend: str = "", system: str = "") -> bool:
    """Check if a debug log filename indicates it belongs to a specific test.

    Debug logs are named like: {backend}_{system}_{test}.debug.log
    Example: openstack_ubuntu-core-26-64_tests_main_lxd.debug.log
    """
    name = log_path.stem  # e.g. openstack_ubuntu-core-26-64_tests_main_lxd.debug
    # Remove the trailing .debug if present
    if name.endswith(".debug"):
        name = name[:-6]

    # The test name in the filename uses underscores instead of slashes
    test_in_filename = test_name.replace("/", "_")
    if test_in_filename not in name:
        return False

    if backend and not name.startswith(backend.replace("/", "_")):
        return False

    if system:
        # Build expected prefix: backend_system_
        expected_prefix = f"{backend.replace('/', '_')}_{system.replace('/', '_')}_"
        if not name.startswith(expected_prefix):
            return False

    return True


def find_test_in_log(log_path: Path, test_name: str, backend: str = "", system: str = "") -> list:
    """Find all occurrences of a test in a log file.

    Returns list of dicts with start_line, end_line, and context.
    For debug logs (which contain only one test's output), the entire file
    is treated as a single occurrence if the filename matches.
    For spread transcript logs, the function searches for phase markers.
    """
    occurrences = []

    # Pattern to match test markers in spread transcript logs
    # Example: 2025-05-13T10:00:00Z Executing google:ubuntu-24.04-64:tests/main/install (1/100)...
    test_pattern = re.compile(
        rf"(?:Executing|Restoring|Preparing)\s+{re.escape(backend)}:{re.escape(system)}:{re.escape(test_name)}"
    )

    with open(log_path, "r", errors="replace") as f:
        lines = f.readlines()

    # If this is a debug log and the filename matches the test, treat the
    # whole file as a single occurrence.  Debug logs do not contain phase
    # markers because they are already scoped to one test.
    if log_filename_matches(log_path, test_name, backend, system):
        errors = []
        for i, line in enumerate(lines):
            for pattern in ERROR_PATTERNS:
                if pattern.search(line):
                    errors.append({
                        "line": i + 1,
                        "text": line.rstrip(),
                    })
                    break
        occurrences.append({
            "start_line": 0,
            "end_line": len(lines),
            "context": [line.rstrip() for line in lines],
            "errors": errors,
        })
        return occurrences

    # Otherwise fall back to searching for spread phase markers.
    current_occurrence = None
    for i, line in enumerate(lines):
        if test_pattern.search(line):
            if current_occurrence:
                current_occurrence["end_line"] = i
                occurrences.append(current_occurrence)
            current_occurrence = {
                "start_line": i,
                "end_line": None,
                "context": [line.rstrip()],
                "errors": [],
            }
        elif current_occurrence:
            current_occurrence["context"].append(line.rstrip())
            # Check for error patterns
            for pattern in ERROR_PATTERNS:
                if pattern.search(line):
                    current_occurrence["errors"].append({
                        "line": i + 1,
                        "text": line.rstrip(),
                    })
                    break

            # Cap context size to avoid memory issues
            if len(current_occurrence["context"]) > 5000:
                current_occurrence["end_line"] = i
                occurrences.append(current_occurrence)
                current_occurrence = None

    if current_occurrence:
        current_occurrence["end_line"] = len(lines)
        occurrences.append(current_occurrence)

    return occurrences


def extract_test_log(log_path: Path, test_name: str, backend: str, system: str, output_path: Path) -> bool:
    """Extract log entries for a specific test to a file."""
    occurrences = find_test_in_log(log_path, test_name, backend, system)
    if not occurrences:
        return False

    with open(output_path, "w") as f:
        f.write(f"# Log for {backend}:{system}:{test_name}\n")
        f.write(f"# Source: {log_path}\n")
        f.write(f"# Found {len(occurrences)} occurrence(s)\n")
        f.write("=" * 60 + "\n\n")

        for idx, occ in enumerate(occurrences):
            f.write(f"## Occurrence {idx + 1} (lines {occ['start_line'] + 1}-{occ['end_line']})\n")
            if occ["errors"]:
                f.write(f"### Error lines ({len(occ['errors'])})\n")
                for err in occ["errors"]:
                    f.write(f"  L{err['line']}: {err['text']}\n")
                f.write("\n")
            f.write("### Full context\n")
            for line in occ["context"]:
                f.write(f"{line}\n")
            f.write("\n" + "=" * 60 + "\n\n")

    return True


def search_logs_for_pattern(logs: list, pattern: str, max_results: int = 100) -> list:
    """Search all log files for a pattern.

    Returns list of matches with file, line, and text.
    """
    compiled = re.compile(pattern, re.IGNORECASE)
    matches = []

    for log_path in logs:
        try:
            with open(log_path, "r", errors="replace") as f:
                for i, line in enumerate(f):
                    if compiled.search(line):
                        matches.append({
                            "file": str(log_path),
                            "line": i + 1,
                            "text": line.rstrip(),
                        })
                        if len(matches) >= max_results:
                            return matches
        except OSError as e:
            print(f"warning: could not read {log_path}: {e}", file=sys.stderr)

    return matches


def analyze_log_for_errors(log_path: Path) -> dict:
    """Analyze a log file for common error patterns.

    Returns dict with error counts and sample lines.
    """
    counts = {name: 0 for name in ["errors", "failures", "panics", "timeouts", "cannot_errors"]}
    samples = {name: [] for name in counts.keys()}

    try:
        with open(log_path, "r", errors="replace") as f:
            for i, line in enumerate(f):
                if ERROR_PATTERNS[0].search(line):  # error
                    counts["errors"] += 1
                    if len(samples["errors"]) < 5:
                        samples["errors"].append((i + 1, line.rstrip()))
                if ERROR_PATTERNS[1].search(line):  # fail
                    counts["failures"] += 1
                    if len(samples["failures"]) < 5:
                        samples["failures"].append((i + 1, line.rstrip()))
                if ERROR_PATTERNS[2].search(line):  # panic
                    counts["panics"] += 1
                    if len(samples["panics"]) < 5:
                        samples["panics"].append((i + 1, line.rstrip()))
                if ERROR_PATTERNS[7].search(line):  # timed out
                    counts["timeouts"] += 1
                    if len(samples["timeouts"]) < 5:
                        samples["timeouts"].append((i + 1, line.rstrip()))
                if ERROR_PATTERNS[6].search(line):  # cannot
                    counts["cannot_errors"] += 1
                    if len(samples["cannot_errors"]) < 5:
                        samples["cannot_errors"].append((i + 1, line.rstrip()))
    except OSError as e:
        print(f"warning: could not analyze {log_path}: {e}", file=sys.stderr)

    return {
        "counts": counts,
        "samples": samples,
        "path": str(log_path),
    }


def _broken_pipe_safe_print(text: str = "") -> None:
    """Print while ignoring BrokenPipeError (e.g. when piped to head)."""
    try:
        print(text)
    except BrokenPipeError:
        pass


if __name__ == "__main__":
    import signal
    # Restore default SIGPIPE handler so Python doesn't turn it into
    # BrokenPipeError / IOError when output is piped to a consumer that
    # closes early (e.g. head).
    signal.signal(signal.SIGPIPE, signal.SIG_DFL)

    parser = argparse.ArgumentParser(description="Extract and search spread logs")
    subparsers = parser.add_subparsers(dest="command", help="Commands")

    # Extract command
    extract_parser = subparsers.add_parser("extract", help="Extract logs for specific tests")
    extract_parser.add_argument("analysis_dir", type=Path, help="Analysis directory")
    extract_parser.add_argument("--test", required=True, help="Test name (e.g., tests/main/install)")
    extract_parser.add_argument("--backend", default="", help="Backend name")
    extract_parser.add_argument("--system", default="", help="System name")
    extract_parser.add_argument("--output", "-o", type=Path, default=Path("test_log.txt"), help="Output file")

    # Search command
    search_parser = subparsers.add_parser("search", help="Search logs for a pattern")
    search_parser.add_argument("analysis_dir", type=Path, help="Analysis directory")
    search_parser.add_argument("pattern", help="Regex pattern to search")
    search_parser.add_argument("--max-results", type=int, default=100, help="Max results")

    # Analyze command
    analyze_parser = subparsers.add_parser("analyze", help="Analyze logs for error patterns")
    analyze_parser.add_argument("analysis_dir", type=Path, help="Analysis directory")

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(1)

    if not args.analysis_dir.exists():
        print(f"error: directory not found: {args.analysis_dir}", file=sys.stderr)
        sys.exit(1)

    logs = find_log_files(args.analysis_dir)
    print(f"found {len(logs)} log file(s)")

    if args.command == "extract":
        found = False
        for log_path in logs:
            if extract_test_log(log_path, args.test, args.backend, args.system, args.output):
                print(f"extracted to: {args.output}")
                found = True
                break
        if not found:
            print(f"error: could not find test {args.backend}:{args.system}:{args.test} in logs", file=sys.stderr)
            sys.exit(1)

    elif args.command == "search":
        matches = search_logs_for_pattern(logs, args.pattern, args.max_results)
        try:
            print(f"found {len(matches)} match(es)")
            for m in matches:
                print(f"  {m['file']}:{m['line']}: {m['text']}")
        except BrokenPipeError:
            pass

    elif args.command == "analyze":
        try:
            for log_path in logs:
                result = analyze_log_for_errors(log_path)
                print(f"\n{log_path.name}")
                for category, count in result["counts"].items():
                    if count > 0:
                        print(f"  {category}: {count}")
                        for line_num, text in result["samples"][category]:
                            print(f"    L{line_num}: {text[:100]}")
        except BrokenPipeError:
            pass
