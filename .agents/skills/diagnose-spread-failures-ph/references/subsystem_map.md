# Snapd Subsystem-to-Test Map

This reference maps snapd source code areas to their relevant spread test suites, and vice versa. Use it during failure correlation to identify related code areas **beyond** the files directly changed in a PR.

## How to Use This Map

When a test fails, look up its test suite prefix in this document to find:
- The source packages most likely to affect it
- Related subsystems that might indirectly cause regressions

When a PR changes a source package, look up the package to find:
- The test suites most likely to regress
- Related packages that often change together

## Test Suite → Source Packages

### Core Lifecycle Tests

| Test Suite | Primary Source Packages | Related Areas |
|:---|:---|:---|
| `tests/main/install*` | `overlord/snapstate/`, `store/`, `snap/` | `boot/`, `image/`, `seed/` |
| `tests/main/refresh*` | `overlord/snapstate/`, `store/`, `snap/` | `boot/`, `image/`, `daemon/` |
| `tests/main/remove*` | `overlord/snapstate/`, `snap/` | `boot/`, `interfaces/` |
| `tests/main/revert*` | `overlord/snapstate/`, `snap/` | `store/` |
| `tests/main/snap-run*` | `cmd/snap-exec/`, `cmd/snap-confine/`, `osutil/` | `interfaces/`, `systemd/` |
| `tests/main/snapd-reexec*` | `cmd/snap-exec/`, `cmd/snap-confine/`, `cmd/snapd/` | `osutil/`, `boot/` |
| `tests/main/services*` | `systemd/`, `overlord/snapstate/`, `daemon/` | `overlord/hookstate/` |
| `tests/main/snap*` (general snap CLI) | `cmd/snap/`, `client/`, `daemon/` | `overlord/snapstate/` |

### Interface Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/main/interfaces-*` | `interfaces/builtin/`, `overlord/ifacestate/` | Each interface test usually maps to `interfaces/builtin/<name>.go` |
| `tests/main/interfaces-network*` | `interfaces/builtin/network*.go`, `overlord/ifacestate/` | |
| `tests/main/interfaces-hardware*` | `interfaces/builtin/hardware*.go`, `interfaces/builtin/serial*.go` | |
| `tests/main/interfaces-desktop*` | `interfaces/builtin/desktop*.go`, `interfaces/builtin/unity*.go` | |
| `tests/main/interfaces-snapd*` | `interfaces/builtin/snapd*.go` | |

### Boot / Ubuntu Core Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/main/uc20*` | `boot/`, `gadget/`, `core-initrd/` | UC20+ boot process |
| `tests/main/uc22*` | `boot/`, `gadget/`, `core-initrd/` | |
| `tests/main/uc24*` | `boot/`, `gadget/`, `core-initrd/` | |
| `tests/main/core20*` | `boot/`, `gadget/`, `image/`, `core-initrd/` | |
| `tests/main/core22*` | `boot/`, `gadget/`, `image/`, `core-initrd/` | |
| `tests/main/core24*` | `boot/`, `gadget/`, `image/`, `core-initrd/` | |
| `tests/main/core-services*` | `systemd/`, `overlord/snapstate/`, `daemon/` | systemd service management on Core |
| `tests/main/initrd*` | `core-initrd/` | Initrd build and boot |
| `tests/main/gadget*` | `gadget/`, `image/` | Gadget asset handling |
| `tests/main/preseed*` | `image/`, `seed/`, `overlord/devicestate/` | Image preseeding |
| `tests/main/seed*` | `seed/`, `image/` | Seed metadata |

### Nested / Device Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/nested/core/*` | `boot/`, `gadget/`, `image/`, `core-initrd/`, `overlord/devicestate/` | Full Core image lifecycle |
| `tests/nested/manual/core20*` | `boot/`, `gadget/`, `image/`, `core-initrd/` | |
| `tests/nested/manual/core22*` | `boot/`, `gadget/`, `image/`, `core-initrd/` | |
| `tests/nested/manual/core24*` | `boot/`, `gadget/`, `image/`, `core-initrd/` | |
| `tests/nested/manual/muinstaller*` | `image/`, `boot/`, `gadget/`, `overlord/devicestate/` | Installer tests |
| `tests/nested/manual/remodel*` | `overlord/devicestate/`, `boot/`, `gadget/`, `image/` | Device remodeling |
| `tests/nested/manual/uc-update*` | `boot/`, `gadget/`, `image/` | UC asset updates |
| `tests/nested/manual/refresh-revert*` | `overlord/snapstate/`, `boot/`, `gadget/` | |

