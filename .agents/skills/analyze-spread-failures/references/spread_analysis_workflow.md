# Spread Test Failure Analysis Checklist

Use this checklist when analyzing spread test failures in snapd PRs. **Work quickly and keep notes brief—only record details that affect the conclusion.**

## Pre-requisites

- [ ] `scripts/download_pr_data.py` has been run (using a temporary directory such as `$TMPDIR`) and produced:
  - `$TMPDIR/pr-*.diff` — the PR diff
  - `$TMPDIR/artifacts/spread-results-*.zip` — results JSON files
  - `$TMPDIR/logs/` or `spread-logs-*.zip` — spread log files
  - `$TMPDIR/debug/` or `spread-artifacts-*.zip` — debug artifacts
- [ ] `scripts/parse_spread_results.py` has been run and produced a summary

## Phase 1: Load PR Changes

1. Read the PR diff (`pr-*.diff`) to understand what changed.
2. Identify the primary subsystems affected:
   - `overlord/snapstate/` — snap lifecycle operations
   - `overlord/ifacestate/` — interface connections and security
   - `interfaces/builtin/` — specific interface implementations
   - `boot/` — bootloader and initramfs
   - `cmd/snap/` — CLI behavior
   - `tests/` — test changes themselves
   - `.github/workflows/` — CI infrastructure
   - `packaging/` — distro packaging
3. Note if the PR touches:
   - Core daemon logic (more likely to cause widespread failures)
   - Spread test files (changes may break or fix tests directly)
   - New features without matching test changes

## Phase 2: Identify Failing Tests

1. Review the output of `parse_spread_results.py`:
   - Total number of failed tasks
   - Number of unique tests vs. duplicate failures (reruns?)
   - Failure phases: `preparing`, `executing`, `restoring`
2. Categorize failures by phase:
   - **`preparing`** — Environment setup failed. Usually infrastructure, image issues, or package dependency problems. Less likely to be PR-caused unless the PR changes test preparation.
   - **`executing`** — The actual test failed. This is the most relevant phase for PR correlation.
   - **`restoring`** — Cleanup failed. Often infrastructure or race conditions. Can mask real executing failures.
3. Note any `aborted` tests — these are usually infrastructure timeouts or crashes.

## Phase 3: Check Multi-System Failure Pattern

For each failing test, determine how many systems it failed on:

- **Same test fails on 3+ systems** → High likelihood the PR introduced a regression. This is the strongest signal.
- **Same test fails on 2 systems** → Moderate likelihood. Could be a subtle regression or flaky behavior.
- **Test fails on only 1 system** → Low likelihood. More likely to be flaky, system-specific, or infrastructure.

Use the `patterns` output from `parse_spread_results.py` to see this quickly.

## Phase 4: Correlate Failures with PR Changes

For each **executing** failure:

1. **Direct path correlation:**
   - Does the test name or directory match a changed file?
   - Example: `tests/main/interfaces-network` failing after changes to `interfaces/builtin/network*.go` → **Direct correlation**
2. **Subsystem correlation:**
   - Do changes in `overlord/snapstate/` correlate with failures in `tests/main/install*`, `tests/main/remove*`?
   - Do boot changes correlate with nested tests or core device tests?
3. **Logic correlation:**
   - Read the test log to see the exact failure message.
   - Search the log for error messages that reference modified functions or error strings.
   - Use `scripts/extract_spread_logs.py search` to find specific error patterns.

## Phase 5: Examine Logs and Debug Data

1. Extract relevant log sections:
   ```bash
    python3 scripts/extract_spread_logs.py extract "$TMPDIR" \
        --test tests/main/<test-name> \
        --backend <backend> \
        --system <system> \
        --output "$TMPDIR"/test-log.txt
   ```
2. Search for error indicators:
   ```bash
    python3 scripts/extract_spread_logs.py search "$TMPDIR" "error:"
   ```
3. If `spread-artifacts-*.zip` (debug data) was downloaded:
   - Extract the zip.
   - Look for `debug/` or `failed-tests/` directories.
   - Check `journal.txt` or system logs if included.
4. Analyze log patterns:
   ```bash
    python3 scripts/extract_spread_logs.py analyze "$TMPDIR"
   ```

## Phase 6: Form Hypothesis

For each unique failing test, classify it:

