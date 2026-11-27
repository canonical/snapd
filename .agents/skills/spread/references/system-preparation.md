# snapd Spread system preparation and helpers

## How snapd Spread systems are prepared

The project-specific source of truth is [`spread.yaml`](../../../../spread.yaml)
and the helpers it calls under [`tests/lib/`](../../../../tests/lib). Standard
system-test suites such as `tests/smoke/`, `tests/main/`, and
`tests/regression/` call `prepare-restore.sh --prepare-suite` and
`--prepare-suite-each`. The suite-level helper selects preparation based on
the requested `SPREAD_SYSTEM`: classic systems use `prepare_classic`, while
Ubuntu Core targets use `prepare_ubuntu_core`. Per-task preparation and
restore then establish and reset the test baseline.

### Classic snapd systems

For classic Ubuntu and other supported classic distributions,
`prepare_classic` configures proxy access and installs the snapd package built
from this checkout, unless `REUSE_SNAPD=1`. It validates the package version,
builds and installs the local snapd snap with `snap install --dangerous`, and
waits for seeding. The local snapd snap is part of the test setup so tests
exercise snapd re-execution; the Core snap is also installed or refreshed using
`CORE_CHANNEL`.

The helper configures snapd service/re-exec behavior, disables automatic
refreshes, caches commonly used snaps, and saves an initial snapd state.
`prepare-suite-each` and `restore-suite-each` record test state, remove
packages installed by a classic test, and reset snapd to the saved baseline.
Suite restore removes the test snapd package. These actions are implemented
in [`prepare.sh`](../../../../tests/lib/prepare.sh) and
[`prepare-restore.sh`](../../../../tests/lib/prepare-restore.sh); do not assume
every suite runs all of them.

### Ubuntu Core systems

`tests/core/` selects `ubuntu-core-*` systems, and other suites may also run on
Core when their system filters allow it. Preparation detects the target from
`SPREAD_SYSTEM`, because an image can initially boot into a classic Ubuntu
environment before it is converted to Core.

If `/var/lib/dpkg/status` exists, `prepare_ubuntu_core` invokes
`setup_reflash_magic` and reboots into an all-snap Core image. It then waits
for snapd and seeding, verifies the fundamental snaps (and the snapd snap on
Core 18 and later), ensures test tooling such as `rsync` is available, caches
test snaps, disables refreshes, applies service settings, and saves the
initial snapd state. After a reflash, the helper verifies that the classic
dpkg database is gone. Later test setup and restore must use Core-aware
helpers; do not assume classic package-management or filesystem behavior.

### Nested systems

For `tests/nested/`, distinguish the outer Spread host from the guest VM under
test. The selected backend allocates the outer host; the nested suite
configuration in [`spread.yaml`](../../../../spread.yaml) prepares that host
with dependencies, `ubuntu-image`, the checkout's snapd DEB, and a remote
connection. It then uses the `tests.nested` tool and helpers under
[`tests/lib/nested/`](../../../../tests/lib/nested) to build and boot the guest.

- A nested **Core** guest is generated with `ubuntu-image`, a model, and
  configured Core/kernel/gadget channels. By default,
  `NESTED_BUILD_SNAPD_FROM_CURRENT=true` repacks snapd and the required
  kernel/gadget/base snaps from the current tree. The image is configured for
  test access (cloud-init or assertions), then booted and given the test
  user/tools.
- A nested **classic** guest starts from a downloaded Ubuntu cloud image and
  is configured with cloud-init for SSH and test access.
- The `tests/nested/core/` and `tests/nested/classic/` suites build the image
  during suite preparation and create/remove a guest for each task.
  `tests/nested/manual/` leaves image and VM lifecycle to each test.

The outer host's `-debug` shell or `spread -exec` session is not the nested guest
shell. Before `restore-each` removes the guest, collect guest-side evidence
such as its serial log, VM status, or service state. The nested lifecycle and
image logic is in [`tests/lib/tools/tests.nested`](../../../../tests/lib/tools/tests.nested),
[`tests/lib/nested/image.sh`](../../../../tests/lib/nested/image.sh), and
[`tests/lib/nested/lifecycle.sh`](../../../../tests/lib/nested/lifecycle.sh).

## Shared test helpers: `tests/lib` and `tests/lib/tools`

`spread.yaml` sets `TESTSLIB` to `tests/lib` and `TESTSTOOLS` to
`tests/lib/tools`; it also puts `tests/bin` on `PATH`. Use these variables
instead of hard-coding checkout paths.

- `tests/lib` contains shared shell scripts and test assets. Source a shell
  helper when a test needs its functions (for example, `prepare.sh`,
  `pkgdb.sh`, or `systems.sh`), and execute lifecycle scripts such as
  `prepare-restore.sh` as configured by the suite. Check existing helpers
  before adding duplicate setup, state, or cleanup logic.
- `tests/lib/tools` is a collection of utility commands, separate from the
  shell libraries. Invoke a command directly, for example
  `"$TESTSTOOLS"/tests.invariant check` or
  `"$TESTSTOOLS"/journal-state get-log -u snapd`. Tools on `PATH`—often via
  symlinks in `tests/bin`—can also be invoked by their command name. Consult
  the command's `-h` output and implementation before relying on its behavior.
- During project preparation, `spread.yaml` copies tools and remote helpers
  from `tests/lib/external/snapd-testing-tools/` into `TESTSTOOLS`. When
  tracing a command, check whether its maintained source is in that external
  subtree or directly in `tests/lib/tools`.

The [tools README](../../../../tests/lib/tools/README) describes command naming
and usage conventions. The source of truth for helper loading and staging is
[`spread.yaml`](../../../../spread.yaml); examples of shared shell setup are in
[`prepare-restore.sh`](../../../../tests/lib/prepare-restore.sh), and
[`tests/lib/tools/`](../../../../tests/lib/tools) contains commands for package,
systemd, invariant, journal, and nested-VM operations.

