Notes on state and changes
===========================

State is central to the consistency and integrity of any snap system. It’s maintained by snapd by managing the external on-disk state of snap installations and its own persistent working state of metadata, expectations, and in-progress operations.

Working persistent state is implemented by `overlord/state.State` with a global lock, `State.Lock/Unlock`, to govern updates. If state is modified after acquiring the lock, it’s atomically updated to disk when the lock is released.

State managers
---------------
State managers are used to manage both the working state and the on-disk snap state. They all implement the `overlord.StateManager` interface. Code-wise, together with a few other auxiliary components, they live in `overlord` and its subpackages. `overlord.Overlord` itself is responsible for the wiring and coordination of all of these.

In broad terms, state managers have assigned responsibilities for different subsystems, and these are in mostly orthogonal areas. They then participate in the management and bookkeeping of the state via various mechanisms.

During startup, and after construction, an optional `StartUp` method is invoked on each state manager. This is followed by the activation of an *ensure loop* which calls a state manager’s corresponding `Ensure` method at least once every 5 minutes.

The *ensure loop* is intended to initiate any automatic state management and corresponding transitions, state repair, and any other consistency-maintaining operations.

The other mechanism for state managers to perform their responsibilities is by providing implementations for different kind of `Task`s (see details in the next sections). Each state manager provides tasks for making changes related to their area of the system. For example `snapstate`'s `link-snap` task exposes an installed snap to the system for use. Higher-level parts of the system and the state managers themselves affect system changes by composing the right kind of tasks under a `state.Change`. Helpers used for this usually return a `state.TaskSet`. These helpers are provided alongside of each manager's code (e.g `snapstate.InstallOne`, `ifacestate.Connect`).

`state.Change`
---------------
A `state.Change` is a graph of `state.Task` structs and their inter-dependencies as edges. The purpose of both a `state.Change` and a `state.Task` is identified by their kind (which should be an explanatory string value).

Time-consuming and user-initiated operations, usually initiated from the API provided by the `daemon` package, should be performed using the `state.Change` functionality.

`state.Change` and `state.Task` instances use the working state to remain persistent, and they can carry input parameters, and their own state, accessible with `Get` and `Set` methods.

 The goals of the `state.Change` mechanisms are such that operations should survive restarts and reboots and that, on error, snapd should try to bring back the external state to a previous good state if possible.

`state.TaskRunner`
-------------------
The `state.TaskRunner` is responsible for `state.Change` and `state.Task` execution, and their state management. The do and undo logic of a `state.Task` is defined by `Task` kind using `TaskRunner.AddHandler`. This is done by each state manager for its relevant task kinds.

`TaskRunner` is wired into the *ensure loop* like a state manager. Its Ensure method is executed last for each loop. The method logic is responsible for spawning and managing goroutines that execute the `Task`s do or undo handlers.

High-level parts of the system can use `State.EnsureBefore` to have the *ensure loop* run before its normal schedule to process `Task`s needing execution.

During execution, a `Task` goes through a series of statuses.  These are represented by `state.Status` and will finish in a ready status of either `DoneStatus, UndoneStatus, ErrorStatus` or `HoldStatus`. A Task needing execution is initially `DoStatus`. While the goroutine for its *do* logic is running it is in `DoingStatus`, respectively if the goroutine for its *undo* logic is running in `UndoingStatus` (after having been marked `UndoStatus` first).

If errors are encountered, the `TaskRunner` will normally try to recursively execute the undo logic of any previously depended-upon `Task`s with the exception of the `Task` that generated the error. Each `Task` handler is instead expected to run any desired undo logic for its own errors as part of its error paths.

Different `Change`s and independent `Task`s are normally executed concurrently.

`Task`s and `State` locking and consistency
--------------------------------------------
Currently, the `Task` do and undo handlers are started without holding the `State` lock, but to simplify consistency, it's easier if a `Task` executes while holding the `State` lock.

Strictly, the `State` lock must only be released when performing slow operations, such as:
-   copying, compressing or uncompressing large amounts of on-disk data
-   network operations

So in practice, most handler code should start with:

```
st.Lock()
defer st.Unlock()
```

where `st` is the runtime `state.State` instance, accessible via `Task.State()` or the handler manager.

The deferred `Unlock` will implicitly commit any working state mutations at the end of the handler.

Due to potential restarts (because of snapd updating itself, reboots or snapd being restarted on failure), the do or undo handler logic in a `Task` may be re-executed if it hasn't already completed. This necessitates the following considerations:
-   on-disk/external state manipulation should be idempotent or equivalent
-   working state manipulation should either be idempotent or designed to combine working state mutations with setting the next status of the task. This approach currently requires using `Task.SetStatus` before returning from the handler

If slow operations need to be performed, the required `Unlock/Lock` should happen before any working state manipulation.

If the `State` lock is released and reacquired in a handler, the code needs to consider that other code could have manipulated some relevant working state. There may be also cases where it’s neither possible nor desirable to hold the `State` lock for the entirety of a state manipulation, such as when a manipulation spans multiple subsystems, and so spans multiple tasks. For all such cases, and to simplify reasoning, snapd offers other coordination mechanisms with differing granularity to the `State` lock.

See also the comment in `overlord/snapstate/handlers.go` about state locking.

Testing `Task` handling logic
------------------------------

Given the previous consideration it is important that `Task` handler logic has tests:

- ensuring that *undo* logic correctly matches and undoes what the *do* logic did
- that task handling logic is idempotent, even if the task is only partially executed the first time around
- error paths undo previous state changes as needed

Conflicts and `Task` precondition blocking
-------------------------------------------
At a higher level, it may be appropriate and simpler to manage whether at most one `Change`/sequence of `Task`s is operating on a given snap at a time. As this could, for example, stop the system connecting an interface on a snap that’s being removed, or disable service manipulation while its snap is being installed.

While creating a new `Change` that will operate on a snap, snapd checks whether there are already any in-progress operations for the snap. If there are, a conflict error is returned rather than initiating the `Change`.

The central logic for such checking lives in `overlord/snapstate/conflict.go`.

Some tasks, or family of tasks, need to release the `State` lock but cannot run together with some other tasks. Such tasks include:
-   hook tasks where at most one should be running at a time for a given snap
-   interface-related tasks that might touch more than one snap at a time, beyond what conflicts can take care of, so preferably at most one of them should be running at a time

To address this, precondition predicates can be hooked into the `TaskRunner` via `TaskRunner.AddBlocked`.

Before running a task, the precondition predicates are invoked and, if none return a value of true, the task is run. The input for these predicates is any candidate-for-running task and the set of currently running tasks.

Cleanup logic
--------------

`Task`s can also have cleanup handlers registered for them with TaskRunner.AddCleanup`. This logic is run only after the entire `Change` the `Task` belonged to is considered ready, which means this is not run under the protection of conflict logic anymore, so while it can be used to clean up temporary state belonging to the task, in all situations, success or not, it cannot touch state that can interfere with other `Change`s or the system in general.
