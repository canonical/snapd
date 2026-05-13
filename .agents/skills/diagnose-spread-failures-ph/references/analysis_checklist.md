# Spread Test Failure Analysis Checklist

This checklist guides systematic analysis of spread test artifacts downloaded by `scripts/fetch_spread_results.py`.

## When to Use This Reference

Use this after running the fetch script **and** the parse script (`scripts/parse_spread_results.py`). Follow the checklist to produce a comprehensive failure analysis.

## Manifest Structure Overview

The manifest JSON contains:

- `pr`: PR metadata (number, title, head_sha, head_ref, html_url, changed_files, changed_files_count)
- `repository`: Owner/repo string
- `output_directory`: Path where artifacts were extracted
- `workflow_runs[]`: Array of workflow run entries
  - `run_id`, `name`, `conclusion`, `status`, `html_url`
  - `artifacts.spread_results[]`: Downloaded `spread-results-*` artifacts
    - `extracted_path`: Directory containing the JSON files
    - `json_files[]`: Metadata about each JSON file found
      - `path`, `relative_path`, `top_level_keys`, `is_array`, `array_length`
  - `artifacts.failure_logs[]`: Downloaded log artifacts matching the pattern
    - `extracted_path`: Directory containing the log files
    - `log_files[]`: Metadata about each log file
      - `path`, `relative_path`, `size_bytes`

## Analysis Workflow

### Step 1: Parse the Manifest with the Automated Parser

**Always run `scripts/parse_spread_results.py` first.** Do not traverse JSON files manually.

```bash
python3 scripts/parse_spread_results.py \
  --manifest /tmp/manifest.json \
  --output /tmp/summary.json
```

The summary JSON contains:
- `workflow_runs[]`: Per-run summaries with `spread_result_summaries` and `failure_log_artifacts`
- `all_failed_tests[]`: Flat list of every failed test with `name`, `system`, `backend`, `status`, `error`, `duration`, `artifact_name`, `run_id`, `run_name`, `workflow_url`
- `log_artifacts[]`: List of all failure log artifacts with `extracted_path` and `log_files`

If parsing returns zero failed tests despite a `conclusion: failure`, read the raw `spread-results-*` JSON and report its `top_level_keys` so the parser can be updated.

### Step 2: Identify Workflow Runs, Attempts, and In-Progress Work

1. Scan `summary.workflow_runs[]` and note any run where `conclusion` is `failure` or `status` is not `completed`.
2. **Watch for multiple attempts of the same workflow:** In the snapd repository, the `Tests` workflow often has re-run attempts. Artifacts are named like `spread-results-{run_id}-1-{system}` (attempt 1) and `spread-results-{run_id}-2-{system}` (attempt 2). The manifest aggregates them under a single `workflow_runs[]` entry for that `run_id`.
3. **Compare attempts** to distinguish persistent regressions from transient flakes:
   - If a failure appears in attempt 1 but disappears in attempt 2, flag it as likely flaky.
   - If a failure appears in every attempt, treat it as a stronger candidate for a real regression.
   - Use `artifact_name` in `all_failed_tests[]` to see which attempt each failure came from.
4. **If `status` is `in_progress`:** Only analyze completed attempts. Do not attempt to read artifacts from incomplete attempts (they may be missing or truncated). Record the in-progress status in your report.

### Step 3: Understand the PR Changes

Before diving into failures, review what the PR actually changes:

1. Read `pr.changed_files[]` from the manifest. Each entry contains:
   - `filename`: Path of the changed file
   - `status`: `added`, `modified`, or `removed`
   - `additions`, `deletions`, `changes`: Line counts
   - `patch`: The actual diff text (may be truncated by GitHub for very large files)
2. Group changed files by area:
   - **Snapd core / daemon**: `overlord/`, `daemon/`, `cmd/snapd/`
   - **CLI client**: `cmd/snap/`
   - **Sandbox**: `cmd/snap-confine/`, `cmd/snap-exec/`
   - **Interfaces**: `interfaces/builtin/`, `interfaces/`
   - **Tests**: `tests/`, `spread.yaml`
   - **Packaging / build**: `packaging/`, `build-aux/`, `Makefile`, `*.yaml`
   - **Initrd / boot**: `core-initrd/`, `boot/`
3. Note any changes to test suites or to the systems/backends used in spread tests.

### Step 4: Enumerate Failed Tests from the Parsed Summary

Use `summary.all_failed_tests[]` instead of manual JSON traversal:

1. Count total failed tests.
2. Group by `system` and `backend` to spot clustering.
3. For each failed test, the parser already provides:
   - Test name/identifier
   - System/backend where it failed
   - Error message or summary (if present in JSON)
   - Duration (if available)
   - Link to the workflow run (`workflow_url`)
   - Source artifact name (which reveals attempt number)

### Step 5: Correlate and Parse Failure Logs

Failure log artifacts are plain-text files that follow a predictable structure. For each log artifact in `summary.log_artifacts[]` (or `workflow_runs[].failure_log_artifacts[]`):

1. **Match the log to a failed test or system** by inspecting the artifact name and file contents. Artifact names often include the test suite, task name, or backend/system identifier (e.g., `ubuntu-22.04-64` or `tests/main/foo`).
2. **Read the log file using the exact path from the manifest. Use `log_files[].path` or `extracted_path` + `relative_path`. Never guess paths.**
3. **Read the log file fully or in chunks**. The typical contents are:
   - **Command history**: A chronological list of shell commands executed during the spread task.
   - **Failed command details**: The exact command that returned a non-zero exit code, often preceded by `+` or shown inline.
   - **Command output**: stdout/stderr of the failing command.
   - **Journal output**: `journalctl` logs captured from the system at the time of failure (look for blocks starting with `-- Logs begin at ...`).
