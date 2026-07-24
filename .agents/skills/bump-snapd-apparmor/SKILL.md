---
name: bump-snapd-apparmor
description: Update the AppArmor userspace bundled in the snapd snap and keep related version checks/tests in sync.
metadata:
  project: snapd
  task-type: packaging
---

## Bump bundled AppArmor in snapd snap

Use this when updating the AppArmor userspace source used by the `apparmor` part in `build-aux/snap/snapcraft.yaml`.

## Files to update

Historical AppArmor bump commits update these files together:

- `build-aux/snap/snapcraft.yaml`
- `cmd/configure.ac`
- `sandbox/apparmor/apparmor_test.go`
- `build-aux/snap/local/apparmor/af_names.h`

Patch-level bumps usually only change version strings and the source checksum. Major/minor bumps may also need build-package or build-flag changes.

## Update steps

1. Check recent bump commits:
   ```bash
   git log --oneline -- build-aux/snap/snapcraft.yaml
   git show --stat <recent-apparmor-bump-commit>
   git show --unified=20 <recent-apparmor-bump-commit> -- build-aux/snap/snapcraft.yaml cmd/configure.ac sandbox/apparmor/apparmor_test.go build-aux/snap/local/apparmor/af_names.h
   ```

2. Verify the upstream tag exists:
   ```bash
   git ls-remote --tags https://gitlab.com/apparmor/apparmor.git 'refs/tags/v<version>'
   ```

3. Download the exact upstream archive and calculate its checksum:
   ```bash
   curl -fL -o /tmp/opencode/apparmor-v<version>.tar.gz \
     https://gitlab.com/apparmor/apparmor/-/archive/v<version>/apparmor-v<version>.tar.gz
   sha256sum /tmp/opencode/apparmor-v<version>.tar.gz
   ```

4. Update `build-aux/snap/snapcraft.yaml`:
   - `source` URL
   - `source-checksum`

5. Update `cmd/configure.ac` for snapcraft builds:
   ```m4
   PKG_CHECK_MODULES([APPARMOR], [libapparmor = <version>], [
   ```

6. Update the fake parser version in `sandbox/apparmor/apparmor_test.go`.

7. Regenerate or compare `af_names.h` from the new archive:
   ```bash
   rm -rf /tmp/opencode/apparmor-v<version>
   tar -xf /tmp/opencode/apparmor-v<version>.tar.gz -C /tmp/opencode
   make -C /tmp/opencode/apparmor-v<version>/parser af_names.h
   diff -u /tmp/opencode/apparmor-v<version>/parser/af_names.h \
     build-aux/snap/local/apparmor/af_names.h
   ```

   The vendored file has a local provenance header prepended. If generated content is otherwise identical, update only the version in that header.

## Verification

Run focused checks:

```bash
go test ./sandbox/apparmor
git diff --check
```

Search for stale version references in relevant file types:

```bash
rg '<old-version>' -g'*.ac' -g'*.go' -g'*.h' -g'*.yaml' -g'*.yml'
```

For full packaging validation, build the snap without `--clean-snapd-only`, because the AppArmor part changed:

```bash
./tests/build-test-snapd-snap
```

Do not use `--clean-snapd-only` for AppArmor bumps; that preserves non-snapd parts and can miss the changed AppArmor part.
