# snapd Development Guide for AI Agents

## Project Overview

**snapd** is the background daemon that manages snap packages across Linux distributions. It's written in Go and consists of a daemon (`snapd`), CLI client (`snap`), and sandbox execution components (`snap-confine`, `snap-exec`).

## Architecture Fundamentals

See `ARCHITECTURE.md` for detailed diagrams and explanations.

### Overlord & State Managers

The core architecture is based on `overlord.Overlord` which coordinates **state managers**:

- **State managers** implement `StateManager` interface with `Ensure()`.
  Managers may optionally define the methods `StartUp()` or `Stop()` as defined
  by the `StateStarterUp` and `StateStopper` interfaces, respectively.
- All operations are persisted to survive reboots via `overlord/state.State` (backed by `state.json`)
- **Operations are modeled as `state.Change` → graph of `state.Task`** with do/undo handlers
- `state.TaskRunner` executes tasks, spawning goroutines for handlers

**Key state managers:**
- `overlord/snapstate`: Snap lifecycle (install/remove/update), manages `SnapState` per snap
- `overlord/ifacestate`: Interface connections, security profiles via `interfaces.Repository`
- `overlord/assertstate`: Signed assertion database (snap-declaration, snap-revision)
- `overlord/devicestate`: Device identity, registration, Ubuntu Core installation/remodeling
- `overlord/hookstate`: Snap hook execution

**Critical import rules:**
- `snapstate` is imported BY other managers but CANNOT import them
- Circular imports broken via function hook variables (see `snapstate.ValidateRefreshes`)
- Hook variables must be assigned in `init()` or `Manager()` constructors

### Snap Execution Pipeline

```
snap run <snap.app>  →  exec(snap-confine)  →  exec(snap-exec)  →  actual app
                        [sandbox setup]        [final prep]
```

`snap-confine` uses capabilities to set up mount namespace, AppArmor profiles,
then execs `snap-exec` which runs the actual snap binary.

### Task Handler Pattern

Task handlers follow strict conventions:
```go
func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
    st := t.State()
    st.Lock()
    defer st.Unlock()  // Auto-commits state changes

    // Extract parameters from task
    var snapsup SnapSetup
    t.Get("snap-setup", &snapsup)

    // Slow operations (I/O, network) require unlocking:
    st.Unlock()
    defer st.Lock()
    // ... copy/download/mount operations ...

    // Non idempotent operations need to set task status before returning.
    st.Lock()
    t.SetStatus(state.DoneStatus)
    st.Unlock()

    return nil  // TaskRunner sets task to DoneStatus
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
    // Undo must be symmetric and idempotent
}
```

**State locking rules:**
- Start handlers with `st.Lock(); defer st.Unlock()`
- Release lock only for slow I/O/network operations
- Working state + status changes must be atomic via `Task.SetStatus()` before unlock

## Developer Workflows

### Building & Testing

**Build natively:**

Note that while there are many binaries, usually you only need `snap` and `snapd` for development. Many go binaries have special build rules (.e.g precise static linking). Snapd can be built with keys to the production
snap store, or with test keys that allow installing snaps
signed with the well-known, insecure test key.

Building several elements of snapd individually:
```bash
go build -o /tmp/build/snap ./cmd/snap
go build -o /tmp/build/snapd ./cmd/snapd
go build -o /tmp/build ./...  # All binaries
```

You may want to build the snapd snap package with `snapcraft pack` instead, as that constructs a complete, cohesive set of programs.

**Run checks (required before commits):**
```bash
./run-checks  # Runs: go fmt, go vet, golangci-lint, unit tests, static checks
```

**Unit tests:**
```bash
go test -check.f TestName  # Run specific test
go test -v -check.vv       # Verbose mode for debugging hangs
LANG=C.UTF-8 go test       # Required locale for many tests
make -C cmd check          # C unit tests
```

**Integration tests (spread):**
```bash
./run-spread garden:ubuntu-22.04-64        # Builds test snapd snap automatically
NO_REBUILD=1 ./run-spread garden:...       # Skip rebuild when iterating
./run-spread -reuse garden:ubuntu-22.04-64 # Reuse systems (faster iteration)
```

