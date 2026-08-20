# LTS vs install/refresh operations

Inventory of every snapd path previously walked for LTS correctness, with
option permutations and the expected behaviour. Long-form rationale:
[DESIGN.md](DESIGN.md). Short mechanism: [FOCUS.md](FOCUS.md).

This file is **snapd-only**. Other snaps are never remapped. Assumptions
unless a row says otherwise: Ubuntu Core 18+, asserted snapd, boot base
managed in the LTS map, model in scope (not classic / UC16).

## Guiding policy

LTS is a **store-channel policy**. It remaps snapd from a transition
track (usually `latest`) onto the track for the device's UC version,
keeping the requested **risk**. It does not retarget a local blob, and
it is second to an explicit caller pin.

1. **Omit-channel rule.** LTS runs only when this operation's caller
   did **not** supply a channel. A supplied channel string skips LTS
   for that operation — including `--channel=latest/stable`, `--edge`,
   `--stable`, `--candidate`, and `--beta`. Later auto-refresh (no
   `--channel`) can still remap.
2. **`--edge` is not `latest/edge`.** On refresh, a risk-only flag
   inherits the current **track** (`channel.Resolve`). `--edge` while
   tracking `18/stable` means `18/edge`. It is still a caller pin, so
   LTS does not override it.
3. **`ExplicitChannel` is a caller pin, not a filled string.** It means
   this operation's caller typed `--channel=` / sent API `"channel"` /
   set `--snap=snapd=<channel>` / set `$SNAPD_SNAPD_CHANNEL`. Internals
   that copy a policy default into `RevisionOptions.Channel` (model
   `default-channel`, cluster channel, prereq `"stable"`, image
   `--channel`) must leave `ExplicitChannel` false so LTS still jumps
   (intercept or post-restart).
4. **Explicit revision wins the blob.** `--revision=N` / API
   `"revision"` sets `ExplicitRevision` and skips LTS. A validation-set
   revision pin is **not** a caller `--revision`: LTS still jumps the
   channel; the pin still selects the blob (fail closed if `N` is not
   on the LTS track).
5. **Cohort does not skip LTS.** Keep the key, remap the channel, send
   both to the store (same as `snap refresh --channel=` while
   in-cohort). Unsatisfiable LTS+cohort fails closed; no fallback to
   `latest`.
6. **Local blobs are never retargeted.** Path install, sideload, try,
   firstboot seed, and offline remodel/recovery use the given file.
   LTS does not reject or remap there. Wrong tracking heals on the next
   **store** refresh without a pin.
7. **Signed / image policy is remapped, not skipped.** Model
   `default-channel`, cluster assertion channel, and image-wide
   `--channel` are policy defaults, not operator `--channel=`.
   `prepare-image --snap=snapd=<channel>` **is** a builder pin and
   skips (it becomes firstboot tracking). `--snap=snapd` with no
   channel still remaps at image/remodel (BB4a/BB4b); store paths jump
   via intercept / post-restart.
8. **Download intercept is the preferred jump.** When the running
   snapd is already LTS-aware, `doDownloadSnap` fetches the tip of the
   **from** track (usually `latest`), reads `SNAPD_LTS_TRACKS` from that
   candidate, and if a jump is defined reroutes and **links the LTS
   blob**. The newer-than-maintenance snapd is only a map oracle; it
   never becomes current. That keeps state (and `patch.Level`) from
   moving forward onto a snapd that is functionally ahead of the frozen
   LTS branch. New LTS branches created later are advertised in the
   `latest` map; already-aware devices jump this way. Treat a successful
   intercept as: tracking is already the LTS channel (as if
   `snap switch --channel=<lts>` had just run), then finish under normal
   rules.
9. **Post-restart is only for unaware.** Intercept runs in the
   **running** daemon, so a shelf unit or old image still on unaware
   snapd cannot jump before link. It installs an aware snapd (still
   tracking `latest`), restarts, and **that** process starts the LTS
   refresh. This is the one path that *does* run a snapd newer than the
   maintenance branch; BB7 (patch-level / undo) matters most here. It is
   required for unaware, not a second preferred jump for aware devices
   (DESIGN.md BB5).
