// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	// snapdMountPath is where target core/snapd is going to be mounted in the target chroot
	snapdMountPath = "/tmp/snapd-preseed"
	syscallChroot  = syscall.Chroot
)

// checkChroot does a basic validity check of the target chroot environment, e.g. makes
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

	// validity checks of the critical mountpoints inside chroot directory.
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

var systemSnapFromSeed = func(seedDir, sysLabel string) (systemSnap string, baseSnap string, err error) {
	seed, err := seedOpen(seedDir, sysLabel)
	if err != nil {
		return "", "", err
	}

	// load assertions into temporary database
	if err := seed.LoadAssertions(nil, nil); err != nil {
		return "", "", err
	}
	model := seed.Model()

	tm := timings.New(nil)

	if err := seed.LoadEssentialMeta(nil, tm); err != nil {
		return "", "", err
	}

	if model.Classic() {
		fmt.Fprintf(Stdout, "ubuntu classic preseeding")
	} else {
		if model.Base() == "core20" {
			fmt.Fprintf(Stdout, "UC20+ preseeding\n")
		} else {
			// TODO: support uc20+
			return "", "", fmt.Errorf("preseeding of ubuntu core with base %s is not supported", model.Base())
		}
	}

	var required string
	if seed.UsesSnapdSnap() {
		required = "snapd"
	} else {
		required = "core"
	}

	var systemSnapPath, baseSnapPath string
	for _, ess := range seed.EssentialSnaps() {
		if ess.SnapName() == required {
			systemSnapPath = ess.Path
		}
		if ess.EssentialType == "base" {
			baseSnapPath = ess.Path
		}
	}

	if systemSnapPath == "" {
		return "", "", fmt.Errorf("%s snap not found", required)
	}

	return systemSnapPath, baseSnapPath, nil
}

const snapdPreseedSupportVer = `2.43.3+`

// chooseTargetSnapdVersion checks if the version of snapd under chroot env
// is good enough for preseeding. It checks both the snapd from the deb
// and from the seeded snap mounted under snapdMountPath and returns the
// information (path, version) about snapd to execute as part of preseeding
// (it picks the newer version of the two).
// The function must be called after syscall.Chroot(..).
func chooseTargetSnapdVersion() (*targetSnapdInfo, error) {
	// read snapd version from the mounted core/snapd snap
	snapdInfoDir := filepath.Join(snapdMountPath, dirs.CoreLibExecDir)
	verFromSnap, _, err := snapdtool.SnapdVersionFromInfoFile(snapdInfoDir)
	if err != nil {
		return nil, err
	}

	// read snapd version from the main fs under chroot (snapd from the deb);
	// assumes running under chroot already.
	hostInfoDir := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	verFromDeb, _, err := snapdtool.SnapdVersionFromInfoFile(hostInfoDir)
	if err != nil {
		return nil, err
	}

	res, err := strutil.VersionCompare(verFromSnap, verFromDeb)
	if err != nil {
		return nil, err
	}

	var whichVer, snapdPath string
	if res < 0 {
		// snapd from the deb under chroot is the candidate to run
		whichVer = verFromDeb
		snapdPath = filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir, "snapd")
	} else {
		// snapd from the mounted core/snapd snap is the candidate to run
		whichVer = verFromSnap
		snapdPath = filepath.Join(snapdMountPath, dirs.CoreLibExecDir, "snapd")
	}

	res, err = strutil.VersionCompare(whichVer, snapdPreseedSupportVer)
	if err != nil {
		return nil, err
	}
	if res < 0 {
		return nil, fmt.Errorf("snapd %s from the target system does not support preseeding, the minimum required version is %s",
			whichVer, snapdPreseedSupportVer)
	}

	return &targetSnapdInfo{path: snapdPath, version: whichVer}, nil
}

