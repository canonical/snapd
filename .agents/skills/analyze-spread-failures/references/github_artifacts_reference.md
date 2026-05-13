# Snapd GitHub Actions Artifacts Reference

This reference describes the artifacts produced by snapd's CI workflows, how to identify them, and what data they contain.

## Workflow Structure

The main CI workflow is `Tests` (`.github/workflows/ci-test.yaml`). It triggers on PRs and pushes to release branches. Key spread-related jobs:

- `spread-fundamental` — Tests on fundamental (required) systems
- `spread-not-fundamental-pr` — Tests on non-fundamental systems (PR only)
- `spread-not-fundamental-not-pr` — Tests on non-fundamental systems (non-PR)
- `spread-nested` — Nested virtualization tests

## Spread Artifacts

### 1. `spread-results-<run_id>-<attempt>-<group>.zip`

**Contents:** `results.json`

**Structure:**
```json
{
  "items": [
    {
      "level": "task",
      "verb": "executing",
      "backend": "google",
      "system": "ubuntu-24.04-64",
      "name": "tests/main/install",
      "variant": "",
      "success": false,
      "skipped": false,
      "log-id": "...",
      "duration": "30s"
    }
  ]
}
```

**Key fields:**
- `level`: `"task"` for tests, `""` for aborted, `"project"` for suite-level
- `verb`: `"preparing"`, `"executing"`, `"restoring"`, `"allocating"`, `"discarding"`, `"debug"`
- `success`: boolean pass/fail
- `skipped`: boolean
- `name`: test path like `tests/main/install`
- `variant`: variant suffix if applicable

**Failure filtering logic (from spread-tests.yaml):**
```bash
jq -r '.items[] | select(.level == "task" and .success == false and .skipped == false and .verb != "checking") | "\(.backend):\(.system):\(.name)\(if .variant != "" then ":\(.variant)" else "" end)"'
```

### 2. `spread-logs-<group>-<systems>.zip`

**Contents:** Individual `.log` files for each backend:system combination.

**Description:**
- Raw spread output logs.
- Each log contains timestamps, test execution traces, debug output.
- Logs are text files, often very large (MBs).
- Use `scripts/extract_spread_logs.py` to search/extract from these.

### 3. `spread-artifacts-<group>-<systems>_<run_id>_<attempt>.tar.gz`

**Contents:** Debug artifacts collected from failed tests.

**Description:**
- Only uploaded when `upload-artifacts: true`.
- Contains per-test directories with debug data.
- May include `journal.txt`, system state, screenshots (for nested).
- Named with `--` replacing `/` in test paths.

### 4. `<job>-results-<run_id>-<group>-<attempt>.zip`

**Contents:** `failed-tests` file (plain text list).

**Description:**
- Space-separated list of `backend:system:name:variant` failed tests.
- Used by the retry mechanism (rerun.yaml).
- Contains tests from the last attempt only.

### 5. `pr_number.zip`

**Contents:** Single file with the PR number.

**Description:**
- Used by `spread-results-reporter.yaml` and `rerun.yaml` to know which PR to comment on.

## Artifact Lifecycle

- Artifacts are retained for **90 days** on GitHub.
- After retention expires, artifacts are no longer downloadable.
- The `rerun.yaml` workflow can trigger automatic reruns (up to 5 attempts).
- The `spread-results-reporter.yaml` workflow posts a summary comment on the PR.

## Authentication Notes

- Public repo artifacts can be downloaded without authentication.
- API rate limits apply: 60 requests/hour for unauthenticated, 5000/hour with token.
- Set `GITHUB_TOKEN` environment variable for authenticated requests.