10. **Store planning does not perform either jump.**
    `resolveChannelForStore` and `initRefreshAllStoreUpdates` only do
    ordinary channel resolution (kernel/gadget pins, empty → `stable`).
    They do **not** rewrite onto the LTS track. That keeps the first
    store download on the from-track (usually `latest`) so intercept
    sees the newest map. Image/remodel seed rewrite (BB4a/BB4b) is a
    different path and still remaps `seed.yaml` / the new-model channel.
11. **Do not interfere when policy does not apply.** Unmapped track,
    unmanaged boot base, classic, UC16, and nil/out-of-scope model
    pass through. Branch is dropped on remap (LTS branches are not
    guaranteed). Holds and refresh-control only defer the next
    download; they are not an LTS opt-out.

## Jump paths

The two store mechanisms are not alternatives for the same event.

| Device | Jump | What is linked / run |
| --- | --- | --- |
| **Already LTS-aware**, new LTS branch defined in the `latest` map | **Download intercept** (preferred) | LTS blob. Candidate on `latest` is read and discarded if a jump is required. |
| **Unaware** (shelf, old image, first aware install) | **Post-restart** (alternative): link aware → restart → new process refreshes onto LTS | Aware snapd on `latest` runs briefly, then LTS. No intercept was possible in the unaware daemon. |

After a successful intercept, tracking is already the LTS channel; later
refresh-all is a normal LTS-track refresh. After the unaware bootstrap,
the post-restart refresh is what leaves `latest` — do not wait for
another `latest` revision or the next auto-refresh timer.

## Lifecycle stages

The operation sections below are grouped by **trigger** (CLI vs automatic
vs related). This table answers a different question: **when in a Core
device's life does this path run, and what can LTS do there?**

One operation may appear in more than one stage when the same code runs
at two times (e.g. seed apply in install-mode and again in recover).
Operation **20** (ubuntu-core → core) installs `core`, not snapd, and is
omitted.

| Stage | What LTS can do | Operations |
| --- | --- | --- |
| **1. Image build** — host-side `snap prepare-image` / ubuntu-image, before any device exists | Rewrite `seed.yaml`. Builder `--snap=snapd=<channel>` is a pin; model / image-wide channel is policy and remaps. | 26 |
| **2. Seed apply** — device consumes a seed (install-mode, first run-mode boot, recover, factory-reset) | Never retarget the local blob. Tracking is whatever the seed already says; a wrong seed heals on the next **store** refresh (stage 3). | 17 |
| **3. Run-mode store** — device is up and talking to the store (operator install/refresh, refresh-all, auto-refresh, validation-set enforce, cluster, missing-snapd prereq) | Aware: download intercept (preferred). Unaware: post-restart after the first aware install. Store planning does not jump. | 1, 2, 5 (store parent), 7, 8, 9, 10, 11, 15, 16, 19, 21 |
| **4. Run-mode local** — operator or API supplies a file/tree, or copies an installed blob | No `doDownloadSnap` retarget. `snap download` talks to the store on a host, not device `snapstate`. | 3, 4, 5 (local `.comp`), 6, 25 |
| **5. Remodel** — new model assertion while the device is running | New-model `default-channel` is policy (remap), not a user pin. Online uses stage-3 rules for the snapd download. Offline uses the stage-4 blob rule; tracking may still be rewritten. May also drive stages 6 and 2. | 12, 13; may also drive 14 and 18 |
| **6. Recovery packing** — writing a new recovery seed while already in run-mode (API create-system, remodel, essential-snap seed-refresh) | Distinct from seed apply: this *produces* a seed. Online download of missing snapd uses intercept/post-restart; installed/local copy is not retargeted. Booting that seed later is stage 2 again. | 14, 18 |
| **7. Tracking and deferral** — change tracking, cohort, or *when* the next refresh runs, without an LTS remap of a blob now | No LTS on this cycle. The next unheld store refresh (stage 3) still follows omit-channel rules. | 22, 23, 24 |

