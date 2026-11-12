---
name: spread-update-workflow
description: Update, migrate, or adapt a snapd Spread test from a requested specification, then verify the resulting behavior. Use for planned test changes such as moving a task to a new Ubuntu release, changing its matrix, refreshing expected fixtures, or adopting a new test requirement; use spread-runner for execution and verification without requiring an existing failure or reproduction report.
argument-hint: "[specification] [BACKEND:SYSTEM:TEST_PATH]"
metadata:
  project: snapd
  task-type: spread-test-update
  execution-time: slow
---

# Update a Spread test

Use this workflow for intentional changes to a snapd Spread test. The input is
a requested behavior, migration, or acceptance specification, not a reported
failure. Unlike `spread-failure-workflow`, this workflow does not require the
old test to fail, an existing failure signature, or a reproduction report.

Use `spread-runner` for Spread command mechanics, backend and system selection,
debug-shell handling, artifact collection, and verification. Use
`spread-failure-workflow` instead when the primary goal is to diagnose an
unknown failure before deciding what should change.

## 1. Establish the specification

Turn the request into explicit acceptance criteria before editing. Record:

- the test or suite being changed
- the intended backend, system, variants, and scenario
- behavior or coverage that must remain unchanged
- expected files, generated data, or configuration that may need updating
- any supplied CI run, image, package, or snap artifact requirements

Inspect the owning `task.yaml`, relevant `spread.yaml` entries, nearby test
assets, and directly used helpers. Confirm that the target system exists for
the intended backend. If the specification leaves behavior materially
ambiguous, ask for clarification; do not infer acceptance criteria from the
current implementation alone.

Record the current commit and dirty state. Do not clean, switch, overwrite, or
revert unrelated user changes.

## 2. Decide whether a baseline is useful

Run the original selector only when its result provides a useful comparison,
such as preserving behavior across a system migration. A passing baseline is
not a prerequisite for making the requested update. If the old system is no
longer available or the update intentionally introduces new coverage, record
that limitation and continue from the specification.

Use `spread-runner` for any baseline command. Preserve relevant backend,
system, variant, scenario environment, and artifact source so the old and new
runs can be compared honestly.

Choose the snapd artifact based on the code under test. For changes limited to
Spread task metadata, scripts, helpers, or fixtures, use `run-spread -d` to
avoid rebuilding snapd unless the specification requires a particular local
snapd revision. Omit `-d` only when snapd product-code changes from the current
checkout must be exercised. Record the downloaded or built revision when it
can affect expected output.

## 3. Implement the smallest complete update

Change only the task, assets, helpers, and configuration needed to satisfy the
acceptance criteria. Preserve meaningful assertions and cleanup. Do not make a
planned migration pass through broad skips, weakened checks, arbitrary sleeps,
or unrelated compatibility changes.

For a system or image migration, explicitly review:

- task `systems` and `backends` filters
- whether the target is configured by the selected backend in `spread.yaml`
- release-specific setup, package assumptions, comments, and workarounds
- fixture and artifact paths derived from `SPREAD_BACKEND` and `SPREAD_SYSTEM`
- base snaps, confinement features, services, filesystem layout, and tools
  whose behavior may differ on the target image
- cleanup and reboot behavior for every task variant

Treat expected-output updates as reviewed test changes, not generated truth.
Never copy target output into fixtures solely to make a diff pass. Explain why
each semantic difference is expected from the specification, target image, or
intentional product behavior. If a difference suggests a snapd defect, image
defect, leaked state, or unstable normalization, diagnose that issue instead
of blessing it.

## 4. Validate incrementally

After the first substantive edit, run the cheapest focused syntax, lint, or
selection check that can reject the change. Then use `spread-runner` to run the
new selector under the specified conditions.

When fixtures must be created or refreshed:

1. Run the updated test on the target with `-shell` when an interactive shell
  is needed in place of the execute step to inspect or prepare test state
  proactively. Use `-debug-socket` for remote inspection when execute should
  run normally and pause only after a failure, preserving generated output and
  failed state. Use the socket path from Spread's ready message with
  `spread exec SOCKET -- COMMAND` to inspect and `spread stop SOCKET` to
  resume; use `-debug` when a human-operated terminal shell is needed.
2. Inspect every difference against the old fixture and acceptance criteria.
3. Add or update fixtures only after the differences are understood.
4. Rerun the same selector from a fresh worker when image state or generated
   output can vary.
5. Run all task variants that select the changed fixture, including reboot or
   environment variants.

An initial missing-fixture or expected-diff failure is evidence collection,
not a reproduced defect. Keep its output, but do not route it through
`spread-fixer` unless investigation reveals an actual test defect.

If the target is CI-only and local verification uses another backend, report
the backend difference explicitly. A passing substitute backend does not prove
that backend-specific fixtures or behavior pass on the requested target.

### Diagnose, correct, and rerun failures

When a validation or Spread run fails, do not stop at reporting the failure
while an evidence-based next action is available:

1. Identify the failed phase, command, exit status, and first relevant error.
2. Keep the Spread debug session paused and inspect the failed worker's live
  state, generated files, logs, and commands needed to explain the failure.
  For remote inspection, use the socket path from Spread's ready message with
  `spread exec SOCKET -- COMMAND` and resume with `spread stop SOCKET`.
  If diagnosis must happen before execute, rerun with `-shell` and perform the
  execute commands selectively. Use downloaded artifacts when a live shell is
  unavailable or persistent evidence is specifically required.
3. Classify it as an expected migration difference, test defect, snapd product
  defect, image or dependency problem, infrastructure failure, or unrelated
  transient failure.
4. Make the smallest correction supported by that evidence. Do not change the
  test to hide product, image, or infrastructure defects.
5. Run the cheapest focused check again, then rerun the same selector and
  conditions that failed.

Continue this loop until the acceptance criteria pass or a concrete blocker
prevents further progress. Retry an unrelated transient or infrastructure
failure once on a fresh worker. If the same blocker recurs, stop rather than
looping indefinitely and report the evidence and required next action. When a
failure changes after an edit, diagnose the new signature independently; do
not treat a different failure as proof that the update is correct.

## 5. Verify the specification and report

Rerun the final target selector using the same backend, system, variants,
scenario environment, and artifact source required by the specification. For
shared helpers or configuration, run the narrowest additional affected tests.

Report:

- the acceptance criteria used
- files changed and the reason for each
- baseline result, when one was useful
- focused checks and exact Spread commands with results
- fixture differences reviewed and why they are expected
- any backend, image, scenario, or artifact differences from the requested
  conditions
- unverified criteria or remaining CI confirmation

Do not claim the update is verified when the requested target did not run, a
variant was omitted, generated differences remain unexplained, or execution
failed before reaching the changed behavior.