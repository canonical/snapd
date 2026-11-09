---
name: spread-reproducer
description: Investigate and reproduce intermittent snapd Spread failures from the canonical/snapd-testing-jobs nightly test predictor report. Use standalone or as the evidence and reproduction stage of spread-failure-workflow.
argument-hint: "[report run URL or ID] [test path]"
metadata:
  project: snapd
  task-type: test-investigation
  execution-time: slow
---

# Reproduce a reported Spread failure

Use this workflow to investigate entries from the
[`nightly-tests-report.yaml`](https://github.com/canonical/snapd-testing-jobs/actions/workflows/nightly-tests-report.yaml)
report. Use the `spread-runner` skill for the mechanics and safety rules of
running and debugging Spread. This skill owns report selection, CI evidence
collection, reproduction attempts, and prior-test correlation; it does not
replace the shared runner guidance. If the evidence identifies a test defect,
hand the completed reproduction report to `spread-fixer` for the focused
test change. For an end-to-end run, reproduce, fix, and verify flow, use
`spread-failure-workflow`, which coordinates these stages.

## Inputs

Accept a report run URL or ID and, when provided, a test path. If either is
missing, list the recent report runs or candidate report rows and ask the user
to select one. Never silently choose a reported failure when several match.

Record these fields before running anything:

- report run ID and scenario
- phase, backend, system, test path, and variant
- predicted success percentage
- failed snapd run IDs and job IDs from the report history

Use the phase from the report. A preparing or restoring failure is not
equivalent to an executing failure.

## 1. Read the report

Prerequisites are `gh`, `jq`, `curl`, GitHub authentication with access to
`canonical/snapd`, and network access to the test predictor. Check them first;
do not trigger a new report workflow unless the user explicitly requests it.

List recent report runs:

```bash
gh run list \
    --repo canonical/snapd-testing-jobs \
    --workflow nightly-tests-report.yaml \
    --limit 10 \
    --json databaseId,displayTitle,createdAt,conclusion,event,url
```

Download the selected run's Markdown reports into a temporary directory:

```bash
report_dir=$(mktemp -d)
gh run download REPORT_RUN_ID \
    --repo canonical/snapd-testing-jobs \
    --pattern 'test-predictor-*-report' \
    --dir "$report_dir"
```

Read both failing and flaky reports, including the `being-fixed` files. Do not
investigate a skipped or actively assigned entry without noting that status.
The history links target `canonical/snapd` and encode the source run and job
IDs.

For the selected row, retrieve uncapped structured history from the predictor
rather than parsing status icons from Markdown:

```bash
curl --fail --show-error --silent --get \
    --data-urlencode 'name=TEST_PATH' \
    --data-urlencode 'verb=PHASE' \
    --data-urlencode 'backend=BACKEND' \
    --data-urlencode 'system=SYSTEM' \
    --data-urlencode 'scenario=SCENARIO' \
    'http://test-predictor.snapd.canonical.com:5000/predict-with-history'
```

Preserve chronological order. Deduplicate only adjacent entries with the same
`run_id` and `job_id`, matching the report generator. Keep failed occurrences
with non-null IDs as the primary evidence set. If structured history and the
report disagree, state the discrepancy and prefer the current structured
response.

## 2. Collect original failure evidence

For every distinct failed `(run_id, job_id)` pair:

1. Inspect the job metadata and log with `gh run view RUN_ID --repo
   canonical/snapd --job JOB_ID --log`.
2. Record the snapd commit, run attempt, workflow inputs, exact failed command,
   exit status, phase, symptom, and the machine name shown in parentheses on
   the log's task lines.
3. Download that run's `spread-results-RUN_ID-*` artifacts into a separate
   temporary directory. There may be several group artifacts; inspect all
   `results.json` files and select rows matching the report backend, system,
   phase, test, and variant.
4. Confirm that separate failures have the same signature before combining
   them. Analyze differing signatures separately.

Do not rely on a report probability alone. The original log establishes the
failure signature. Use parenthesized machine names in linked job logs to
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

The predictor dependency API can corroborate repeated co-failures:

```bash
curl --fail --show-error --silent --get \
    --data-urlencode 'test=TEST_PATH' \
    --data-urlencode 'backend=BACKEND' \
    --data-urlencode 'system=SYSTEM' \
    --data-urlencode 'scenario=SCENARIO' \
    'http://test-predictor.snapd.canonical.com:5000/dependencies/fail-given-fail'
```

Treat this only as corroboration: it groups same-run failures and does not
prove that the other test ran first or caused the target failure. If artifacts
have expired or lack `results.json`, extract executed test order from the
parenthesized machine names in each linked job log and clearly label the
ordering evidence as log-derived. Never infer same-machine order from
timestamps alone when machine identity is missing.

## 4. Reproduce the target alone

Recreate the original source revision and conditions before testing a
predecessor hypothesis. Do not switch or clean a dirty user checkout. If the
reported commit differs from the current checkout, use a temporary Git
worktree at the reported commit.

Follow `spread-runner` and start with a fresh local `garden` QEMU VM, `-debug`,
the same system, test, variant, relevant environment, and artifact build
method. When the report's backend is `openstack`, using `garden` is the
expected local substitution because OpenStack is reserved for CI. Record this
backend difference; a Garden reproduction is not an identical OpenStack
environment.

If Garden fails before the test starts and the user authorizes a cloud run,
retry the same selector on `openstack-dev` when that staging backend is
configured. Record that it uses the staging project and is not the production
CI environment. Do not switch to production `openstack` without explicit
authorization.

For a failure selected from a report, use `./run-spread -d -- <Spread args>` so
the test uses a prebuilt snapd snap rather than rebuilding from the current
checkout. This downloads the latest successful master build, not necessarily
the snapd revision used by the report; record that difference. If the supplied
GitHub job is tied to a local or branch source change, omit `-d` so
`run-spread` builds the snap from the matching checkout. Do not use
`NO_REBUILD=1` for a report run unless the existing local artifact is verified
to be the intended build.

When reproducing a CI job, also mirror its caller workflow's `spread-env`,
package artifacts, and snap artifact selection using the CI-scenario table in
`spread-runner`. The workflow's `scenario` value (`generic`, `deb`, `master`,
or `features`) is a reporting label, not a local environment variable; do not
set `SCENARIO` yourself.