func prepareCore20Mountpoints(prepareImageDir, tmpPreseedChrootDir, snapdSnapBlob, baseSnapBlob, aaFeaturesDir, writable string) (cleanupMounts func(), err error) {
	underPreseed := func(path string) string {
		return filepath.Join(tmpPreseedChrootDir, path)
	}

	if err := os.MkdirAll(filepath.Join(writable, "system-data", "etc"), 0755); err != nil {
		return nil, err
	}
	where := filepath.Join(snapdMountPath)
	if err := os.MkdirAll(where, 0755); err != nil {
		return nil, err
	}

	var mounted []string

	doUnmount := func(mnt string) {
		cmd := exec.Command("umount", mnt)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(Stdout, "cannot unmount: %v\n'umount %s' failed with: %s", err, mnt, out)
		}
	}

	cleanupMounts = func() {
		// unmount all the mounts but the first one, which is the base
		// and it is cleaned up last
		for i := len(mounted) - 1; i > 0; i-- {
			mnt := mounted[i]
			doUnmount(mnt)
		}

		entries, err := osutil.LoadMountInfo()
		if err != nil {
			fmt.Fprintf(Stdout, "cannot parse mount info when cleaning up mount points: %v", err)
			return
		}
		// cleanup after handle-writable-paths
		for _, ent := range entries {
			if ent.MountDir != tmpPreseedChrootDir && strings.HasPrefix(ent.MountDir, tmpPreseedChrootDir) {
				doUnmount(ent.MountDir)
			}
		}

		// finally, umount the base snap
		if len(mounted) > 0 {
			doUnmount(mounted[0])
		}
	}

	cleanupOnError := func() {
		if err == nil {
			return
		}

		if cleanupMounts != nil {
			cleanupMounts()
		}
	}
	defer cleanupOnError()

	mounts := [][]string{
		{"-o", "loop", baseSnapBlob, tmpPreseedChrootDir},
		{"-o", "loop", snapdSnapBlob, snapdMountPath},
		{"-t", "tmpfs", "tmpfs", underPreseed("run")},
		{"-t", "tmpfs", "tmpfs", underPreseed("var/tmp")},
		{"--bind", underPreseed("var/tmp"), underPreseed("tmp")},
		{"-t", "proc", "proc", underPreseed("proc")},
		{"-t", "sysfs", "sysfs", underPreseed("sys")},
		{"-t", "devtmpfs", "udev", underPreseed("dev")},
		{"-t", "securityfs", "securityfs", underPreseed("sys/kernel/security")},
		{"--bind", writable, underPreseed("writable")},
	}

	var out []byte
	for _, mountArgs := range mounts {
		cmd := exec.Command("mount", mountArgs...)
		if out, err = cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cannot prepare mountpoint in preseed mode: %v\n'mount %s' failed with: %s", err, strings.Join(mountArgs, " "), out)
		}
		mounted = append(mounted, mountArgs[len(mountArgs)-1])
	}

	cmd := exec.Command(underPreseed("/usr/lib/core/handle-writable-paths"), tmpPreseedChrootDir)
	if out, err = cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("handle-writable-paths failed with: %v\n%s", err, out)
	}

	for _, dir := range []string{
		"etc/udev/rules.d", "etc/systemd/system", "etc/dbus-1/session.d",
		"var/lib/snapd/seed", "var/cache/snapd", "var/cache/apparmor",
		"var/snap", "snap", "var/lib/extrausers",
	} {
		if err = os.MkdirAll(filepath.Join(writable, dir), 0755); err != nil {
			return nil, err
		}
	}

	underWritable := func(path string) string {
		return filepath.Join(writable, path)
	}
	mounts = [][]string{
		{"--bind", underWritable("system-data/var/lib/snapd"), underPreseed("var/lib/snapd")},
		{"--bind", underWritable("system-data/var/cache/snapd"), underPreseed("var/cache/snapd")},
		{"--bind", underWritable("system-data/var/cache/apparmor"), underPreseed("var/cache/apparmor")},
		{"--bind", underWritable("system-data/var/snap"), underPreseed("var/snap")},
		{"--bind", underWritable("system-data/snap"), underPreseed("snap")},
		{"--bind", underWritable("system-data/etc/systemd"), underPreseed("etc/systemd")},
		{"--bind", underWritable("system-data/etc/dbus-1"), underPreseed("etc/dbus-1")},
		{"--bind", underWritable("system-data/etc/udev/rules.d"), underPreseed("etc/udev/rules.d")},
		{"--bind", underWritable("system-data/var/lib/extrausers"), underPreseed("var/lib/extrausers")},
		{"--bind", filepath.Join(snapdMountPath, "/usr/lib/snapd"), underPreseed("/usr/lib/snapd")},
		{"--bind", filepath.Join(prepareImageDir, "system-seed"), underPreseed("var/lib/snapd/seed")},
	}

	if aaFeaturesDir != "" {
		mounts = append(mounts, []string{"--bind", aaFeaturesDir, underPreseed("sys/kernel/security/apparmor/features")})
	}

	for _, mountArgs := range mounts {
		cmd := exec.Command("mount", mountArgs...)
		if out, err = cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cannot prepare mountpoint in preseed mode: %v\n'mount %s' failed with: %s", err, strings.Join(mountArgs, " "), out)
		}
		mounted = append(mounted, mountArgs[len(mountArgs)-1])
	}

	return cleanupMounts, nil
}