4. **Locate the failure point** by scanning for:
   - Exit-code indicators (`error`, `exit status`, `returned exit code`, `FAILED`)
   - Spread task markers (`-----`, `=======`, `+ ` prefix for commands)
   - The last successful command and the command that immediately follows it (the failing one)
5. **Extract the failure context**:
   - Copy the failing command itself.
   - Copy 10–20 lines of command history leading up to it (shows the test setup).
   - Copy any stderr / error message directly after the failing command.
   - Copy any relevant journal output around the failure time (service crashes, AppArmor denials, OOM kills, etc.).
6. **Cross-reference with JSON results**: Confirm that the test name found in the summary matches the log artifact name or content, ensuring you are analyzing the correct failure.

### Step 6: Correlate Failures with PR Changes

This is the critical diagnostic step: determine whether each failure is caused by the PR or is unrelated (flaky / pre-existing).

1. **Map failed tests to changed directories**:
   - If a test under `tests/main/interfaces/` fails and the PR modifies `interfaces/builtin/`, the failure is likely a direct regression.
   - If a test under `tests/main/snapd-reexec/` fails and the PR changes `cmd/snap-exec/`, investigate re-execution logic.
   - If a test under `tests/main/refresh/` fails and the PR modifies `overlord/snapstate/`, the failure is likely related.
2. **Inspect the patch of changed files** for clues:
   - Look for changes to the exact subsystem mentioned in the test name.
   - Look for changes to error messages or return codes that the test may assert against.
   - Look for changes to security profiles, mount logic, or systemd units that spread tests validate end-to-end.
3. **Check backport/fix patterns** (common in snapd packaging):
   - If the PR changes a version-specific directory (e.g., `core-initrd/24.04/`), check whether the corresponding newer directory (e.g., `core-initrd/26.04/`) already contains the same change. This strongly suggests a legitimate backport/fix rather than a regression.
   - Read the newer version's file directly from the local repository to confirm.
4. **Assess failure likelihood**:
   - **Likely caused by PR**: Failure appears in a test area directly touched by changed files, or the error message references changed code paths.
   - **Likely unrelated / flaky**: Failure appears in a completely unrelated subsystem, or is a known flaky test (e.g., network-dependent, timing-sensitive), or it disappears on a re-run attempt.
   - **Uncertain**: Log contains environment errors (SSH timeout, VM provisioning failure, external network issue) rather than assertion failures.
5. **Record the correlation** for each failed test:
   - Related changed files (if any)
   - Confidence level: `direct`, `indirect`, `unrelated`, `unclear`
   - Rationale: One-sentence explanation of why the PR did or did not cause this failure.

### Step 7: Synthesize Findings

Produce a structured report including:

1. **Summary**: PR number, total workflow runs inspected, number of failed runs, total failed tests, number of changed files.
2. **PR Change Overview**: Brief bullet list of the main areas touched by the PR.
3. **Per-Workflow-Run Breakdown**:
   - Run name and URL
   - Number of attempts analyzed
   - List of `spread-results-*` artifacts and their top-level structure
4. **Failed Test Details**:
   - For each failed test: name, system, workflow run, link to log file (if found), key error snippet.
   - **Correlation**: Related changed files, confidence level (`direct` / `indirect` / `unrelated` / `unclear`), and rationale.
5. **Log Artifacts**: List of downloaded log artifacts and what they contain.
6. **Recommendations**:
   - If failures cluster around a specific system or test suite, highlight it.
   - If failures are clearly caused by the PR, suggest which changed files to inspect.
   - If failures appear unrelated, flag them as potential flakes or infrastructure issues and suggest re-running the specific test.

## Common JSON Patterns to Expect

Since the exact schema of `spread-results-*` is not fixed, expect variations. The parser handles the most common ones; if it misses a variant, expect:

- **Flat array of test records**: `[{"name": "tests/main/foo", "status": "failed", ...}, ...]`
- **Nested by system/backend**: `{"openstack_ubuntu-core-24-64": {"tests/core/services": {"status": "failed"}}}`
- **Summary + details**: `{"summary": {"failed": 2}, "details": [...]}`

Always inspect `top_level_keys` first when the parser cannot extract data.

## Log Parsing Tips

- **Use `grep` for quick navigation**: Search for `FAIL`, `error:`, `cannot`, `exit status`, `FAILED`, `panic`, or `+ ` (command echo) inside the log directory.
- **Journal blocks**: Look for `journalctl`, `-- Logs begin`, or timestamped systemd entries. Focus on AppArmor denials (`audit`), OOM killer messages, or service failures.
- **Command echo lines**: In spread logs, executed commands are often prefixed with `+ `. The line before the error block is usually the failing command.
- **Large files**: If a log is >1 MB, read the last 300 lines first (tail) because the failure is typically at the end of the test output. Then read chunks around identified error markers.
- **Correlating multiple artifacts**: If a single test produces multiple log files (e.g., separate `journal.log` and `debug.log`), read them together to build the full picture.
- **Path discipline**: When reading a log file, construct the path from `extracted_path` + `relative_path` in the manifest. Do not guess filenames.

## Tips for Large Artifacts

- If `json_files` is large, focus on files with the most data (largest `array_length` or deepest nesting).
- If log files are large, read the first and last 200 lines to find error boundaries, or search for keywords: `FAIL`, `ERROR`, `--- FAIL`, `panic`, `cannot`, `error:`.
- Use grep on the output directory for quick keyword searches across logs.
