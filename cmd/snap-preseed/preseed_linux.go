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
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
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

	if osutil.FileExists(filepath.Join(preseedChroot, dirs.SnapStateFile)) {
		return fmt.Errorf("the system at %q appears to be preseeded, pass --reset flag to clean it up", preseedChroot)
	}

	// sanity checks of the critical mountpoints inside chroot directory.
	required := map[string]bool{}
	// required mountpoints are relative to the preseed chroot
	for _, p := range []string{"/sys/kernel/security", "/proc", "/dev"} {
		required[filepath.Join(preseedChroot, p)] = true
	}
	entries, err := osutil.LoadMountInfo()
	if err != nil {
		return fmt.Errorf("cannot parse mount info: %v", err)
	}
	for _, ent := range entries {
		if _, ok := required[ent.MountDir]; ok {
			delete(required, ent.MountDir)
		}
	}
	// non empty required indicates missing mountpoint(s)
	if len(required) > 0 {
		var sorted []string
		for path := range required {
			sorted = append(sorted, path)
		}
		sort.Strings(sorted)
		parts := append([]string{""}, sorted...)
		return fmt.Errorf("cannot preseed without the following mountpoints:%s", strings.Join(parts, "\n - "))
	}

	path := filepath.Join(preseedChroot, "/sys/kernel/security/apparmor")
	if exists := osutil.FileExists(path); !exists {
		return fmt.Errorf("cannot preseed without access to %q", path)
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

	model, err := seed.Model()
	if err != nil {
		return "", err
	}

	// TODO: implement preseeding for core.
	if !model.Classic() {
		return "", fmt.Errorf("preseeding is only supported on classic systems")
	}

	var required string
	if seed.UsesSnapdSnap() {
		required = "snapd"
	} else {
		required = "core"
	}

	var snapPath string
	ess := seed.EssentialSnaps()
	if len(ess) > 0 {
		// core / snapd snap is the first essential snap.
		if ess[0].SnapName() == required {
			snapPath = ess[0].Path
		}
	}

	if snapPath == "" {
		return "", fmt.Errorf("%s snap not found", required)
	}

	return snapPath, nil
}

const snapdPreseedSupportVer = `2.43.3+`

func checkTargetSnapdVersion(infoPath string) error {
	ver, err := snapdtool.SnapdVersionFromInfoFile(infoPath)
	if err != nil {
		return err
	}

	res, err := strutil.VersionCompare(ver, snapdPreseedSupportVer)
	if err != nil {
		return err
	}
	if res < 0 {
		return fmt.Errorf("snapd %s from the target system does not support preseeding, the minimum required version is %s",
			ver, snapdPreseedSupportVer)
	}
	return nil
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

	fstype, fsopts := squashfs.FsType()
	cmd := exec.Command("mount", "-t", fstype, "-o", strings.Join(fsopts, ","), coreSnapPath, where)
	if err := cmd.Run(); err != nil {
		removeMountpoint()
		return nil, fmt.Errorf("cannot mount %s at %s in preseed mode: %v ", coreSnapPath, where, err)
	}

	unmount := func() {
		fmt.Fprintf(Stdout, "unmounting: %s\n", snapdMountPath)
		cmd := exec.Command("umount", snapdMountPath)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(Stderr, "%v", err)
		}
	}

	// read version from the mounted core snap
	infoPath := filepath.Join(snapdMountPath, dirs.CoreLibExecDir, "info")
	if err := checkTargetSnapdVersion(infoPath); err != nil {
		unmount()
		removeMountpoint()
		return nil, err
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
