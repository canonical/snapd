---
name: run-spread-test
description: Run spread integration tests for snapd. Tests are slow - collect as much information as possible in each run to avoid wasting cycles.
metadata:
  project: snapd
  task-type: test
  execution-time: slow
---

## Run spread tests

Command: `./run-spread [spread-flags] <backend>:<system>:<test-path>`

Tests take 5-15 minutes each. Maximize information per run.

## Prerequisites

Build the snap first (see `build-snapd-snap` skill), then use `NO_REBUILD=1`.
If no local snap exists: either build one, or use `./run-spread --download` to fetch from master.

## Quick reference

```bash
# Standard (snap already built)
NO_REBUILD=1 ./run-spread -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt

# First run (auto-builds snap)
./run-spread -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt

# Download prebuilt from master (test-only changes)
./run-spread --download -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt

# Reuse VMs for faster iteration
NO_REBUILD=1 ./run-spread -reuse garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

Always specify `<backend>:<system>:<test-path>` — bare `./run-spread` runs everything.

## Flags

- `-debug`: Get SSH shell on failure. Use by default.
- `-reuse`: Keep VMs alive between runs (faster iteration).

## Backend and system

Always use `garden`. Do not use `qemu`, `google-*`, `openstack` etc. unless user explicitly says so.

System selection:
- AppArmor changes → recent Ubuntu LTS, optionally Debian
- SELinux changes → openSUSE SELinux or Fedora
- Systemd changes → recent Ubuntu + Fedora
- General → most recent Ubuntu LTS

Discover system names from `spread.yaml` under `backends:garden:systems:`. Don't hardcode — they change.

## Suites that don't need the snap

Use `spread` directly (not `run-spread`) for:
- `tests/unit/` — Go unit tests
- `tests/cross/` — cross-compilation

```bash
spread garden:ubuntu-24.04-64:tests/unit/go
```

All other suites (`tests/main/`, `tests/nested/`, `tests/regression/`) require the snap — use `run-spread`.

## Parallel execution

Single invocation is serial on garden (1 worker). For true parallelism, delegate to subagents — max 4 concurrent (each VM uses 3-4 GB RAM).

## Test path patterns

```bash
tests/main/snap-mgmt            # Single test
tests/main/interfaces-...       # Glob
tests/main/...                  # All in directory
```

## Failure output structure

```
Error executing garden:<system>:<test> :
-----
<execute section output — shows the failing commands>
-----
Debug output for garden:<system>:<test> :
-----
<debug section output — extra diagnostic info>
-----
Starting shell to debug...
```

Capture as much of the error and debug output as possible to avoid re-runs.

## Interpreting failures

Phases: prepare → execute → restore → debug (on failure)

- Execute failure: look for MATCH/NOMATCH assertions (`grep -qE` wrappers from `tests/lib/`)
- Prepare failure: missing deps or system incompatibility
- Restore failure: incomplete cleanup (fix to avoid pollution)

Debug shell useful commands:
```bash
journalctl -u snapd
snap changes
snap tasks <change-id>
```

## Rules

- Always use `-debug` to avoid re-runs on failure
- Always set `NO_REBUILD=1` when snap exists
- Never run `./run-spread` without a test path
- Never use non-garden backends unless instructed
- Max 4 parallel subagent tests (RAM)
- When unsure if suite needs snap, assume yes — build first
- Verify `built-snap/snapd_*.snap.keep` exists before running with `NO_REBUILD=1`
