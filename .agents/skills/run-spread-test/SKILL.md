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

**Command**: `./run-spread <backend>:<system>:<test-path>`

**Critical**: Tests take 5-15 minutes each. Collect as much information as possible in each run to avoid wasting cycles.

## Prerequisites

Build the snapd snap first (load the `build-snapd-snap` skill for details):
- First build: `./tests/build-test-snapd-snap`
- Fast rebuild: `./tests/build-test-snapd-snap --clean-snapd-only`

**Skip rebuild when snap exists**: Use `--no-rebuild` / `-n` flag with `./run-spread`. If no prebuilt snap exists in `built-snap/`, the script exits with an error — drop `--no-rebuild` to trigger a build, or run the build skill first.

## Command Format

```bash
./run-spread [--no-rebuild|-n] [--] [spread-flags] <backend>:<system>:<test-path>
```

**IMPORTANT**: A `<backend>:<system>:<test-path>` argument is REQUIRED. Never call `./run-spread` without specifying what to run — spread would attempt to execute all tests on all backends.

**run-spread flags**:
- `--no-rebuild` / `-n`: Skip rebuilding snapd snap (equivalent to `NO_REBUILD=1`)

**spread flags** (passed through after optional `--`):
- `-debug`: Create SSH shell on test failure (use when logs lack clarity)
- `-reuse`: Keep VMs running between tests (faster iteration)

## Backend and System Selection

**Always use `garden` backend** for local development (no credentials, fast, full control).

**IMPORTANT**: Do NOT use other backends (`qemu`, `google-*`, `openstack`, etc.) unless the user explicitly instructs you to do so.

**System selection by change type**:

| Change Type | Primary Systems | Also Test |
|------------|-----------------|-----------|
| AppArmor | ubuntu-24.04-64, ubuntu-22.04-64 | debian-12-64, arch-linux-64 |
| SELinux | opensuse-tumbleweed-selinux-64, fedora-42-64 | centos-9-64 |
| Systemd | ubuntu-24.04-64, ubuntu-26.04-64, fedora-42-64 | - |
| General | ubuntu-24.04-64 | ubuntu-22.04-64 |

**Available systems**: Check `spread.yaml` under `backends:garden:systems:` for the current list of available systems for the garden backend.

**When in doubt**: Follow user's guidance or use ubuntu-24.04-64.

## Test Path Specification

```bash
tests/main/snap-mgmt              # Single test
tests/main/interfaces-*            # Pattern matching
tests/main/...                     # All tests in directory
```

Common suites: `tests/main/`, `tests/nested/`, `tests/regression/`

## Common Workflows

**Standard run (after building snap)**:
```bash
./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

**With debug shell (when logs unclear)**:
```bash
./run-spread --no-rebuild -- -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt
```
On failure, provides interactive SSH to inspect logs, re-run commands, examine state.

**Faster iteration (reuse VMs)**:
```bash
./run-spread --no-rebuild -- -reuse garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

**First run (builds snap automatically)**:
```bash
./run-spread garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

## Running Multiple Tests in Parallel

**Option 1: Single invocation** (spread handles parallelism):
```bash
./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/test-a garden:ubuntu-24.04-64:tests/main/test-b
```

**Option 2: Delegate to subagents** (true parallel execution):
- Each subagent runs one test
- **Limit: 4 parallel tests maximum** (each VM uses 3-4 GB RAM)
- Requires 16GB+ system RAM for 4 parallel tests
- Use when tests are independent and time savings justify complexity

Example:
```bash
# Subagent 1: ./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/test-1
# Subagent 2: ./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/test-2
# (up to 4 total)
```

## Interpreting Test Failures

**Test phases**:
1. `prepare`: Setup (install dependencies, configure system)
2. `execute`: Main test logic and assertions
3. `restore`: Cleanup (should be idempotent)
4. `debug`: Debug output (shown on failure)

**Common failure patterns**:
- **Execute failure**: Check assertion failures (MATCH, NOMATCH), compare actual vs expected output
- **Prepare failure**: Missing dependencies or system incompatibility
- **Restore failure**: Incomplete cleanup logic (fix to avoid test pollution)

**When to use `-debug`**:
- Test fails but logs don't show clear reason
- Need to inspect files, run commands manually, check service status
- Don't use by default (adds overhead, requires interaction)

**Debug shell commands**:
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

**AppArmor changes**:
```bash
# Primary
./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/security-apparmor
# Verify portability
./run-spread --no-rebuild garden:debian-12-64:tests/main/security-apparmor
```

**SELinux changes**:
```bash
./run-spread --no-rebuild garden:opensuse-tumbleweed-selinux-64:tests/main/selinux-classic-confinement
./run-spread --no-rebuild garden:fedora-42-64:tests/main/selinux-clean
# Other SELinux tests: selinux-data-context, selinux-lxd, selinux-snap-restorecon
```

**Investigating unclear failure**:
```bash
# First run
./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/flaky-test
# If logs unclear, add -debug
./run-spread --no-rebuild -- -debug garden:ubuntu-24.04-64:tests/main/flaky-test
```

**Multiple related tests**:
```bash
# Pattern matching
./run-spread --no-rebuild garden:ubuntu-24.04-64:tests/main/interfaces-*
# Or delegate to up to 4 subagents for parallel execution
```

## Best Practices

**DO**:
- Use `--no-rebuild` or `-n` flag when snap already built
- Collect maximum information per run to minimize cycles
- Prefer `garden` backend for local development
- Choose systems appropriate to change type (see table above)
- Use `-debug` when logs lack clarity
- Plan what information you need before executing
- Follow user's guidance when in doubt
- Limit parallel tests to 4 when using subagents (RAM constraint)

**DON'T**:
- Call `./run-spread` without a `backend:system:test` argument (would run everything)
- Use backends other than `garden` unless explicitly instructed
- Waste test cycles running without clear purpose
- Exceed 4 parallel tests with subagents (RAM exhaustion)
- Ignore `restore` section failures (indicates cleanup issues)
- Assume portability between LSMs without testing
- Use `-debug` by default (only when needed)
- Forget `--no-rebuild` when snap exists (wastes time)
