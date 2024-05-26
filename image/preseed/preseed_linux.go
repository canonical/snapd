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
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	// snapdMountPath is where target core/snapd is going to be mounted in the target chroot
	snapdMountPath = "/tmp/snapd-preseed"
	syscallChroot  = syscall.Chroot
	// list of the permitted sysfs overlay paths
	// this list has to be kept in sync with sysfs paths used by snap interfaces, e.g. gpio
	permitedSysfsOverlays = []string{
		"sys/class/backlight", "sys/class/bluetooth", "sys/class/gpio",
		"sys/class/leds", "sys/class/ptp", "sys/class/pwm",
		"sys/class/rtc", "sys/class/video4linux", "sys/devices/platform",
		"sys/devices/pci0000:00",
	}
)

// checkChroot does a basic validity check of the target chroot environment, e.g. makes
// sure critical virtual filesystems (such as proc) are mounted. This is not meant to
// be exhaustive check, but one that prevents running the tool against a wrong directory
// by an accident, which would lead to hard to understand errors from snapd in preseed
// mode.
func checkChroot(preseedChroot string) error {
	exists, isDir := mylog.Check3(osutil.DirExists(preseedChroot))

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
	entries := mylog.Check2(osutil.LoadMountInfo())

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
	seed := mylog.Check2(seedOpen(seedDir, sysLabel))
	mylog.Check(

		// load assertions into temporary database
		seed.LoadAssertions(nil, nil))

	model := seed.Model()

	tm := timings.New(nil)
	mylog.Check(seed.LoadEssentialMeta(nil, tm))

	if model.Classic() {
		fmt.Fprintf(Stdout, "ubuntu classic preseeding\n")
	} else {
		coreVersion := mylog.Check2(naming.CoreVersion(model.Base()))

		if coreVersion >= 20 {
			fmt.Fprintf(Stdout, "UC20+ preseeding\n")
		} else {
			return "", "", fmt.Errorf("preseeding of ubuntu core with base %s is not supported: core20 or later is expected", model.Base())
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

const (
	snapdPreseedSupportVer  = `2.43.3+`
	snapdPreseedResetReexec = `2.59`
)

// chooseTargetSnapdVersion checks if the version of snapd under chroot env
// is good enough for preseeding. It checks both the snapd from the deb
// and from the seeded snap mounted under snapdMountPath and returns the
// information (path, version) about snapd to execute as part of preseeding
// (it picks the newer version of the two).
// The function must be called after syscall.Chroot(..).
func chooseTargetSnapdVersion() (*targetSnapdInfo, error) {
	// read snapd version from the mounted core/snapd snap
	snapdInfoDir := filepath.Join(snapdMountPath, dirs.CoreLibExecDir)
	verFromSnap, _ := mylog.Check3(snapdtool.SnapdVersionFromInfoFile(snapdInfoDir))

	// read snapd version from the main fs under chroot (snapd from the deb);
	// assumes running under chroot already.
	hostInfoDir := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	verFromDeb, _ := mylog.Check3(snapdtool.SnapdVersionFromInfoFile(hostInfoDir))

	res := mylog.Check2(strutil.VersionCompare(verFromSnap, verFromDeb))

	var whichVer, snapdPath, preseedPath string
	if res < 0 {
		// snapd from the deb under chroot is the candidate to run
		whichVer = verFromDeb
		snapdPath = filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir, "snapd")
		preseedPath = filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir, "snap-preseed")
	} else {
		// snapd from the mounted core/snapd snap is the candidate to run
		whichVer = verFromSnap
		snapdPath = filepath.Join(snapdMountPath, dirs.CoreLibExecDir, "snapd")
		preseedPath = filepath.Join(snapdMountPath, dirs.CoreLibExecDir, "snap-preseed")
	}

	res = mylog.Check2(strutil.VersionCompare(whichVer, snapdPreseedSupportVer))

	if res < 0 {
		return nil, fmt.Errorf("snapd %s from the target system does not support preseeding, the minimum required version is %s",
			whichVer, snapdPreseedSupportVer)
	}

	return &targetSnapdInfo{path: snapdPath, preseedPath: preseedPath, version: whichVer}, nil
}

func prepareCore20Mountpoints(opts *preseedCoreOptions) (cleanupMounts func(), err error) {
	underPreseed := func(path string) string {
		return filepath.Join(opts.PreseedChrootDir, path)
	}

	underOverlay := func(path string) string {
		return filepath.Join(opts.SysfsOverlay, path)
	}
	mylog.Check(os.MkdirAll(filepath.Join(opts.WritableDir, "system-data", "etc"), 0755))

	where := filepath.Join(snapdMountPath)
	mylog.Check(os.MkdirAll(where, 0755))

	var mounted []string

	doUnmount := func(mnt string) {
		cmd := exec.Command("umount", mnt)
		if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
			fmt.Fprintf(Stdout, "cannot unmount: %v\n'umount %s' failed with: %s", err, mnt, out)
		}
	}

	underWritable := func(path string) string {
		return filepath.Join(opts.WritableDir, path)
	}

	currentLink := underWritable("system-data/snap/snapd/current")
	currentSnapdMountPoint := underWritable("system-data/snap/snapd/preseeding")

	cleanupMounts = func() {
		path := mylog.Check2(os.Readlink(currentLink))
		if err == nil && path == "preseeding" {
			os.Remove(currentLink)
		}

		// unmount all the mounts but the first one, which is the base
		// and it is cleaned up last
		for i := len(mounted) - 1; i > 0; i-- {
			mnt := mounted[i]
			doUnmount(mnt)
		}

		entries := mylog.Check2(osutil.LoadMountInfo())

		// cleanup after handle-writable-paths
		for _, ent := range entries {
			if ent.MountDir != opts.PreseedChrootDir && strings.HasPrefix(ent.MountDir, opts.PreseedChrootDir) {
				doUnmount(ent.MountDir)
			}
		}

		// finally, umount the base snap
		if len(mounted) > 0 {
			doUnmount(mounted[0])
		}

		// Remove mount point if empty
		os.Remove(currentSnapdMountPoint)
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
		{"-o", "loop", opts.BaseSnapPath, opts.PreseedChrootDir},
		{"-o", "loop", opts.SnapdSnapPath, snapdMountPath},
		{"-t", "tmpfs", "tmpfs", underPreseed("run")},
		{"-t", "tmpfs", "tmpfs", underPreseed("var/tmp")},
		{"--bind", underPreseed("var/tmp"), underPreseed("tmp")},
		{"-t", "proc", "proc", underPreseed("proc")},
		{"-t", "sysfs", "sysfs", underPreseed("sys")},
		{"-t", "devtmpfs", "udev", underPreseed("dev")},
		{"-t", "securityfs", "securityfs", underPreseed("sys/kernel/security")},
		{"--bind", opts.WritableDir, underPreseed("writable")},
	}

	if opts.SysfsOverlay != "" {
		// bind mount only permitted directories under sys/class and sys/devices
		for _, dir := range permitedSysfsOverlays {
			info := mylog.Check2(os.Stat(underOverlay(dir)))
			if err == nil && info.IsDir() {
				mylog.Check(
					// ensure dir exists
					os.MkdirAll(underPreseed(dir), os.ModePerm))

				mounts = append(mounts, []string{"--bind", underOverlay(dir), underPreseed(dir)})
			}
		}
	}

	var out []byte
	for _, mountArgs := range mounts {
		cmd := exec.Command("mount", mountArgs...)
		if out = mylog.Check2(cmd.CombinedOutput()); err != nil {
			return nil, fmt.Errorf("cannot prepare mountpoint in preseed mode: %v\n'mount %s' failed with: %s", err, strings.Join(mountArgs, " "), out)
		}
		mounted = append(mounted, mountArgs[len(mountArgs)-1])
	}

	cmd := exec.Command(underPreseed("/usr/lib/core/handle-writable-paths"), opts.PreseedChrootDir)
	if out = mylog.Check2(cmd.CombinedOutput()); err != nil {
		return nil, fmt.Errorf("handle-writable-paths failed with: %v\n%s", err, out)
	}

	for _, dir := range []string{
		"etc/udev/rules.d", "etc/systemd/system", "etc/dbus-1/session.d",
		"var/lib/snapd/seed", "var/cache/snapd", "var/cache/apparmor",
		"var/snap", "snap", "var/lib/extrausers",
	} {
		mylog.Check(os.MkdirAll(filepath.Join(opts.WritableDir, dir), 0755))
	}
	mylog.Check(

		// because of the way snapd snap is built, we need the
		// 'current' symlink to exist when the snapd binary is
		// invoked, so that any runtime libraries will be correctly
		// resolved, so we bind-mount the snapd snap at a side
		// location, and create the symlink 'current' to point to that
		// location This symlink can then be easily replaced by snapd
		// as it preseeds the image
		os.MkdirAll(filepath.Dir(currentLink), 0755))
	mylog.Check(os.MkdirAll(currentSnapdMountPoint, 0755))
	mylog.Check(os.Symlink("preseeding", currentLink))

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
		{"--bind", snapdMountPath, underPreseed("/snap/snapd/preseeding")},
		{"--bind", filepath.Join(opts.PrepareImageDir, "system-seed"), underPreseed("var/lib/snapd/seed")},
	}

	if opts.AppArmorKernelFeaturesDir != "" {
		mounts = append(mounts, []string{"--bind", opts.AppArmorKernelFeaturesDir, underPreseed("sys/kernel/security/apparmor/features")})
	}

	for _, mountArgs := range mounts {
		cmd := exec.Command("mount", mountArgs...)
		if out = mylog.Check2(cmd.CombinedOutput()); err != nil {
			return nil, fmt.Errorf("cannot prepare mountpoint in preseed mode: %v\n'mount %s' failed with: %s", err, strings.Join(mountArgs, " "), out)
		}
		mounted = append(mounted, mountArgs[len(mountArgs)-1])
	}

	return cleanupMounts, nil
}

