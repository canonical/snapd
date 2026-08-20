# Post-restart LTS jump (unaware bootstrap)

Focused exploration of how an **LTS-aware** snapd, just started after an
**unaware** daemon linked it from `latest`, should jump onto the LTS
track. Operation inventory and omit-channel rules:
[LTS-OPERATIONS.md](LTS-OPERATIONS.md). Long-form design:
[DESIGN.md](DESIGN.md) (BB5). Short intercept: [FOCUS.md](FOCUS.md).

Not merged into those files yet. **Not implemented.**

This is **not** the preferred jump. Already-aware devices jump in
`doDownloadSnap` and never link `latest`. This path exists because
unaware snapd cannot intercept: the first aware blob is always linked
and executed first.

Constraint that drives the design: the just-started binary may be
**functionally newer** than the frozen LTS branch we need to land on.
Minimise mutating `state.json` (especially `patch.Level`) and
completing the rest of a mixed refresh under that binary.

---

## Viable interception

An intercept point is only useful if we can answer both of:

1. **What to do with the snapd that is already partly installed.**
2. **How to start installing the LTS snapd from that point.**

Until those two are concrete, “run first after restart” is not a
mechanism.

### What “partly installed” actually is

Unaware snapd already finished **local install of an aware revision on
`latest`**:

- `download-snap` / `mount-snap` / `link-snap` for that revision are
  **Done**.
- `backend.LinkSnap` made it `current`. `SnapState.Current` and the
  sequence include it. Tracking is still `latest/<risk>`.
- `FinishTaskWithRestart` for `RestartDaemon` set `link-snap` to Done
  and requested the daemon restart immediately
  ([overlord/restart/restart.go](overlord/restart/restart.go)). It does
  **not** Retry on `link-snap`. That task cannot be the intercept.
- `auto-connect` (and `set-auto-aliases`, `setup-aliases`, hooks,
  `update-managed-boot-config`, …) are still Do/Doing. That is the
  official wait point after restart (`FinishRestart` in
  [overlord/ifacestate/handlers.go](overlord/ifacestate/handlers.go)
  `doAutoConnect`; see `isChangeRequestingSnapdRestart` in
  [overlord/snapstate/snapstate.go](overlord/snapstate/snapstate.go)).
- Remaining snapd tasks still carry **snap-setup for the `latest`
  revision** (usually via `snap-setup-task` on the original download).
- In a mixed auto-refresh, non-essential snaps wait on snapd’s
  **EndEdge**, not on `link-snap`
  ([splitRefresh](overlord/snapstate/snapstate.go)). They must not run
  until the LTS revision is current **and** snapd’s remaining tasks
  have finished under that revision.

So the vehicle is already **linked and running**. What is incomplete is
post-link bookkeeping (interfaces, aliases, hooks) and every other snap
in the same change.

Undo of that `link-snap` would restore the **unaware** revision and
re-exec it. Unaware still cannot intercept. **Do not undo back to
unaware.**

### (1) What to do with the `latest` vehicle

Treat it as a **throwaway current**, not as a refresh to finish.

- Do **not** run `auto-connect` / aliases / hooks / boot-config for the
  `latest` revision. That would finish installing a snapd we intend to
  replace, and would do more work (and more state) on the too-new
  binary.
- Keep it as `current` only until an LTS `link-snap` replaces it. Then
  it is a previous sequence revision (normal retain / later discard).
- After the LTS blob is downloaded (and linked), **rewrite remaining
  snapd tasks’ snap-setup** (Channel, SideInfo, Revision,
  DownloadInfo) onto the LTS revision — same idea as the download
  intercept rewriting `snap-setup` in place. Otherwise `auto-connect`
  would still set up the `latest` revision.
- Expect a **second daemon restart** when LTS `link-snap` runs. Unaware
  → aware-on-`latest` → LTS is two hops; the first hop is unavoidable.

Do **not** complete the `latest` install and then start a second,
independent `snap refresh snapd`. That doubles post-link work and lets
other snaps proceed at EndEdge after the *first* snapd graph, still on
`latest`.

### (2) How to start the LTS install from there

A **new** change for snapd will conflict with the in-flight refresh
(`CheckChangeConflictMany`). `ensureUbuntuCoreTransition` already
bails out when `changeInFlight`. Ensure-only BB5 can help a **quiet**
device (aware already current, nothing running); it cannot hijack a
mixed refresh-all.

The in-flight change must grow a second snapd install **in front of
the existing post-link tasks**:

- Build a store update/install task set for snapd on the LTS channel
  (omit-channel / not `ExplicitChannel`; keep cohort; fail closed).
  Ignore this change id in conflict checks (`FromChange`), as
  `doPrerequisites` does when nesting installs.
- **Inject** that task set after the Done `link-snap` and before
  `auto-connect`. `InjectTasks` already does: extras wait for
  `mainTask`, halt tasks of `mainTask` wait for extras
  ([overlord/snapstate/handlers.go](overlord/snapstate/handlers.go)).
  `mainTask` must be the Done `link-snap`, not `auto-connect` (that
  would put LTS *after* auto-connect).
- Because extras sit before `auto-connect`, they sit before EndEdge.
  Other snaps that `WaitFor` snapd EndEdge stay blocked. No separate
  hold is required for that graph; `AddBlocked` is still useful as a
  belt if other changes or tasks do not wait on EndEdge.
- The injected `download-snap` can use the **running** (just-started)
  map to plan LTS+cohort, or download `latest` again and intercept.
  The running daemon *is* the vehicle whose map we wanted; planning
  LTS here does not skip a newer map. Prefer a direct LTS
  `SnapAction` to avoid a third blob.