| Hypothesis | Indicators |
|:---|:---|
| **PR-caused regression** | Direct correlation with changed code; fails on multiple systems; executing phase; error message matches new/modified logic |
| **Flaky test** | Fails on only one system; intermittent across runs; timeout or race condition; known in `snapd-testing-skip` |
| **Infrastructure** | Preparing/restoring phase; many unrelated tests fail on same system; aborted tests; image or network issues |
| **Pre-existing** | Failure matches known issue; not correlated with PR changes; test fails on master too |

## Phase 7: Suggest Code Fixes (Conditional)

Only for failures that are actual issues in snapd production code — either PR regressions or meaningful pre-existing bugs. Limit suggestions to production code only, not unit tests. Skip this phase entirely for flaky tests, infrastructure issues, or unrelated failures. Only suggest the fix; do NOT actually make any code changes.

For each qualifying failure:

1. **Identify the root cause in snapd production code:**
    - Read the relevant local source files (not just the diff) to understand the current implementation.
    - Map the observed error message or failure behavior to a specific code path, function, or logic flaw.
    - If the PR introduced the issue, pinpoint the changed lines that caused the regression.
    - If the issue is pre-existing, determine whether it is meaningful to fix (e.g., the test is revealing a real bug, not just a timing issue or test fragility).
    - Focus on production code files only; do not suggest fixes in unit test files.

2. **Determine the fix approach:**
   - Consider the minimal change that resolves the failure while preserving existing behavior.
   - Check if error handling, validation, or state management needs adjustment.
   - Look at related functions or tests for patterns on how similar cases are handled.

3. **Document the suggestion:**
   - File and approximate function/line range to modify.
   - Brief description of the code change (not a full patch, but specific enough to act on).
   - Rationale linking the suggested change to the observed test failure.

**Do NOT propose code changes when:**
- The failure is infrastructure-related (preparing/restoring, image issues, network timeouts).
- The failure is a known flaky test (single-system, intermittent, timeout/race).
- The failure is in the test itself (test logic needs adjustment rather than snapd production code).
- The required fix would be in unit test code rather than production code.
- There is no clear correlation between the failure and snapd source code.

**Important:** Only suggest code fixes. Do NOT actually make any code changes.

## Phase 8: Produce Concise Report

Keep the report brief. Quote only the single most relevant log line per test. Group similar failures together.

Structure:

```markdown
## Spread Failure Analysis for PR #<number>

### Summary
- N failed tasks across M unique tests on X systems
- Primary subsystem changed: <subsystem>
- Risk: High / Medium / Low

### Per-Test Analysis

| Test | Systems | Phase | Hypothesis | Confidence |
|:---|:---|:---|:---|:---|
| tests/main/... | 22.04, 24.04 | executing | PR regression | High |
| tests/nested/... | 24.04 | preparing | Infrastructure | Low |

### Key Evidence
- Multi-system failure on tests/main/... ( strongest signal )
- Error "..." matches changed function in <file>

### Recommendations
1. Fix `tests/main/...` before merge (regression)
2. Re-run `tests/nested/...` to confirm infrastructure flakiness

### Code Fix Suggestions for Production Code (only if applicable)
- `tests/main/...` — Modify `overlord/snapstate/install.go` function `doInstall` to validate X before Y; rationale: error log shows nil dereference when Y is accessed before validation.
- `tests/main/...` — Update `interfaces/builtin/network.go` `BeforePreparePlug` to reject empty "path" attribute; rationale: test failure indicates unhandled empty path causing sandbox setup failure.
```

## Conciseness Rules

- Prefer a compact table over a multi-line entry per test.
- If the hypothesis is obvious (e.g., multi-system executing failure on a directly changed subsystem), state it in one sentence.
- Do not paste full log sections; a single quoted error line is sufficient.
- Skip "Local codebase context" and "Evidence" subsections unless they provide decisive information.
- If all failures are infrastructure or flaky, say so in the summary and omit the per-test table entirely.

## Key Heuristics

- **More systems = more likely PR fault.** A test failing on 3+ systems is almost certainly a regression.
- **Preparing/restoring failures are rarely PR-caused.** Focus on `executing` phase failures unless the PR changes test setup.
- **Error message > pass/fail.** One relevant error line is worth more than a full log dump.
- **Timeout failures are often flaky.** But consistent multi-system timeouts suggest a performance regression.
- **Interface tests fail together.** Multiple interface failures often mean `interfaces/builtin/common.go` or backend changes.
- **Nested tests are expensive and meaningful.** Nested failures are less likely to be flaky.