func systemForPreseeding(systemsDir string) (label string, err error) {
	systemLabels, err := filepath.Glob(filepath.Join(systemsDir, "systems", "*"))
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot list available systems: %v", err)
	}
	if len(systemLabels) != 1 {
		return "", fmt.Errorf("expected a single system for preseeding, found %d", len(systemLabels))
	}
	return filepath.Base(systemLabels[0]), nil
}

var makePreseedTempDir = func() (string, error) {
	return ioutil.TempDir("", "preseed-")
}

var makeWritableTempDir = func() (string, error) {
	return ioutil.TempDir("", "writable-")
}

func prepareCore20Chroot(prepareImageDir, aaFeaturesDir string) (preseed *preseedOpts, cleanup func(), err error) {
	sysDir := filepath.Join(prepareImageDir, "system-seed")
	sysLabel, err := systemForPreseeding(sysDir)
	if err != nil {
		return nil, nil, err
	}
	snapdSnapPath, baseSnapPath, err := systemSnapFromSeed(sysDir, sysLabel)
	if err != nil {
		return nil, nil, err
	}

	if snapdSnapPath == "" {
		return nil, nil, fmt.Errorf("snapd snap not found")
	}
	if baseSnapPath == "" {
		return nil, nil, fmt.Errorf("base snap not found")
	}

	tmpPreseedChrootDir, err := makePreseedTempDir()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot prepare uc20 chroot: %v", err)
	}
	writableTmpDir, err := makeWritableTempDir()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot prepare uc20 chroot: %v", err)
	}

	cleanupMounts, err := prepareCore20Mountpoints(prepareImageDir, tmpPreseedChrootDir, snapdSnapPath, baseSnapPath, aaFeaturesDir, writableTmpDir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot prepare uc20 mountpoints: %v", err)
	}

	cleanup = func() {
		cleanupMounts()
		if err := os.RemoveAll(tmpPreseedChrootDir); err != nil {
			fmt.Fprintf(Stdout, "%v", err)
		}
		if err := os.RemoveAll(writableTmpDir); err != nil {
			fmt.Fprintf(Stdout, "%v", err)
		}
	}

	opts := &preseedOpts{
		PrepareImageDir:  prepareImageDir,
		PreseedChrootDir: tmpPreseedChrootDir,
		SystemLabel:      sysLabel,
		WritableDir:      writableTmpDir,
	}
	return opts, cleanup, nil
}

