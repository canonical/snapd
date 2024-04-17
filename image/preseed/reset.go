// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2022 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package preseed

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
)

// bash-completion symlinks; note there are symlinks that point at
// completer, and symlinks that point at the completer symlinks.
// e.g.
// lxd.lxc -> /snap/core/current/usr/lib/snapd/complete.sh
// lxc -> lxd.lxc
func resetCompletionSymlinks(completersPath string) error {
	files, err := os.ReadDir(completersPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error reading %s: %v", completersPath, err)
	}
	completeShSymlinks := make(map[string]string)
	var otherSymlinks []string

	// pass 1: find all symlinks pointing at complete.sh
	for _, fileInfo := range files {
		if fileInfo.Type()&os.ModeSymlink == 0 {
			continue
		}
		fullPath := filepath.Join(completersPath, fileInfo.Name())
		if dirs.IsCompleteShSymlink(fullPath) {
			if err := os.Remove(fullPath); err != nil {
				return fmt.Errorf("error removing symlink %s: %v", fullPath, err)
			}
			completeShSymlinks[fileInfo.Name()] = fullPath
		} else {
			otherSymlinks = append(otherSymlinks, fullPath)
		}
	}
	// pass 2: find all symlinks that point at the symlinks found in pass 1.
	for _, other := range otherSymlinks {
		target, err := os.Readlink(other)
		if err != nil {
			return fmt.Errorf("error reading symlink target of %s: %v", other, err)
		}
		if _, ok := completeShSymlinks[target]; ok {
			if err := os.Remove(other); err != nil {
				return fmt.Errorf("error removing symlink %s: %v", other, err)
			}
		}
	}

	return nil
}

// ResetPreseededChroot removes all preseeding artifacts from preseedChroot
// (classic Ubuntu only).
var ResetPreseededChroot = func(preseedChroot string) error {
	var err error
	preseedChroot, err = filepath.Abs(preseedChroot)
	if err != nil {
		return err
	}

	exists, isDir, err := osutil.DirExists(preseedChroot)
	if err != nil {
		return fmt.Errorf("cannot reset %q: %v", preseedChroot, err)
	}
	if !exists {
		return fmt.Errorf("cannot reset non-existing directory %q", preseedChroot)
	}
	if !isDir {
		return fmt.Errorf("cannot reset %q, it is not a directory", preseedChroot)
	}

	// globs that yield individual files
	globs := []string{
		dirs.SnapStateFile,
		dirs.SnapSystemKeyFile,
		filepath.Join(dirs.SnapBlobDir, "*.snap"),
		filepath.Join(dirs.SnapUdevRulesDir, "*-snap.*.rules"),
		filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.*.*.conf"),
		filepath.Join(dirs.SnapServicesDir, "snap.*.service"),
		filepath.Join(dirs.SnapServicesDir, "snap.*.timer"),
		filepath.Join(dirs.SnapServicesDir, "snap.*.socket"),
		filepath.Join(dirs.SnapServicesDir, "snap-*.mount"),
		filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants", "snap-*.mount"),
		filepath.Join(dirs.SnapServicesDir, "default.target.wants", "snap-*.mount"),
		filepath.Join(dirs.SnapServicesDir, "snapd.mounts.target.wants", "snap-*.mount"),
		filepath.Join(dirs.SnapUserServicesDir, "snap.*.service"),
		filepath.Join(dirs.SnapUserServicesDir, "snap.*.socket"),
		filepath.Join(dirs.SnapUserServicesDir, "snap.*.timer"),
		filepath.Join(dirs.SnapUserServicesDir, "default.target.wants", "snap.*.service"),
		filepath.Join(dirs.SnapUserServicesDir, "sockets.target.wants", "snap.*.socket"),
		filepath.Join(dirs.SnapUserServicesDir, "timers.target.wants", "snap.*.timer"),
		filepath.Join(runinhibit.InhibitDir, "*.lock"),
	}

	for _, gl := range globs {
		matches, err := filepath.Glob(filepath.Join(preseedChroot, gl))
		if err != nil {
			// the only possible error from Glob() is ErrBadPattern
			return err
		}
		for _, path := range matches {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("error removing %s: %v", path, err)
			}
		}
	}

	// directories that need to be removed recursively (but
	// leaving parent directory intact).
	globs = []string{
		filepath.Join(dirs.SnapDataDir, "*"),
		filepath.Join(dirs.SnapCacheDir, "*"),
		filepath.Join(apparmor_sandbox.CacheDir, "*"),
		filepath.Join(dirs.SnapDesktopFilesDir, "*"),
		filepath.Join(dirs.SnapDBusSessionServicesDir, "*"),
		filepath.Join(dirs.SnapDBusSystemServicesDir, "*"),
	}

	for _, gl := range globs {
		matches, err := filepath.Glob(filepath.Join(preseedChroot, gl))
		if err != nil {
			// the only possible error from Glob() is ErrBadPattern
			return err
		}
		for _, path := range matches {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("error removing %s: %v", path, err)
			}
		}
	}

	// directories removed entirely
	paths := []string{
		dirs.SnapAssertsDBDir,
		dirs.FeaturesDir,
		dirs.SnapDesktopIconsDir,
		dirs.SnapDeviceDir,
		dirs.SnapCookieDir,
		dirs.SnapMountPolicyDir,
		dirs.SnapAppArmorDir,
		dirs.SnapSeqDir,
		dirs.SnapMountDir,
		dirs.SnapSeccompBase,
	}

	for _, path := range paths {
		if err := os.RemoveAll(filepath.Join(preseedChroot, path)); err != nil {
			// report the error and carry on
			return fmt.Errorf("error removing %s: %v", path, err)
		}
	}

	for _, completersPath := range []string{dirs.CompletersDir, dirs.LegacyCompletersDir} {
		if err := resetCompletionSymlinks(filepath.Join(preseedChroot, completersPath)); err != nil {
			return err
		}
	}

	return nil
}
