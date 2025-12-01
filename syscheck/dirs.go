// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	checks = append(checks, checkSnapMountDir, checkLibExecDir)
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

	// distributions which support migration from /snap to /var/lib/snapd/snap
	migratedAltDirDistros = []string{
		"opensuse", // openSUSE Tumbleweed, Slowroll, Leap, but not SLE
	}
)

func checkSnapMountDir() error {
	if err := dirs.SnapMountDirDetectionOutcome(); err != nil {
		return err
	}

	smd := dirs.StripRootDir(dirs.SnapMountDir)
	switch {
	case release.DistroLike(migratedAltDirDistros...) && smd == dirs.AltSnapMountDir:
		// some distributions support migration from /snap -> /var/lib/snapd/snap/
	case release.DistroLike(defaultDirDistros...) && smd != dirs.DefaultSnapMountDir:
		fallthrough
	case release.DistroLike(altDirDistros...) && smd != dirs.AltSnapMountDir:
		return fmt.Errorf("unexpected snap mount directory %v on %v", smd, release.ReleaseInfo.ID)
	}

	return nil
}

var (
	// distributions known to use /usr/lib/snapd/
	defaulLibExectDirDistros = []string{
		"ubuntu",
		"ubuntu-core",
		"ubuntucoreinitramfs",
		"debian",
		"yocto",
		"altlinux",
		"antergos",
		"arch",
		"archlinux",
		"gentoo",
		"manjaro",
		"manjaro-arm",
	}

	// distributions known to use /usr/libexec/snapd/
	altLibExecDirDistros = []string{
		"fedora",
		"opensuse-tumbleweed",
		"opensuse-slowroll",
	}

	bothLibExecDirDistros = []string{
		"opensuse-leap", // openSUSE Leap uses /usr/libexec/snapd
		// starting from 16.0, but used /usr/lib/snapd
		// in earlier versions
	}
)

func checkLibExecDir() error {
	d := dirs.StripRootDir(dirs.DistroLibExecDir)
	switch {
	case release.DistroLike(bothLibExecDirDistros...):
	// Distributions which use either location, likely depending on their
	// version, but doing version check is too much a hassle. openSUSE Leap is
	// one of those cases.
	case release.DistroLike(altLibExecDirDistros...) && d != dirs.AltDistroLibexecDir:
		// RHEL, CentOS, Fedora and derivatives, openSUSE Tumbleweed (since
		// snapshot 20200826) and Slowroll; both RHEL and CentOS list "fedora"
		// in ID_LIKE
		fallthrough
	case release.DistroLike(defaulLibExectDirDistros...) && d != dirs.DefaultDistroLibexecDir:
		return fmt.Errorf("unexpected snapd tooling directory %v on %v", d, release.ReleaseInfo.ID)
	}

	return nil
}
