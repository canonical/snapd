# Spread failure triage and verification

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
Debug socket session ready at "<socket path>". Follow
[the shared debug-socket procedure](./common-procedures.md#debug-socket-inspection)
to inspect the worker and continue.
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
and symptom. Keep a matching failure paused and collect evidence using
[the shared debug-socket procedure](./common-procedures.md#debug-socket-inspection).
Do not edit code based only on the original CI log when a local reproduction
is available.

Record the run context and preserve it for verification as described in
[the shared procedure](./common-procedures.md#reproducible-run-context).

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

Keep the Spread process running while inspecting a failure. For headless
remote inspection, follow
[the shared debug-socket procedure](./common-procedures.md#debug-socket-inspection).
Use `-debug` for a human-operated shell after failure, or `-shell` for an
interactive shell before task execution.

1. Read the complete failed command and its output before running commands.
2. Identify the failed phase and the exact assertion or command that failed.
3. Use a focused remote command or the interactive shell to test the smallest
   plausible root-cause hypothesis.
4. Collect enough state to distinguish a product bug, test bug, dependency
   failure, image problem, or infrastructure issue.
5. Resume the run only after capturing the evidence needed for a proposed
   solution. Spread will then continue with restore and cleanup.

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

