---
name: reproduce-spread-failure
description: Reproduce intermittent snapd Spread failures from the canonical/snapd-testing-jobs nightly test predictor report. Use when investigating a failing or flaky report entry, inspecting failed CI logs and results, or determining whether tests executed earlier on the same Spread machine repeatedly contaminate a later test.
argument-hint: "[report run URL or ID] [test path]"
metadata:
  project: snapd
  task-type: test-investigation
  execution-time: slow
---

# Reproduce a reported Spread failure

Use this workflow to investigate entries from the
[`nightly-tests-report.yaml`](https://github.com/canonical/snapd-testing-jobs/actions/workflows/nightly-tests-report.yaml)
report. Use the `run-spread-test` skill for the mechanics and safety rules of
running and debugging Spread. This skill adds report selection, CI evidence
collection, and prior-test correlation.

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
   exit status, phase, and symptom.
3. Download that run's `spread-results-RUN_ID-*` artifacts into a separate
   temporary directory. There may be several group artifacts; inspect all
   `results.json` files and select rows matching the report backend, system,
   phase, test, and variant.
4. Confirm that separate failures have the same signature before combining
   them. Analyze differing signatures separately.

Do not rely on a report probability alone. The original log establishes the
failure signature, and `results.json` establishes execution order.

## 3. Find tests run earlier on the same machine

In Spread JSON, “same machine” means the same `.instance` value in the same
run artifact. Sharing only a backend, system, run, or job is insufficient.

For each matching failed target row:

1. Select rows with the identical `.instance`.
2. Sort them by parsed `.start`; retain file order as a tie-breaker.
3. Locate the target occurrence and collect only task rows that started before
   it. Exclude project/suite pseudo-rows and setup metadata.
4. Record the immediate predecessor separately from all earlier tests.
5. Include predecessor phase, success, variant, and restore failure. A failed
   restore is a strong contamination candidate even when its test execution
   passed.

Rank candidates across distinct failed occurrences using:

1. number and fraction of target failures where it appeared earlier on the
   same instance
2. number of times it was the immediate predecessor
3. matching backend, system, scenario, and variant
4. predecessor failure or restore anomalies
5. absence, or a much lower frequency, before successful target occurrences

Do not rank ubiquitous setup tests highly merely because they precede every
task. When successful target occurrences are available, use them as controls
and report both `failures preceded / failures inspected` and
`successes preceded / successes inspected`.

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
have expired or lack `results.json`, extract executed test order from each job
log and clearly label the ordering analysis as lower confidence.

## 4. Reproduce the target alone

Recreate the original source revision and conditions before testing a
predecessor hypothesis. Do not switch or clean a dirty user checkout. If the
reported commit differs from the current checkout, use a temporary Git
worktree at the reported commit.

Follow `run-spread-test` and start with a fresh `garden` machine, `-debug`, the
same system, test, variant, relevant environment, and artifact build method.
Changing the CI backend to `garden` is an explicit condition difference.

Use this default retry budget unless the user provides another one:

- Run the target alone up to five times, one invocation at a time.
- Use a fresh machine for every attempt. Do not use `-reuse` for the solo
   baseline because state from a prior attempt would invalidate it.
- Stop retrying as soon as the same failure signature is reproduced and keep
   that attempt's `-debug` shell open for evidence collection.
- If an attempt fails with a different signature, record it separately and
   continue; it does not satisfy the reproduction goal.
- If allocation, image download, credentials, or another infrastructure error
   prevents the test from reaching the relevant phase, record it but do not
   count it toward the five valid attempts.
- Never hide Spread output in a shell retry loop. Run attempts individually so
   the interactive debug shell and complete output remain available.

Five valid attempts is a bounded default, not proof that an intermittent issue
is absent. If the observed report history suggests a lower failure rate, state
that the budget is insufficient and calculate a larger proposed budget rather
than running indefinitely. Obtain user approval before materially exceeding
five slow attempts.

Also try Spread's native repeat mode for an intermittent target. Start with
five repetitions unless the report evidence justifies another bounded value:

```bash
NO_REBUILD=1 ./run-spread -debug -repeat 5 garden:SYSTEM:TEST_PATH
```

Preserve all required environment assignments and the variant from the
original failure in the real command. `-repeat 5` is one repeat experiment
containing five executions; it is not equivalent to five fresh-machine
attempts. Record which repetition failed and inspect Spread output or
`results.json` to determine whether the same instance was retained and whether
prepare and restore ran between repetitions. Keep the debug shell open on the
first matching failure.

If only `-repeat` reproduces the issue, report it as `REPRODUCED_ALONE` with
repeat mode explicitly required in the reproduction command. Compare the first
successful repetition with the failing repetition for leaked state, timing,
and restore differences. Confirm with a second fresh `-repeat` invocation when
resources permit; this distinguishes a repeat-dependent failure from stale
state left by an earlier local experiment.

Classify a reproduction only when phase, failed command, exit status, and
symptom match. Similar-looking failures in another phase are separate issues.

## 5. Test predecessor contamination

Only do this when the target does not reproduce alone in either individual or
`-repeat` mode, or when evidence already strongly implicates leaked state.

Test candidates in rank order, one at a time:

1. Start from a fresh machine for each candidate experiment.
2. Run the candidate and target serially on the same worker in one Spread
   invocation when possible. Confirm their actual order in Spread output.
3. Preserve normal restore behavior between the tests; contamination often
   comes from incomplete restore.
4. If one invocation cannot express the order, use `-reuse` for two sequential
   invocations and verify the same instance was retained. Record this as a
   condition difference.
5. Try each of the top three candidates up to three valid times. Stop testing a
   candidate when it reproduces the original signature, then repeat that
   positive sequence once on a fresh machine and run a target-only
   fresh-machine control.
6. Stop the candidate search when the reproduced sequence and state evidence
   identify a plausible mechanism, or when the bounded candidate budget is
   exhausted. Ask before expanding beyond the top three candidates.

Do not batch several candidates before the target: that cannot identify which
one changed the state. Do not edit the product or test until a baseline or
sequence reproduces the original signature.

When a sequence reproduces, use the debug shell to compare state immediately
after the predecessor and at target failure. Focus on resources the predecessor
owns: services, mounts, snaps, users, files, environment, firewall rules,
devices, and nested VMs. Then trace its `restore` section and shared helpers.

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
- Nightly report: <URL>
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
<ranking, failed-occurrence frequency, successful controls, and mechanism>

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
- exact solo reproduction command, attempts, and results
- predecessor ranking with failed and successful control frequencies
- exact sequence commands and results
- verified facts, hypotheses, and missing evidence
- classification: product defect, test defect, leaked test state, external
  dependency, image defect, infrastructure/transient failure, or inconclusive
- path to the reproduction handoff report

Use “correlated predecessor” until the sequence experiment reproduces and a
state mechanism is identified. If nothing reproduces, do not claim a cause;
name the strongest repeated predecessor, the evidence for and against it, and
the next discriminating experiment. A following fixing agent must begin by
reading the handoff, rerun its exact reproduction command, and preserve those
conditions while implementing and verifying a fix.