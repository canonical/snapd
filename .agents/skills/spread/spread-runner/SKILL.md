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
Use [the shared procedures](../references/common-procedures.md) for recording
run conditions and handling debug sockets.

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

## References

Load only the reference needed for the current task:

- [Selection, scenarios, backends, and suites](../references/selection.md)
- [System preparation and shared test helpers](../references/system-preparation.md)
- [Failure triage and verification](../references/failure-triage.md)
- [Shared run-context and debug-socket procedures](../references/common-procedures.md)
