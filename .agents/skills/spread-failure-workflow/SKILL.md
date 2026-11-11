---
name: spread-failure-workflow
description: Run, reproduce, diagnose, fix, and verify a failing snapd Spread test end to end. Use when given a BACKEND:SYSTEM:TEST_PATH and optionally GitHub Actions job URL(s).
argument-hint: "BACKEND:SYSTEM:TEST_PATH [GitHub job URL(s)]"
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
- `spread-reproducer` owns CI job evidence and reproduction handoffs.
- `spread-fixer` owns focused fixes to Spread tests and their helper code.

Follow their detailed procedures at the corresponding stage. Do not replace
their safety rules or duplicate their detailed commands here.

## Allowed actions and CI access boundary

This skill may inspect and change the local checkout as required to reproduce
and fix the failure, and may run local tests using `spread-runner`. It may read
existing CI evidence only from user-pasted logs or by executing the bundled
[CI log fetcher](../../../tests/utils/spread-log/fetch_spread_ci_logs.py). Do not
use `gh`, `curl`, browser/network tools, or another script for CI access. Do
not trigger CI, write to GitHub, or upload CI data.

## Workflow session directory

From the snapd repository root, create one unique session directory in the
project's ignored `.logs` directory at the start of the workflow, for example:

```bash
PROJECT_ROOT="$(git rev-parse --show-toplevel)"
mkdir -p "$PROJECT_ROOT/.logs"
SESSION_DIR="$(mktemp -d "$PROJECT_ROOT/.logs/spread-failure-workflow-XXXXXX")"
printf '%s\n' "$SESSION_DIR"
```

Keep its absolute path for the entire workflow. Store fetched logs, result
artifacts, run metadata, analysis notes, attempt evidence, and the default
reproduction handoff in this directory (the fetcher may create unique
subdirectories beneath it). Pass `--session-dir "$SESSION_DIR"` to every
fetcher invocation. Keep all workflow logs, artifacts, notes, and handoffs
inside this session directory; do not use `/tmp` or create another temporary
directory for another stage. If the user supplies an explicit handoff path,
honor it. Do not remove the session directory while the workflow is running;
include its path in the final report.

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

- one or more GitHub Actions job URLs for the failing test
- a pasted GitHub job log, with its repository, run ID, and job ID if available

Use a supplied source to understand the failure and find relevant executions.
The system and test in the selector must match the intended target. Normally,
local runs use the `garden` backend, which provisions local QEMU VMs. If Garden
does not reach the behavior under test and the user explicitly requests
`openstack`, use the configured `openstack` backend with the same system and
test. Otherwise, a user-authorized cloud attempt uses configured
`openstack-dev`; record that it uses the staging project. Never switch to
production `openstack` without explicit user authorization or trigger a new CI
run unless the user explicitly requests it. For other backend or system
mismatches, explain the difference and ask which target to investigate instead
of silently switching.
If the selector is incomplete or ambiguous, ask for clarification.

## 1. Collect and summarize prior CI evidence

If direct GitHub Actions job URLs are supplied, retrieve no more than the
three most recent failures and their available Spread result artifacts in one
fetcher invocation. When the supplied URLs are chronological, use the final
three; if their order is unclear, ask which failures are the latest instead
of downloading all of them.
The fetcher stores this invocation's files in a unique subdirectory beneath
the workflow session directory and prints its path:

```bash
python3 tests/utils/spread-log/fetch_spread_ci_logs.py \
    --session-dir "$SESSION_DIR" \
    --job-url 'JOB_URL_1' 'JOB_URL_2'
```

Use that output directory when inspecting the logs and result artifacts. Match
result rows to the exact backend, system, test, phase, and variant. A direct
job log establishes only that job's evidence. Do not search related workflow
runs unless the user asks for that broader investigation.

If no CI source is supplied, record the user's failure description and
continue with a local baseline. Do not invent CI history.

Before any local test run, record the current commit and dirty state, the
selected test, and the available CI source evidence. Do not clean, switch, or
overwrite a dirty user checkout. Redact credentials and tokens in notes and
final output.

## 2. Run the baseline and reproduce

Use `spread-runner` to check suite requirements and understand how the selected
snapd, Ubuntu Core, or nested system is prepared before running the supplied
system and test with a failure pause enabled. Prefer `-debug-socket` for remote
agent-driven inspection; use `-debug` only when an interactive terminal shell
is needed. Use Garden locally, including when
reproducing an OpenStack CI failure, and preserve relevant test arguments and
environment from the CI evidence. For nested tests, distinguish the outer
Spread host from the guest under test. Record Garden-versus-OpenStack as a
condition difference; do not claim the environments are identical.

Choose the snapd artifact from the supplied job's workflow inputs and logs. If
the job uses a prebuilt master snap, use `./run-spread -d -- <Spread args>` and
record that `-d` downloads the latest successful master build, which may differ
from the job's artifact. If the job builds snapd from its checked-out source,
use a worktree at that commit and omit `-d`. Use `NO_REBUILD=1` only when the
existing local artifact is verified to match the intended source.

For CI scenarios (`generic`, `deb`, `master`, or `features`), preserve the
caller workflow's `spread-env`, package artifacts, and snap artifact
selection. Use the scenario guide in `spread-runner`; the workflow's
`scenario` value is a reporting label, not an environment variable to export
locally.

Create the default reproduction handoff at
`$SESSION_DIR/reproduction-report.md` before repeated or slow attempts, unless
the user provides another path. Follow `spread-reproducer` for CI job evidence,
bounded intermittent retries, and predecessor contamination analysis. Use
fresh or reused machines consistently with the baseline conditions. Start the
first reproduction invocation with `-repeat 3` on a fresh worker, preserving
the selected variant and artifact source.

Reproduction must match the original failure's phase, failed command, exit
status, and symptom. Keep the debug session paused on a matching failure and
collect the minimal evidence needed to distinguish a test defect from a
product, image, dependency, or infrastructure problem. For remote inspection,
use `spread exec -- COMMAND` and resume with `spread stop`.

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
