// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 f* Copyright (C) 2025 Canonical Ltd
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

package syscheck

import (
	"fmt"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
)

func init() {
	checks = append(checks, checkSnapMountDir)
}

var (
	// distributions known to use /snap/
	defaultDirDistros = []string{
		"ubuntu",
		"ubuntu-core",
		"ubuntucoreinitramfs",
		"debian",
		"opensuse",
		"suse",
		"yocto",
	}

	// distributions known to use /var/lib/snapd/snap/
	altDirDistros = []string{
		"altlinux",
		"antergos",
		"arch",
		"archlinux",
		"fedora",
		"gentoo",
		"manjaro",
		"manjaro-arm",
	}
)

func checkSnapMountDir() error {
	if err := dirs.SnapMountDirDetectionOutcome(); err != nil {
		return err
	}

	smd := dirs.StripRootDir(dirs.SnapMountDir)
	switch {
	case release.DistroLike(defaultDirDistros...) && smd != dirs.DefaultSnapMountDir:
		fallthrough
	case release.DistroLike(altDirDistros...) && smd != dirs.AltSnapMountDir:
		return fmt.Errorf("unexpected snap mount directory %v on %v", smd, release.ReleaseInfo.ID)
	}

	return nil
}
