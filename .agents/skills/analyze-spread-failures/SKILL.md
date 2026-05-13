---
name: analyze-spread-failures
description: Analyzes spread integration test failures in GitHub pull requests for the canonical/snapd repository. Downloads PR diffs, spread-results artifacts, spread logs, and debug artifacts from GitHub Actions to determine whether test failures are caused by PR changes, flaky tests, or infrastructure issues. Use when the user provides a snapd GitHub PR number or URL and asks about spread test failures, CI failures, failing integration tests, or wants to diagnose which tests failed and why. Also use when the user wants to download spread test results, logs, or debug artifacts from a PR.
compatibility: python3
allowed-tools: python3 bash grep cat curl
---

# Analyze Spread Test Failures

## Overview

Analyzes spread integration test failures in snapd pull requests by downloading and correlating GitHub Actions artifacts with both PR code changes and the current local snapd codebase. The workflow determines whether failures are PR regressions, flaky tests, or infrastructure issues.

Because the agent is running inside a local snapd repository checkout, the current codebase state is available and should be consulted alongside the PR diff. The diff shows what changed, but the local files provide the full context of the existing code that the PR modifies.

## Workflow

### 1. Input Validation

Ensure the user has provided:
- A GitHub PR number or URL for the snapd repository (canonical/snapd)

**If missing:** Ask for the PR number or URL.

**Optional:** The user may specify particular tests they are interested in. Note these for filtering later.

### 2. Download Spread Data

Create a unique temporary directory for this analysis instance and run the download script to fetch all relevant data from the PR's latest completed workflow run:

```bash
TMPDIR=$(mktemp -d /tmp/spread-analysis-XXXXXX)
python3 scripts/download_pr_data.py <PR> -o "$TMPDIR"
```

This downloads:
- `$TMPDIR/pr-<number>.diff` — the PR code changes
- `$TMPDIR/spread-results-*.zip` — JSON result files from each spread job
- `$TMPDIR/spread-logs-*.zip` — raw spread log files
- `$TMPDIR/spread-artifacts-*.zip` — debug artifacts (if available)

**Local codebase context:** The local working directory contains the current snapd repository state. Use it to read the full content of files the PR touches, which often reveals context that the diff alone cannot (e.g., surrounding functions, existing error handling, related tests).

**Authentication:** The script uses the `GITHUB_TOKEN` environment variable. If unset, unauthenticated requests work for public repos but have lower rate limits.

**Security — Do NOT expose the token:** Under no circumstances should the value of `GITHUB_TOKEN` be printed, echoed, logged, or included in the analysis output. The token must remain secret throughout the workflow.

**Error handling:**
- If no completed workflow run exists: Report that the PR has no completed CI run and stop.
- If some artifacts are missing: Note which ones are unavailable and continue with what was downloaded.

**Cleanup:** The temporary directory (`$TMPDIR`) can be removed after the analysis is complete. It is intentionally placed under `/tmp` to avoid conflicts with other executions and to allow the system to clean it up automatically.

### 3. Parse Results

Generate a structured failure report and correlate with PR changes:

```bash
python3 scripts/parse_spread_results.py "$TMPDIR" --diff "$TMPDIR"/pr-*.diff --json "$TMPDIR"/failure-report.json
```

This produces:
- A printed summary of failed tests grouped by system and test name
- Pattern analysis showing which tests fail on multiple systems vs. single systems
- Correlation of failures with PR code changes
- `$TMPDIR/failure-report.json` for programmatic use

### 4. Examine Local Codebase (if needed)

For failing tests that correlate with modified files, read the current local versions of those files in the working directory to understand the full context:

```bash
# Identify files touched by the PR
grep -E '^diff --git ' "$TMPDIR"/pr-*.diff | awk '{print $3}' | sed 's|^a/||' | sort -u > "$TMPDIR"/changed-files.txt

# Read relevant local files for context
cat <changed-file-path>
```

