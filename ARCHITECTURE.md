The snap daemon, `snapd`, together with all the  other binaries defined in this repository, manage the installation, lifecycle and updates of software packaged as a *snap* across many Linux distributions.

A snap package is a self-contained read-only SquashFS file carrying application-specific content alongside metadata, chiefly in `meta/snap.yaml`. When installing a snap, snapd ensures that the SquashFS content will be available by mounting it.

Alongside individual snaps, snapd can also orchestrate the lifecycle of an entire  system when all the components are snaps. This is the principle behind Ubuntu Core systems. There the root filesystem is a *base* snap mounted in combination with  writable space, alongside a kernel installed from its own snap. 

For security, snap applications and services are executed in a sandbox by default. Access to system resources, and interactions with other snaps, are mediated via so called *interfaces*. Each interface encapsulates an access policy that's implemented using mount namespaces, AppArmor profiles and other native Linux security features.

For robustness, snapd ensures that all operations either succeed or revert their changes to the previous state of the system, even in the face of restarts, reboots or failures.  To achieve this robustness, much of both the internal state and operational state of snapd is persisted to disk (as `overlord/state.State`).

All the binaries, and their entry points, are defined under the `cmd` package. It contains `cmd/snap` for the `snap` command and daemon client, and both  `snap-confine` and `snap-exec` to handle the execution pipeline for snaps, alongside the `snap run` subcommand.

## **Entry points and the execution pipeline**

Entry points for launching software in a snap are mainly either: 

* symlinks to the `snap` command from `/snap/bin`  
* systemd units for services that invoke explicitly the `snap run` command with the snap  service  reference information

In both cases,execution starts within the `snap run` command provided with the application (via the symlink) or the service (provided explicitly) reference information.

`snap run` reads the needed metadata and prepares the command line to run exec `snap-confine. snap-confine` is the binary responsible for setting up the execution sandbox, preparing mount namespace and activating sandbox profiles.

When run, `snap-confine` uses *capabilities (in the kernel sense)* to perform the set-up operations. It then relinquishes them before proceeding, as per the command line, to run exec `snap-exec. snap-exec` is responsible for the final setup within the sandbox before running exec with the actual snap target binary.

## **Overlord and state managers**

