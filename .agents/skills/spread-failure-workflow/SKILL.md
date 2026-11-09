---
name: spread-failure-workflow
description: Run, reproduce, diagnose, fix, and verify a failing snapd Spread test end to end. Use when given a BACKEND:SYSTEM:TEST_PATH and optionally a GitHub Actions job log or nightly test report URL.
argument-hint: "BACKEND:SYSTEM:TEST_PATH [GitHub run/job URL or report URL]"
metadata:
  project: snapd
  task-type: spread-failure-resolution
  execution-time: slow
---

# Resolve a failing Spread test

Run this workflow when the user wants a failing snapd Spread test investigated
end to end: run the specified test, reproduce the failure, fix its root cause,
and run it again to verify the result.
When the user asks whether a reported failure can be fixed or says to let this
workflow handle it, treat that as a request to continue through implementation
and verification once the evidence supports a safe, focused fix. Do not stop
after diagnosis to ask whether to implement; ask only when the intended behavior
or responsible layer remains genuinely ambiguous.

This skill coordinates three specialized skills:

- `spread-runner` owns test execution and live debug-shell mechanics.
- `spread-reproducer` owns nightly test predictor reports, CI evidence, and
  reproduction handoffs.
- `spread-fixer` owns focused fixes to Spread tests and their helper code.

Follow their detailed procedures at the corresponding stage. Do not replace
their safety rules or duplicate their detailed commands here.

## Inputs

Require a test selector in this form:

```text
BACKEND:SYSTEM:TEST_PATH
```

For example:

```text
garden:ubuntu-24.04-64:tests/main/snap-mgmt
```

An optional second input can be:

- a GitHub Actions run or job URL for the failing test
- a pasted GitHub job log, with its repository, run ID, and job ID if available
- a testing-report run URL or ID, such as a `canonical/snapd-testing-jobs`
  nightly predictor report

Use a supplied source to understand the failure and find relevant executions.
The system and test in the selector must match the intended target. Normally,
local runs use the `garden` backend, which provisions local QEMU VMs; `openstack`
is reserved for CI. If Garden fails before the test starts and the user
authorizes a cloud attempt, use configured `openstack-dev` with the same system
and test, and record that it uses the staging project. Do not use production
`openstack` as an unapproved fallback or trigger a new CI run unless the user
explicitly requests it. Reading existing CI reports and job logs for evidence
is allowed. For other backend or system mismatches, explain the difference and
ask which target to investigate instead of silently switching. If the selector
is incomplete or ambiguous, ask for clarification.

## 1. Collect and summarize prior CI evidence

If a nightly predictor report URL or ID is supplied, use
`spread-reproducer` to inspect the report and its structured test history.
Select entries matching the supplied test, backend, and system. Inspect the
relevant failing and flaky rows, phases, variants, run/job links, and available
`results.json` artifacts. For each linked failed execution, use the
parenthesized machine name in its job log to reconstruct the ordered test
sequence on that same machine before the failure; correlate with result
artifacts when available. Report how many matching failed and successful
executions were found and what conditions they used. Do not conflate prepare,
execute, and restore failures.

If a direct GitHub Actions run or job URL/log is supplied, use `gh` to inspect
the owning run, job metadata, log, and available Spread result artifacts.
Match result rows to the exact backend, system, test, phase, and variant.
A direct job log establishes only that job's evidence. Search related workflow
runs only when the workflow and test inputs can be matched reliably; do not
claim an exhaustive history unless the report or artifacts provide it. Never
trigger a CI workflow unless the user explicitly asks.

If no CI source is supplied, record the user's failure description and
continue with a local baseline. Do not invent CI history.

Before any local test run, record the current commit and dirty state, the
selected test, and the available CI source evidence. Do not clean, switch, or
overwrite a dirty user checkout. Redact credentials and tokens in notes and
final output.

## 2. Run the baseline and reproduce

Use `spread-runner` to check suite requirements and understand how the selected
snapd, Ubuntu Core, or nested system is prepared before running the supplied
system and test with debug enabled. Use Garden locally, including when
reproducing an OpenStack CI failure, and preserve relevant test arguments and
environment from the CI evidence. For nested tests, distinguish the outer
Spread host from the guest under test. Record Garden-versus-OpenStack as a
condition difference; do not claim the environments are identical.