The first reproduction invocation must use Spread's native repeat mode with
three repetitions on a fresh machine:

```bash
# Report-based failure:
./run-spread -d -- -debug -repeat 3 BACKEND:SYSTEM:TEST_PATH

# Failure tied to local/branch source changes:
./run-spread -- -debug -repeat 3 BACKEND:SYSTEM:TEST_PATH
```

Preserve all required environment assignments and the variant from the
original failure in the real command. Spread's `-repeat 3` means three
additional repetitions after the initial execution, for four executions total
on the same worker; it is not equivalent to three fresh-machine attempts.
Record which repetition failed and inspect Spread output or `results.json` to
determine whether the same instance was retained and whether prepare and
restore ran between repetitions. Keep the debug shell open on the first
matching failure.

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
the interactive debug shell and complete output remain available. Five valid
attempts is a bounded default, not proof that an intermittent issue is absent.
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
./run-spread -d -- -debug -workers 1 -order \
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

When a sequence reproduces, use the debug shell to compare state after its
predecessors and at target failure. Focus on resources those tests own:
services, mounts, snaps, users, files, environment, firewall rules, devices,
and nested VMs. Then trace the relevant `restore` sections and shared helpers.

## 6. Write a reproduction handoff

Create the report before the first local attempt and update it after every
attempt so evidence survives a lost terminal or context change. Unless the
user provides a path, write it under
`.agents/reproduction-reports/<UTC-date>-<test-slug>.md` in the snapd checkout.
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
- CI source (report or job log): <URL or reference>
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

- selected report row and original failed run/job links
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