## How to read a row

Each operation lists **independent axes**, not a cartesian product. Values
on one line separated by `|` are mutually exclusive. Flags on different
lines usually combine unless marked illegal.

**LTS columns:**

| Value | Meaning |
| --- | --- |
| **Remap** | Image/remodel planning rewrites the channel onto the LTS track (BB4a `seedwriter`, BB4b remodel). Not used on store install/refresh. |
| **Keep** | Store planning leaves the resolved from-track / policy channel. Jump is intercept (aware) or post-restart (unaware). |
| **Intercept** | After download, `doDownloadSnap` retargets using the **candidate** squashfs map. Preferred jump when the running snapd is already LTS-aware. |
| **Skip** | This operation does not apply LTS. Requested channel/revision/blob is kept. |
| **Pass-through** | Policy does not apply (unmapped track, unmanaged base, out of scope, nil model). Planned channel unchanged. |
| **Fail closed** | Intercept (or BB4a/BB4b remap) still runs; unsatisfiable cohort or validation-set on the LTS track errors. No fallback to `latest`. |
| **N/A** | No store channel / no download of snapd. |

## Internal mechanisms

| Mechanism | Where | When it runs | Map source |
| --- | --- | --- | --- |
| Seedwriter (BB4a) | `seed/seedwriter.Writer.resolveChannel` | `snap prepare-image` / ubuntu-image | Running snapd (image builder) |
| Remodel (BB4b) | `devicestate` remodel snapd channel | Online remodel of snapd | Running snapd |
| Download intercept | `maybeRedirectSnapdToLTSTrack` in `doDownloadSnap` | After a **store** `download-snap` of asserted snapd, unless `ExplicitChannel` / `ExplicitRevision`. Preferred jump for **already-aware** snapd (new LTS branches in the `latest` map). | **Candidate** squashfs |
| Post-restart (BB5) | `SnapManager` StartUp / Ensure (not implemented) | After linking an **unaware→aware** snapd and restarting. Required for shelf / old-image units; not the preferred jump for aware devices. | Running (just-installed) snapd |

Store `resolveChannelForStore` / `initRefreshAllStoreUpdates` do **not**
LTS-remap. First `SnapAction` stays on the from-track so intercept can
read the newest map. Cohort/v-set fail-closed happens on the intercept's
second action when a jump is required.

**Always skip LTS (any mechanism):** not snapd; unasserted / empty SnapID;
path / sideload / try / seed blob; `ExplicitChannel`; `ExplicitRevision`;
classic / UC16 / out-of-scope model (`ErrLTSNotAllowed`); unmanaged base
or unmapped track (`ErrLTSBaseNotManaged` / `ErrLTSNoTrack`).

**Never skip LTS by themselves:** cohort, validation sets (revision pin
still wins the **blob**), `--ignore-validation`, holds (no download this
cycle; next unheld refresh can remap), confinement flags, aliases, quota,
transaction, instance key.

---

## User-initiated (CLI / REST)

### 1. Store install (one snap)

**Trigger:** `snap install NAME` · `POST /v2/snaps/{name}` `action=install`