func prepareClassicChroot(preseedChroot string) (*targetSnapdInfo, func(), error) {
	if err := syscallChroot(preseedChroot); err != nil {
		return nil, nil, fmt.Errorf("cannot chroot into %s: %v", preseedChroot, err)
	}

	if err := os.Chdir("/"); err != nil {
		return nil, nil, fmt.Errorf("cannot chdir to /: %v", err)
	}

	// GlobalRootDir is now relative to chroot env. We assume all paths
	// inside the chroot to be identical with the host.
	rootDir := dirs.GlobalRootDir
	if rootDir == "" {
		rootDir = "/"
	}

	coreSnapPath, _, err := systemSnapFromSeed(dirs.SnapSeedDirUnder(rootDir), "")
	if err != nil {
		return nil, nil, err
	}

	// create mountpoint for core/snapd
	where := filepath.Join(rootDir, snapdMountPath)
	if err := os.MkdirAll(where, 0755); err != nil {
		return nil, nil, err
	}

	removeMountpoint := func() {
		if err := os.Remove(where); err != nil {
			fmt.Fprintf(Stderr, "%v", err)
		}
	}

	fstype, fsopts := squashfs.FsType()
	mountArgs := []string{"-t", fstype, "-o", strings.Join(fsopts, ","), coreSnapPath, where}
	cmd := exec.Command("mount", mountArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		removeMountpoint()
		return nil, nil, fmt.Errorf("cannot mount %s at %s in preseed mode: %v\n'mount %s' failed with: %s", coreSnapPath, where, err, strings.Join(mountArgs, " "), out)
	}

	unmount := func() {
		fmt.Fprintf(Stdout, "unmounting: %s\n", snapdMountPath)
		cmd := exec.Command("umount", snapdMountPath)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(Stderr, "%v", err)
		}
	}

	targetSnapd, err := chooseTargetSnapdVersion()
	if err != nil {
		unmount()
		removeMountpoint()
		return nil, nil, err
	}

	return targetSnapd, func() {
		unmount()
		removeMountpoint()
	}, nil
}

type preseedFilePatterns struct {
	Exclude []string `json:"exclude"`
	Include []string `json:"include"`
}

func createPreseedArtifact(opts *preseedOpts) (digest []byte, err error) {
	artifactPath := filepath.Join(opts.PrepareImageDir, "system-seed", "systems", opts.SystemLabel, "preseed.tgz")
	systemData := filepath.Join(opts.WritableDir, "system-data")

	patternsFile := filepath.Join(opts.PreseedChrootDir, "usr/lib/snapd/preseed.json")
	pf, err := os.Open(patternsFile)
	if err != nil {
		return nil, err
	}

	var patterns preseedFilePatterns
	dec := json.NewDecoder(pf)
	if err := dec.Decode(&patterns); err != nil {
		return nil, err
	}

	args := []string{"-czf", artifactPath, "-p", "-C", systemData}
	for _, excl := range patterns.Exclude {
		args = append(args, "--exclude", excl)
	}
	for _, incl := range patterns.Include {
		// tar doesn't support globs for files to include, since we are not using shell we need to
		// handle globs explicitly.
		matches, err := filepath.Glob(filepath.Join(systemData, incl))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			relPath, err := filepath.Rel(systemData, m)
			if err != nil {
				return nil, err
			}
			args = append(args, relPath)
		}
	}

	cmd := exec.Command("tar", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%v (%s)", err, out)
	}

	sha3_384, _, err := osutil.FileDigest(artifactPath, crypto.SHA3_384)
	return sha3_384, err
}

// runPreseedMode runs snapd in a preseed mode. It assumes running in a chroot.
// The chroot is expected to be set-up and ready to use (critical system directories mounted).
func runPreseedMode(preseedChroot string, targetSnapd *targetSnapdInfo) error {
	// run snapd in preseed mode
	cmd := exec.Command(targetSnapd.path)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAPD_PRESEED=1")
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout

	// note, snapdPath is relative to preseedChroot
	fmt.Fprintf(Stdout, "starting to preseed root: %s\nusing snapd binary: %s (%s)\n", preseedChroot, targetSnapd.path, targetSnapd.version)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running snapd in preseed mode: %v\n", err)
	}

	return nil
}

