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

var (
	// distributions known to use /usr/lib/snapd/
	defaulLibExectDirDistros = []string{
		"ubuntu",
		"ubuntu-core",
		"ubuntucoreinitramfs",
		"debian",
		"opensuse-leap",
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
		"opensuse-leap",
		// openSUSE Leap 16.0 has switch to /usr/libexec/snapd/ like Tumbleweed, while Leap 15.6 is still using /usr/lib/snapd now.
		// So till now snapd on Leap 16.0 does not work.
		// Since We do not have distro version check here,
		// The most simple way to fix the problem is to let 'opensuse-leap' both two list above.
	}
)

func checkLibExecDir() error {
	d := dirs.StripRootDir(dirs.DistroLibExecDir)
	switch {
	case release.DistroLike([]string{"opensuse-leap",}...):
		return nil
	case release.DistroLike(altLibExecDirDistros...) && d != dirs.AltDistroLibexecDir:
		// RHEL, CentOS, Fedora and derivatives, openSUSE Tumbleweed (since
		// snapshot 20200826) and Slowroll; both RHEL and CentOS list "fedora"
		// in ID_LIKE
		fallthrough
	case release.DistroLike(defaulLibExectDirDistros...) && d != dirs.DefaultDistroLibexecDir:
		// It is possible to write  distro version check here, but introduce complexity
		return fmt.Errorf("unexpected snapd tooling directory %v on %v", d, release.ReleaseInfo.ID)
	}

	return nil
}
