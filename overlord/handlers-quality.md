# Task Handler Quality Checklist

This checklist is intended as review input for snapd state task handlers and
the surrounding task construction, conflict checks, and task-runner wiring.

The `##` headings and top-level `- ` bullets below are parsed by
`.agents/skills/review-task-handler/scripts/score.py`. Every heading other than
`Sources` must match a scored category there, so renaming or adding one also
requires updating `score.py`; `###` subheadings are not supported inside them.
Each top-level `- ` line outside code fences counts as one criterion, so never
wrap a criterion onto a continuation line starting with `- `. When adding or
removing criteria, update the expected counts in
`.agents/skills/review-task-handler/scripts/test_score.py`.

## General

These criteria apply to the handler set as a whole: the do handler, any undo
handler, and any cleanup handler. Throughout, restart, reboot, retry, and
partial execution are intentional cases, not exceptional ones.

- Has one clear, narrowly scoped external-state transition.
- Uses explanatory task kind, summary, state keys, and errors consistent with neighbouring handlers.
- Keeps state changes and external effects understandable as one recoverable transaction.
- Has an undo handler for external effects unless the task is deliberately final or irreversible and the omission is justified.
- Preserves manager boundaries and uses established backend and device-context helpers.
- Errors are contextual, actionable, lowercase, and normally use `cannot ...`; unexpected invariants use `internal error: ...`.
- Discards task-owned temporary artifacts in the handler that created them or in undo; deferring that to a registered cleanup handler is a narrow exception for data that undo may still need.

## Concurrency And Coordination

- At operation construction time, conflict checks reject a new change when an incompatible in-progress change affects the same resource.
- Within a change, task dependencies express required execution order.
- Uses lanes to partition a change into independent failure and rollback domains, and does not rely on them to serialize tasks.
- Uses `TaskRunner.AddBlocked` to defer runnable tasks when unlocked external work must not overlap, including tasks from otherwise compatible changes. This is especially relevant when a task can affect multiple snaps and expressing every potential overlap as an operation-construction conflict would be brittle or unnecessarily reject user operations.
- Registers a `TaskRunner.AddCleanup` handler only for task-local data that must outlive the task because undo may still need it and that becomes safe to discard once the change is ready; the cleanup runs outside conflict protection and must not modify shared state in ways that can interfere with other changes or the system.
- A registered cleanup handler accounts for running after failed and undone tasks too, checking the task's final status where the correct action differs.

## State And Locking

- Protects working-state access with the `State` lock, conventionally via `st.Lock(); defer st.Unlock()` at the top of the handler unless its structure requires a justified variant.
- Does not read and later overwrite mutable shared state across an unlock, especially `SnapState`, unless execution is serialized at the task-runner level; re-reads and revalidates relevant state after reacquiring the lock.
- Persists task working data needed by later tasks or restart recovery.
- Couples persistent working-state mutations with `t.SetStatus(DoneStatus)` or `UndoneStatus` before the final unlock and handler return when otherwise a restart could replay an incompatible handler state.
- Requests a restart or boot transition through `FinishTaskWithRestart` or the manager's equivalent, so the task's final `DoneStatus` or `UndoneStatus` and the restart request are recorded together instead of setting the status and requesting the restart separately. This applies to undo as well, where the helper also unschedules restarts the do phase had requested.
- Waits with `snapstate.FinishRestart` and retries when the handler must not proceed until a scheduled restart has happened, rather than assuming the reboot already occurred.
- Uses `state.Retry` in do or undo for transient or deliberately deferred work rather than treating it as a terminal failure.
- Uses `state.Wait` when progress requires an external event or manual action, with deliberate `WaitedStatus` semantics.

## Slow Operation Locking

- Direct handler code releases the `State` lock before potentially slow or blocking filesystem I/O, hashing, subprocess execution, polling, and network work, unless a bounded lock-held exception is required and justified.
- Helper, fallback, retry, and error paths preserve the same boundary; the handler keeps the unlocked window scoped to slow work and reacquires the `State` lock promptly.

## Do Handler

- Validates task inputs, device context, current state, and preconditions before making irreversible external changes.
- Is idempotent: a rerun after any interruption either completes safely or recognizes completed work.
- Records enough prior state or ownership information for a precise undo, without restoring unrelated concurrent changes.
- Handles cancellation through the supplied tomb/context where the operation supports it.
- On an error after beginning its own external changes, rolls back its own partial external effects before returning the error. The task runner does not undo the failing task itself.
- Separates essential operation failure from best-effort ancillary work, logs the latter, and does not silently hide failures that affect correctness.

## Undo Handler

- Is a meaningful inverse of the successful do path, including persistent state, filesystem effects, boot settings, services, and security state as applicable.
- Does not assume undo only follows a fully successful do: it is safe when do completed only partially, when undo is retried, and when the target is already absent.
- Handles cancellation through the supplied tomb/context where it applies, and prefers an undo design that avoids long-running or cancellable work, even at the cost of extra preparation in do.
- Uses task-recorded before-state to restore only what this task changed; first checks that the current state is still the state it owns before overwriting it.
- Removes temporary artifacts created by do but preserves valid reusable or shared artifacts when that is the intended lifecycle.
- Continues independent restoration steps where safe, preserving or returning the error that prevents restoration to a known-good state.
- Treats explicitly non-critical restoration failures as logged best-effort failures only when that leaves the system semantically correct.

## Tests Expected

Unit tests should cover every handler path while focusing on observable
behaviour. This matters most for error paths, each of which may need to roll
back effects already begun.

- A happy-path task-runner test verifies final task/change status, persisted state, and observable external effects.
- A do-then-undo test induces a later dependent-task failure and verifies the task is `UndoneStatus`.
- Undo assertions verify restored state and filesystem/backend effects, not merely that the handler returned `nil`.
- Error-path tests inject failures at meaningful points after partial effects and prove the handler rolls back its own partial effects.
- Restart/idempotency tests re-run at interruption points, particularly after external work but before the final state/status update.
- Retry, polling, offline, and transient-error behaviour is tested where relevant.
- Tests applicable conflict rejection and task dependency ordering.
- Covers lane-local rollback and task-runner blocking proportionately, where they materially affect this task.
- Tests use system-boundary fakes/mocks and validate observable effects; they avoid implementation-detail-heavy mocks.
- Tests cover intentional best-effort behaviour and its logging/continuation semantics, both for ancillary work in do and for non-critical restoration steps in undo.

## Sources

- `CODING.md`
- `overlord/README.md`
- `overlord/state/change.go`
- `overlord/state/task.go`
- `overlord/state/taskrunner.go`
- `overlord/snapstate/conflict.go`
- `overlord/snapstate/handlers.go`
- `overlord/snapstate/snapstate.go`
- `overlord/restart/restart.go`
- `overlord/snapstate/handlers_mount_test.go`
- `overlord/devicestate/devicestate_serial_test.go`
- `overlord/devicestate/handlers_systems.go`
