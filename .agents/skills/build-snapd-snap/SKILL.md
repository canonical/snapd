---
name: build-snapd-snap
description: Build the snapd snap artifact for testing using ./tests/build-test-snapd-snap
metadata:
  project: snapd
  task-type: build
---

## Build the snapd snap

Command: `./tests/build-test-snapd-snap [OPTIONS]`
Output: `built-snap/snapd_*.snap.keep` (~30-40 MB)

## When to build

Build when snapd code or snap packaging changed. Skip when only test files (`tests/lib/`, `tests/unit/`) changed — use `./run-spread --download` or `NO_REBUILD=1` instead.

## Options

| Flag | When to use | Build time |
|------|-------------|------------|
| `--clean-snapd-only` | **Default.** Go code changes (cmd/, daemon/, overlord/, interfaces/) | 1-2 min |
| _(no flags)_ | Non-snapd parts changed: apparmor, dynamic-linker, runtime, `build-aux/snap/` scripts | Several min |
| `--no-clean` | Rapid iteration, confident no deps changed. May produce incorrect builds. | <1 min |

`--clean-snapd-only` rebuilds only the `snapd` part from `build-aux/snap/snapcraft.yaml`, preserving all other parts (apparmor, dynamic-linker, runtime, patchelf, squashfs-tools, libcrypto-fips).

Use full clean (no flags) only when certain that non-snapd parts need rebuilding.

## Verification

```bash
ls -lh built-snap/snapd_*.snap.keep
```

## Integration with spread

- `run-spread` looks for the snap in `built-snap/`
- Set `NO_REBUILD=1` to skip rebuilding when snap already exists
- See `run-spread-test` skill for running tests