*See also the overlord package  [README](https://github.com/canonical/snapd/blob/master/overlord/README.md).*

snapd execution is orchestrated by `overlord.Overlord` and the *state managers* under it. These are initialized and driven by `Overlord`  via the `StateManage`r interface.

Execution comprises of:

* a start up phase when `StateManager.StartUp` is called on all managers  
* the *ensure loop* is then called at least once every 5 minutes,  or repeatedly  when there are operations to complete  
* each iteration of the ensure loop calls the `StateManager.Ensure` methods for all the state managers  
* on shutdown, the `StateManager.Stop` method is called on all state managers

`The StateManager.Ensure` methods implement small state machines that first  check if any transition requiring a system change is necessary and secondly set up the corresponding change. The regular querying of the store and snap updates are implemented in this way, for example.

Any system change operation is realized as a set and dependency graph of tasks. Each state manager implements different sets of *task kinds*, with each responsible for a relatively orthogonal set of concerns and behaviors. The graph and tasks are realized as `state.Change` and `state.Task, which` are persisted to survive reboots and restarts. `state.TaskRunner` is the execution engine of `state.Changes` and is wired in the ensure loop as a manager itself.

The `overlord/snapstate` manager is responsible for:

* managing the snapd persisted  internal state for each installed snap (see `snapstate.SnapState`)   
* implementing tasks for their lifecycle and the lifecycle of their *components*, if any.  
* ensure logic for regular automatic updates  
* keeping the external system state for snaps consistent.

The `overlord/snapstate` manager task handlers use helpers from `snapstate/backend` to influence external on-disk snap state. The `backend` in turn uses the `wrappers` package to maintain the linkage data that exposes a snap to the system, be it applications and their alias symlinks in `/snap/bin`, systemd units, or desktop integration for the snap. 

`The overlord/snapstate` task handlers get their parameters from their `state.Task` data chiefly as `snapstate.SnapSetup`.

`snapstate.DeviceContext` is an interface defined by `snapstate` to access relevant information about the device/system. Concrete implementations are supplied by `overlord/devicestate`. This interface is used across all managers and beyond.

Important information provided though `snapstate.DeviceContext`  includes the `snapstate.StoreService` (implemented by `store.Store`), which is used to access the snap store, and the model (as `asserts.Model`) of the device. `snapstate` provides various ways to get hold of a `DeviceContext`: `DeviceCtx` for use in task handlers, or `DeviceCtxFromState` and `DevicePastSeeding` for outside. Task handlers need to use a mechanism that takes the `Task` itself. This is because the `DeviceContext` might be contextual to a `Change`, due to the way some operations deal with transitions to different device models (so called *remodel* operations), and the `DeviceContext` within  them refers to the transitioned-to model and corresponding store. 

Paradigmatic tasks for `snapstate` include `mount-snap` (handlers: `SnapManager.doMountSnap, SnapManager.undoMountSnap`) and `link-snap` (handlers: `SnapManader.doLinkSnap, SnapManager.undoLinkSnap`).

Snap metadata, as used throughout snapd and some basic operation helpers, are defined in the `snap` package. This metadata usually comes from parsing `snap.yaml` files.

The `overlord/ifacestate` manager using the `interfaces` package is responsible for keeping  the per-snap security profiles up-to-date used to sandbox them.  It ensures that interface connections  (persistently modeled by `ifacestate.ConnectionState`) are correctly reflected in the profiles. The `interfaces.Repository` is the main abstraction to manage reflecting those connections, while `interfaces/builtin` carries each interface logic implementation. 

Paradigmatic handlers for `ifacestate` include `setup-profiles`, `auto-connect` and `connect`.

Assertions are signed documents used to carry policy or verification information. The `overlord/assertstate` manager, using the `asserts` package, is responsible for maintaining the system assertion database. This includes updating and retrieving assertions, as needed, and to verify snaps. The `snap-declaration` assertion`,` for example, carries identity and sandbox policy information for a snap, while `snap-revision` carries verification information for a specific snap revision.

Paradigmatic functionality in `assertstate` is `DB` to get read-only access to the database, more direct retrieval helpers (e.g. `SnapDeclaration`) , `RefreshSnapAssertions` and the `verify-snap` task.

More state managers exist to cover other aspects of snaps (`hookstate` for hooks, etc.) 

The import dependencies for the manager packages code are fairly dense. As it defines some fundamental shared types and mechanisms, `snapstate` is imported by many other managers.

At the other end of the import/export scale, `devicestate` *uses* most of the other managers.  Reverse dependencies are addressed by having either:

* the manager initialization injects functional hooks into other manager packages.  
* subpackages in the managers that expose an external facade of useful functionality (`configstate/config` is an example of the latter).

Many managers cache their instance with `overlord/state.State.Cache,` using a private unique key so that is accessible from their top-level functions with signatures like  `mgrstate.Func(st *state.State…)`.

## **Asking snapd to do something**

snapd provides a HTTP API over the `/run/snapd.socket` unix socket. This is how most of `snap's` own command operations are requested. 

The API is implemented by the `daemon` package. In most cases, its code fulfills requests by using helpers provided by the `overlord` state managers. These are used to build a `state.TaskSet` for a given operation (for example `ifacetate.Connect` for interface connection, or `snapstate.InstallWithGoal` for installing) and populating a `state.Change` ready for execution with those tasks. The API then produces a so-called "async" response with status `202 Accepted` and  including the `Change id` for tracking via `/v2/changes/{id}` endpoint of the API.

Snap can make requests to snapd via the `snapctl` command, which internally uses the `/v2/snapctl` endpoint over the `/run/snap-snapd.socket` unix socket. The command itself does very little other than forward parameters to the endpoint and directing output and exit codes back from its response. The implementation logic for the various `snapctl` subcommands lives in `hookstate/ctlcmd`.

## **Devices and boot support** 

On Ubuntu Core devices, where kernel and boot assets are provided by snaps, snapd is responsible for configuring the boot process and then for the full lifecycle of the device. On other devices, snapd is responsible for the parts of the device lifecycle and device identity as related to snaps.

`overlord/devicestate` is the state manager responsible for orchestrating all of this. For example, it has code to drive full device installation from a never-before-booted Ubuntu Core image or for a more limited installation of a set of snaps in a system on first boot, from a so-called *seed system* configuration. It also has code to register and give an identity (`asserts.Seria`l tied to the device `asserts.Model`)  to the device with device services and/or the store.

For boot and disk configuration, as well as boot assets management, `devicestat`e and `snapstate` code  use functionality  from the `boot`, `gadget` and `kernel`  packages. Bootloader specific code lives in `bootloader` and is mostly used via `boot` and not directly.  
 