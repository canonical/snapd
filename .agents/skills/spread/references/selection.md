# Spread test selection and CI scenarios

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
and environment before cleanup continues. For headless remote debugging, use
`-debug-socket`; follow [the shared debug-socket procedure](../references/common-procedures.md#debug-socket-inspection)
to inspect the worker and resume the run. Task `debug` and `debug-each` scripts
are separate and can emit diagnostics on failure.

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

- `-debug-socket`: Pause on failure for remote commands through `spread -exec`;
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

## spread.yaml structure

Key sections: `environment:` (global vars), `backends:` (garden/openstack/qemu configs), `backends:<backend>:systems:` (available systems), `suites:` (test suite configs). Individual tests can override settings in their `task.yaml`.