**Calls:** daemon `revnoOpts` → `snapstate.Install` (`markExplicitPins`) →
`StoreInstallGoal` → `validateAndPrune` → `resolveChannelForStore` →
`doDownloadSnap`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Channel omitted (`snap install snapd`) | Keep | Yes if already aware | Default `stable` is **not** a pin. First action stays on `stable`/`latest`; intercept (aware) or post-restart (unaware) jumps to e.g. `18/stable`. |
| `--channel=TRACK[/RISK[/BRANCH]]` | Skip | Skip | Caller pin. Includes `--channel=latest/stable` and `--channel=18/edge`. |
| `--stable` / `--candidate` / `--beta` / `--edge` | Skip | Skip | Risk shortcut is a caller pin. New install: that risk on `latest` (then skipped, so stays `latest/<risk>`). |
| `--revision=N` (channel empty or set) | Skip | Skip | `ExplicitRevision`. Blob is `N`. |
| `--cohort=KEY` without channel | Keep | Fail closed if unsatisfiable | First `SnapAction` is from-track+cohort; intercept second action is LTS+cohort. |
| `--cohort=KEY` **with** `--channel=` | Skip | Skip | Channel pin wins. |
| `--ignore-validation` | Keep | Yes; second action ignores v-sets | Does not skip LTS. |
| `--classic` / `--devmode` / `--jailmode` / aliases / quota / transaction | Keep | Yes | LTS-irrelevant. |
| Parallel instance `snapd_foo` | Skip | Skip | Not the snapd snap (`InstanceName` split). |
| `NAME+comp` with snapd | Keep | Skip if snapd grows prereqs | Snapd has no components/prereqs today; intercept also skips if `Prereq` becomes non-empty. |

### 2. Store install (many snaps)

**Trigger:** `snap install A B C` · `POST /v2/snaps` `action=install` `snaps=[…]`

**Calls:** `InstallMany` (`markExplicitPins` per provided `RevisionOptions`) →
`StoreInstallGoal` → same as (1) per snap

CLI rejects channel/mode flags for multiple **store** names.

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Names only, snapd included | Keep snapd | Yes | Same as omitted-channel install of snapd. |
| REST per-snap `"channel"` on snapd | Skip | Skip | Caller pin (`ExplicitChannel`). |
| REST per-snap `"revision"` on snapd | Skip | Skip | Caller pin. |
| `--transaction` / `--ignore-running` / quota | Keep | Yes | LTS-irrelevant. |

### 3. Path / sideload install (one)

**Trigger:** `snap install PATH.snap` · `POST /v2/snaps` multipart sideload

**Calls:** `InstallPath` / `PathInstallGoal` → `resolveChannel` (**no** LTS
remap) → mount local blob. No `doDownloadSnap`.

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Asserted or unasserted `snapd_*.snap` | Skip | N/A | Blob cannot be retargeted. Channel, if any, is tracking only. |
| `--dangerous` / `--name=` / modes | Skip | N/A | LTS-irrelevant. |
| Channel/revision sent on the request | Skip | N/A | Still the local blob. |

### 4. Path / sideload install (many)

**Trigger:** `snap install a.snap b.snap`

Same as (3) per file. CLI rejects `--channel` when more than one snap
name is given. **Skip** / no intercept.

### 5. Install components onto an already-installed snap

**Trigger:** `snap install NAME+comp` · refresh with extra components

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Store `NAME+comp` when NAME is snapd | Same as (1) or (8) for the parent | Same as parent download | Components do not skip LTS on the parent snapd download. |
| Local `path.comp` | Skip | N/A | No snapd blob fetch. |

### 6. `snap try`

**Trigger:** `snap try [DIR]` · REST `action=try`

**Calls:** `TryPath`. Live mount of a tree. **Skip** / no intercept.

### 7. Refresh (one snap)

**Trigger:** `snap refresh NAME` · `POST /v2/snaps/{name}` `action=refresh`

**Calls:** `snapstate.Update` (`markExplicitPins`) → `StoreUpdateGoal` →
`resolveChannelForStore` → `doDownloadSnap`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Channel omitted (`snap refresh snapd`) | Keep | Yes | Uses tracking. Intercept jumps `latest/…` → LTS track, **same risk**. |
| `--channel=…` | Skip | Skip | Caller pin. `--channel=latest/edge` stays `latest/edge`. |
| `--edge` while tracking `18/stable` | Skip | Skip | Inherits track → planned `18/edge`, **not** remapped away, and not treated as `latest/edge`. |
| `--stable` / `--candidate` / `--beta` | Skip | Skip | Same inherit-track pin. |
| `--revision=N` | Skip | Skip | Blob is `N`. |
| `--amend` (unasserted → store) | Keep if no channel pin | Yes once asserted | Unasserted skip applies only while SnapID is empty. Amend onto LTS is **not** a special extra constraint. |
| `--cohort=KEY` without channel | Keep | Fail closed | From-track+cohort, then intercept LTS+cohort. |
| `--leave-cohort` | Keep | Yes | Cohort cleared; LTS still applies. |
| `--ignore-validation` | Keep | Yes | Does not skip LTS. |
| Modes / `--ignore-running` / transaction | Keep | Yes | LTS-irrelevant. |

