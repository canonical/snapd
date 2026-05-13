#!/usr/bin/env python3
"""
Parse spread-results JSON artifacts and produce a structured failure summary.

This script handles the deterministic work of:
1. Reading the manifest produced by fetch_spread_results.py
2. Parsing each spread-results JSON file (handling multiple known schemas)
3. Extracting failed tests with system, backend, and error context
4. Matching failure logs to failed tests by artifact name
5. Producing a flat JSON/CSV summary for easy LLM consumption

Usage:
    python3 scripts/parse_spread_results.py --manifest /tmp/manifest.json --output /tmp/summary.json
    python3 scripts/parse_spread_results.py --manifest /tmp/manifest.json --format csv --output /tmp/summary.csv
    cat /tmp/manifest.json | python3 scripts/parse_spread_results.py --format json
"""

import argparse
import csv
import json
import re
import sys
from pathlib import Path


def _looks_like_test_record(obj: dict) -> bool:
    """Heuristic: does this dict look like a test result record?"""
    if not isinstance(obj, dict):
        return False
    # Must have a name-like key and a result-like key
    has_name = any(k in obj for k in ("name", "test", "task", "check", "id"))
    has_result = any(k in obj for k in ("status", "result", "outcome", "state", "success", "failed"))
    return has_name and has_result


def _is_failed_status(value) -> bool:
    """Check if a status value indicates failure."""
    if isinstance(value, bool):
        # "success": false means failure
        return not value
    if isinstance(value, int):
        # "failed": 1 or "task_failed": 2 means failure
        return value > 0
    if not isinstance(value, str):
        return False
    return value.lower() in ("failed", "error", "fail", "unsuccessful", "aborted", "broken")


def _extract_test_records(data, system_hint: str = "", backend_hint: str = "") -> list:
    """
    Recursively extract test records from spread-results JSON.
    
    Returns list of dicts with keys like:
      name, system, backend, status, error, duration, phase, details
    """
    records = []

    if isinstance(data, list):
        for item in data:
            records.extend(_extract_test_records(item, system_hint, backend_hint))
        return records

    if not isinstance(data, dict):
        return records

    # If this dict itself looks like a test record, extract it
    if _looks_like_test_record(data):
        name = (
            data.get("name")
            or data.get("test")
            or data.get("task")
            or data.get("check")
            or data.get("id")
            or "unknown"
        )

        # Determine failure status from multiple possible fields
        status = (
            data.get("status")
            or data.get("result")
            or data.get("outcome")
            or data.get("state")
            or ""
        )

        # Check boolean/integer failure indicators
        is_failed = _is_failed_status(status)
        if not is_failed and "success" in data:
            is_failed = data["success"] is False
        if not is_failed and "failed" in data:
            val = data["failed"]
            is_failed = (val is True) or (isinstance(val, int) and val > 0)

        if is_failed:
            record = {
                "name": name,
                "system": data.get("system", system_hint),
                "backend": data.get("backend", backend_hint),
                "status": status or "failed",
                "error": data.get("error", data.get("message", data.get("detail", ""))),
                "duration": data.get("duration", data.get("time", "")),
                "phase": data.get("phase", data.get("stage", "")),
                "details": json.dumps(data) if len(json.dumps(data)) < 2000 else "",
            }
            records.append(record)
        return records

    # Otherwise, iterate keys. Some keys are system/backend names.
    for key, value in data.items():
        hint_system = system_hint
        hint_backend = backend_hint

        # Detect backend_system keys like "openstack_ubuntu-core-24-64"
        if isinstance(value, dict) and not _looks_like_test_record(value):
            if re.match(r"^[a-z]+_[a-z0-9-]+$", key, re.I):
                parts = key.split("_", 1)
                if len(parts) == 2:
                    hint_backend = parts[0]
                    hint_system = parts[1]
            elif "backend" in key.lower() or "system" in key.lower():
                hint_system = key

        records.extend(_extract_test_records(value, hint_system, hint_backend))

    return records


def _extract_summary_counts(data) -> dict:
    """
    Extract summary-level counts if the JSON has them.
    Returns dict like {"failed": N, "aborted": N, "passed": N, ...} per system.
    """
    summaries = {}
    if not isinstance(data, dict):
        return summaries

    for key, value in data.items():
        system = key
        backend = ""
        if re.match(r"^[a-z]+_[a-z0-9-]+$", key, re.I):
            parts = key.split("_", 1)
            backend = parts[0]
            system = parts[1]

        if isinstance(value, dict):
            counts = {}
            for count_key in ("failed", "aborted", "passed", "skipped", "task-failed", "task_failed", "executing"):
                if count_key in value:
                    try:
                        counts[count_key.replace("-", "_")] = int(value[count_key])
                    except (ValueError, TypeError):
                        pass
            if counts:
                summaries[key] = {
                    "system": system,
                    "backend": backend,
                    "counts": counts,
                }
    return summaries


def parse_manifest(manifest_path: str | None) -> dict:
    """Read manifest JSON from file or stdin."""
    if manifest_path and manifest_path != "-":
        manifest_text = Path(manifest_path).read_text(encoding="utf-8")
    else:
        manifest_text = sys.stdin.read()
    return json.loads(manifest_text)


