---
name: spread-runner
description: Run and debug snapd Spread tests, choose the correct test suite and runner, investigate failures in an interactive shell or remote debug-socket session, and verify changes. Use standalone or as the execution stage of spread-failure-workflow, spread-reproducer, or spread-fixer.
metadata:
  project: snapd
  task-type: test
  execution-time: slow
---

## Scope

This skill owns Spread execution and debugging mechanics. The
`spread-failure-workflow` skill coordinates a complete run-reproduce-fix-verify
cycle. Use
`spread-reproducer` to investigate failures from supplied CI job URLs, and
`spread-fixer` to make a focused fix to a test or its helpers. Those skills
should use this one for test commands and verification rather than duplicating
runner guidance.

## Run spread tests

Command: `./run-spread [wrapper-options] -- [spread-flags] <backend>:<system>:<test-path>`

Tests take 5-15 minutes each. Maximize information per run.

Use `-s NAME` (or `--scenario NAME`) to pass the assignments in
`tests/scenarios/NAME` to the Spread process. Scenario values override inherited
environment values with the same names; other environment variables are
preserved. `./run-spread --help` lists the supported scenario names. For
example, `-s snapd_snap` passes `SPREAD_SNAP_REEXEC=1`.

Choose the snapd artifact based on what is being reproduced:

- For a failure identified in a test report, use `-d` to download a prebuilt
  snapd snap instead of building from the current checkout.
- For a test-only change, including task metadata, scripts, fixtures, and
  helper updates, use `-d` unless the requested conditions require a specific
  locally built snapd revision. The current project files are still sent to
  the worker; `-d` only avoids rebuilding the snapd snap.
- For a GitHub job tied to a local or branch snapd product-code change, omit
  `-d` so `run-spread` builds the snap from the checked-out source.
- `-d` downloads the latest successful master build, which may not match the
  report's source commit. Record the downloaded build and any source mismatch.
- Put Spread flags and the selector after `--` when using `run-spread` options,
  for example: `./run-spread -d -- -debug-socket BACKEND:SYSTEM:TEST_PATH`.
- Put the scenario name before `--`, for example:
  `./run-spread -d -s snapd_snap -- -repeat 3 BACKEND:SYSTEM:TEST_PATH`.
- Use `NO_REBUILD=1` only when an existing local snap is known to match the
  intended source; it may be stale.

## Match CI scenarios

The `scenario` input in `.github/workflows/spread-tests.yaml` labels results
for reporting; it is not itself a Spread environment variable. `generic` is
the default. Reproduce the workflow's actual artifact selection and `spread-env`
values instead. Do not set `SCENARIO` locally.
The mappings below follow the callers in `nightly-spread.yaml`,
`weekly-feature-tagging.yaml`, and `ci-release.yaml`.

| CI scenario | Runtime setup to reproduce |
|---|---|
| `generic` | The label implies no environment variables. Still copy the caller's `spread-env`: for example, `ci-release.yaml` leaves the scenario at its default `generic` for the deb test but sets `SPREAD_SNAPD_DEB_FROM_REPO=false SPREAD_SNAP_REEXEC=0`. |
| `deb` | The nightly build-from-current workflow uses `SPREAD_SNAPD_DEB_FROM_REPO=false SPREAD_MODIFY_CORE_SNAP_FOR_REEXEC=0 SPREAD_SNAP_REEXEC=0`. Use the exact set for the workflow being reproduced. |
| `master` | The label alone does not select the master-push snap. In nightly master jobs, the reusable workflow defaults to the snap built in that workflow. Locally, `-d` (or `DOWNLOAD_SNAPD_SNAP=1`) downloads the latest successful `ci-master-push.yaml` artifact, which may differ from a particular CI run. |
| `features` | Set `SPREAD_TAG_FEATURES=1` and pass `-artifacts <directory>` so the tagged feature data is collected. This enables feature diagnostics, not experimental snapd features. |

The nightly `deb` workflow also sets `use-snapd-snap-from-master: true` while
disabling re-exec; artifact selection is separate from the deb-related
variables, so mirror both when matching that workflow. If CI has prebuilt deb
packages in `built-pkgs/<system>`, it sets
`SPREAD_USE_PREBUILT_PACKAGES=true`; only set that locally when the matching
package artifacts are present. Otherwise, leave it unset so the test setup can
build packages in the worker.

Across scenarios, CI also injects `SPREAD_STORE_USER` and
`SPREAD_STORE_PASSWORD` from secrets. Never hard-code or include their values
in logs or handoffs. `run-spread` sets `SPREAD_USE_PREBUILT_SNAPD_SNAP=true`
after building or downloading a snap; do not set it manually when using the
wrapper.

