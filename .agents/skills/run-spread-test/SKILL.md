---
name: run-spread-test
description: Run spread integration tests for snapd. Tests are slow - collect as much information as possible in each run to avoid wasting cycles.
metadata:
  project: snapd
  task-type: test
  execution-time: slow
---

## Overview

Run spread integration tests in VMs to verify snapd functionality across different Linux distributions.

Command: `./run-spread <backend>:<system>:<test-path>`

Critical: Tests take 5-15 minutes each. Collect as much information as possible in each run to avoid wasting cycles.

## Prerequisites

Build the snapd snap first (load the `build-snapd-snap` skill for details):
- First build: `./tests/build-test-snapd-snap`
- Fast rebuild: `./tests/build-test-snapd-snap --clean-snapd-only`

Skip rebuild when snap exists: Set `NO_REBUILD=1` in the environment before calling `./run-spread`. If no prebuilt snap exists in `built-snap/`, the script exits with an error — unset `NO_REBUILD` to trigger a build, or run the build skill first.

Download prebuilt snap from 'master' branch: Use `./run-spread --download <backend>:<system>:<test>`. This is useful when only test infrastructure files changed and you don't need a locally built snap.

## Command Format

```bash
./run-spread [spread-flags] <backend>:<system>:<test-path>
```

A `<backend>:<system>:<test-path>` argument is REQUIRED. Never call `./run-spread` without specifying what to run — spread would attempt to execute all tests on all backends.

Spread flags (passed through):
- `-debug`: Create SSH shell on test failure. Use this by default — it avoids needing a re-run when logs are unclear.
- `-reuse`: Keep VMs running between tests (faster iteration)

## Backend and System Selection

Always use `garden` backend for local development (no credentials, fast, full control).

Do NOT use other backends (`qemu`, `google-*`, `openstack`, etc.) unless the user explicitly instructs you to do so.

System selection by change type:
- AppArmor changes: Pick a recent Ubuntu LTS and optionally a Debian system
- SELinux changes: Pick an openSUSE SELinux or Fedora system
- Systemd changes: Pick a recent Ubuntu and a Fedora system
- General changes: Pick the most recent Ubuntu LTS available

Available systems: Discover concrete system names from `spread.yaml` under `backends:garden:systems:`. Do not hardcode system names — they change as releases are added/removed.

When in doubt: Follow user's guidance or pick the most recent Ubuntu LTS from `spread.yaml`.

## Debugging a failing test

A failing test produces a log like shown below, where the test execution log appears in the place of `<TEST EXECUTION LOG>`, and the test's debug section execution log will appear in place of `<TEST debug: SECTION EXECUTION LOG>`:


```bash
+ exec spread -debug garden:ubuntu-24.04-64:tests/main/snap-download-corrupted-cleanup:valid_cache
2026-05-26 14:01:03 Project content is packed for delivery (124.87MB).
2026-05-26 14:01:03 If killed, discard servers with: spread -reuse-pid=3762927 -discard
2026-05-26 14:01:03 Allocating garden:ubuntu-24.04-64...
2026-05-26 14:01:03 Waiting for garden:ubuntu-24.04-64 to make SSH available at localhost:6374...
2026-05-26 14:01:03 Allocated garden:ubuntu-24.04-64.
2026-05-26 14:01:03 Connecting to garden:ubuntu-24.04-64...
2026-05-26 14:01:10 Connected to garden:ubuntu-24.04-64 at localhost:6374.
2026-05-26 14:01:10 Sending project content to garden:ubuntu-24.04-64...
2026-05-26 14:01:12 Preparing garden:ubuntu-24.04-64 (garden:ubuntu-24.04-64)...
2026-05-26 14:01:57 Preparing garden:ubuntu-24.04-64:tests/main/ (garden:ubuntu-24.04-64)...
2026-05-26 14:02:44 Checking garden:ubuntu-24.04-64:tests/main/snap-download-corrupted-cleanup:valid_cache (garden:ubuntu-24.04-64)...
2026-05-26 14:02:44 Preparing garden:ubuntu-24.04-64:tests/main/snap-download-corrupted-cleanup:valid_cache (garden:ubuntu-24.04-64)...
2026-05-26 14:02:51 Executing garden:ubuntu-24.04-64:tests/main/snap-download-corrupted-cleanup:valid_cache (garden:ubuntu-24.04-64) (1/1)...
2026-05-26 14:02:54 Error executing garden:ubuntu-24.04-64:tests/main/snap-download-corrupted-cleanup:valid_cache (garden:ubuntu-24.04-64) :
-----
<TEST EXECUTION LOG>
-----
.
2026-05-26 14:02:54 Debug output for garden:ubuntu-24.04-64:tests/main/snap-download-corrupted-cleanup:valid_cache (garden:ubuntu-24.04-64) :
-----
<TEST debug: SECTION EXECUTION LOG>
-----
.
2026-05-26 14:02:54 Starting shell to debug...
```

Attempt to capture as much as possible of the error execution log and debug section execution logs, so that you do not need to re-run the same test again.

## Test Path Specification

```bash
tests/main/snap-mgmt              # Single test
tests/main/interfaces-...         # Pattern matching
tests/main/...                    # All tests in directory
```

