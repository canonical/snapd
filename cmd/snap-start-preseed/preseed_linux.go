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
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/timings"
)

var (
	// mountPath is where target core/snapd is going to be mounted in the target chroot
	mountPath     = "/snapd-prebake"
	syscallMount  = syscall.Mount
	syscallChroot = syscall.Chroot
)

// checkChroot does a basic sanity check of the target chroot environment, e.g. makes
// sure critical virtual filesystems (such as proc) are mounted. This is not meant to
// be exhaustive check, but one that prevents running the tool against a wrong directory
// by an accident, which would lead to hard to understand errors from snapd in pre-bake
// mode.
func checkChroot(prebakeChroot string) error {
	exists, isDir, err := osutil.DirExists(prebakeChroot)
	if err != nil {
		return fmt.Errorf("cannot verify target chroot directory %s: %v", prebakeChroot, err)
	}
	if !exists || !isDir {
		return fmt.Errorf("target chroot directory %s doesn't exist or is not a directory", prebakeChroot)
	}

	// sanity checks of the critical mountpoints inside chroot directory
	for _, p := range []string{"/sys/kernel/security/apparmor", "/proc/self", "/dev/mem"} {
		path := filepath.Join(prebakeChroot, p)
		if exists := osutil.FileExists(path); !exists {
			return fmt.Errorf("target chroot directory validation error: %s doesn't exist", path)
		}
	}

	return nil
}

var systemSnapFromSeeds = func(rootDir string) (string, error) {
	seedDir := filepath.Join(dirs.SnapSeedDirUnder(rootDir))
	seed, err := seed.Open(seedDir)
	if err != nil {
		return "", err
	}

	if err := seed.LoadAssertions(nil, nil); err != nil {
		return "", err
	}

	tm := timings.New(nil)
	if err := seed.LoadMeta(tm); err != nil {
		return "", err
	}

	var coreSnapPath string
	ess := seed.EssentialSnaps()
	// TODO: handle core18, snapd snap.
	for _, snap := range ess {
		if snap.SnapName() == "core" {
			coreSnapPath = snap.Path
			break
		}
	}
	if coreSnapPath == "" {
		return "", fmt.Errorf("core snap not found")
	}

	return coreSnapPath, nil
}

func prepareChroot(prebakeChroot string) (func(), error) {
	if err := syscallChroot(prebakeChroot); err != nil {
		return nil, fmt.Errorf("cannot chroot into %s: %v", prebakeChroot, err)
	}

	// GlobalRootDir is now relative to chroot env
	rootDir := dirs.GlobalRootDir
	coreSnapPath, err := systemSnapFromSeeds(rootDir)
	if err != nil {
		return nil, err
	}

	// create mountpoint for core/snapd
	where := filepath.Join(rootDir, mountPath)
	if err := os.MkdirAll(where, 0755); err != nil {
		return nil, err
	}

	removeMountpoint := func() {
		for i := 0; i < 5; i++ {
			err := os.Remove(where)
			if err != nil {
				fmt.Fprintf(Stderr, "%v", err)
			} else {
				return
			}
			time.Sleep(time.Second)
		}
	}

	cmd := exec.Command("mount", "-t", "squashfs", coreSnapPath, where)
	if err := cmd.Run(); err != nil {
		removeMountpoint()
		return nil, fmt.Errorf("cannot mount %s at %s in pre-bake mode: %v ", coreSnapPath, where, err)
	}

	// TODO: check snapd version

	unmount := func() {
		fmt.Fprintf(Stdout, "unmounting: %s\n", mountPath)
		cmd := exec.Command("umount", mountPath)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(Stderr, "%v", err)
		}
	}

	return func() {
		unmount()
		removeMountpoint()
	}, nil
}

// startPrebakeMode runs snapd in a prebake mode. It assumes running in a chroot.
// The chroot is expected to be set-up and ready to use (critical system directories mounted).
func startPrebakeMode(prebakeChroot string) error {
	// exec snapd relative to new chroot, e.g. /snapd-prebake/usr/lib/snapd/snapd
	path := filepath.Join(mountPath, "/usr/lib/snapd/snapd")

	// run snapd in pre-baking mode
	cmd := exec.Command(path)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAPD_PREBAKE_IMAGE=1")
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout

	fmt.Printf("starting pre-baking mode: %s\n", prebakeChroot)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("image-prebaking error: %v\n", err)
	}

	return nil
}