func systemForPreseeding(systemsDir string) (label string, err error) {
	systemLabels := mylog.Check2(filepath.Glob(filepath.Join(systemsDir, "systems", "*")))
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot list available systems: %v", err)
	}
	if len(systemLabels) != 1 {
		return "", fmt.Errorf("expected a single system for preseeding, found %d", len(systemLabels))
	}
	return filepath.Base(systemLabels[0]), nil
}

var makePreseedTempDir = func() (string, error) {
	return os.MkdirTemp("", "preseed-")
}

var makeWritableTempDir = func() (string, error) {
	return os.MkdirTemp("", "writable-")
}

func prepareCore20Chroot(opts *CoreOptions) (popts *preseedCoreOptions, cleanup func(), err error) {
	popts = &preseedCoreOptions{
		CoreOptions: *opts,
	}
	sysDir := filepath.Join(opts.PrepareImageDir, "system-seed")
	popts.SystemLabel = mylog.Check2(systemForPreseeding(sysDir))

	popts.SnapdSnapPath, popts.BaseSnapPath = mylog.Check3(systemSnapFromSeed(sysDir, popts.SystemLabel))

	if popts.SnapdSnapPath == "" {
		return nil, nil, fmt.Errorf("snapd snap not found")
	}
	if popts.BaseSnapPath == "" {
		return nil, nil, fmt.Errorf("base snap not found")
	}

	popts.PreseedChrootDir = mylog.Check2(makePreseedTempDir())

	popts.WritableDir = mylog.Check2(makeWritableTempDir())

	cleanupMounts := mylog.Check2(prepareCore20Mountpoints(popts))

	cleanup = func() {
		cleanupMounts()
		mylog.Check(os.RemoveAll(popts.PreseedChrootDir))
		mylog.Check(os.RemoveAll(popts.WritableDir))
		mylog.Check(os.RemoveAll(snapdMountPath))
	}

	return popts, cleanup, nil
}

