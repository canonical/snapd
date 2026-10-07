# snapd Development Guide for AI Agents

## Scope

snapd manages snap packages and Ubuntu Core systems. The repository contains
the `snapd` daemon, the `snap` client, sandbox helpers such as `snap-confine`
and `snap-exec`, and supporting tools.

This file is a quick reference for project-specific constraints. The documents
listed under [Authoritative references](#authoritative-references) own the
detailed guidance and should be consulted before changing the corresponding
subsystem.

## Safety and approval

Get explicit user approval before using root privileges or changing host state,
including:

- installing, removing, or refreshing snaps or system packages
- stopping, starting, or restarting host services
- replacing or running the host's snapd daemon

Prefer spread with the `garden` backend, a VM, or another isolated environment
for system-level testing.

## Architecture essentials

### Overlord and state managers

`overlord.Overlord` coordinates state managers and persistent operations:

- State managers implement `StateManager.Ensure()`.
- Optional lifecycle interfaces are `StateStarterUp`, `StateWaiter`,
  `StateShutDowner`, and `StateStopper`. See `overlord/stateengine.go` for their
  exact contracts and invocation order.
- Operations are persisted as a `state.Change` containing a dependency graph
  of `state.Task` values, so work can survive daemon and system restarts.
- `state.TaskRunner` executes task do and undo handlers concurrently when their
  dependencies permit it.

Key manager package roots include `overlord/snapstate`,
`overlord/ifacestate`, `overlord/assertstate`, `overlord/devicestate`, and
`overlord/hookstate`.

### Manager dependencies

Import rules apply to Go packages, not recursively to every package beneath a
directory:

- The `overlord/snapstate` manager package root is imported by peer managers
  and must not directly import their package roots in the opposite direction.
  `assertstate` and `hookstate` should likewise mostly be consumed rather than
  consume peer manager roots.
- Subpackages such as `overlord/ifacestate/ifacerepo`,
  `overlord/snapstate/backend`, and `overlord/configstate/config` are distinct
  package boundaries. They may be imported when the dependency direction is
  sound, no cycle is introduced, and nearby production code establishes the
  precedent.
- Break unavoidable cross-manager cycles with exported function hooks or
  registration mechanisms. Assign hooks from `init` functions or manager
  constructors. `snapstate.ValidateRefreshes` is a representative example.

See `CODING.md` before introducing or changing a cross-manager dependency.

### Task handlers

Follow these invariants rather than copying a simplified handler template:

- Hold the `state.State` lock while reading or mutating working state.
- Release the lock only around slow external operations such as network or
  substantial filesystem work. Revalidate mutable state after reacquiring the
  lock; do not overwrite it using values read before the unlock.
- Make external state changes idempotent or otherwise safe to repeat after a
  restart.
- Persist the task-owned prior state or ownership information needed for a
  precise undo without reverting unrelated concurrent changes.
- Atomically combine non-idempotent working-state changes with the appropriate
  `Task.SetStatus` transition before unlocking.
- On a terminal error, clean up the current task's partial external effects;
  the task runner normally undoes completed prerequisite tasks, not the failing
  task itself. Undo handlers must nevertheless tolerate partial execution when
  an in-flight task is aborted because another task fails.
- Use conflicts to reject incompatible changes, task dependencies for ordering,
  and lanes for failure domains rather than serialization. Use
  `TaskRunner.AddBlocked` to defer tasks when unlocked external work must not
  overlap, especially when a task can affect multiple snaps and expressing all
  potential overlap as conflicts would be brittle or unnecessarily reject user
  operations.
- Use `TaskRunner.AddCleanup` only for task-local temporary data. Cleanup runs
  after the whole change is ready and outside conflict protection.

`overlord/README.md` is authoritative for task lifecycle, locking, retries,
lanes, and undo behavior. Use nearby real handlers as implementation examples.

Task handlers that need device information must use the task-aware
`snapstate.DeviceCtx(st, task, providedCtx)` API, commonly as
`DeviceCtx(t.State(), t, nil)` inside `snapstate`. A remodel can make the device
context specific to the task's change. Code without a task may legitimately
use `DeviceCtxFromState` or another context-specific helper. See
`overlord/snapstate/devicectx.go` and `ARCHITECTURE.md`.

## Coding and testing essentials

- Follow `gofmt -s` and the naming and package-layout guidance in `CODING.md`.
- Error messages start lowercase, have no trailing period, and normally use
  "cannot" rather than "failed to". Prefix programming errors with
  "internal error:".
- Introduce an exported error type only when callers need to inspect it.
- Prefer tests in a dedicated `<package>_test` package. Expose internals through
  conventional `export_test.go` files only where warranted.
- snapd tests use gocheck and `testutil` to complement Go's standard testing
  package. Benchmarks use the standard `testing` package.
- Mock at system boundaries where practical. Conventional `Mock*` helpers
  return a parameterless restore function.
- Keep `*util` packages low-level: they should not import non-`*util` packages
  and should minimize dependencies.
- Most code under `overlord` is daemon-only and must not be imported into other
  snapd tools. The controlled `nomanagers` subset of
  `overlord/configstate/configcore` is an exception.

## Validation

Start with the narrowest check that can falsify the change, then broaden based
on risk and the touched surface.

| Touched surface | Focused validation |
| --- | --- |
| Go package | Run the relevant package and gocheck test pattern; use `LANG=C.UTF-8` where locale matters |
| Shared Go behavior | Expand to affected packages, then use `./run-checks` before committing |
| C code under `cmd` | Run the relevant target, commonly `make -C cmd check` |
| Spread task definition | Run the focused shell and format checks below |
| Spread test or system behavior | Follow the `run-spread-test` skill and target a specific test path |
| Documentation only | Run the applicable linter and `git diff --check` |

For one spread task, run:

```sh
./tests/lib/external/snapd-testing-tools/utils/spread-shellcheck <test-path>/task.yaml
./tests/lib/external/snapd-testing-tools/utils/check-test-format --tests <test-path>/task.yaml
```

Use `check-test-format --dir <test-path>` to check a directory recursively.

## Authoritative references

- `ARCHITECTURE.md`: entry points, execution pipeline, subsystem ownership,
  and device-context rationale.
- `CODING.md`: coding, dependency, testing, PR, and merge policy.
- `overlord/README.md`: persistent state, changes, tasks, handlers, locking,
  lanes, and conflicts.
- `interfaces/builtin/README.md`: implementing built-in interfaces and
  declaration policy.
- `HACKING.md`: local development and debugging procedures. Its host-mutating
  commands remain subject to the approval rule above.

## PR and commit conventions

- Format titles and commit subjects as
  `affected/packages: short summary in lowercase`.
- See `CODING.md` for the full PR and commit policy.