### Snapcraft Build

Build snapd snap (preferred for testing):
```bash
snapcraft  # Uses build-aux/snap/snapcraft.yaml
sudo snap install --dangerous snapd_*.snap
```

## Coding Conventions

Abbreviated coding conventions. See `CODING.md` for details.

### Naming & Style

- Follow `gofmt -s` (enforced by `run-checks`)
- Error messages: lowercase, no period, "cannot X" not "failed to X"
- Error types: introduce `*Error` structs only when callers need to inspect them
- Check similar code in same package for naming consistency

### Package Structure

- Packages should have clear, focused responsibilities at consistent abstraction levels
- Abstract/primitive packages at bottom (e.g., `boot` → `bootloader`)
- Application-specific packages at top (e.g., `snapstate` → `snapstate/backend` → `boot`)
- **`*util` packages**: Cannot import non-util packages, minimize dependencies
- **`overlord/*state` packages**: Only for snapd daemon, not imported by CLI tools
  - Exception: subset of `overlord/configstate/configcore` (via `nomanagers` build tag)

### Error Handling

```go
// Prefix internal programming errors
return fmt.Errorf("internal error: unexpected state %v", state)

// Use "cannot" for user-facing errors
return fmt.Errorf("cannot install snap: %v", err)

// Keep error chains concise—avoid "cannot: cannot: cannot"
```

### Testing Patterns

**Use `gocheck` (not stdlib testing):**
```go
package mypackage_test  // Test from exported API perspective

import . "gopkg.in/check.v1"

type mySuite struct{}
var _ = Suite(&mySuite{})

func (s *mySuite) TestFeature(c *C) {
    c.Assert(value, Equals, expected)
}
```

Note that as a special exception, benchmarks are expected
to use stdlib `testing` package.

**Export internals via `export_test.go`:**
```go
// export_test.go (in package mypackage)
var TimeNow = timeNow  // Export unexported var for testing

func MockTimeNow(f func() time.Time) (restore func()) {
    restore = testutil.Backup(&timeNow)
    timeNow = f
    return restore
}
```

**Test requirements:**
- Test both `do` and `undo` task handlers symmetrically
- Verify handlers are idempotent (can safely re-run after partial execution)
- Test error paths that perform cleanup
- Minimize mocking—mock at system boundaries (systemd, store) not internal packages
- Use `<package>test` helpers (e.g., `assertstest`, `devicestatetest`) for complex fixtures

**Spread test section order (enforced by CI):**
1. `summary` (required)
2. `details` (required)
3. `backends`, `systems`, `manual`, `priority`, `warn-timeout`, `kill-timeout`
4. `environment`, `prepare`, `restore`, `debug`
5. `execute` (required)


**Running specific tests:**

To run specific go tests using the check framework, use commands like this:

```
go test -v "${package_path}" -check.v -check.f "${test_pattern}"
```

## PR & Commit Guidelines

**PR format:**
- Title: `affected/packages: short summary in lowercase`
- Keep diffs ≤500 lines (split if larger)
- Separate refactoring from behavior changes
- Refactoring must not touch tests unless unavoidable

**Commit messages:**
```
overlord/snapstate: add helper to get gating holds
gadget,image: remove LayoutConstraints struct
o/snapstate: add user and gating holds helpers  # Abbreviate when obvious
many: correct struct fields and output keys     # Many packages affected
spread: remove old release of distribution      # spread.yaml affected
```

**Merging strategy:**
- **Prefer "Squash and Merge"** (simplifies cherry-picking)
- Use "Rebase and Merge" only when commit history is valuable
- Never use "Create a merge commit"

## Key Files & Patterns

- **`overlord/README.md`**: Deep dive on state managers, task lifecycle, conflicts
- **`ARCHITECTURE.md`**: Entry points, execution pipeline, manager responsibilities
- **`CODING.md`**: Full coding conventions, error handling, testing philosophy
- **`spread.yaml`**: Integration test configuration with backend definitions
- **Task parameters**: Via `task.Get("snap-setup", &snapsup)` as `SnapSetup` structs
- **Manager caching**: `state.State.Cache()` with private keys for manager instances