### 8. Refresh (many named snaps)

**Trigger:** `snap refresh A B` · REST `snaps=[A,B]`

**Calls:** `UpdateMany` → `markExplicitPins` only if per-snap `revOpts`
were supplied (CLI many-refresh does not)

CLI rejects `--channel` / `--revision` / `--cohort` / `--amend` / modes
on this path.

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| snapd in the name list, no channel | Keep | Yes | Same as omitted-channel refresh of snapd. |
| REST per-snap channel on snapd | Skip | Skip | Caller pin. |

### 9. Refresh all installed snaps

**Trigger:** `snap refresh` (no names) · REST refresh with empty `snaps`

**Calls:** `UpdateMany(names=nil)` → `initRefreshAllStoreUpdates` (no
`markExplicitPins`) → store plan / possible `switch-snap-channel`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| snapd tracking `latest/…` | Keep | Yes if running is already aware and a download runs | Aware: intercept on the new `latest` blob (preferred). Unaware: link aware, restart, post-restart jump (BB5). Do not rely on planning or waiting for a further `latest` revision after that restart. |
| snapd already on LTS track | No-op / same-track refresh | Fast-path skip | Stay on LTS. |
| `--ignore-running` / `--transaction` | Keep | Yes | LTS-irrelevant. |
| `SNAP_REFRESH_FROM_TIMER=1` | N/A | N/A | CLI ignores; systemd must not drive CLI refresh-all. |

### 10. Refresh to enforce validation sets (REST)

**Trigger:** `POST /v2/snaps` `action=refresh` `validation-sets=[…]` (no snap names)

**Calls:** `ResolveValidationSetsEnforcementError` → install and/or update
with `ValidationSets` on `RevisionOptions` (not a user `--channel`)

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| snapd constrained, no revision pin | Keep | Fail closed | From-track + v-sets; intercept second action is LTS + v-sets. |
| snapd revision-pinned by the set | Keep channel; **revision wins blob** | Fail closed if pin ∉ LTS | `completeStoreAction` clears channel and sends the pinned revision. Intercept honours enforced sets on the second action. |
| `--ignore-validation` is not this API | — | — | This path exists to **enforce**. |

### 11. `snap validate --enforce`

**Trigger:** `snap validate --enforce account/name[=N]` · REST validation-sets `action=enforce`

Same store install/refresh as (10) for constrained snaps. **`--monitor` /
`--forget`:** no install/refresh, LTS N/A.

Snaps with sticky ignore-validation are skipped for that constraint
(existing v-set rule, not an LTS skip).

### 12. Remodel (online)

**Trigger:** `POST /v2/model` JSON

**Calls:** BB4b `ltstrack.Resolve(newModel, modelDefaultChannel, nil)` then
store install/refresh of missing/changed snaps (including snapd)

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| New model `default-channel` for snapd (`latest` or omitted) | Remap (BB4b) | Yes | Model default is **not** a user pin. |
| New model already on LTS track | Keep | Fast-path | Identity. |
| Unmapped track on new model | Pass-through | Pass-through | `ErrLTSNoTrack`. |
| Local snaps on online remodel | Illegal | — | API rejects. |

### 13. Remodel (offline)

**Trigger:** `POST /v2/model` multipart + local snaps

**Calls:** `PathUpdateGoal` / reuse installed blobs. **Skip** retarget.
Tracking may still be rewritten from the new model (BB4b planning on the
channel string); the **blob** is local.

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Local snapd blob | No blob remap | N/A | Same as path install. |
| Reuse installed snapd | Channel may remap in tasks | N/A unless a store download is added | Offline by definition uses local/installed copies. |