`SnapManager.StartUp` is the first manager hook **after**
`patch.Apply` and **before** TaskRunner. That is when to inject, so
the first `TaskRunner.Ensure` never starts `auto-connect` on `latest`.

`FinishRestart` inside `doAutoConnect` is **too late** unless
injection already happened: the handler is already running the
`latest` auto-connect. It can Retry, but other snaps that do not wait
on that task could already be runnable. Do not rely on it as the only
gate.

### Patch level is a different gate

`patch.Apply` runs in `overlord.New` **before** StartUp
([overlord/overlord.go](overlord/overlord.go)). If the `latest` binary
applies a `patch.Level` the LTS blob cannot read, the second hop
cannot start (BB7). Task injection cannot undo that.

If we take “minimise mutation on a too-new snapd” seriously, we need
an **earlier** check: before `patch.Apply`, if tracking is a
transition track and the running map says LTS, **skip applying new
patches** (new snapd can read old state) and jump; apply patches only
once LTS is current. Alternative: snap-failure / refuse start so we
never run the too-new binary — that does not install LTS by itself.

Skipping patches is independent of (1)/(2). It does not replace
injection.

---

## Startup vs tasks

```mermaid
sequenceDiagram
  participant Old as unaware_snapd
  participant Link as link_snap
  participant New as aware_on_latest
  participant Patch as patch_Apply
  participant Start as SnapManager_StartUp
  participant Ens as SnapManager_Ensure
  participant TR as TaskRunner_Ensure

  Old->>Link: download and link latest aware blob
  Link->>Link: status Done plus RestartDaemon
  Note over Link: auto-connect still Do
  Link->>New: exec new snapd
  New->>Patch: overlord.New
  Note over Patch: first state mutation on the new binary
  New->>Start: before Loop
  Note over Start: inject LTS tasks after link-snap
  New->>Ens: first Ensure, before TaskRunner
  Note over Ens: changeInFlight true, cannot start a second snapd change
  New->>TR: TaskRunner last
  TR->>TR: LTS download mount link, then auto-connect
```

- First Loop Ensure: managers in registration order, **TaskRunner
  last** (`AddManager(o.runner)`). `SnapManager.Ensure` runs before
  pending tasks, but a new exclusive change cannot stop the in-flight
  one.
- `TaskRunner.AddBlocked` can serialise tasks
  ([overlord/state/taskrunner.go](overlord/state/taskrunner.go)).

---

## Candidates, evaluated as (1) + (2)

| Candidate | When | (1) leftover `latest` snapd | (2) start LTS install | Avoids `patch.Apply`? | Blocks other snaps? |
| --- | --- | --- | --- | --- | --- |
| **Before `patch.Apply`** | `overlord.New` | Too early to change the link; can only skip patches or refuse start | Cannot talk to the store | **Yes** (only place) | N/A (no tasks yet) |
| **`SnapManager.StartUp` + inject after `link-snap`** | After patches, before Loop | Leave linked; do not auto-connect `latest`; rewrite snap-setup after LTS download | Nested update, `FromChange`, `InjectTasks(link-snap, …)` | No | Yes, if extras sit before EndEdge |
| **Inject only (same, from Ensure)** | First Ensure | Same as StartUp if TaskRunner has not run yet; racey if it has | Same | No | Same |
| **`FinishRestart` in `auto-connect`** | First post-restart snapd task | Already entering auto-connect of `latest` | Retry + inject is possible but late | No | Only for tasks that wait on this auto-connect |
| **Ensure exclusive change** (ubuntu-core transition + BB6) | `SnapManager.Ensure` | Does not see the in-flight graph | New snapd change **conflicts** while refresh-all is Doing | No | **No** for mixed refresh |
| **`AddBlocked` hold-all** | TaskRunner | Does not replace the vehicle | Does not start LTS by itself | No | Yes, complement to inject |

Quiet device (aware already current on `latest`, no change in flight):
Ensure exclusive change *is* enough for (2). Mixed auto-refresh is the
case that decides the intercept.

---

## Stance (for the next implementation choice)

**Viable intercept for the unaware hop:** at `SnapManager.StartUp`,
if tracking is a transition track and the running map Resolves to LTS,
inject a snapd store install/refresh onto that LTS channel after the
Done `link-snap` and before `auto-connect`; rewrite remaining snapd
snap-setup onto the LTS revision; let the existing EndEdge keep other
snaps waiting. Optional `AddBlocked` while that inject is in flight.

**Separate gate:** consider skipping `patch.Apply` on this first start
when a jump is required, so the too-new binary does not raise
`patch.Level` above the LTS blob.

Do **not** treat Ensure-only BB5 as sufficient for mixed refresh-all.
Do **not** finish the `latest` snapd graph and refresh again.

Same-change injection from the **old** (unaware) daemon is still
rejected: it has no map. Injection from the **new** process is a
different site (UC042 “as it starts to run”).

---

## Open before coding

- Exact task set to inject: full `Update` graph vs download + mount +
  link only (then reuse existing auto-connect after snap-setup
  rewrite).
- Second restart: remaining tasks and `finish-restart` flags must wait
  for the **LTS** daemon, not treat the first restart as enough.
- Quiet-device path: same StartUp inject is a no-op if `link-snap` is
  not sitting Done-with-pending-auto-connect; then Ensure can create
  a normal snapd refresh (no in-flight conflict).
- `ExplicitChannel` / `--channel=` on the original unaware refresh:
  skip this jump (omit-channel rule).
- Undo: if LTS download/link fails, original `link-snap` undo still
  points at unaware; define whether we stay on `latest` aware or fail
  the whole change.