This provides:
- The existing code structure around the changed lines
- Related functions, constants, or error strings that appear in logs
- Test helper code that may explain failure behavior

### 5. Examine Logs (if needed)

For specific failing tests, extract detailed log sections:

```bash
# Extract log for a specific test
python3 scripts/extract_spread_logs.py extract "$TMPDIR" \
    --test tests/main/<test-name> \
    --backend <backend> \
    --system <system> \
    --output "$TMPDIR"/test-log.txt

# Search all logs for an error pattern
python3 scripts/extract_spread_logs.py search "$TMPDIR" "<error-pattern>"

# Analyze all logs for error statistics
python3 scripts/extract_spread_logs.py analyze "$TMPDIR"
```

### 6. Analyze Failures

Follow the detailed checklist in `references/spread_analysis_workflow.md`, augmented with local codebase context:

1. **Read PR diff** — Understand what subsystems changed and which files were touched
2. **Read local files** — For changed source files, read the current local versions to understand the full surrounding context (functions, error paths, related logic)
3. **Review failure report** — Identify unique failing tests, failure phases, and affected systems
4. **Correlate with PR and local code** — Check if failures relate to modified code paths, and use the local file content to interpret error messages or logic paths
5. **Check multi-system pattern** — Same test failing on multiple systems increases PR-causation likelihood
6. **Read logs** — Examine specific error messages for evidence; cross-reference error strings with current local source
7. **Form hypothesis** — Classify as PR-caused, flaky, infrastructure, or unknown
8. **Propose code fixes (conditional)** — Only if the failure is determined to be an actual issue in snapd code (either introduced by the PR or pre-existing snapd code that is genuinely broken and meaningful to fix). Do not propose code changes for flaky tests, infrastructure issues, or tests that are failing for unrelated reasons.

### 7. Output Results

Structure the final analysis as a **concise** report. Prioritize speed and token efficiency: summarize rather than transcribe, quote only the most relevant 1–2 log lines per test, and skip sections that add no value.

Required sections:

- **Summary (1–3 bullets):** Total failures, unique tests, affected systems, PR subsystems changed
- **Per-test analysis:** One compact line per failing test: `test-name` → systems, phase, hypothesis, confidence
- **Key evidence:** Only the strongest 1–3 correlation signals (e.g., multi-system failure, direct path match, error string match)
- **Recommendations:** One-line actions (fix, re-run, investigate infrastructure, no action)
- **Code fix suggestions (conditional):** Only include if a failing test is determined to be caused by an actual issue in snapd code (PR regression or meaningful pre-existing bug). For each such test, provide:
  - The specific file(s) and function/code area to change
  - A brief description of what the fix should do
  - A short rationale tying the suggested change to the observed failure
  
  Do NOT include code fix suggestions for flaky tests, infrastructure failures, or failures with no clear snapd code correlation.

Omit unless it changes the conclusion:
- Verbatim log dumps
- Local codebase context summaries
- Long evidence lists
- Low-confidence hypotheses

## References

- `references/spread_analysis_workflow.md` — Step-by-step analysis checklist with heuristics
- `references/github_artifacts_reference.md` — Artifact naming, structure, and lifecycle details

## Tips

- **The local repo provides full context.** A diff shows deltas, but reading the current local file reveals the existing error handling, related functions, and test helpers that the PR interacts with.
- **Multi-system failures are the strongest signal.** If the same test fails on 3+ systems, it is almost certainly a PR regression.
- **Preparing/restoring failures are rarely PR-caused.** Focus analysis on `executing` phase failures.
- **Look at error messages, not just pass/fail.** The specific error text is the best evidence for correlation. Search the local source for those exact strings to find the code path hit.
- **Nested test failures are significant.** Nested tests are expensive and less likely to be flaky.
- **Rate limits:** Unauthenticated GitHub API requests are limited to 60/hour. Set `GITHUB_TOKEN` for 5000/hour.
- **Artifact expiration:** GitHub artifacts expire after 90 days. Older PRs may have no downloadable artifacts.