func runUC20PreseedMode(opts *preseedOpts) error {
	cmd := exec.Command("chroot", opts.PreseedChrootDir, "/usr/lib/snapd/snapd")
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAPD_PRESEED=1")
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout
	fmt.Fprintf(Stdout, "starting to preseed UC20+ system: %s\n", opts.PreseedChrootDir)

	if err := cmd.Run(); err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.ENOEXEC {
			return fmt.Errorf(`error running snapd, please try installing the "qemu-user-static" package: %v`, err)
		}

		return fmt.Errorf("error running snapd in preseed mode: %v\n", err)
	}

	digest, err := createPreseedArtifact(opts)
	if err != nil {
		return fmt.Errorf("cannot create preseed.tgz: %v", err)
	}

	if err := writePreseedAssertion(digest, opts); err != nil {
		return fmt.Errorf("cannot create preseed assertion: %v", err)
	}

	return nil
}

// Core20 runs preseeding of UC20 system prepared by prepare-image in prepareImageDir
// and stores the resulting preseed preseed.tgz file in system-seed/systems/<systemlabel>/preseed.tgz.
// Expects single systemlabel under systems directory.
func Core20(prepareImageDir, preseedSignKey, aaFeaturesDir string) error {
	var err error
	prepareImageDir, err = filepath.Abs(prepareImageDir)
	if err != nil {
		return err
	}

	popts, cleanup, err := prepareCore20Chroot(prepareImageDir, aaFeaturesDir)
	if err != nil {
		return err
	}
	defer cleanup()

	popts.PreseedSignKey = preseedSignKey
	return runUC20PreseedMode(popts)
}

// Classic runs preseeding of a classic ubuntu system pointed by chrootDir.
func Classic(chrootDir string) error {
	var err error
	chrootDir, err = filepath.Abs(chrootDir)
	if err != nil {
		return err
	}

	if err := checkChroot(chrootDir); err != nil {
		return err
	}

	var targetSnapd *targetSnapdInfo

	// XXX: if prepareClassicChroot & runPreseedMode were refactored to
	// use "chroot" inside runPreseedMode (and not syscall.Chroot at the
	// beginning of prepareClassicChroot), then we could have a single
	// runPreseedMode/runUC20PreseedMode function that handles both classic
	// and core20.
	targetSnapd, cleanup, err := prepareClassicChroot(chrootDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// executing inside the chroot
	return runPreseedMode(chrootDir, targetSnapd)
}

func MockSyscallChroot(f func(string) error) (restore func()) {
	osutil.MustBeTestBinary("mocking can be done only in tests")

	oldSyscallChroot := syscallChroot
	syscallChroot = f
	return func() { syscallChroot = oldSyscallChroot }
}

func MockSnapdMountPath(path string) (restore func()) {
	osutil.MustBeTestBinary("mocking can be done only in tests")

	oldMountPath := snapdMountPath
	snapdMountPath = path
	return func() { snapdMountPath = oldMountPath }
}

func MockSystemSnapFromSeed(f func(rootDir, sysLabel string) (string, string, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking can be done only in tests")

	oldSystemSnapFromSeed := systemSnapFromSeed
	systemSnapFromSeed = f
	return func() { systemSnapFromSeed = oldSystemSnapFromSeed }
}

func MockMakePreseedTempDir(f func() (string, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking can be done only in tests")

	old := makePreseedTempDir
	makePreseedTempDir = f
	return func() {
		makePreseedTempDir = old
	}
}

func MockMakeWritableTempDir(f func() (string, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking can be done only in tests")

	old := makeWritableTempDir
	makeWritableTempDir = f
	return func() {
		makeWritableTempDir = old
	}
}