## Debugging

**Debug snapd daemon:**
```bash
sudo systemctl stop snapd.service snapd.socket
sudo SNAPD_DEBUG=1 SNAPD_DEBUG_HTTP=3 ./snapd
# SNAPD_DEBUG_HTTP: 1=requests, 2=responses, 4=bodies (bitfield)
```

**Debug snap CLI:**
```bash
SNAP_CLIENT_DEBUG_HTTP=7 snap install ...  # Same bitfield as above
```

## Developing Interfaces

Interfaces define how snaps access system resources and interact with each other. Each interface consists of plugs (consumers) and slots (providers).

### Interface Structure

**Every interface must:**
- Be registered via `registerIface()` in its `init()` function
- Implement the `Interface` interface (at minimum `Name()` and `AutoConnect()`)
- Live in `interfaces/builtin/` package

**Common interface pattern:**
```go
type myInterface struct {
    commonInterface  // Embeds standard behavior
}

func (iface *myInterface) Name() string {
    return "my-interface"
}

func init() {
    registerIface(&myInterface{commonInterface{
        name:                 "my-interface",
        summary:              "allows access to X",
        implicitOnCore:       true,
        baseDeclarationPlugs: myBaseDeclarationPlugs,
        baseDeclarationSlots: myBaseDeclarationSlots,
    }})
}
```

### Security Backend Methods

Interfaces generate security profiles by implementing backend-specific methods:

- **`AppArmorConnectedPlug/Slot`**: AppArmor rules when connected
- **`AppArmorPermanentPlug/Slot`**: AppArmor rules always present
- **`SecCompConnectedPlug/Slot`**: Seccomp syscall filters when connected
- **`UDevConnectedPlug/Slot`**: UDev rules for device access
- **`KModConnectedPlug/Slot`**: Kernel modules to load

**Example AppArmor snippet:**
```go
func (iface *myInterface) AppArmorConnectedPlug(spec *apparmor.Specification,
    plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
    spec.AddSnippet("/dev/my-device rw,")
    return nil
}
```

### Base Declaration Policy

Base declarations define default connection/installation policies:

```go
const myBaseDeclarationPlugs = `
  my-interface:
    allow-installation: false  # Super-privileged, needs snap-declaration
    deny-auto-connection: true # Manual connection required
`
```

**Policy evaluation order (first match wins):**
1. `deny-*` in plug snap-declaration
2. `allow-*` in plug snap-declaration
3. `deny-*` in slot snap-declaration
4. `allow-*` in slot snap-declaration
5. `deny-*` in plug base-declaration
6. `allow-*` in plug base-declaration
7. `deny-*` in slot base-declaration
8. `allow-*` in slot base-declaration

### Sanitizers for Validation

Implement sanitizers to validate plug/slot attributes:

```go
func (iface *myInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
    path, ok := plug.Attrs["path"].(string)
    if !ok || path == "" {
        return fmt.Errorf("my-interface must contain path attribute")
    }
    return nil
}
```

### Interface Testing Requirements

- Test both plug and slot sides
- Test connection scenarios
- Test AppArmor/seccomp snippet generation
- Verify base declaration policy evaluation
- Use `ifacetest.BackendSuite` for backend tests

### Key Files

- **`interfaces/builtin/README.md`**: Complete policy evaluation guide
- **`interfaces/core.go`**: Core interface types and sanitizer interfaces
- **`interfaces/builtin/common.go`**: `commonInterface` with standard behavior
- **`interfaces/repo.go`**: Interface repository managing connections

## Common Patterns to Follow

1. **State transitions are persistent**: All `state.Change` and `state.Task` survive restarts
2. **Task handlers are retriable**: Design for idempotency
3. **Device context is contextual**: Use `DeviceCtx(task)` in handlers, not `DeviceCtxFromState()`
4. **Conflicts prevent concurrent ops**: Check `snapstate/conflict.go` for snap operation serialization
5. **Backend abstraction**: Use `snapstate/backend` for disk state, never manipulate directly
6. **Interface security profiles are additive**: Each connected interface adds to AppArmor/seccomp profiles
