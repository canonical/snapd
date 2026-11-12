---
name: spread-reproducer
description: Investigate and reproduce intermittent snapd Spread failures from supplied GitHub Actions job URLs. Use standalone or as the evidence and reproduction stage of spread-failure-workflow.
argument-hint: "[GitHub Actions job URL(s)] [test path]"
metadata:
  project: snapd
  task-type: test-investigation
  execution-time: slow
---

# Reproduce a Spread failure from CI jobs

Use this workflow to investigate failures from supplied GitHub Actions job
URLs. Use the `spread-runner` skill for the mechanics and safety rules of
running and debugging Spread. This skill owns CI evidence collection,
reproduction attempts, and prior-test correlation; it does not replace the
shared runner guidance. If the evidence identifies a test defect, hand the
completed reproduction handoff to `spread-fixer` for the focused test change.
For an end-to-end run, reproduce, fix, and verify flow, use
`spread-failure-workflow`, which coordinates these stages.

## Inputs

Accept one or more GitHub Actions job URLs and, when needed, the test path to
investigate. If no job URLs are supplied, ask for them; do not query GitHub for
recent runs. If the supplied jobs contain failures for multiple tests and no
test path was specified, ask the user which test to investigate.

Record these fields before running anything:

- phase, backend, system, test path, and variant from available evidence
- scenario and snapd run IDs/job IDs from the supplied URLs

Use the phase shown by the job log and matching result rows. A preparing or
restoring failure is not equivalent to an executing failure.

## Allowed actions and CI access boundary

This skill may inspect the workspace, analyze the selected test and its
downloaded evidence, and run local reproduction commands through
`spread-runner`. It may read existing CI evidence only by executing the bundled
[CI log fetcher](../../../tests/utils/spread-log/fetch_spread_ci_logs.py). Do not use `gh`, `curl`,
browser/network tools, or another script for CI access. Do not trigger CI,
write to GitHub, or upload CI data. User-pasted logs may be read directly.

The fetcher requires `gh` authenticated for the relevant Canonical repository.
From the snapd repository root, create one unique directory in the project's
ignored `.logs` directory at the start of the workflow and reuse it for all
workflow outputs:

```bash
PROJECT_ROOT="$(git rev-parse --show-toplevel)"
mkdir -p "$PROJECT_ROOT/.logs"
SESSION_DIR="$(mktemp -d "$PROJECT_ROOT/.logs/spread-failure-workflow-XXXXXX")"
printf '%s\n' "$SESSION_DIR"
```

Pass `--session-dir "$SESSION_DIR"` on every fetcher invocation. Each
invocation writes logs, run metadata, and result artifacts to a unique
subdirectory beneath that shared directory. Keep analysis notes and the
default reproduction handoff in the same directory. Do not use `/tmp`, create
another temporary directory, or remove the session directory while the
workflow is using it.

## 1. Fetch CI job logs

If direct job URLs are supplied, fetch no more than the three most recent
failures together, including all available artifacts for those runs. When the
supplied URLs are chronological, use the final three; if their order is
unclear, ask which failures are the latest rather than downloading all of them:

```bash
python3 tests/utils/spread-log/fetch_spread_ci_logs.py \
    --session-dir "$SESSION_DIR" \
    --job-url 'JOB_URL_1' 'JOB_URL_2'
```

Read the job logs and downloaded run artifacts from the printed output
directory. Match result rows to the supplied backend, system, test, phase, and
variant. A job log establishes only that job's evidence; do not search related
workflow runs unless the user asks for that broader investigation.

## 2. Collect original failure evidence

For every distinct failed `(run_id, job_id)` pair:

1. Inspect the downloaded job log and run metadata.
2. Record the snapd commit, run attempt, workflow inputs, exact failed command,
   exit status, phase, symptom, and the machine name shown in parentheses on
   the log's task lines.
3. Inspect the downloaded `spread-logs-*` and `spread-results-RUN_ID-*`
   artifacts. Use debug logs for the failure details and inspect all
   `results.json` files, selecting rows matching the backend, system, phase,
   test, and variant. If artifacts or results are unavailable, record that
   limitation.
4. Confirm that separate failures have the same signature before combining
   them. Analyze differing signatures separately.