### 14. Create recovery system

**Trigger:** `POST /v2/systems` · also during remodel / seed-refresh

**Calls (online):** `snapstate.Download(…, Channel: model.DefaultChannel)`
with `ExplicitChannel` false → `downloadTasks` → `resolveChannelForStore`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Online, snapd missing, model `default-channel: latest` (or implicit) | Keep | Yes | First download is model/from-track; intercept (aware) or post-restart packs LTS snapd. |
| Online, snapd already installed at a valid revision | N/A (local copy) | N/A | Uses the installed blob. |
| Validation-set revision pin | Keep channel; revision wins blob | Fail closed | Same as (10). |
| Offline + `LocalSnaps` | Skip | N/A | Local blob. |

---

## Automatic / internal

### 15. Auto-refresh

**Trigger:** `SnapManager.Ensure` → `autoRefresh`

**Calls:** same as (9) (`initRefreshAllStoreUpdates`), `IsAutoRefresh`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Scheduled refresh of snapd | Keep | Yes if already aware | Same as refresh-all. Unaware bootstrap is post-restart (BB5), not this timer. |
| Continued after run-inhibit | Keep | Yes | Still not a user `--channel`. |
| Gated (`gate-auto-refresh` hold) | No download this cycle | N/A | Next proceed/unhold can remap. |
| Pre-download (busy snap) | Keep on the later refresh | When the real download runs | Pre-download does not skip LTS policy on the eventual switch. |

### 16. Re-refresh (epoch hop)

**Trigger:** `check-rerefresh` after install/refresh

Inherits parent flags. Snapd epoch is 0 today → **N/A**. If it ever hops
via store refresh, same rules as the parent change (auto vs explicit
channel).

### 17. Firstboot / install-mode / recover / factory-reset seed install

**Trigger:** `populateStateFromSeed` / `SeedingGoal`

**Always local `sn.Path`.** Never the store at install time.

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Any mode (run / install / recover / factory-reset) | Skip | N/A | Tracking comes from `seed.yaml` (already remapped at image build if BB4a ran). Wrong seed channel: first store refresh of snapd, then intercept (if already aware) or post-restart (if this install was unaware→aware). |

### 18. Seed refresh of gadget / kernel / base / snapd

**Trigger:** essential-snap refresh on UC20+ may create a recovery system

The **refresh** of snapd is (7)/(9)/(15). Packing the new seed is (14)
(online download or current blobs). Same LTS rules as those rows.

### 19. Prerequisite / default-provider install

**Trigger:** `prerequisites` task during another install/refresh

**Calls:** `StoreInstallGoal` / update with **no** `markExplicitPins`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Missing snapd, `$SNAPD_SNAPD_CHANNEL` unset | Keep | Yes | Empty channel; same as `snap install snapd`. (Today `considerSnapdAsPrereq` is classic-only; classic is out of LTS scope until enabled.) |
| `$SNAPD_SNAPD_CHANNEL` set | Skip | Skip | Test/image override; `ExplicitChannel: true`. |
| Missing base / default-provider (not snapd) | N/A | N/A | Not snapd. |
| Content-provider **update** | N/A unless the provider is snapd | — | Snapd is not a content provider. |

### 20. ubuntu-core → core transition

**Trigger:** `ensureUbuntuCoreTransition`

Installs **core**, not snapd. **N/A**.

### 21. Cluster assertion apply

**Trigger:** `ClusterManager.Ensure`

**Calls:** `StoreSnap` / `StoreUpdate` with cluster channel,
`ExplicitChannel` false → `InstallWithGoal` / `UpdateWithGoal`

