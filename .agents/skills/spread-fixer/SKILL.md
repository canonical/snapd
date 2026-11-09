---
name: spread-fixer
description: Fix a reproduced defect in a snapd Spread test, task.yaml, helper, or cleanup logic. Use standalone with a reproduction report or as the fix stage of spread-failure-workflow; rely on spread-runner for execution and verification.
argument-hint: "[reproduction report path] [test path]"
metadata:
  project: snapd
  task-type: test-fix
  execution-time: slow
---

# Fix a Spread test

Use this workflow when evidence shows that a snapd Spread test or its helper
code is defective. The input should be a reproduction handoff created by
`spread-reproducer`, or equivalent evidence that records the original
failure and conditions.

Use `spread-runner` for Spread command mechanics, debug-shell handling, and
verification. This skill owns the test-specific diagnosis and change. Do not
repeat the report-selection or predecessor-correlation work from
`spread-reproducer`. For an end-to-end run, reproduce, fix, and verify flow,
use `spread-failure-workflow`.

## 1. Confirm the handoff and scope

Read the complete reproduction report before changing code. Confirm:

- the reported failure signature and phase
- the exact command and conditions that reproduce it
- the report status and reproduction confidence
- the owning test, task, helper, or cleanup code

If the handoff is missing or does not include an executable reproduction,
ask for it or use `spread-reproducer` first. Do not infer a test defect
from a CI failure alone.

This skill is limited to defects in Spread tests and their support code, such
as incorrect assertions, timing assumptions, feature detection, prerequisites,
or incomplete cleanup/restore. If evidence instead points to a snapd product
defect, image defect, external dependency, or infrastructure failure, do not
change the test to hide it. Report the evidence and route the issue to the
appropriate fix workflow.

## 2. Reproduce the baseline

Before editing, rerun the exact command from the handoff and preserve its
conditions: commit, test path, backend, system, image or snap revision,
environment, flags, artifact source, and fresh or reused machine state.

Follow `spread-runner` for building or obtaining the snap, selecting the
runner, using `-debug`, and collecting evidence. Do not change unrelated
conditions just to make a local run convenient. If the baseline cannot be
reproduced, stop before patching unless the report contains independent,
decisive evidence for the test defect; explain the limitation and keep any
proposed change explicitly unverified.

## 3. Diagnose and make the narrowest test fix

Inspect the failing test's `task.yaml`, scripts, and shared helpers, along
with the relevant debug-shell state and logs. Verify that the observed behavior
is a test defect, not merely a symptom of a product or environment problem.

Make the smallest change that corrects the test's expectation or behavior.
Preserve meaningful coverage: do not weaken or remove an assertion, add
unbounded retries or sleeps, or skip a test just to make it pass. For cleanup
defects, ensure the test restores only state it owns and that cleanup runs on
failure as well as success.

If the fix changes shared test helpers, identify other affected tests and run
the narrowest relevant checks for them as well.

## 4. Verify and update the handoff

First run the cheapest focused check for the changed code. Then rerun the exact
Spread command from the baseline under the same conditions, following
`spread-runner`. If the reproduction used a fresh machine, verify with a
fresh machine; if it used a reused machine, restore the relevant state or
reproduce the baseline before comparing results.

If the original report is from the CI-only `openstack` backend and the local
baseline used `garden`, rerun the same Garden command and conditions. Report
that as local verification, not as proof of success on OpenStack; CI
confirmation requires a separate CI run.

If the fix targets a non-generic CI scenario, also run the same test and
variant in a generic scenario after verifying the original conditions. Use a
generic caller's actual environment and artifact selection, and keep the
backend and system the same when supported. Treat this as an additional
compatibility check, not a substitute for the original-condition run. If no
generic caller supports the selector, document that limitation rather than
silently switching targets.

Update the reproduction report with:

- the test-defect evidence and root cause
- the changed files and the reason for each change
- focused-check and Spread commands with their results
- any differences from the baseline conditions
- whether the original failure signature is gone and whether cleanup passed

Do not claim the issue is fixed if the original conditions could not be
recreated or verification is inconclusive. Report those limits clearly.
