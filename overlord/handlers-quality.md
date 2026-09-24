# Task Handler Quality Checklist

This checklist is intended as review input for snapd state task handlers and
the surrounding task construction, conflict checks, and task-runner wiring.

## General

- Has one clear, narrowly scoped external-state transition.
- Uses explanatory task kind, summary, state keys, and errors consistent with neighbouring handlers.
- Keeps state changes and external effects understandable as one recoverable transaction.
- Preserves manager boundaries and uses established backend and device-context helpers.
- Errors are contextual, actionable, lowercase, and normally use `cannot ...`; unexpected invariants use `internal error: ...`.
- Restart, reboot, retry, and partial execution are intentional cases, not exceptional ones.
- `TaskRunner.AddCleanup` handlers run only after the whole change is ready and outside conflict protection; they remove task-local temporary state and do not modify shared state in ways that can interfere with other changes or the system.

## Concurrency And Coordination

- At operation construction time, conflict checks reject a new change when an incompatible in-progress change affects the same resource.
- Within a change, task dependencies express required execution order.
- Lanes partition a change into independent failure and rollback domains; they do not serialize tasks.
- Uses `TaskRunner.AddBlocked` to defer runnable tasks when unlocked external work must not overlap, including tasks from otherwise compatible changes. This is especially relevant when a task can affect multiple snaps and expressing every potential overlap as an operation-construction conflict would be brittle or unnecessarily reject user operations.

## State And Locking

- Protects working-state access with the `State` lock.
- Begins with `st.Lock(); defer st.Unlock()` unless its structure requires a justified variant.
- Does not read and later overwrite mutable shared state across an unlock, especially `SnapState`, unless execution is serialized at the task-runner level.
- Performs required state validation while holding the lock, and reconsiders relevant state after reacquiring it.
- Persists task working data needed by later tasks, undo, or restart recovery.
- Couples persistent working-state mutations with `t.SetStatus(DoneStatus)` or `UndoneStatus` before the final unlock and handler return when otherwise a restart could replay an incompatible handler state.
- Uses `state.Retry` for transient or deliberately deferred work rather than treating it as a terminal failure.
- Uses `state.Wait` when progress requires an external event or manual action, with deliberate `WaitedStatus` semantics.

## Slow Operation Locking

- Direct handler code releases the `State` lock before potentially slow or blocking filesystem I/O, hashing, subprocess execution, polling, and network work, unless a bounded lock-held exception is required and justified.
- Helper, fallback, retry, and error paths preserve the same boundary; the handler keeps the unlocked window scoped to slow work and reacquires the `State` lock promptly.

## Do Handler

- Validates task inputs, device context, current state, and preconditions before making irreversible external changes.
- Is idempotent: a rerun after any interruption either completes safely or recognizes completed work.
- Records enough prior state or ownership information for a precise undo, without restoring unrelated concurrent changes.
- Handles cancellation through the supplied tomb/context where the operation supports it.
- On an error after beginning its own external changes, cleans up its own partial work before returning the error. The task runner does not undo the failing task itself.
- Separates essential operation failure from best-effort ancillary work, logs the latter, and does not silently hide failures that affect correctness.
- If it causes a restart or boot transition, atomically records the task and restart state using the project's restart helper.

## Undo Handler

- Has an undo handler for external effects unless the task is deliberately final or irreversible and the omission is justified.
- Is a meaningful inverse of the successful do path, including persistent state, filesystem effects, boot settings, services, and security state as applicable.
- Is safe when do completed only partially, when undo is retried, and when the target is already absent.
- Uses task-recorded before-state to restore only what this task changed; first checks that the current state is still the state it owns before overwriting it.
- Removes temporary artifacts created by do but preserves valid reusable or shared artifacts when that is the intended lifecycle.
- Continues independent cleanup where safe, preserving or returning the error that prevents restoration to a known-good state.
- Treats explicitly non-critical cleanup failures as logged best-effort failures only when that leaves the system semantically correct.
- Does not assume undo only follows a fully successful do.

## Tests Expected

- Ideally, unit tests cover every handler path while focusing on observable behaviour; this is especially important because each error path may need to clean up effects already begun.
- A happy-path task-runner test verifies final task/change status, persisted state, and observable external effects.
- A do-then-undo test induces a later dependent-task failure and verifies the task is `UndoneStatus`.
- Undo assertions verify restored state and filesystem/backend effects, not merely that the handler returned `nil`.
- Error-path tests inject failures at meaningful points after partial effects and prove local cleanup occurs.
- Restart/idempotency tests re-run at interruption points, particularly after external work but before the final state/status update.
- Retry, polling, offline, and transient-error behaviour is tested where relevant.
- Tests applicable coordination behaviour: conflict rejection, dependency ordering, and lane-local rollback, task-runner blocking (without overinvesting in the latter two)
- Tests use system-boundary fakes/mocks and validate observable effects; they avoid implementation-detail-heavy mocks.
- Tests cover intentional best-effort cleanup and its logging/continuation semantics.

## Sources

- `CODING.md`
- `overlord/README.md`
- `overlord/state/change.go`
- `overlord/state/task.go`
- `overlord/state/taskrunner.go`
- `overlord/snapstate/conflict.go`
- `overlord/snapstate/handlers.go`
- `overlord/snapstate/handlers_mount_test.go`
- `overlord/devicestate/devicestate_serial_test.go`
- `overlord/devicestate/handlers_systems.go`