`SPREAD_EXPERIMENTAL_FEATURES=feature1,feature2` is separate from
`SPREAD_TAG_FEATURES`; it enables the named experimental features and
corresponds to the workflow's `spread-experimental-features` input.

```bash
# Generic has no implied environment; this example builds the current checkout
./run-spread -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH

# Deb: match nightly build-from-current
SPREAD_SNAPD_DEB_FROM_REPO=false \
SPREAD_MODIFY_CORE_SNAP_FOR_REEXEC=0 \
SPREAD_SNAP_REEXEC=0 \
./run-spread -d -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH

# ci-release deb-test (scenario label remains generic)
SPREAD_SNAPD_DEB_FROM_REPO=false \
SPREAD_SNAP_REEXEC=0 \
./run-spread -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH

# Master-push snap artifact
./run-spread -d -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH

# Feature diagnostics and artifact collection
SPREAD_TAG_FEATURES=1 \
./run-spread -- -debug-socket -repeat 3 -artifacts spread-artifacts BACKEND:SYSTEM:TEST_PATH
```

## Spread mental model

The selector has three required parts: `backend` chooses the execution
provider, `system` chooses a configured OS/image, and `test-path` selects a
test or suite. A selected path can expand into jobs for multiple tasks or
variants. Use `spread -list` to preview the jobs selected by a broad path.
Check `spread.yaml` for supported systems, backends, suite settings, and test
selection; a test's `task.yaml` can add or override task-specific settings.
Settings cascade from project through backend, system, suite, and task.

`run-spread` prepares or downloads the snapd snap when needed, then invokes
Spread. Spread provisions workers and schedules jobs, each corresponding to a
project/backend/system/suite/task/variant combination. Hooks can be defined at
several configuration levels: `prepare` and `restore` set up and clean up
their scope, while `prepare-each` and `restore-each` run around each task.
The task itself runs `prepare`, `execute`, and `restore`; cleanup is attempted
even if setup or execution fails.

`-debug` pauses at the failure with an interactive shell on that same worker
and environment before cleanup continues. For agent-driven or headless remote
debugging, use `-debug-socket` instead. It pauses without opening a terminal and
creates `.spread.socket` in the project directory. While the Spread process is
paused, run `spread exec -- COMMAND` from that directory to execute a command in
the failed task's directory and environment; run `spread stop` when evidence
collection is complete so Spread can continue with restore. Each `spread exec`
uses a separate remote command and does not share process state with the task.
The socket commands are separate from task `debug` or `debug-each` scripts,
which can emit diagnostics on failure.

Tasks are normally ordered randomly. Spread logs a seed that can help replay
the order, but with multiple workers task assignment can vary due to scheduling.
When testing a suspected predecessor sequence, use `-order` to request the
selected task order and verify the actual order from logs or result artifacts.

Result artifacts can include `results.json`. Its `.instance` identifies the
worker for a task row within that artifact, and `.start` helps establish
execution order. Use the instance and timestamps when investigating whether
an earlier test could have left state behind; sharing a backend, system, or
run does not by itself prove two tasks used the same worker.