| Permutation | Planning | Intercept | Expected |
| --- | --- | --- | --- |
| Missing clustered snapd, channel `latest/…` | Keep | Yes | Signed policy, not a caller pin; intercept/post-restart jump. |
| Installed snapd tracking `18/stable`, cluster says `latest/stable` | Keep cluster channel | Yes if download | Inequality still schedules an update; intercept jumps if the candidate map requires LTS. |
| Unmapped cluster track | Pass-through | Pass-through | `ErrLTSNoTrack`. |
| Remove (`ClusterSnapState` removed) | N/A | N/A | Not an install/refresh. |

---

## Related (not an install/refresh of a blob)

### 22. `snap switch`

**Calls:** `Switch` → `resolveChannel` (kernel/gadget model pins only).
**No LTS remap. No intercept** (no download).

| Permutation | Expected |
| --- | --- |
| `snap switch snapd --channel=latest/stable` | Tracking becomes `latest/stable`. Next store refresh **without** `--channel` remaps back. |
| `--cohort` / `--leave-cohort` | Tracking/cohort only. |

### 23. `snap revert`

Already-downloaded revision. **N/A**.

### 24. Hold / unhold / `--list` / `--time` / `--tracking`

No install/refresh. A **hold** defers the next auto-refresh; it does not
skip LTS on the refresh that eventually runs.

### 25. `snap download`

Client talks to the store directly (not `snapstate.Download` on device
context). **No LTS remap** in snapd. Channel/revision/cohort select the
downloaded files only.

### 26. `snap prepare-image` / ubuntu-image (BB4a)

**Calls:** `seedwriter.Writer.resolveChannel` after `channel.Resolve` /
`channel.ResolvePinned`

| Permutation | Planning (seed.yaml) | Intercept | Expected |
| --- | --- | --- | --- |
| Model `default-channel` omitted or `latest/…` | Remap | N/A (image build) | e.g. `18/stable`. Becomes firstboot tracking. |
| Image-wide `--channel=candidate` | Remap | N/A | `18/candidate` (risk kept, track remapped). |
| `--snap=snapd` (no channel) | Remap | N/A | Same as model/image default. |
| `--snap=snapd=edge` or `=latest/candidate` | Skip LTS after track-inherit | N/A | Builder pin, same as CLI `--channel=`. `edge` inherits model track then **skips** LTS. |
| `--snap=/path/snapd.snap` | Skip | N/A | Local blob. |
| Unasserted snapd in model | Skip | N/A | Empty SnapID. |
| UC16 (`base` empty or `core`) | Skip | N/A | No separate snapd snap. |
| Unmapped track | Pass-through | N/A | Keep planned channel. |

---

## Cross-cutting pins (any store path)

These combine with the rows above.

| Pin | Skip LTS? | Notes |
| --- | --- | --- |
| Caller `--channel=` / API `"channel"` / risk shortcut / `$SNAPD_SNAPD_CHANNEL` / `--snap=snapd=<channel>` | **Yes** | `ExplicitChannel` |
| Caller `--revision=` / API `"revision"` | **Yes** | `ExplicitRevision`; blob is `N` |
| Validation-set revision | **No** (channel still jumps) | Revision wins the blob; fail closed if `N` is not on LTS |
| Cohort | **No** | LTS+cohort; fail closed if unsatisfiable |
| `--ignore-validation` | **No** | Second intercept action also ignores v-sets |
| Hold / refresh-control | Deferred | No download this cycle |
| `patch.Level` too old on LTS blob | Fail | BB7 pre-flight on intercept |
| Unmapped track / unmanaged base / classic / UC16 | Pass-through | Do not interfere |

## Evaluation status

Walked against the omit-channel / explicit-revision rules. After the
prereq / create-recovery / cluster / prepare-image fix, the leftover
gaps in DESIGN.md question 10 are **closed**.

**Jump paths:** download intercept is implemented (preferred for
already-aware). Post-restart for unaware (BB5) is **required** and not
implemented. Product/spread leftovers: missing-map failure mode, Case 3
firstboot until the first store refresh, UC039 ordering if a refresh-all
is still in flight at unaware→aware restart — see DESIGN.md §7 step 14.