func mountSnapdSnap(rootDir string, coreSnapPath string) (cleanup func(), err error) {
	// create mountpoint for core/snapd
	where := filepath.Join(rootDir, snapdMountPath)
	mylog.Check(os.MkdirAll(where, 0755))

	removeMountpoint := func() {
		mylog.Check(os.Remove(where))
	}

	fstype, fsopts := squashfs.FsType()
	mountArgs := []string{"-t", fstype, "-o", strings.Join(fsopts, ","), coreSnapPath, where}
	cmd := exec.Command("mount", mountArgs...)
	if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
		removeMountpoint()
		return nil, fmt.Errorf("cannot mount %s at %s in preseed mode: %v\n'mount %s' failed with: %s", coreSnapPath, where, err, strings.Join(mountArgs, " "), out)
	}

	unmount := func() {
		fmt.Fprintf(Stdout, "unmounting: %s\n", where)
		cmd := exec.Command("umount", where)
		mylog.Check(cmd.Run())
	}

	return func() {
		unmount()
		removeMountpoint()
	}, nil
}

func getSnapdVersion(rootDir string) (string, error) {
	coreSnapPath, _ := mylog.Check3(systemSnapFromSeed(dirs.SnapSeedDirUnder(rootDir), ""))

	cleanup := mylog.Check2(mountSnapdSnap(rootDir, coreSnapPath))

	defer cleanup()

	snapdInfoDir := filepath.Join(rootDir, snapdMountPath, dirs.CoreLibExecDir)
	ver, _ := mylog.Check3(snapdtool.SnapdVersionFromInfoFile(snapdInfoDir))

	return ver, nil
}

