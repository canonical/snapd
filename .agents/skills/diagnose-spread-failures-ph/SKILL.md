---
name: diagnose-spread-failures-ph
description: Diagnoses GitHub Actions spread test failures for pull requests in the snapd repository. Downloads and analyzes spread-results-* JSON artifacts and failure log artifacts from workflow runs to identify which tests failed, on which systems, and correlates them with available logs. Use when investigating CI failures, spread test regressions, or GitHub Actions workflow artifacts for a snapd PR.
compatibility: python3
allowed-tools: python3 bash grep cat
---

# Diagnose Spread Failures

## Overview

This skill automates retrieval and analysis of spread test artifacts from GitHub Actions runs associated with a snapd pull request. It handles the deterministic work of API calls and artifact downloads, then guides structured analysis of the results to pinpoint failures.

## Workflow

### 1. Input Validation

Ensure the user has provided:
- **Pull request number** (required)
- **GitHub token** (required; must be set via the `GITHUB_TOKEN` environment variable)
- **Repository** (optional; default is `canonical/snapd`)
- **Log artifact pattern** (optional; default is `*logs*`)
- **Workflow name filter** (optional; default is no filter)

**If PR number is missing:** Stop and ask for it.

**Verify the token safely before use. Never print `GITHUB_TOKEN` to the console.**

```bash
bash scripts/check_github_token.sh
```

This script exits 0 if the token is present and non-empty, and exits 1 with a safe error message if it is missing. The token value is never written to stdout or stderr.

**If the check fails:** Ask the user to set `GITHUB_TOKEN` via `export GITHUB_TOKEN=<token>`. Do not echo, print, or log the token value anywhere in the session.

### 2. Fetch Spread Artifacts

Run the artifact fetch script. **Do not perform API calls manually.** The script reads `GITHUB_TOKEN` from the environment; never pass it as a command-line argument.

```bash
python3 scripts/fetch_spread_results.py \
  --pr <PR_NUMBER> \
  [--repo canonical/snapd] \
  [--log-pattern "*logs*"] \
  [--workflow-name "Spread"] \
  [--output-dir /tmp/spread-results] \
  [--manifest /tmp/manifest.json]
```

**Important:** Ensure `GITHUB_TOKEN` is already exported in the environment before running this. Do not write the token into the command line.

**What the script does:**
1. Resolves the PR head SHA via the GitHub API.
2. Finds workflow runs for that SHA.
3. Downloads all `spread-results-*` artifacts (JSON summaries).
4. Downloads log artifacts matching the configured glob pattern.
5. Extracts all artifacts locally and emits a manifest JSON.

**On success:** The manifest path is printed (or emitted to stdout if `--manifest -`).

### 3. Parse Spread Results into Structured Summary

Before attempting manual JSON exploration, run the deterministic parser to extract failed tests into a flat, predictable format. **Do not parse the JSON files manually.**

```bash
python3 scripts/parse_spread_results.py \
  --manifest /tmp/manifest.json \
  --output /tmp/summary.json
```

**What the script does:**
1. Reads every JSON file referenced by the manifest (from `spread-results-*` artifacts).
2. Recursively detects and parses various known JSON schemas (flat arrays, nested by system/backend, summary+details).
3. Extracts all failed test records into a flat list with fields: `name`, `system`, `backend`, `status`, `error`, `duration`, `phase`, `artifact_name`, `run_id`, `run_name`, `workflow_url`.
4. Presents the data as structured JSON or CSV.

**If parsing produces no results:** The JSON schema may be unrecognized. Read the raw `spread-results-*` JSON and report its structure so the parser can be updated.

### 4. Generate Compact Text Report

For large manifests, produce a compact human-readable text report that lists failed tests, summary counts, and log artifact locations in a single document. This is easier to read and reason about than raw JSON.

```bash
python3 scripts/generate_text_report.py \
  --manifest /tmp/manifest.json \
  --output /tmp/report.txt
```

**Use this report when:**
- The manifest contains dozens of artifacts and manual JSON traversal would be error-prone.
- You need a quick overview of failures across all workflow runs and attempts.
- You want a flat list of failed tests with system/backend and error snippets.

**After generating the report, read it** (`cat /tmp/report.txt`) to begin analysis.

### 5. Analyze Results

Use `references/analysis_checklist.md` to systematically analyze the failures. **Start by reading the compact text report** produced in the previous step:

```bash
cat /tmp/report.txt
```

