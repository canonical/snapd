// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/timings"
)

var (
	// snapdMountPath is where target core/snapd is going to be mounted in the target chroot
	snapdMountPath = "/tmp/snapd-preseed"
	syscallMount   = syscall.Mount
	syscallChroot  = syscall.Chroot
)

// checkChroot does a basic sanity check of the target chroot environment, e.g. makes
// sure critical virtual filesystems (such as proc) are mounted. This is not meant to
// be exhaustive check, but one that prevents running the tool against a wrong directory
// by an accident, which would lead to hard to understand errors from snapd in preseed
// mode.
func checkChroot(preseedChroot string) error {
	exists, isDir, err := osutil.DirExists(preseedChroot)
	if err != nil {
		return fmt.Errorf("cannot verify %q: %v", preseedChroot, err)
	}
	if !exists || !isDir {
		return fmt.Errorf("cannot verify %q: is not a directory", preseedChroot)
	}

	// sanity checks of the critical mountpoints inside chroot directory
	for _, p := range []string{"/sys/kernel/security/apparmor", "/proc/self", "/dev/mem"} {
		path := filepath.Join(preseedChroot, p)
		if exists := osutil.FileExists(path); !exists {
			return fmt.Errorf("cannot pre-seed without access to %q", path)
		}
	}

	return nil
}

var seedOpen = seed.Open

var systemSnapFromSeed = func(rootDir string) (string, error) {
	seedDir := filepath.Join(dirs.SnapSeedDirUnder(rootDir))
	seed, err := seedOpen(seedDir, "")
	if err != nil {
		return "", err
	}

	// load assertions into temporary database
	if err := seed.LoadAssertions(nil, nil); err != nil {
		return "", err
	}

	tm := timings.New(nil)
	if err := seed.LoadMeta(tm); err != nil {
		return "", err
	}

	// TODO: handle core18, snapd snap.
	if seed.UsesSnapdSnap() {
		return "", fmt.Errorf("preseeding with snapd snap is not supported yet")
	}

	var coreSnapPath string
	ess := seed.EssentialSnaps()
	if len(ess) > 0 {
		if ess[0].SnapName() == "core" {
			coreSnapPath = ess[0].Path
		}
	}

	if coreSnapPath == "" {
		return "", fmt.Errorf("core snap not found")
	}

	return coreSnapPath, nil
}

func prepareChroot(preseedChroot string) (func(), error) {
	if err := syscallChroot(preseedChroot); err != nil {
		return nil, fmt.Errorf("cannot chroot into %s: %v", preseedChroot, err)
	}

	if err := os.Chdir("/"); err != nil {
		return nil, fmt.Errorf("cannot chdir to /: %v", err)
	}

	// GlobalRootDir is now relative to chroot env. We assume all paths
	// inside the chroot to be identical with the host.
	rootDir := dirs.GlobalRootDir
	if rootDir == "" {
		rootDir = "/"
	}

	coreSnapPath, err := systemSnapFromSeed(rootDir)
	if err != nil {
		return nil, err
	}

	// create mountpoint for core/snapd
	where := filepath.Join(rootDir, snapdMountPath)
	if err := os.MkdirAll(where, 0755); err != nil {
		return nil, err
	}

	removeMountpoint := func() {
		if err := os.Remove(where); err != nil {
			fmt.Fprintf(Stderr, "%v", err)
		}
	}

	cmd := exec.Command("mount", "-t", "squashfs", coreSnapPath, where)
	if err := cmd.Run(); err != nil {
		removeMountpoint()
		return nil, fmt.Errorf("cannot mount %s at %s in preseed mode: %v ", coreSnapPath, where, err)
	}

	// TODO: check snapd version

	unmount := func() {
		fmt.Fprintf(Stdout, "unmounting: %s\n", snapdMountPath)
		cmd := exec.Command("umount", snapdMountPath)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(Stderr, "%v", err)
		}
	}

	return func() {
		unmount()
		removeMountpoint()
	}, nil
}

// runPreseedMode runs snapd in a preseed mode. It assumes running in a chroot.
// The chroot is expected to be set-up and ready to use (critical system directories mounted).
func runPreseedMode(preseedChroot string) error {
	// exec snapd relative to new chroot, e.g. /snapd-preseed/usr/lib/snapd/snapd
	path := filepath.Join(snapdMountPath, dirs.CoreLibExecDir, "snapd")

	// run snapd in preseed mode
	cmd := exec.Command(path)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAPD_PRESEED=1")
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout

	fmt.Fprintf(Stdout, "starting to preseed root: %s\n", preseedChroot)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running snapd in preseed mode: %v\n", err)
	}

	return nil
}
