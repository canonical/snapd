// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

func resetPreseededChroot(preseedChroot string) error {
	// globs that yield individual files
	globs := []string{
		dirs.SnapStateFile,
		dirs.SnapSystemKeyFile,
		filepath.Join(dirs.SnapBlobDir, "*.snap"),
		filepath.Join(dirs.SnapUdevRulesDir, "*-snap.*.rules"),
		filepath.Join(dirs.SnapBusPolicyDir, "snap.*.*.conf"),
		filepath.Join(dirs.SnapServicesDir, "snap.*.service"),
		filepath.Join(dirs.SnapServicesDir, "snap.*.timer"),
		filepath.Join(dirs.SnapServicesDir, "snap.*.socket"),
		filepath.Join(dirs.SnapServicesDir, "snap-*.mount"),
		filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants", "snap-*.mount"),
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
		filepath.Join(dirs.AppArmorCacheDir, "*"),
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
		dirs.SnapDesktopFilesDir,
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

	return nil
}