The original log establishes the failure signature. Use parenthesized machine
names in linked job logs to
reconstruct same-machine execution order, and cross-check it against
`results.json` where available.

## 3. Find tests run earlier on the same machine

Use the machine name shown in parentheses in the linked execution log to
identify the worker that ran the failed target. Build each timeline only
within that job attempt and exact machine name; do not combine machines
because they share a backend, system, run, or job, or because a name is reused
in a different attempt. When available, cross-check the log identity against
the `.instance` value in the corresponding `results.json`; do not assume the
two identifiers are interchangeable unless the artifact or log connects them.

For each matching failed target row:

1. Read the linked job log chronologically and select task entries carrying
   the identical parenthesized machine name as the failed target. Record each
   test path, phase, variant, start/completion order, and outcome. Exclude
   entries for other machines and non-test setup messages.
2. Locate the target failure and collect every earlier test task on that
   machine in the same attempt, in order. Record the immediate predecessor
   separately, but do not use it as a substitute for analyzing the complete
   predecessor list. Include each test's execute and restore outcomes.
3. Cross-check this timeline against result rows with the identical
   `.instance` and parsed `.start` values. Include only tasks that started
   before the target; retain file order as a tie-breaker. If the artifact is
   unavailable, use the log timeline and mark the analysis as log-derived.
4. Repeat for each matching failed target occurrence. Keep successful target
   occurrences as controls, using the same machine-correlation rules.

Compare every distinct test path that preceded the target across all failed
and successful target occurrences. For each path, report its occurrence count
and fraction among failed targets, the same count and fraction among
successful controls, and its observed positions in the ordered sequences.
Include phase, variant, execute/restore outcomes, and whether it was immediate
or farther back in the sequence. Do not stop at the immediate predecessor or
discard lower-ranked tests from the comparison. A test's presence before the
failure is a correlation, not proof that it caused the failure; retain every
predecessor in the comparison even if another predecessor is already a strong
candidate.

Rank candidates using:

1. higher frequency before failed targets and lower frequency before
   successful controls
2. repeated appearance across distinct failed occurrences, including its
   position or distance from the target
3. matching backend, system, scenario, and variant
4. predecessor execute or restore anomalies
5. repeated ordered groups of tests that precede failures but not controls

Do not rank ubiquitous setup tests highly merely because they precede every
task. When successful target occurrences are available, compare every
predecessor against them and report both `failures preceded / failures
inspected` and `successes preceded / successes inspected` for each candidate.

If artifacts have expired or lack `results.json`, extract executed test order
from the parenthesized machine names in each downloaded job log and clearly
label the ordering evidence as log-derived. Never infer same-machine order
from timestamps alone when machine identity is missing.

## 4. Reproduce the target alone

Recreate the original source revision and conditions before testing a
predecessor hypothesis. Do not switch or clean a dirty user checkout. Keep
implementation changes on the current branch; do not switch branches or commit
unless the user's initial request explicitly asks. If the job's commit differs
from the current checkout, a temporary Git worktree may be used for historical
reproduction only; do not apply the fix there.

Follow `spread-runner` and normally start with a fresh local `garden` QEMU VM,
`-debug-socket` for remote inspection, the same system, test, variant, relevant
environment, and artifact build method. If the user explicitly requests
`openstack`, or the selector specifies it, use that backend. Record backend
differences; a Garden reproduction is not an identical OpenStack environment.

If Garden fails before the test starts and the user authorizes a cloud run,
use the backend they name. If they do not name one, use `openstack-dev` when
configured and record that it uses the staging project; do not switch to
production `openstack` without explicit authorization.

Use the job's workflow inputs and logs to select the snapd artifact. If the job
uses a prebuilt master snap, use `./run-spread -d -- <Spread args>` and record
that this downloads the latest successful master build, which may differ from
the job's artifact. If the job builds snapd from its checked-out source, use a
worktree at that commit and omit `-d`. Do not use `NO_REBUILD=1` unless the
existing local artifact is verified to be the intended build.

When reproducing a CI job, also mirror its caller workflow's `spread-env`,
package artifacts, and snap artifact selection using the CI-scenario table in
`spread-runner`. The workflow's `scenario` value (`generic`, `deb`, `master`,
or `features`) is a reporting label, not a local environment variable; do not
set `SCENARIO` yourself.