This gives you the flat list of all failed tests, summary counts, and artifact locations without manual JSON traversal. Then, for deep analysis:
- Review the parsed machine data in `/tmp/summary.json` for structured fields.
- Review PR changed files (`pr.changed_files`) from the manifest.
- Identify workflow runs with `conclusion: failure`. Note that a single workflow run may have multiple **attempts** (artifacts named like `spread-results-{run_id}-1-{system}` vs `spread-results-{run_id}-2-{system}`). Compare attempts to distinguish persistent regressions from transient flakes.
- If a workflow is `in_progress`, only analyze completed attempts and note the in-progress status.
- For each failed test, read the associated log artifact under `extracted_path` and extract error context.
- **Correlate each failure with PR changes** to determine if it is a regression or unrelated (flaky / pre-existing).
- Synthesize a concise report.

Load the checklist before starting analysis:
```bash
cat references/analysis_checklist.md
```

This gives you the flat list of all failed tests, summary counts, and artifact locations without manual JSON traversal. Then, for deep analysis:
- Review the parsed machine data in `/tmp/summary.json` for structured fields.
- Review PR changed files (`pr.changed_files`) from the manifest.
- Identify workflow runs with `conclusion: failure`. Note that a single workflow run may have multiple **attempts** (artifacts named like `spread-results-{run_id}-1-{system}` vs `spread-results-{run_id}-2-{system}`). Compare attempts to distinguish persistent regressions from transient flakes.
- If a workflow is `in_progress`, only analyze completed attempts and note the in-progress status.
- For each failed test, read the associated log artifact under `extracted_path` and extract error context.
- **Correlate each failure with PR changes** to determine if it is a regression or unrelated (flaky / pre-existing).
- Synthesize a concise report.

Load the checklist before starting analysis:
```bash
cat references/analysis_checklist.md
```

### 6. Report Findings

Present the analysis in this structure:

1. **Summary**: PR number, total workflow runs inspected, number of failed runs, total failed tests, number of changed files.
2. **PR Change Overview**: Brief bullet list of the main areas touched by the PR.
3. **Failed Tests**: For each failed test, list:
   - Test name
   - System/backend
   - Workflow run name and URL
   - Correlated log artifact (if found)
   - Key error snippet or summary
   - **Correlation with PR**: Related changed files, confidence level (`direct` / `indirect` / `unrelated` / `unclear`), and rationale.
4. **Artifacts**: List of `spread-results-*` and log artifacts downloaded, with their extracted paths.
5. **Recommendations**:
   - If tests cluster around a specific system or test suite, highlight it.
   - If failures are clearly caused by the PR, suggest which changed files to inspect.
   - If failures appear unrelated, flag them as potential flakes or infrastructure issues and suggest re-running the specific test.

## Security

**Never print, echo, or log `GITHUB_TOKEN` in the session.**

- Use `scripts/check_github_token.sh` to verify the token is set; it never reveals the value.
- If the token is missing, ask the user to run `export GITHUB_TOKEN=<token>` in their shell before starting.
- The fetch script reads the token from the environment only. Do not pass it via `--token` or inline in commands.
- If you need to verify environment variables, use `env | grep -i github` (this shows the variable name but masks the value in most shells) or simply run the check script.

## Error Handling

* **If the token check script exits non-zero:** The token is missing or invalid. Ask the user to set it with `export GITHUB_TOKEN=<token>` and re-run.
* **If the fetch script exits non-zero:** Report the exact stderr output. Common causes:
  - Missing `GITHUB_TOKEN` environment variable
  - PR number does not exist
  - No workflow runs found for the PR
  - No `spread-results-*` artifacts found
* **If the parse script exits non-zero or produces no results:** Report the exact stderr. If it returns zero failed tests despite failures, read the raw `spread-results-*` JSON and describe its structure.
* **If the manifest/summary has no failed runs:** Report that CI appears green for the spread workflow.
* **If log artifacts do not match the pattern:** Report which artifacts were available and suggest adjusting `--log-pattern`.
* **If a workflow is `in_progress`:** Only analyze completed attempts from that workflow. Note the in-progress status in the report and do not attempt to download artifacts from incomplete runs (they may be missing or incomplete).

## References

- `references/analysis_checklist.md` — Step-by-step checklist for analyzing downloaded artifacts and producing a failure report.
- `references/subsystem_map.md` — Maps snapd source code directories to their relevant spread test suites, and vice versa. Use this during correlation to identify related code areas beyond the directly changed files.