func prepareClassicChroot(preseedChroot string, reset bool) (*targetSnapdInfo, func(), error) {
	mylog.Check(syscallChroot(preseedChroot))
	mylog.Check(os.Chdir("/"))

	// GlobalRootDir is now relative to chroot env. We assume all paths
	// inside the chroot to be identical with the host.
	rootDir := dirs.GlobalRootDir
	if rootDir == "" {
		rootDir = "/"
	}

	cleanups := []func(){}
	cleanup := func() {
		for _, c := range cleanups {
			c()
		}
	}
	addCleanup := func(fun func()) {
		cleanups = append([]func(){fun}, cleanups...)
	}
	defer func() {
		cleanup()
	}()

	// The best would have been to check if /proc is a mountpoint.
	// But we would need /proc/self/mountinfo for that.
	if !osutil.FileExists(filepath.Join(rootDir, "/proc/self/cmdline")) {
		cmd := exec.Command("mount", "-t", "proc", "none", filepath.Join(rootDir, "/proc"))
		if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
			return nil, nil, fmt.Errorf("Cannot mount proc and /proc is not available: %s\nOutput: %s", err, out)
		}
		addCleanup(func() {
			cmd := exec.Command("umount", filepath.Join(rootDir, "/proc"))
			if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
				fmt.Fprintf(Stdout, "cannot unmount /proc: %s\noutput: %s\n", err, out)
			}
		})
	}

	// We need loop devices to work to be able to mount the snap.
	if !osutil.FileExists(filepath.Join(rootDir, "/dev/loop-control")) {
		cmd := exec.Command("mount", "-t", "devtmpfs", "none", filepath.Join(rootDir, "/dev"))
		if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
			return nil, nil, fmt.Errorf("Cannot mount devtmpfs and /dev/loop-control not available: %s\nOutput: %s", err, out)
		}
		addCleanup(func() {
			cmd := exec.Command("umount", "--lazy", filepath.Join(rootDir, "/dev"))
			if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
				fmt.Fprintf(Stdout, "cannot unmount /dev: %s\noutput: %s\n", err, out)
			}
		})
	}

	coreSnapPath, _ := mylog.Check3(systemSnapFromSeed(dirs.SnapSeedDirUnder(rootDir), ""))

	unmountSnapd := mylog.Check2(mountSnapdSnap(rootDir, coreSnapPath))

	addCleanup(unmountSnapd)

	// because of the way snapd snap is built, we need the
	// 'current' symlink to exist when the snapd binary is
	// invoked, so that any runtime libraries will be correctly
	// resolved, so we bind-mount the snapd snap at a side
	// location, and create the symlink 'current' to point to that
	// location This symlink can then be easily replaced by snapd
	// as it preseeds the image
	currentLink := filepath.Join(rootDir, "snap/snapd/current")
	mylog.Check(os.MkdirAll(filepath.Dir(currentLink), 0755))

	if reset {
		os.Remove(currentLink)
	}
	mylog.Check(os.Symlink(snapdMountPath, currentLink))

	addCleanup(func() {
		path := mylog.Check2(os.Readlink(currentLink))
		if err == nil && path == snapdMountPath {
			os.Remove(currentLink)
		}
	})

	targetSnapd := mylog.Check2(chooseTargetSnapdVersion())

	stolenCleanup := cleanup
	cleanup = func() {}
	return targetSnapd, stolenCleanup, nil
}

