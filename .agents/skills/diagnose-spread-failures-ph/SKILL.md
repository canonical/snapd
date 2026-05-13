---
name: diagnose-spread-failures
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

**If PR number is missing or `GITHUB_TOKEN` is not set:** Stop and ask for the required information.

### 2. Fetch Spread Artifacts

Run the artifact fetch script. **Do not perform API calls manually.**

```bash
GITHUB_TOKEN=<TOKEN> python3 scripts/fetch_spread_results.py \
  --pr <PR_NUMBER> \
  [--repo canonical/snapd] \
  [--log-pattern "*logs*"] \
  [--workflow-name "Spread"] \
  [--output-dir /tmp/spread-results] \
  [--manifest /tmp/manifest.json]
```

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

### 4. Analyze Results

Use `references/analysis_checklist.md` to systematically examine the parsed summary (`/tmp/summary.json`):

1. Read the parsed summary JSON. It contains `workflow_runs`, `all_failed_tests`, and `log_artifacts`.
2. Review the PR changed files (`pr.changed_files`) from the manifest to understand what areas were touched.
3. Identify workflow runs with `conclusion: failure`. Note that a single workflow run may have multiple **attempts** (artifacts named like `spread-results-{run_id}-1-{system}` vs `spread-results-{run_id}-2-{system}`). Compare attempts to distinguish persistent regressions from transient flakes.
4. If a workflow is `in_progress`, only analyze completed attempts and note the in-progress status.
5. For each failed test in `all_failed_tests`, read the associated log artifact under `extracted_path` and extract error context.
6. **Correlate each failure with PR changes** to determine if it is a regression or unrelated (flaky / pre-existing).
7. Synthesize a concise report.

Load the checklist before starting analysis:
```bash
cat references/analysis_checklist.md
```

### 5. Report Findings

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

## Error Handling

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
