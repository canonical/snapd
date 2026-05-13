#!/usr/bin/env python3
"""
Generate a compact human-readable text report from spread test artifacts.

Reads the manifest produced by fetch_spread_results.py, parses every
spread-results JSON file (handling multiple schemas), and emits a flat
text report that is easy for an agent to read and reason about.

Usage:
    python3 scripts/generate_text_report.py --manifest /tmp/manifest.json --output /tmp/report.txt
    python3 scripts/generate_text_report.py --manifest /tmp/manifest.json          # writes to stdout
"""

import argparse
import json
import sys
from pathlib import Path
from fnmatch import fnmatch


def _is_failed(value: str) -> bool:
    return value.lower() in ("failed", "error", "fail", "unsuccessful", "aborted", "broken")


def _extract_records(data, system_hint: str = "", backend_hint: str = "") -> list:
    """Recursively extract failed test records from spread-results JSON."""
    records = []
    if isinstance(data, list):
        for item in data:
            records.extend(_extract_records(item, system_hint, backend_hint))
        return records
    if not isinstance(data, dict):
        return records

    # Detect test record by presence of name + status keys
    name_keys = ("name", "test", "task", "check", "id")
    status_keys = ("status", "result", "outcome", "state")
    has_name = any(k in data for k in name_keys)
    has_status = any(k in data for k in status_keys)

    if has_name and has_status:
        name = (
            data.get("name") or data.get("test") or data.get("task")
            or data.get("check") or data.get("id") or "unknown"
        )
        status = (
            data.get("status") or data.get("result") or data.get("outcome")
            or data.get("state") or "unknown"
        )
        if _is_failed(status):
            records.append({
                "name": name,
                "system": data.get("system", system_hint),
                "backend": data.get("backend", backend_hint),
                "status": status,
                "error": str(data.get("error", data.get("message", data.get("detail", "")))),
            })
        return records

    # Otherwise recurse, using keys as system/backend hints
    for key, value in data.items():
        hint_system = system_hint
        hint_backend = backend_hint
        if isinstance(value, dict) and not (has_name and has_status):
            # Keys like "openstack_ubuntu-core-24-64" or "ubuntu-core-24"
            if "_" in key and any(c.islower() for c in key.split("_")[0]):
                parts = key.split("_", 1)
                hint_backend = parts[0]
                hint_system = parts[1]
            else:
                hint_system = key
        records.extend(_extract_records(value, hint_system, hint_backend))

    return records


def _extract_summary_counts(data) -> list:
    """Extract top-level summary counts if present."""
    results = []
    if not isinstance(data, dict):
        return results
    for key, value in data.items():
        if not isinstance(value, dict):
            continue
        counts = {}
        for ck in ("failed", "aborted", "passed", "skipped", "task-failed", "task_failed", "executing"):
            if ck in value:
                try:
                    counts[ck.replace("-", "_")] = int(value[ck])
                except (ValueError, TypeError):
                    pass
        if counts:
            results.append({"key": key, "counts": counts})
    return results


def generate_report(manifest: dict) -> str:
    lines = []
    pr = manifest.get("pr", {})
    lines.append("=" * 60)
    lines.append(f"PR #{pr.get('number')} — {pr.get('title', 'Unknown')}")
    lines.append(f"URL: {pr.get('html_url', '')}")
    lines.append(f"Changed files: {pr.get('changed_files_count', 0)}")
    lines.append("=" * 60)
    lines.append("")

    for run in manifest.get("workflow_runs", []):
        run_id = run.get("run_id", "?")
        run_name = run.get("name", "Unknown")
        conclusion = run.get("conclusion", "?")
        status = run.get("status", "?")
        lines.append(f"## Workflow run: {run_name} (run_id={run_id})")
        lines.append(f"   Conclusion: {conclusion} | Status: {status}")
        lines.append(f"   URL: {run.get('html_url', '')}")
        lines.append("")

        # Spread results
        for art in run.get("artifacts", {}).get("spread_results", []):
            art_name = art.get("artifact_name", "unknown")
            lines.append(f"### Spread results: {art_name}")

            for jf in art.get("json_files", []):
                if "error" in jf:
                    lines.append(f"  [JSON parse error: {jf['error']}]")
                    continue
                json_path = jf.get("path")
                if not json_path:
                    continue
                try:
                    content = json.loads(Path(json_path).read_text(encoding="utf-8"))
                except (json.JSONDecodeError, OSError) as e:
                    lines.append(f"  [Cannot read JSON: {e}]")
                    continue

                # Summary counts
                summaries = _extract_summary_counts(content)
                if summaries:
                    lines.append("  Summary counts:")
                    for s in summaries:
                        items = ", ".join(f"{k}={v}" for k, v in s["counts"].items())
                        lines.append(f"    {s['key']}: {items}")

                # Failed test records
                records = _extract_records(content)
                if records:
                    lines.append(f"  Failed tests ({len(records)}):")
                    for rec in records:
                        system = rec.get("system") or "unknown"
                        backend = rec.get("backend") or ""
                        loc = f"{backend}_{system}" if backend else system
                        err = rec.get("error", "")
                        err_str = f" | error: {err[:120]}" if err else ""
                        lines.append(f"    - {rec['name']} [{loc}] (status: {rec['status']}){err_str}")
                else:
                    lines.append("  No failed tests found in this artifact.")
                lines.append("")

        # Failure logs
        logs = run.get("artifacts", {}).get("failure_logs", [])
        if logs:
            lines.append("### Failure logs:")
            for art in logs:
                art_name = art.get("artifact_name", "unknown")
                extract_path = art.get("extracted_path", "")
                log_files = art.get("log_files", [])
                lines.append(f"  - {art_name}")
                lines.append(f"    Path: {extract_path}")
                if log_files:
                    for lf in log_files:
                        size = lf.get("size_bytes", 0)
                        rel = lf.get("relative_path", "")
                        lines.append(f"      + {rel} ({size} bytes)")
                else:
                    lines.append("      (no log files extracted)")
            lines.append("")

        lines.append("")

    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Generate text report from spread artifacts")
    parser.add_argument("--manifest", default="-", help="Manifest JSON path (default: stdin)")
    parser.add_argument("--output", default="-", help="Output path (default: stdout)")
    args = parser.parse_args()

    try:
        if args.manifest == "-":
            manifest_text = sys.stdin.read()
        else:
            manifest_text = Path(args.manifest).read_text(encoding="utf-8")
        manifest = json.loads(manifest_text)
    except (json.JSONDecodeError, OSError) as e:
        print(f"error: cannot read manifest: {e}", file=sys.stderr)
        return 1

    report = generate_report(manifest)

    if args.output == "-":
        print(report)
    else:
        Path(args.output).write_text(report, encoding="utf-8")

    return 0


if __name__ == "__main__":
    sys.exit(main())