## Tests That Don't Need the Snapd Snap

The following suites do not install or use the prebuilt snapd snap during execution. For these, invoke `spread` directly without `run-spread`:

```bash
spread garden:ubuntu-24.04-64:tests/unit/go
spread garden:ubuntu-24.04-64:tests/cross/go-build
```

This avoids the snap build/check entirely and is significantly faster. The suite's prepare phase installs snapd from distro packages when `USE_PREBUILT_SNAPD_SNAP` is not set to `true`.

When to use `spread` directly (without `run-spread`):
- `tests/unit/` suite — runs Go unit tests and static checks, no snap needed
- `tests/cross/` suite — cross-compiles Go commands, no snap needed
- Any test where only test infrastructure files (e.g., `tests/lib/`) were changed and you don't need the snapd snap to be rebuilt/installed

When you must use `run-spread`:
- `tests/main/`, `tests/nested/`, `tests/regression/` — these suites install and exercise the snapd snap

## Common Workflows

Standard run (after building snap):
```bash
NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

Faster iteration (reuse VMs):
```bash
NO_REBUILD=1 ./run-spread -reuse garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

First run (builds snap automatically):
```bash
./run-spread -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

Download prebuilt snap from master (test-only changes):
```bash
./run-spread --download -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

## Running Multiple Tests in Parallel

Option 1 — Single invocation (serial execution on garden backend since it uses 1 worker):
```bash
NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/test-a garden:ubuntu-24.04-64:tests/main/test-b
```

Option 2 — Delegate to subagents (true parallel execution):
- Each subagent runs one test
- Limit: 4 parallel tests maximum (each VM uses 3-4 GB RAM)
- Requires 16GB+ system RAM for 4 parallel tests
- Use when tests are independent and time savings justify complexity

Example:
```bash
# Subagent 1: NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/test-1
# Subagent 2: NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/test-2
# (up to 4 total)
```

## Interpreting Test Failures

Test phases:
1. `prepare`: Setup (install dependencies, configure system)
2. `execute`: Main test logic and assertions
3. `restore`: Cleanup (should be idempotent)
4. `debug`: Debug output (shown on failure)

Common failure patterns:
- Execute failure: Check assertion failures (MATCH, NOMATCH — these are `grep -qE` wrappers from `tests/lib/`), compare actual vs expected output
- Prepare failure: Missing dependencies or system incompatibility
- Restore failure: Incomplete cleanup logic (fix to avoid test pollution)

Debug shell commands:
```bash
journalctl -u snapd              # View snapd logs
snap changes                     # See all operations
snap tasks <change-id>           # Detailed task info
ls -la /var/snap/                # Check test files
```

## spread.yaml Configuration

The `spread.yaml` file defines test infrastructure:
- `environment:`: Global env vars for all tests
- `backends:`: Backend configurations (garden, google, qemu)
- `backends:<backend>:systems:`: Available systems per backend
- `suites:`: Test suite configs (tests/main/, tests/nested/)

Tests can override via `tests/<suite>/<test>/task.yaml` with their own `environment`, `systems`, `backends`, etc.

## Examples by Scenario

AppArmor changes:
```bash
# Primary
NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/security-apparmor
# Verify portability
NO_REBUILD=1 ./run-spread -debug garden:debian-12-64:tests/main/security-apparmor
```

Investigating unclear failure:
```bash
NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/flaky-test
# -debug gives you a shell on failure to inspect the system
```

Multiple related tests:
```bash
# Pattern matching
NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/interfaces-...
# Or delegate to up to 4 subagents for parallel execution
```

## Best Practices

DO:
- Use `-debug` by default to get a shell on failure without re-running
- Prefer `NO_REBUILD=1` in environment when snap already built
- When unclear whether a suite needs the prebuilt snap, assume it does — build with `./tests/build-test-snapd-snap --clean-snapd-only` then run with `NO_REBUILD=1`
- Collect maximum information per run to minimize cycles
- Prefer `garden` backend for local development
- Choose systems appropriate to change type (see system selection guidelines above)
- Plan what information you need before executing
- Follow user's guidance when in doubt
- Limit parallel tests to 4 when using subagents (RAM constraint)
- Verify whether the test snapd snap has been built, i.e. there is a file
  `built-snap/snapd_*.snap.keep`, if not, suggest building it, or use the
  build-snapd-snap skill to do so, respecting guidelines listed in the skill.

DON'T:
- Call `./run-spread` without a `backend:system:test` argument (would run everything)
- Use backends other than `garden` unless explicitly instructed
- Waste test cycles running without clear purpose
- Exceed 4 parallel tests with subagents (RAM exhaustion)
- Ignore `restore` section failures (indicates cleanup issues)
- Assume portability between LSMs without testing
- Forget `NO_REBUILD=1` when snap exists (wastes time rebuilding)
- Call `./run-spread` without `NO_REBUILD=1` when a snap already exists in `built-snap/` — without it, `run-spread` triggers a full rebuild which takes several minutes
- Use `run-spread` for `tests/unit/` or `tests/cross/` suites — use `spread` directly instead (no snap needed)
- Assume a suite does NOT need the snap without checking — when unclear, assume it does and build with `build-snapd-snap --clean-snapd-only` before running with `NO_REBUILD=1`