The first reproduction invocation must use Spread's native repeat mode with
three repetitions on a fresh machine:

```bash
# CI job that uses a prebuilt master snap:
./run-spread -d -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH

# CI job that builds snapd from its checked-out source:
./run-spread -- -debug-socket -repeat 3 BACKEND:SYSTEM:TEST_PATH
```

Preserve all required environment assignments and the variant from the
original failure in the real command. Spread's `-repeat 3` means three
additional repetitions after the initial execution, for four executions total
on the same worker; it is not equivalent to three fresh-machine attempts.
Record which repetition failed and inspect Spread output or `results.json` to
determine whether the same instance was retained and whether prepare and
restore ran between repetitions. Keep the debug session paused on the first
matching failure, inspect it with `spread exec -- COMMAND`, and then use
`spread stop` to let the repetitions continue.

If all three repetitions pass, do not conclude that an intermittent failure
is absent. When the report history or failure rate warrants further
investigation, continue with up to five independent fresh-worker attempts,
one invocation at a time. Do not use `-reuse` for these solo attempts. Stop
once the same failure signature is reproduced; record different signatures
separately because they do not satisfy the reproduction goal.

If allocation, image download, credentials, or another infrastructure error
prevents the test from reaching the relevant phase, record it but do not count
it toward the five valid attempts. When the error is clearly unrelated and
transient, retry the same command once on a fresh worker with the same inputs
and artifact mode. Record the retry as an infrastructure retry. If the same
blocker recurs, stop and report it rather than looping.

Never hide Spread output in a shell retry loop. Run attempts individually so
the debug pause and complete output remain available. Five valid attempts is a
bounded default, not proof that an intermittent issue is absent.
If observed history suggests a lower failure rate, state that the budget is
insufficient and calculate a larger proposed budget. Obtain user approval
before materially exceeding five slow attempts.

If only `-repeat` reproduces the issue, report it as `REPRODUCED_ALONE` with
repeat mode explicitly required in the reproduction command. Compare the
first successful repetition with the failing repetition for leaked state,
timing, and restore differences. Confirm with a second fresh `-repeat 3`
invocation when resources permit; this distinguishes a repeat-dependent
failure from stale state left by an earlier local experiment.

Classify a reproduction only when phase, failed command, exit status, and
symptom match. Similar-looking failures in another phase are separate issues.

## 5. Replay the observed predecessor sequence

Only do this when the target does not reproduce alone in either individual or
`-repeat` mode, or when same-machine CI evidence already implicates prior test
state.

When the failed worker's earlier test sequence is available, replay it in its
observed order, ending with the target. First try the full observed sequence
when it is practical. Preserve each test's system, variant, and relevant
environment from the CI evidence. Use one worker and Spread's `-order` flag
so the requested task order is explicit:

```bash
./run-spread -d -- -debug-socket -workers 1 -order \
    BACKEND:SYSTEM:PREDECESSOR_1 \
    BACKEND:SYSTEM:PREDECESSOR_2 \
    BACKEND:SYSTEM:TARGET
```

Use the source-appropriate artifact mode from section 4, and replace the
example selectors with the actual ordered CI sequence. Check with `-list` or
the resulting log that Spread selected and executed the intended tasks in the
requested order on the same worker. If the CLI cannot represent a repeated
task or exact sequence in one invocation, use sequential `-reuse` invocations
and verify the worker identity was retained; record this condition difference.
Start each sequence experiment from a fresh machine. Do not add `-repeat` to
the sequence command unless repeating the entire ordered sequence is
intentional.

If the full sequence is too long to run, or does not reproduce, compare every
distinct predecessor—not just the immediate one—in chronological order:

1. Run each predecessor followed by the target on a fresh worker, preserving
   that predecessor's original variant and conditions. Do not skip lower-ranked
   predecessors; the failure may be caused by a test farther back in the run.
2. If individual predecessor/target pairs do not reproduce, test ordered
   groups from the observed sequence. Preserve chronology and use a
   divide-and-conquer or leave-one-out reduction to find the smallest
   reproducing subsequence without assuming the last test caused the failure.