### Security Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/main/apparmor*` | `interfaces/apparmor/`, `interfaces/builtin/` | AppArmor profile generation |
| `tests/main/seccomp*` | `interfaces/seccomp/`, `interfaces/builtin/` | Seccomp filter generation |
| `tests/main/security*` | `interfaces/`, `cmd/snap-confine/`, `osutil/` | General security |
| `tests/main/udev*` | `interfaces/udev/`, `interfaces/builtin/` | UDev rules |
| `tests/main/mount*` | `osutil/mount/`, `cmd/snap-confine/`, `systemd/` | Mount namespace handling |

### Daemon / API Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/main/snapd` | `daemon/`, `cmd/snapd/` | Daemon startup and lifecycle |
| `tests/main/rest-api*` | `daemon/`, `client/` | REST API behavior |
| `tests/main/assertions*` | `asserts/`, `daemon/`, `overlord/assertstate/` | Assertion handling |
| `tests/main/store*` | `store/`, `daemon/`, `client/` | Store interactions |
| `tests/main/snap-download*` | `store/`, `client/`, `daemon/` | Snap download logic |

### Device / Identity Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/main/device*` | `overlord/devicestate/`, `asserts/`, `daemon/` | Device registration |
| `tests/main/serial*` | `overlord/devicestate/`, `asserts/` | Serial assertion |
| `tests/main/system*` | `overlord/configstate/`, `systemd/`, `daemon/` | System configuration |

### Packaging / Build Tests

| Test Suite | Primary Source Packages | Notes |
|:---|:---|:---|
| `tests/main/build*` | `cmd/`, `packaging/`, `Makefile` | Build system |
| `tests/main/deb-*` | `packaging/`, `cmd/` | Debian packaging |
| `tests/main/snap-*` (packaging) | `packaging/`, `build-aux/` | Snap packaging |

## Source Package → Test Suites

### overlord/ Packages

| Source Package | Primary Test Suites | Notes |
|:---|:---|:---|
| `overlord/snapstate/` | `tests/main/install*`, `tests/main/refresh*`, `tests/main/remove*`, `tests/main/revert*`, `tests/main/snap-run*` | Core snap lifecycle |
| `overlord/ifacestate/` | `tests/main/interfaces-*` | Interface connection management |
| `overlord/assertstate/` | `tests/main/assertions*`, `tests/main/device*` | Assertion database |
| `overlord/devicestate/` | `tests/nested/core/*`, `tests/main/device*`, `tests/main/serial*`, `tests/main/preseed*` | Device identity, remodeling |
| `overlord/hookstate/` | `tests/main/hooks*`, `tests/main/services*` | Hook execution |
| `overlord/configstate/` | `tests/main/system*`, `tests/main/services*` | System configuration |
| `overlord/` (general) | `tests/main/snapd`, `tests/main/snap*` | Overlord state machine |

### cmd/ Packages

| Source Package | Primary Test Suites | Notes |
|:---|:---|:---|
| `cmd/snap/` | `tests/main/snap*`, `tests/main/snap-run*` | CLI client |
| `cmd/snapd/` | `tests/main/snapd`, `tests/main/services*` | Daemon binary |
| `cmd/snap-confine/` | `tests/main/snap-run*`, `tests/main/snapd-reexec*`, `tests/main/security*` | Sandbox setup |
| `cmd/snap-exec/` | `tests/main/snap-run*`, `tests/main/snapd-reexec*` | Snap execution |
| `cmd/snap-repair/` | `tests/main/repair*` | Repair tool |
| `cmd/snap-bootstrap/` | `tests/main/uc20*`, `tests/nested/core/*` | Bootstrap for UC20+ |

