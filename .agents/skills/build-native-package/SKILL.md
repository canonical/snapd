---
name: build-native-package
description: Build snapd native distribution packages (deb, rpm, pkg) using kulturysta. Use when building Debian, Ubuntu, Fedora, openSUSE, Arch, CentOS, or Amazon Linux packages.
metadata:
  project: snapd
  task-type: build
---

## Build package

Run from the workspace root, substituting the target directory name:

```bash
(
# e.g. fedora-44, replace with ubuntu-26.04, or debian-sid, or arch
cd packaging/fedora-44
../kulturysta           # full build with tests
../kulturysta --no-tests  # faster, skip tests
)
```

`kulturysta` must be invoked from inside the target packaging directory. It reads
three shell code blocks from the `README.md` in that directory:

- `## Host Script` — runs on the host; creates the `.build/` directory structure
- `## Build Container` — the `podman run` command used to launch the container
- `## Container Script` — piped into the container on stdin; performs the actual build

Output lands in `packaging/<target>/.build/`.

## Targets

| Target | Output |
|--------|--------|
| `ubuntu-26.04`, `ubuntu-16.04`, `debian-sid` | `.deb` |
| `fedora`, `fedora-44`, `fedora-42`, `fedora-latest`, `fedora-rawhide` | `.rpm` |
| `opensuse`, `opensuse-tumbleweed`, `opensuse-16.0` | `.rpm` |
| `centos-7`, `centos-9`, `amzn-2`, `amzn-2023` | `.rpm` |
| `arch` | `.pkg.tar.zst` |

## Requirements

podman 5+. Named volumes cache package downloads across runs.

## On failure

`.build/` is preserved. Debs in `.build/*.deb`, RPMs in `.build/RPMS/`.