3. Do not stop the comparison when the first predecessor or sequence
   reproduces. A positive result shows that sequence can trigger the failure,
   not that it is the only trigger. Continue testing the other observed
   predecessors within the approved attempt budget, then confirm each positive
   sequence on a fresh worker and run a target-only fresh-worker control.
4. Preserve normal restore behavior between tests. If the original run
   indicates a failed or incomplete restore, record that and reproduce the
   observed cleanup condition only when the logs support it.
5. Run attempts individually. Follow the bounded attempt guidance in section
   4 and ask before materially exceeding five slow attempts; if comparing all
   predecessors would exceed that budget, report the untested candidates and
   request approval rather than silently dropping them.

Do not batch unrelated candidates before the target: that cannot identify
which test or ordered interaction changed the state. Do not edit the product
or test until a baseline or sequence reproduces the original signature.

When a sequence reproduces, use `spread exec` to compare state after its
predecessors and at target failure. Focus on resources those tests own:
services, mounts, snaps, users, files, environment, firewall rules, devices,
and nested VMs. Then trace the relevant `restore` sections and shared helpers.

## 6. Write a reproduction handoff

Create the report before the first local attempt and update it after every
attempt so evidence survives a lost terminal or context change. Unless the
user provides a path, write it to `$SESSION_DIR/reproduction-report.md`.
Treat it as an investigation artifact and do not commit it unless requested.

The report must be self-contained and start with one of these statuses:

- `REPRODUCED_ALONE`
- `REPRODUCED_AFTER_PREDECESSOR`
- `NOT_REPRODUCED`
- `BLOCKED`

Use this structure:

```markdown
# Spread failure reproduction: <test>

Status: <status>
Updated: <UTC timestamp>

## Reported failure
- CI job URLs or pasted job log: <URL or reference>
- Snapd runs/jobs: <links>
- Scenario/backend/system/phase/variant: <values>
- Original signature: <failed command, exit status, decisive output>

## Reproduction environment
- Snapd commit: <SHA>
- Worktree and dirty state: <path and summary>
- Snap artifact source/revision: <details>
- Backend/system/image: <details>
- Relevant environment: <redacted values>

## Exact reproduction
<copy-paste setup and Spread commands in execution order>

## Attempts
| # | Mode/repeat count | Predecessor | Fresh/reused | Result | Signature match | Evidence |
|---:|---|---|---|---|---|---|

## Predecessor analysis
<same-machine log timeline; every predecessor's failed/control frequency and
position; exact ordered sequence; tested and untested candidates; mechanism>

## Debug evidence
<minimal commands and outputs that distinguish the root-cause hypothesis>

## Handoff for fixing agent
- Reproduction confidence: <high/medium/low and why>
- Suspected owning code or test cleanup: <paths/symbols>
- Smallest next experiment or likely fix: <action>
- Verification command: <same command and conditions used above>
- Unknowns and condition differences: <list>
```

The `Exact reproduction` section is mandatory even when reproduction needs a
predecessor. Include all setup commands, working directory, environment
assignments, build/download command, and the exact ordered Spread invocation.
Redact credentials and tokens. Commands must be directly executable by a
following agent; do not replace essential values with prose such as “run the
same test again.”

## 7. Report the conclusion

Report:

- supplied job URLs and original failed run/job links
- original failure signature and number of matching occurrences
- ordered tests executed on the same named machine before each failure,
  including the machine name from the log
- exact solo reproduction command, attempts, and results
- comparison of every observed predecessor with failed and successful
  controls, including tests not replayed because of the attempt budget
- exact ordered sequence commands and results, including the observed worker
- verified facts, hypotheses, and missing evidence
- classification: product defect, test defect, leaked test state, external
  dependency, image defect, infrastructure/transient failure, or inconclusive
- path to the reproduction handoff report

Use “correlated predecessor” until the sequence experiment reproduces and a
state mechanism is identified. If nothing reproduces, do not claim a cause;
name the strongest repeated predecessor, the evidence for and against it, and
the next discriminating experiment. A following fixing agent must begin by
reading the handoff, rerun its exact reproduction command, and preserve those
conditions while implementing and verifying a fix. For test or helper defects,
use `.agents/skills/spread-fixer/SKILL.md`.
