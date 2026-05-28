---
name: build-snapd-snap
description: Build the snapd snap artifact for testing using ./tests/build-test-snapd-snap
metadata:
  project: snapd
  task-type: build
---

## Overview

This skill covers how to build the snapd snap artifact required for spread integration testing.

Command: `./tests/build-test-snapd-snap`

Purpose: Builds the snapd snap package using snapcraft, creating a snap file in the `built-snap/` directory.

Output: `built-snap/snapd_*.snap.keep`

## When to Use This Skill

Use this skill when you need to:
- Build the snapd snap before running spread tests
- Rebuild after making changes to snapd code
- Determine which build option to use based on code changes

When you do NOT need to build:
- The `tests/unit/` suite does not require a prebuilt snapd snap. Use `spread` directly for those tests (e.g., `spread garden:ubuntu-24.04-64:tests/unit/go`).
- Changes only to test infrastructure files (`tests/lib/`, `tests/unit/`, etc.) that don't affect the snap contents — use `./run-spread --download` to fetch the prebuilt snap from master, or set `NO_REBUILD=1` if a snap already exists locally.

Preferred approach: Use `--clean-snapd-only` to ensure the snapd part is fully rebuilt (clean + rebuild) while preserving other parts.

## Build Options

### Option 1: Clean Snapd Only (--clean-snapd-only) - PREFERRED

```bash
./tests/build-test-snapd-snap --clean-snapd-only
```

Use this as the default choice for most development work.

What it does:
- Cleans and fully rebuilds only the `snapd` part in `build-aux/snap/snapcraft.yaml`
- Ensures the snapd part is completely rebuilt from scratch (clean + rebuild)
- Preserves all other parts (dynamic-linker, runtime, apparmor, etc.)

Perfect for:
- Changes to Go code in cmd/, daemon/, overlord/, interfaces/, etc.
- Iterating on snapd daemon logic
- Bug fixes in Go code
- Most day-to-day development work

Avoids rebuilding:
- `dynamic-linker` (custom glibc dynamic linker)
- `runtime` (runtime library dependencies)
- `apparmor` (AppArmor parser and libraries)
- `patchelf` (ELF manipulation tool)
- `squashfs-tools` (squashfs filesystem tools)
- `libcrypto-fips` (FIPS-compliant crypto libraries)

Build time: Approximately 1-2 minutes (Go compilation and linking only).

### Option 2: Full Clean (no flags) - Use When Needed

```bash
./tests/build-test-snapd-snap
```

Use when `--clean-snapd-only` is not sufficient:
- Non-snapd parts of `build-aux/snap/snapcraft.yaml` were changed
- AppArmor part configuration in `build-aux/snap/snapcraft.yaml` was modified
- Changes to `build-aux/snap/local/` scripts
- Changes to dynamic-linker, runtime, or other non-snapd parts
- First build of the day after pulling major changes
- When in doubt about dependencies

Build time: Several minutes (builds glibc, apparmor, and all dependencies).

### Option 3: No Clean (--no-clean) - Use With Caution

```bash
./tests/build-test-snapd-snap --no-clean
```

Use when: Iterating rapidly and confident that no dependencies changed.

What it does: Skips `snapcraft clean` (equivalent to `SNAPCRAFT_NO_CLEAN=1`). Note: `built-snap/` directory is still removed and recreated.

Warning: May produce incorrect builds if any dependencies have changed. Use with caution.

Build time: Fastest option, typically under a minute.

## Snapcraft Parts Reference

The snapd snap consists of multiple parts defined in `build-aux/snap/snapcraft.yaml`:

- `snapd`: Main Go code (daemon, commands, libraries) - the core snapd implementation
- `apparmor`: AppArmor parser and libraries for security confinement
- `dynamic-linker`: Custom glibc dynamic linker for the snap
- `runtime`: Runtime library dependencies (libc, libcap, libseccomp, etc.)
- `patchelf`: ELF manipulation tool
- `squashfs-tools`: Tools for creating and managing squashfs filesystems
- `libcrypto-fips`: FIPS-compliant crypto libraries (conditional)

## Decision Tree

Use this guide to choose the appropriate build option:

```
Changes made to:

1. Go code in cmd/, daemon/, overlord/, interfaces/, etc.
   -> Use: --clean-snapd-only (PREFERRED - ensures full rebuild of snapd part)
   -> Reason: Cleans and fully rebuilds snapd part only

2. AppArmor part in build-aux/snap/snapcraft.yaml
   -> Use: full clean (no flags)
   -> Reason: apparmor part must be rebuilt

3. Scripts in build-aux/snap/local/
   -> Use: full clean (no flags)
   -> Reason: Build process itself changed

4. Parts configuration in build-aux/snap/snapcraft.yaml (non-snapd parts)
   -> Use: full clean (no flags)
   -> Reason: Multiple parts may be affected

5. Changes to dynamic-linker, runtime, or other non-snapd parts
   -> Use: full clean (no flags)
   -> Reason: Those specific parts need rebuilding

6. Unsure about what changed or dependencies unclear
   -> Use: --clean-snapd-only (PREFERRED - covers most cases without rebuilding everything)
   -> Reason: Fast rebuild of the snapd part; only use full clean (no flags) as a last resort
     when you are certain non-snapd parts (apparmor, dynamic-linker, runtime, etc.) need rebuilding

7. Rapid iteration with no dependency changes (USE WITH CAUTION)
   -> Use: --no-clean
   -> Reason: Fastest option, but verify build correctness

Default choice for most development: --clean-snapd-only
Full clean (no flags) is a last resort — only use when certain that non-snapd parts need rebuilding.
```

## Verification

After the build completes successfully, verify the snap file was created:

```bash
ls -lh built-snap/snapd_*.snap.keep
```

You should see a snap file with a size of approximately 30-40 MB (varies by architecture and build configuration).

## Integration with Spread Tests

The built snap is automatically used by the spread test infrastructure:

- The `run-spread` script looks for the snap in `built-snap/`
- Set `NO_REBUILD=1` in the environment when calling `run-spread` to skip rebuilding if the snap already exists
- See the `run-spread-test` skill for details on running spread tests

## Common Scenarios

Scenario 1 — Fixed a bug in snapd daemon:
```bash
# Only Go code changed
./tests/build-test-snapd-snap --clean-snapd-only
```

Scenario 2 — Modified AppArmor part configuration:
```bash
# Non-snapd part changed
./tests/build-test-snapd-snap
```

Scenario 3 — Modified snapcraft.yaml to add new runtime dependency:
```bash
# Runtime part affected
./tests/build-test-snapd-snap
```

Scenario 4 — Quick iteration on interface implementation:
```bash
# First build: use --clean-snapd-only
./tests/build-test-snapd-snap --clean-snapd-only

# Subsequent iterations: use --no-clean
./tests/build-test-snapd-snap --no-clean
```

## Best Practices

DO:
- Use `--clean-snapd-only` as your default for Go code changes (ensures full rebuild)
- Use full clean when in doubt about dependencies
- Verify the snap was created after build completes
- Check build logs for warnings or errors

DON'T:
- Use `--no-clean` unless you're certain dependencies haven't changed
- Ignore build warnings (they may indicate real issues)
- Assume a previous build is still valid after pulling changes