type preseedFilePatterns struct {
	Exclude []string `json:"exclude"`
	Include []string `json:"include"`
}

func createPreseedArtifact(opts *preseedCoreOptions) (digest []byte, err error) {
	artifactPath := filepath.Join(opts.PrepareImageDir, "system-seed", "systems", opts.SystemLabel, "preseed.tgz")
	systemData := filepath.Join(opts.WritableDir, "system-data")

	patternsFile := filepath.Join(opts.PreseedChrootDir, "usr/lib/snapd/preseed.json")
	pf := mylog.Check2(os.Open(patternsFile))

	var patterns preseedFilePatterns
	dec := json.NewDecoder(pf)
	mylog.Check(dec.Decode(&patterns))

	args := []string{"-czf", artifactPath, "-p", "-C", systemData}
	for _, excl := range patterns.Exclude {
		args = append(args, "--exclude", excl)
	}
	for _, incl := range patterns.Include {
		// tar doesn't support globs for files to include, since we are not using shell we need to
		// handle globs explicitly.
		matches := mylog.Check2(filepath.Glob(filepath.Join(systemData, incl)))

		for _, m := range matches {
			relPath := mylog.Check2(filepath.Rel(systemData, m))

			args = append(args, relPath)
		}
	}

	cmd := exec.Command("tar", args...)
	if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
		return nil, fmt.Errorf("%v (%s)", err, out)
	}

	sha3_384, _ := mylog.Check3(osutil.FileDigest(artifactPath, crypto.SHA3_384))
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
	mylog.Check(cmd.Run())

	return nil
}

func reexecReset(preseedChroot string, targetSnapd *targetSnapdInfo) error {
	cmd := exec.Command(targetSnapd.preseedPath, "--reset-chroot")
	cmd.Env = os.Environ()
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout
	mylog.Check(cmd.Run())

	return nil
}

func runUC20PreseedMode(opts *preseedCoreOptions) error {
	cmd := exec.Command("chroot", opts.PreseedChrootDir, "/usr/lib/snapd/snapd")
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAPD_PRESEED=1")
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout
	fmt.Fprintf(Stdout, "starting to preseed UC20+ system: %s\n", opts.PreseedChrootDir)
	mylog.Check(cmd.Run())

	digest := mylog.Check2(createPreseedArtifact(opts))
	mylog.Check(writePreseedAssertion(digest, opts))

	return nil
}

// Core20 runs preseeding of UC20 system prepared by prepare-image in prepareImageDir
// and stores the resulting preseed preseed.tgz file in system-seed/systems/<systemlabel>/preseed.tgz.
func Core20(opts *CoreOptions) error {
	opts.PrepareImageDir = mylog.Check2(filepath.Abs(opts.PrepareImageDir))

	popts, cleanup := mylog.Check3(prepareCore20Chroot(opts))

	defer cleanup()

	return runUC20PreseedMode(popts)
}

// Classic runs preseeding of a classic ubuntu system pointed by chrootDir.
func Classic(chrootDir string) error {
	chrootDir = mylog.Check2(filepath.Abs(chrootDir))
	mylog.Check(checkChroot(chrootDir))

	var targetSnapd *targetSnapdInfo

	// XXX: if prepareClassicChroot & runPreseedMode were refactored to
	// use "chroot" inside runPreseedMode (and not syscall.Chroot at the
	// beginning of prepareClassicChroot), then we could have a single
	// runPreseedMode/runUC20PreseedMode function that handles both classic
	// and core20.
	const reset = false
	targetSnapd, cleanup := mylog.Check3(prepareClassicChroot(chrootDir, reset))

	defer cleanup()

	// executing inside the chroot
	return runPreseedMode(chrootDir, targetSnapd)
}

func ClassicReset(chrootDir string) error {
	chrootDir = mylog.Check2(filepath.Abs(chrootDir))

	snapdVersion := mylog.Check2(getSnapdVersion(chrootDir))

	res := mylog.Check2(strutil.VersionCompare(snapdVersion, snapdPreseedResetReexec))

	if res < 0 {
		return ResetPreseededChroot(chrootDir)
	}

	const reset = true
	targetSnapd, cleanup := mylog.Check3(prepareClassicChroot(chrootDir, reset))

	defer cleanup()

	return reexecReset(chrootDir, targetSnapd)
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