def analyze_manifest(manifest: dict) -> dict:
    """
    Analyze manifest and produce structured summary.
    
    Returns dict with:
      - pr: PR metadata
      - workflow_runs: per-run summary
      - all_failed_tests: flat list of failed tests
      - log_artifacts: list of log artifacts with extracted paths
    """
    pr = manifest.get("pr", {})
    output_dir = manifest.get("output_directory", "")
    runs = manifest.get("workflow_runs", [])

    result = {
        "pr": {
            "number": pr.get("number"),
            "title": pr.get("title"),
            "html_url": pr.get("html_url"),
            "changed_files_count": pr.get("changed_files_count"),
        },
        "workflow_runs": [],
        "all_failed_tests": [],
        "log_artifacts": [],
    }

    for run in runs:
        run_summary = {
            "run_id": run.get("run_id"),
            "run_number": run.get("run_number"),
            "name": run.get("name"),
            "conclusion": run.get("conclusion"),
            "status": run.get("status"),
            "html_url": run.get("html_url"),
            "spread_result_summaries": [],
            "failed_tests": [],
            "failure_log_artifacts": [],
        }

        # Parse spread-results artifacts
        for art in run.get("artifacts", {}).get("spread_results", []):
            artifact_name = art.get("artifact_name", "")
            artifact_summary = {
                "artifact_name": artifact_name,
                "artifact_id": art.get("artifact_id"),
                "extracted_path": art.get("extracted_path"),
                "json_files_analyzed": 0,
                "failed_tests_found": 0,
                "summary_counts": {},
            }

            for jf in art.get("json_files", []):
                if "error" in jf:
                    continue
                json_path = jf.get("path")
                if not json_path:
                    continue
                try:
                    content = json.loads(Path(json_path).read_text(encoding="utf-8"))
                except (json.JSONDecodeError, OSError):
                    continue

                artifact_summary["json_files_analyzed"] += 1

                # Try to get summary counts first
                summaries = _extract_summary_counts(content)
                if summaries:
                    artifact_summary["summary_counts"] = summaries

                # Extract individual failed tests
                records = _extract_test_records(content)
                for rec in records:
                    rec["artifact_name"] = artifact_name
                    rec["run_id"] = run.get("run_id")
                    rec["run_name"] = run.get("name")
                    rec["workflow_url"] = run.get("html_url")
                artifact_summary["failed_tests_found"] += len(records)
                run_summary["failed_tests"].extend(records)
                result["all_failed_tests"].extend(records)

            run_summary["spread_result_summaries"].append(artifact_summary)

        # Parse failure log artifacts
        for art in run.get("artifacts", {}).get("failure_logs", []):
            log_entry = {
                "artifact_name": art.get("artifact_name"),
                "artifact_id": art.get("artifact_id"),
                "extracted_path": art.get("extracted_path"),
                "log_files": art.get("log_files", []),
            }
            run_summary["failure_log_artifacts"].append(log_entry)
            result["log_artifacts"].append(log_entry)

        result["workflow_runs"].append(run_summary)

    return result


def write_json(data: dict, output_path: str | None):
    """Write JSON to file or stdout."""
    text = json.dumps(data, indent=2)
    if output_path and output_path != "-":
        Path(output_path).write_text(text, encoding="utf-8")
    else:
        print(text)


def write_csv(failed_tests: list, output_path: str | None):
    """Write flat CSV of failed tests."""
    if not failed_tests:
        print("No failed tests found.", file=sys.stderr)
        return

    fieldnames = [
        "name", "system", "backend", "status", "error",
        "duration", "phase", "artifact_name", "run_id", "run_name", "workflow_url"
    ]
    # Ensure all records have the keys
    rows = []
    for rec in failed_tests:
        row = {k: rec.get(k, "") for k in fieldnames}
        rows.append(row)

    if output_path and output_path != "-":
        with open(output_path, "w", newline="", encoding="utf-8") as f:
            writer = csv.DictWriter(f, fieldnames=fieldnames)
            writer.writeheader()
            writer.writerows(rows)
    else:
        writer = csv.DictWriter(sys.stdout, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Parse spread-results artifacts from manifest"
    )
    parser.add_argument(
        "--manifest",
        default="-",
        help="Path to manifest JSON (default: '-' for stdin)",
    )
    parser.add_argument(
        "--output",
        default="-",
        help="Output file path (default: '-' for stdout)",
    )
    parser.add_argument(
        "--format",
        choices=["json", "csv"],
        default="json",
        help="Output format (default: json)",
    )

    args = parser.parse_args()

    try:
        manifest = parse_manifest(args.manifest)
    except json.JSONDecodeError as e:
        print(f"error: cannot parse manifest JSON: {e}", file=sys.stderr)
        return 1
    except Exception as e:
        print(f"error: cannot read manifest: {e}", file=sys.stderr)
        return 1

    result = analyze_manifest(manifest)

    if args.format == "json":
        write_json(result, args.output)
    else:
        write_csv(result["all_failed_tests"], args.output)

    return 0


if __name__ == "__main__":
    sys.exit(main())