### interfaces/ Packages

| Source Package | Primary Test Suites | Notes |
|:---|:---|:---|
| `interfaces/builtin/` | `tests/main/interfaces-*` | One-to-one with interface tests |
| `interfaces/apparmor/` | `tests/main/apparmor*` | AppArmor backend |
| `interfaces/seccomp/` | `tests/main/seccomp*` | Seccomp backend |
| `interfaces/udev/` | `tests/main/udev*` | UDev backend |
| `interfaces/` (core) | `tests/main/interfaces-*`, `tests/main/security*` | Repository, connection logic |

### boot/ and Related

| Source Package | Primary Test Suites | Notes |
|:---|:---|:---|
| `boot/` | `tests/main/uc20*`, `tests/main/uc22*`, `tests/main/uc24*`, `tests/nested/core/*` | Bootloader management |
| `gadget/` | `tests/main/gadget*`, `tests/main/uc20*`, `tests/nested/core/*` | Gadget asset handling |
| `seed/` | `tests/main/seed*`, `tests/main/preseed*` | Seed metadata |
| `image/` | `tests/main/preseed*`, `tests/nested/core/*`, `tests/main/image*` | Image building |
| `core-initrd/` | `tests/main/initrd*`, `tests/nested/core/*`, `tests/main/uc*` | Initrd packaging |

### Other Key Packages

| Source Package | Primary Test Suites | Notes |
|:---|:---|:---|
| `daemon/` | `tests/main/snapd`, `tests/main/rest-api*`, `tests/main/services*` | REST API, daemon lifecycle |
| `store/` | `tests/main/store*`, `tests/main/snap-download*`, `tests/main/refresh*`, `tests/main/install*` | Store client |
| `client/` | `tests/main/snap*`, `tests/main/rest-api*`, `tests/main/store*` | Go client library |
| `systemd/` | `tests/main/services*`, `tests/main/mount*`, `tests/main/snap-run*` | Systemd interaction |
| `osutil/` | `tests/main/mount*`, `tests/main/snap-run*`, `tests/main/security*` | OS utilities |
| `asserts/` | `tests/main/assertions*`, `tests/main/device*`, `tests/main/serial*` | Assertion format and validation |
| `snap/` | `tests/main/install*`, `tests/main/refresh*`, `tests/main/snap*` | Snap metadata and validation |
| `packaging/` | `tests/main/build*`, `tests/main/deb-*` | Distribution packaging |

## Common Cross-Cutting Patterns

### Interface Tests
Interface tests (`tests/main/interfaces-<name>`) are highly correlated with:
- `interfaces/builtin/<name>.go` — the interface definition itself
- `interfaces/builtin/common.go` — shared interface behavior
- `overlord/ifacestate/` — connection logic
- `interfaces/apparmor/`, `interfaces/seccomp/`, `interfaces/udev/` — security backend changes

A change to any of these can break multiple interface tests, even if the test name doesn't match the changed file exactly.

### Ubuntu Core / Nested Tests
Nested and UC tests are highly correlated with:
- `boot/` — bootloader changes
- `gadget/` — gadget asset changes
- `core-initrd/` — initrd changes
- `image/` — image building
- `overlord/devicestate/` — device identity and remodeling

A single change in `boot/` can affect `tests/nested/core/*`, `tests/main/uc20*`, `tests/main/uc22*`, and `tests/main/uc24*` simultaneously.

### Snap Lifecycle Tests
Install, refresh, remove, and revert tests are highly correlated with:
- `overlord/snapstate/` — the core state machine
- `store/` — store client behavior
- `snap/` — metadata and validation
- `boot/` — boot assets for Core systems

A change in `overlord/snapstate/` can affect all lifecycle tests, plus related tests like `tests/main/snap-run*` (which validates the post-installation state).

## Grep Hints

Search this file for:
- A **test suite prefix** (e.g., `tests/main/interfaces-`) to find related source packages
- A **source directory** (e.g., `overlord/snapstate/`) to find affected test suites
- A **subsystem keyword** (e.g., `UC20`, `initrd`, `interface`) to find cross-cutting patterns