For more on the matrix, lifecycle hooks, debugging, and task ordering, see the
[Spread Plus documentation](https://github.com/canonical/spread-plus#the-cascading-matrix).

## How snapd Spread systems are prepared

The project-specific source of truth is [`spread.yaml`](../../../spread.yaml)
and the helpers it calls under [`tests/lib/`](../../../tests/lib). Standard
system-test suites such as `tests/smoke/`, `tests/main/`, and
`tests/regression/` call `prepare-restore.sh --prepare-suite` and
`--prepare-suite-each`. The suite-level helper selects preparation based on
the requested `SPREAD_SYSTEM`: classic systems use `prepare_classic`, while
Ubuntu Core targets use `prepare_ubuntu_core`. Per-task preparation and
restore then establish and reset the test baseline.

### Classic snapd systems

For classic Ubuntu and other supported classic distributions,
`prepare_classic` configures proxy access and installs the snapd package built
from this checkout, unless `REUSE_SNAPD=1`. It validates the package version,
builds and installs the local snapd snap with `snap install --dangerous`, and
waits for seeding. The local snapd snap is part of the test setup so tests
exercise snapd re-execution; the Core snap is also installed or refreshed using
`CORE_CHANNEL`.

The helper configures snapd service/re-exec behavior, disables automatic
refreshes, caches commonly used snaps, and saves an initial snapd state.
`prepare-suite-each` and `restore-suite-each` record test state, remove
packages installed by a classic test, and reset snapd to the saved baseline.
Suite restore removes the test snapd package. These actions are implemented
in [`prepare.sh`](../../../tests/lib/prepare.sh) and
[`prepare-restore.sh`](../../../tests/lib/prepare-restore.sh); do not assume
every suite runs all of them.

### Ubuntu Core systems

`tests/core/` selects `ubuntu-core-*` systems, and other suites may also run on
Core when their system filters allow it. Preparation detects the target from
`SPREAD_SYSTEM`, because an image can initially boot into a classic Ubuntu
environment before it is converted to Core.

If `/var/lib/dpkg/status` exists, `prepare_ubuntu_core` invokes
`setup_reflash_magic` and reboots into an all-snap Core image. It then waits
for snapd and seeding, verifies the fundamental snaps (and the snapd snap on
Core 18 and later), ensures test tooling such as `rsync` is available, caches
test snaps, disables refreshes, applies service settings, and saves the
initial snapd state. After a reflash, the helper verifies that the classic
dpkg database is gone. Later test setup and restore must use Core-aware
helpers; do not assume classic package-management or filesystem behavior.

### Nested systems

For `tests/nested/`, distinguish the outer Spread host from the guest VM under
test. The selected backend allocates the outer host; the nested suite
configuration in [`spread.yaml`](../../../spread.yaml) prepares that host
with dependencies, `ubuntu-image`, the checkout's snapd DEB, and a remote
connection. It then uses the `tests.nested` tool and helpers under
[`tests/lib/nested/`](../../../tests/lib/nested) to build and boot the guest.

- A nested **Core** guest is generated with `ubuntu-image`, a model, and
  configured Core/kernel/gadget channels. By default,
  `NESTED_BUILD_SNAPD_FROM_CURRENT=true` repacks snapd and the required
  kernel/gadget/base snaps from the current tree. The image is configured for
  test access (cloud-init or assertions), then booted and given the test
  user/tools.
- A nested **classic** guest starts from a downloaded Ubuntu cloud image and
  is configured with cloud-init for SSH and test access.
- The `tests/nested/core/` and `tests/nested/classic/` suites build the image
  during suite preparation and create/remove a guest for each task.
  `tests/nested/manual/` leaves image and VM lifecycle to each test.

The outer host's `-debug` shell or `spread exec` session is not the nested guest
shell. Before `restore-each` removes the guest, collect guest-side evidence
such as its serial log, VM status, or service state. The nested lifecycle and
image logic is in [`tests/lib/tools/tests.nested`](../../../tests/lib/tools/tests.nested),
[`tests/lib/nested/image.sh`](../../../tests/lib/nested/image.sh), and
[`tests/lib/nested/lifecycle.sh`](../../../tests/lib/nested/lifecycle.sh).

## Shared test helpers: `tests/lib` and `tests/lib/tools`

`spread.yaml` sets `TESTSLIB` to `tests/lib` and `TESTSTOOLS` to
`tests/lib/tools`; it also puts `tests/bin` on `PATH`. Use these variables
instead of hard-coding checkout paths.

- `tests/lib` contains shared shell scripts and test assets. Source a shell
  helper when a test needs its functions (for example, `prepare.sh`,
  `pkgdb.sh`, or `systems.sh`), and execute lifecycle scripts such as
  `prepare-restore.sh` as configured by the suite. Check existing helpers
  before adding duplicate setup, state, or cleanup logic.
- `tests/lib/tools` is a collection of utility commands, separate from the
  shell libraries. Invoke a command directly, for example
  `"$TESTSTOOLS"/tests.invariant check` or
  `"$TESTSTOOLS"/journal-state get-log -u snapd`. Tools on `PATH`—often via
  symlinks in `tests/bin`—can also be invoked by their command name. Consult
  the command's `-h` output and implementation before relying on its behavior.
- During project preparation, `spread.yaml` copies tools and remote helpers
  from `tests/lib/external/snapd-testing-tools/` into `TESTSTOOLS`. When
  tracing a command, check whether its maintained source is in that external
  subtree or directly in `tests/lib/tools`.

The [tools README](../../../tests/lib/tools/README) describes command naming
and usage conventions. The source of truth for helper loading and staging is
[`spread.yaml`](../../../spread.yaml); examples of shared shell setup are in
[`prepare-restore.sh`](../../../tests/lib/prepare-restore.sh), and
[`tests/lib/tools/`](../../../tests/lib/tools) contains commands for package,
systemd, invariant, journal, and nested-VM operations.

## Prerequisites

For local or branch snapd product-code changes, let `run-spread` build the snap
from the current checkout. For a CI job that used a prebuilt master snap, use
`./run-spread -d -- <Spread args>` to download a prebuilt snap. Use
`NO_REBUILD=1` only when reusing a verified matching local artifact.

## Quick reference

```bash
# Report-based failure (download prebuilt snapd from master)
./run-spread -d -- -debug-socket garden:ubuntu-24.04-64:tests/main/snap-mgmt

# Failure tied to local/branch changes (build current checkout)
./run-spread -debug-socket garden:ubuntu-24.04-64:tests/main/snap-mgmt

# Reuse a verified matching local snap
NO_REBUILD=1 ./run-spread -debug-socket garden:ubuntu-24.04-64:tests/main/snap-mgmt

# Reuse VMs for faster iteration with a verified matching local snap
NO_REBUILD=1 ./run-spread -reuse -debug-socket garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

Always specify `<backend>:<system>:<test-path>` — bare `./run-spread` runs everything.

## Flags

- `-debug-socket`: Pause on failure for remote commands through `spread exec`;
  use for agent-driven or headless debugging.
- `-debug`: Open an interactive SSH shell on failure when a terminal is
  available.
- `-reuse`: Keep VMs alive between runs (faster iteration).

## Backend and system

In this snapd workflow, `garden` runs local QEMU VMs and is the normal
developer backend. `openstack-dev` uses the staging OpenStack project. If
Garden cannot provision a worker and the user authorizes a cloud attempt, use
the backend they explicitly name; if they do not name one, use `openstack-dev`
with the same system and test, and record that it is staging. The `openstack`
backend uses CI credentials and requires explicit user authorization. Garden
and OpenStack provide similar test systems, but they are not identical
environments, so a Garden pass does not by itself prove the test passes in CI.

Other configured backends may be needed for specialized suites, such as
nested-VM tests. Follow that suite's configuration in `spread.yaml`; do not
silently replace its required backend.

System selection:
- AppArmor changes → recent Ubuntu LTS, optionally Debian
- SELinux changes → openSUSE SELinux or Fedora
- Systemd changes → recent Ubuntu + Fedora
- General → most recent Ubuntu LTS

Discover system names from `spread.yaml` under `backends:garden:systems:`. Don't hardcode — they change.

## Suites that don't need the snap

Use `spread` directly (not `run-spread`) for:
- `tests/unit/` — unit tests, including non-Go tests and tests using different
  Go runtimes
- `tests/cross/` — cross-compilation tests

```bash
spread garden:ubuntu-24.04-64:tests/unit/go
```

Use `run-spread` for suites that test snapd on a system with a built or
downloaded snap. Common suites include:

| Test path | What it covers |
|---|---|
| `tests/smoke/` | Essential system-level checks |
| `tests/main/` | Full-system snapd tests; these can modify the test system |
| `tests/regression/` | Tests for previously reported regressions |
| `tests/core/` | Ubuntu Core-specific tests |
| `tests/nested/` | Tests that build or control a nested image or VM |

These are orientation, not a complete list; `spread.yaml` is authoritative for
suite configuration, supported systems, and backends. In particular, nested
suites may require a backend configured for nested virtualization. Do not
assume every suite can run with the default backend.

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
Debug socket session ready. Use `spread exec -- <command>` to run commands,
then `spread stop` to continue.
```

Capture as much of the error and debug output as possible to avoid re-runs.

## Reproduce before changing code

Before proposing or applying a fix, run the failing test without modifying the
test or product code. Use the same system and relevant environment as the
reported failure when available. For an OpenStack CI failure, reproduce locally
on `garden` with the same system and test; this is the expected local backend
substitution, and must be recorded as a condition difference. This baseline
run must pause on failure: use `-debug-socket` for remote inspection or
`-debug` for an interactive terminal.

Record whether the run reproduces the same failed phase, command, exit status,
and symptom. Keep the debug session paused and collect evidence when it
reproduces; for remote inspection, use `spread exec -- COMMAND` and resume with
`spread stop`.
Do not edit code based only on the original CI log when a local reproduction is
available.

Also record the conditions needed to repeat the baseline: commit and local
changes, backend and system, image or snap revisions, built or downloaded snapd
artifact, relevant environment variables, Spread flags, test arguments, and
whether the VM was fresh or reused. These conditions define the verification
run.

For the first reproduction invocation, use `-repeat 3` on a fresh worker to
check for an intermittent failure. If the CI job used a prebuilt master snap,
use `./run-spread -d -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH`; omit `-d`
when the checked-out local or branch source change must be built. Do not claim
a fix from a single passing run if the original failure was intermittent.

If a run fails for a clearly unrelated transient problem before reaching the
behavior under test (for example, a store download stalls or a worker fails to
boot), retry the same command once on a fresh worker with the same selector,
artifact mode, and inputs. Record the original error and retry result. If the
same unrelated problem happens again, stop and report the blocker rather than
looping; do not treat it as a reproduced test failure.

If reproduction is blocked by allocation, image download, credentials,
external service availability, or another infrastructure problem, stop before
patching unless the existing evidence independently proves the root cause.
Report the blocker and distinguish any proposed change as unverified. Never
describe a patch as fixing the issue unless the original failure was reproduced
and the same test subsequently passed with the patch.

## Debug a failure

Keep the Spread process running when it pauses on failure. For `-debug`, the
interactive shell belongs to that terminal session. For `-debug-socket`, use
`spread exec -- COMMAND` from the project directory to inspect the failed task,
then run `spread stop` to resume the run. Do not start another Spread process
or stop the paused run before collecting evidence.

For agent-driven or headless remote debugging, start Spread with
`-debug-socket`. When a task fails, Spread pauses and creates `.spread.socket`
in the project directory. Keep the Spread process running and issue focused
diagnostics from that directory with `spread exec -- 'COMMAND'`. For example:

For example, through snapd's wrapper:

```bash
./run-spread -d -- -debug-socket BACKEND:SYSTEM:TEST_PATH
spread exec -- 'journalctl -u snapd --no-pager -n 100'
spread exec -- 'pwd'
spread stop
```

Each `spread exec` uses a separate SSH command in the failed task's working
directory and environment, and returns output and exit status directly; it
does not share process state with the task. Use `spread stop` after collecting
evidence so Spread can continue with restore and cleanup. The socket is local
to the project directory and exists only while a debug-socket session is
active. Use `-debug` for a human-operated interactive shell, or `-shell` when
an interactive shell is needed before task execution; `-debug-socket` pauses
only after a task error.

1. Read the complete failed command and its output before running commands.
2. Identify the failed phase and the exact assertion or command that failed.
3. Use `spread exec` or the interactive shell to test the smallest plausible
   root-cause hypothesis.
4. Collect enough state to distinguish a product bug, test bug, dependency
   failure, image problem, or infrastructure issue.
5. Resume the run with `spread stop` or exit the interactive shell only after
   the evidence needed for a proposed solution has been captured. Spread will
   then continue with restore and cleanup.

Use the failed phase to focus the investigation:

| Phase or symptom | First checks |
|---|---|
| `prepare` fails | Check the selected system/backend, required packages, image setup, and test prerequisites. The test body may not have run. |
| `execute` fails | Find the exact command or assertion in the test's `task.yaml` and helpers. Compare its expected result with the live system state and relevant service logs. |
| `restore` fails | Inspect the test's cleanup and restore helpers. Treat leftover state as a possible cause of later failures. |
| Allocation, download, DNS, or external service fails | Confirm whether execution reached the behavior under test. Record an infrastructure/dependency blocker instead of changing an unrelated assertion or masking it with retries. |

Start with evidence tied to the failure. For example, inspect the relevant
service journal for a service assertion, package state for a dependency
failure, or process output for a command failure. Avoid broad diagnostic dumps
unless the focused checks leave a concrete question unanswered.

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

- Always pause on failure: use `-debug-socket` for remote inspection or
  `-debug` when an interactive terminal shell is needed.
- Reproduce the original failure before editing or claiming a fix
- Verify the fix under the same recorded conditions as the reproduction
- Use `-d` when the CI job used a prebuilt master snap; omit it when reproducing a local/branch source change
- Use `NO_REBUILD=1` only for a verified matching local artifact
- Never run `./run-spread` without a test path
- Retry once on a fresh worker after a clearly unrelated transient failure; stop if it repeats
- Use `garden` for normal local tests; for an authorized cloud attempt, use the backend the user names, or `openstack-dev` if they do not name one
- Do not use the production `openstack` backend without explicit user authorization
- Max 4 parallel subagent tests (RAM)
- When unsure if suite needs snap, check the suite and task configuration before running
- Verify `built-snap/snapd_*.snap.keep` exists and matches the intended source before using `NO_REBUILD=1`
