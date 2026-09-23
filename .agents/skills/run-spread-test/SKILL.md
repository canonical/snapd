---
name: run-spread-test
description: Run and debug spread integration tests for snapd, investigate failures in the live debug shell, determine the root cause, and propose or implement a solution. Tests are slow - collect as much information as possible in each run to avoid wasting cycles.
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
NO_REBUILD=1 ./run-spread -reuse -debug garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

Always specify `<backend>:<system>:<test-path>` — bare `./run-spread` runs everything.

## Flags

- `-debug`: Get SSH shell on failure. Use by default.
- `-reuse`: Keep VMs alive between runs (faster iteration).

## Backend and system

Always use `garden`. Do not use `qemu`, `openstack` etc. unless user explicitly says so.

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

## Reproduce before changing code

Before proposing or applying a fix, run the failing test without modifying the
test or product code. Use the same system and relevant environment as the
reported failure when available, changing only the backend to `garden` when
requested. This baseline run must use `-debug`.

Record whether the run reproduces the same failed phase, command, exit status,
and symptom. Keep the debug shell open and collect evidence when it reproduces.
Do not edit code based only on the original CI log when a local reproduction is
available.

Also record the conditions needed to repeat the baseline: commit and local
changes, backend and system, image or snap revisions, built or downloaded snapd
artifact, relevant environment variables, Spread flags, test arguments, and
whether the VM was fresh or reused. These conditions define the verification
run.

For an intermittent failure, make a reasonable number of baseline attempts or
use the test's repeat support when resources permit. Do not claim a fix from a
single passing run if the original failure was intermittent.

If reproduction is blocked by allocation, image download, credentials,
external service availability, or another infrastructure problem, stop before
patching unless the existing evidence independently proves the root cause.
Report the blocker and distinguish any proposed change as unverified. Never
describe a patch as fixing the issue unless the original failure was reproduced
and the same test subsequently passed with the patch.

## Debug a failure

Keep the spread command running when it prints `Starting shell to debug...`.
The shell belongs to that same terminal session; do not start another spread
process or exit the shell before collecting evidence.

1. Read the complete failed command and its output before running commands.
2. Identify the failed phase and the exact assertion or command that failed.
3. Use the live shell to test the smallest plausible root-cause hypothesis.
4. Collect enough state to distinguish a product bug, test bug, dependency
   failure, image problem, or infrastructure issue.
5. Exit the shell only after the evidence needed for a proposed solution has
   been captured. Spread will then continue with restore and cleanup.

Start with commands relevant to the failure rather than running a large,
generic diagnostic dump. Useful examples:

```bash
# snapd and system state
systemctl status snapd --no-pager
journalctl -u snapd --no-pager -n 300
snap changes
snap tasks <change-id>

# failed process or assertion
command -v <command>
<command> --version
env | sort
cat /etc/os-release
uname -a

# packaging and external dependencies
apt-cache policy <package>
snap info <snap>
getent hosts <host>
curl -v <url>
```

Re-run only the failed command in the debug shell when it is safe and will not
destroy evidence. Preserve its exit status and output. Do not rerun the whole
task manually because prepare and restore state may make the result misleading.

If the terminal integration reports that the command needs input, continue in
the existing terminal session and send debug commands there one at a time.
Do not launch the slow test again merely to obtain a new shell.

## Diagnose and propose a solution

Trace the failure to the layer that owns the broken behavior. Inspect the
failed test's `task.yaml`, its helper scripts, and the relevant implementation
or image configuration. Compare expected behavior with the live system state.
Do not assume every failed assertion is a snapd code defect.

Classify the conclusion and provide one of these concrete outcomes:

- **Test defect:** patch the test or helper when its expectation, timing,
  cleanup, feature detection, or system assumptions are wrong.
- **Product defect:** patch snapd when the observed behavior violates the
  intended contract. Add the narrowest regression coverage that reproduces it.
- **External dependency:** identify the dependency, failing operation, and
  evidence that the local code and image are not the cause. Propose a retry,
  pin, availability check, or upstream action as appropriate.
- **Image defect:** identify the missing or incorrect package, service,
  kernel capability, account, permission, or image configuration. Propose the
  image change or a justified test prerequisite.
- **Infrastructure/transient failure:** identify the failing backend, network,
  storage, DNS, or allocation operation and the evidence that the test did not
  reach the behavior under test. Do not hide deterministic failures with retries.

When the evidence supports a repository change and the user asked to resolve
the issue, implement the smallest root-cause fix. Otherwise provide a precise
patch proposal or explanation naming the owning component and next action.
Clearly separate verified facts from hypotheses.

After a patch, first run the cheapest focused check for the changed code. Then
rerun the same Spread command under the recorded baseline conditions. Keep the
backend, system, image state, environment, flags, test arguments, VM freshness,
and artifact build method unchanged. Only the proposed fix and an artifact
rebuilt from that fix may differ.

If the baseline used a fresh VM, verification must use a fresh VM. If it used a
reused VM, restore it to the same relevant state or reproduce again on a fresh
VM before verification. Do not compare a reused post-failure machine with a
fresh baseline when leaked state may affect the result.

Report the baseline result, post-fix result, and all condition differences. If
the same conditions cannot be recreated, mark the verification as inconclusive
and do not claim the issue is fixed.

## Interpreting failures

Phases: prepare → execute → restore → debug (on failure)

- Execute failure: look for MATCH/NOMATCH assertions (`grep -qE` wrappers from `tests/lib/`)
- Prepare failure: missing deps or system incompatibility
- Restore failure: incomplete cleanup (fix to avoid pollution)

## spread.yaml structure

Key sections: `environment:` (global vars), `backends:` (garden/openstack/qemu configs), `backends:<backend>:systems:` (available systems), `suites:` (test suite configs). Individual tests can override settings in their `task.yaml`.

## Rules

- Always use `-debug` to avoid re-runs on failure
- Reproduce the original failure before editing or claiming a fix
- Verify the fix under the same recorded conditions as the reproduction
- Always set `NO_REBUILD=1` when snap exists
- Never run `./run-spread` without a test path
- Never use non-garden backends unless instructed
- Max 4 parallel subagent tests (RAM)
- When unsure if suite needs snap, assume yes — build first
- Verify `built-snap/snapd_*.snap.keep` exists before running with `NO_REBUILD=1`
