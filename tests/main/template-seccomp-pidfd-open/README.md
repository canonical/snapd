# Seccomp pidfd_open Integration Test

This integration test verifies that the `pidfd_open` syscall is allowed by the default seccomp profile for all snaps.

## Test Structure

- `test-pidfd-open.c` - C program that tests the pidfd_open syscall
- `test-snapd-pidfd-open/` - Snap package directory
  - `meta/snap.yaml` - Snap metadata
  - `bin/` - Will contain the compiled test binary

## How the Test Works

1. **Prepare phase**:
   - Compiles the C program on the host system (statically linked)
   - Creates a snap package using `snap pack`
   - Installs the snap

2. **Execute phase**:
   - Runs the test program inside the snap
   - Verifies that pidfd_open either succeeds or returns ENOSYS (not supported by kernel)
   - Fails if the syscall is blocked by seccomp (EPERM/EACCES)

3. **Restore phase**:
   - Removes the test snap and cleans up

## Test Program Behavior

The C program (`test-pidfd-open.c`) attempts to call `pidfd_open` on its own PID and reports:
- **Success**: If the syscall works (returns a valid file descriptor)
- **Not supported**: If the kernel doesn't support the syscall (ENOSYS)
- **Blocked**: If seccomp blocks the syscall (EPERM/EACCES) - this is a test failure

## System Requirements

- Excludes ubuntu-core systems (no gcc available)
- Runs on all classic Ubuntu systems