Choose the snapd artifact from the failure source: for a report-based failure,
use `./run-spread -d -- <Spread args>` to download a prebuilt snap; for a GitHub
job tied to a local or branch source change, omit `-d` so the current checkout is built.
Record the artifact's source and revision. `-d` downloads the latest successful
master build and may not match the report's source commit. Use
`NO_REBUILD=1` only when the existing local artifact is verified to match the
intended source.

For CI scenarios (`generic`, `deb`, `master`, or `features`), preserve the
caller workflow's `spread-env`, package artifacts, and snap artifact
selection. Use the scenario guide in `spread-runner`; the workflow's
`scenario` value is a reporting label, not an environment variable to export
locally.

Create a reproduction handoff under
`.agents/reproduction-reports/<UTC-date>-<test-slug>.md` before repeated or
slow attempts, unless the user provides another path. Follow
`spread-reproducer` for report-based evidence, bounded intermittent retries,
and predecessor contamination analysis. Use fresh or reused machines
consistently with the baseline conditions. Start the first reproduction
invocation with `-repeat 3` on a fresh worker, preserving the selected variant
and artifact source.

Reproduction must match the original failure's phase, failed command, exit
status, and symptom. Keep the debug shell open on a matching failure and
collect the minimal evidence needed to distinguish a test defect from a
product, image, dependency, or infrastructure problem.

If the failure does not reproduce, or execution is blocked before reaching the
behavior under test, do not patch speculatively. Record attempts and blockers
in the handoff and report the result as inconclusive or blocked. For a clearly
unrelated transient failure before the behavior under test, retry the same
command once on a fresh worker before reporting it as blocked; if the same
blocker recurs, stop rather than looping.

## 3. Fix the diagnosed cause

Use `spread-fixer` when the evidence identifies a defect in the test,
`task.yaml`, helper scripts, or cleanup/restore logic. Read and preserve the
reproduction handoff, make the smallest change that retains meaningful test
coverage, and update the handoff with the diagnosis and patch.
When the original failure reproduces and the evidence identifies a safe,
focused test defect, implement the fix and verify it in this workflow; do not
stop after diagnosis to ask whether to proceed. If the failure is not
reproduced or the responsible behavior is ambiguous, do not patch speculatively.

If the evidence identifies a snapd product defect, fix the owning product code
and add or retain focused regression coverage; do not weaken the test to hide
the product failure. For image, external dependency, or infrastructure
failures, do not make an unrelated test change. Report the responsible layer
and next action instead.

Never turn a deterministic failure into a pass by adding unbounded retries,
arbitrary sleeps, or broad skips.

## 4. Rerun and verify

After the fix, first run the cheapest focused check, then use `spread-runner`
to rerun the same `BACKEND:SYSTEM:TEST_PATH` under the recorded conditions.
Preserve the same backend, system, relevant environment, flags, artifact
source, and fresh/reused machine requirements. If a necessary condition
changed, record it and do not describe the result as an exact comparison.

When a fix was made for a non-generic CI scenario, run an additional
compatibility check for the same test and variant in a generic scenario after
the original-condition verification passes. Select a real generic workflow
caller and mirror its environment, package artifacts, and snap artifact
selection; the `generic` label alone does not guarantee that no environment
variables are set. Use the same backend and system when supported. This check
is additional and must not replace verification in the scenario that failed.
If no generic caller supports the selector, do not silently change the backend
or system; record the limitation and ask before substituting another target.
Include the generic command and result in the handoff.

If the same failure remains, return to diagnosis and iterate. Do not claim a
fix from an unrelated passing run, a different failure signature, or a run
that did not reach the behavior under test.

Update the handoff with the baseline, root cause, files changed, focused-check
result, exact verification command and result, and any condition differences.
Include a suggested commit title and the complete commit message in the
handoff when a code fix is made. Do not commit changes or the handoff unless
requested.

## 5. Report the outcome

Summarize:

- the selected backend, system, and test
- the supplied CI source and matching execution history, including limits
- baseline and reproduction result
- diagnosed cause and changes made, or why no safe fix was made
- the exact verification command and result
- the reproduction handoff path
- the suggested commit title and complete commit message, when a code fix was
  made

Clearly separate verified facts from hypotheses. State explicitly if the
failure was not reproduced, the fix was not verified, or the available CI
source did not provide exhaustive execution history.
