// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// For testing purposes
var osSymlink = os.Symlink

// doSync is a mockable wrapper around syscall.Sync for tests.
var doSync = syscall.Sync

// atomicWriteFile is a mockable wrapper around osutil.AtomicWriteFile, used
// by writeDriversTreeMeta, so tests can simulate a marker-write failure
// (e.g. ENOSPC) without needing to actually exhaust disk space.
var atomicWriteFile = osutil.AtomicWriteFile

// atomicSymlink is a mockable wrapper around osutil.AtomicSymlink for tests.
var atomicSymlink = osutil.AtomicSymlink

// kernelDriversTreeGeneratorVersion identifies the logic that produced a
// kernel drivers tree (the on-disk symlinks/files under
// <destDir>/lib/{modules,firmware}).
//
// IMPORTANT: bump this whenever there is a change to the layout or organization of the
// kernel drivers or firmware trees.
var kernelDriversTreeGeneratorVersion = 1

// driversTreeMeta is the content of the <destDir>/kernel.json marker file
// written after every successful kernel drivers tree build.
type driversTreeMeta struct {
	GeneratorVersion int `json:"generator-version"`
}

func driversTreeMetaPath(destDir string) string {
	return filepath.Join(destDir, "kernel.json")
}

// writeDriversTreeMeta records the generator version that produced destDir.
func writeDriversTreeMeta(destDir string) error {
	meta := driversTreeMeta{GeneratorVersion: kernelDriversTreeGeneratorVersion}
	data, err := json.Marshal(&meta)
	if err != nil {
		return err
	}
	return atomicWriteFile(driversTreeMetaPath(destDir), data, 0644, 0)
}

var (
	errGeneratorMetaCorrupted = errors.New("kernel drivers tree generator metadata file is corrupted")
)

// readDriversTreeMeta returns the generator metadata recorded for
// destDir. If no marker value is present a default zero value with
// GeneratorVersion set to 0 is returned and no error.
func readDriversTreeMeta(destDir string) (driversTreeMeta, error) {
	data, err := os.ReadFile(driversTreeMetaPath(destDir))
	if errors.Is(err, fs.ErrNotExist) {
		return driversTreeMeta{
			// Explicit zero value so that there are no misconceptions
			// of what it means
			GeneratorVersion: 0,
		}, nil
	}
	if err != nil {
		return driversTreeMeta{}, err
	}
	var meta driversTreeMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		// The marker could be corrupted, which means that the tree likely needs a rebuild.
		return driversTreeMeta{}, errGeneratorMetaCorrupted
	}
	return meta, nil
}

// DriversTreeOutdated returns true when the kernel modules & firmware tree at
// destDir was built by an older version of the generator code, indicating it
// may need to be checked or rebuilt.
func DriversTreeOutdated(destDir string) (bool, error) {
	v, err := readDriversTreeMeta(destDir)
	if err != nil {
		if errors.Is(err, errGeneratorMetaCorrupted) {
			// Corrupted metadata file warrants a rebuild.
			return true, nil
		}
		return false, err
	}
	logger.Debugf("checking kernel tree generator version, current %v, on disk %v",
		kernelDriversTreeGeneratorVersion, v.GeneratorVersion)
	// A tree marked with a version *newer* than what is currently running (e.g.
	// after a snapd revert) is deliberately NOT considered outdated: rebuilding
	// it with older, possibly-buggy logic could regress a fix already applied
	// by the newer generator. Only consider the kernel tree to be outdated if
	// the current snapd version is strictly newer.
	return kernelDriversTreeGeneratorVersion > v.GeneratorVersion, nil
}

// We expect as a minimum something that starts with three numbers
// separated by dots for the kernel version.
var utsRelease = regexp.MustCompile(`^([0-9]+\.){2}[0-9]+`)

// KernelVersionFromModulesDir returns the kernel version for a mounted kernel
// snap (this would be the output if "uname -r" for a running kernel). It
// assumes that there is a folder named modules/$(uname -r) inside the snap.
func KernelVersionFromModulesDir(mountPoint string) (string, error) {
	modsDir := filepath.Join(mountPoint, "modules")
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return "", err
	}

	kversion := ""
	for _, node := range entries {
		if !node.Type().IsDir() {
			continue
		}
		if !utsRelease.MatchString(node.Name()) {
			continue
		}
		if kversion != "" {
			return "", fmt.Errorf("more than one modules directory in %q", modsDir)
		}
		kversion = node.Name()
	}
	if kversion == "" {
		return "", fmt.Errorf("no modules directory found in %q", modsDir)
	}

	return kversion, nil
}

// firmwareSymlinkTarget describes the symlink entry that should exist for
// one entry found in a kernel/component mount's firmware/ directory.
type firmwareSymlinkTarget struct {
	name   string
	target string
}

// firmwareSymlinkTargets computes the desired firmware symlinks for a
// mount, shared by createFirmwareSymlinks and the live sync used by
// Regenerate mode.
func firmwareSymlinkTargets(fwMount MountPoints, fwDest string) ([]firmwareSymlinkTarget, error) {
	fwOrig := fwMount.UnderCurrentPath("firmware")
	entries, err := os.ReadDir(fwOrig)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debugf("no firmware found in %q", fwOrig)
			return nil, nil
		}
		return nil, err
	}

	fwTarget := fwMount.UnderTargetPath("firmware")
	var targets []firmwareSymlinkTarget
	for _, node := range entries {
		switch node.Type() {
		case 0, fs.ModeDir:
			// "updates" is included in (latest) kernel snaps but
			// is empty, and we use if for firmware shipped in
			// components, so we ignore it.
			if node.Name() == "updates" {
				continue
			}
			// Create link for regular files or directories
			targets = append(targets, firmwareSymlinkTarget{
				name:   node.Name(),
				target: filepath.Join(fwTarget, node.Name()),
			})
		case fs.ModeSymlink:
			// Replicate link (it should be relative)
			// TODO check this in snap pack
			lpath := filepath.Join(fwDest, node.Name())
			dest, err := os.Readlink(filepath.Join(fwOrig, node.Name()))
			if err != nil {
				return nil, err
			}
			if filepath.IsAbs(dest) {
				return nil, fmt.Errorf("symlink %q points to absolute path %q", lpath, dest)
			}
			targets = append(targets, firmwareSymlinkTarget{name: node.Name(), target: dest})
		default:
			return nil, fmt.Errorf("%q has unexpected file type: %s",
				node.Name(), node.Type())
		}
	}

	return targets, nil
}

func createFirmwareSymlinks(fwMount MountPoints, fwDest string) error {
	if err := os.MkdirAll(fwDest, 0755); err != nil {
		return err
	}

	// Symbolic links inside firmware folder - it cannot be directly a
	// symlink to "firmware" as we will use firmware/updates/ subfolder for
	// components.
	targets, err := firmwareSymlinkTargets(fwMount, fwDest)
	if err != nil {
		return err
	}
	for _, t := range targets {
		lpath := filepath.Join(fwDest, t.name)
		if err := os.Symlink(t.target, lpath); err != nil {
			return err
		}
	}

	return nil
}

func createModulesSubtree(kMntPts MountPoints, kernelTree, kversion string, compsMntPts []ModulesCompMountPoints) error {
	// Although empty we need "lib" because "depmod" always appends
	// "/lib/modules/<kernel_version>" to the directory passed with option
	// "-b".
	modsRoot := filepath.Join(kernelTree, "lib", "modules", kversion)
	if err := os.MkdirAll(modsRoot, 0755); err != nil {
		return err
	}

	// Discover the content of the modules directory of the current mount
	// once. The target mount may not exist yet (for example, during
	// install/preseed the target is the future runtime mount), so it must
	// not be read for discovery; only the current mount is guaranteed to be
	// available. The discovered directory names are reused for both the
	// current and the target symlinks (see createKernelModulesSymlinks).
	currentMntDir := kMntPts.UnderCurrentPath("modules", kversion)
	entries, err := os.ReadDir(currentMntDir)
	if err != nil {
		return err
	}

	// Copy modinfo files (modules.*) from the snap; these might be
	// overwritten if kernel-modules components are installed (see
	// setupModsFromComp, which runs depmod). The files are copied from the
	// current mount only: their content is path-independent (module paths
	// are relative to the modules directory), so there is no need to
	// re-copy them for the target mount, which in any case may not exist
	// yet during install/preseed. Only the symlinks are re-pointed to the
	// target mount below, since they encode absolute mount paths.
	//
	// While scanning the modules tree, also collect the directories found
	// under it, to be set up as symlinks below, skipping the ones that are
	// either reserved or not useful in the drivers tree.

	modDirs := map[string]bool{
		"kernel": true, // the default kernel drivers tree
		"vdso":   true, // the expected vdso libs tree
	}
	for _, e := range entries {
		switch {
		case !e.Type().IsDir(): // files & symlinks
			// Copy modprobe artifacts (modules.*).
			if strings.HasPrefix(e.Name(), "modules.") {
				target := filepath.Join(modsRoot, e.Name())
				if err := osutil.CopyFile(filepath.Join(currentMntDir, e.Name()), target, osutil.CopyFlagDefault); err != nil {
					return err
				}
			}
		case e.IsDir():
			n := e.Name()
			switch n {
			// Drop and log entries which would cause conflicts. We are
			// expecting those to have raised an error during snap pack.
			case "updates":
				// Reserved for modules coming from kernel-modules
				// components; do not link it back to the kernel snap.
				logger.Debugf("skipping directory %q in the kernel modules tree, reserved for components", n)
			case "build":
				// Typically a symlink to the kernel source tree; not
				// useful in the drivers tree.
				logger.Debugf("skipping directory %q in the kernel modules tree, typically the kernel source tree", n)
			default:
				modDirs[n] = true
			}
		}
	}

	// Symbolic links to current mount of the kernel snap
	if err := createKernelModulesSymlinks(modsRoot, currentMntDir, modDirs); err != nil {
		return err
	}

	// If necessary, add modules from components and run depmod
	if err := setupModsFromComp(kernelTree, kversion, compsMntPts); err != nil {
		return err
	}

	// Change symlinks to target ones when needed. Reuse the directories
	// discovered from the current mount: the target mount holds the same
	// kernel snap content, just mounted at a different path.
	if !kMntPts.CurrentEqualsTarget() {
		targetMntDir := kMntPts.UnderTargetPath("modules", kversion)
		if err := createKernelModulesSymlinks(modsRoot, targetMntDir, modDirs); err != nil {
			return err
		}
	}

	return nil
}

func createKernelModulesSymlinks(modsRoot, kMntPt string, dirs map[string]bool) error {
	for d := range dirs {
		lname := filepath.Join(modsRoot, d)
		to := filepath.Join(kMntPt, d)

		os.Remove(lname)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	return nil
}

func setupModsFromComp(kernelTree, kversion string, compsMntPts []ModulesCompMountPoints) error {
	// This folder needs to exist always to allow for directory swapping
	// in the future, even if right now we don't have components.
	compsRoot := filepath.Join(kernelTree, "lib", "modules", kversion, "updates")
	if err := os.MkdirAll(compsRoot, 0755); err != nil {
		return err
	}

	if len(compsMntPts) == 0 {
		return nil
	}

	// Symbolic links to components
	for _, cmp := range compsMntPts {
		lname := filepath.Join(compsRoot, cmp.LinkName)
		to := cmp.UnderCurrentPath("modules", kversion)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	// Run depmod
	stdout, stderr, err := osutil.RunSplitOutput("depmod", "-b", kernelTree, kversion)
	if err != nil {
		return osutil.OutputErrCombine(stdout, stderr, err)
	}
	logger.Noticef("depmod output:\n%s\n", string(osutil.CombineStdOutErr(stdout, stderr)))

	// Change symlinks to target ones when needed
	for _, cmp := range compsMntPts {
		if cmp.CurrentEqualsTarget() {
			continue
		}
		lname := filepath.Join(compsRoot, cmp.LinkName)
		to := cmp.UnderTargetPath("modules", kversion)
		// remove old link
		os.Remove(lname)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	return nil
}

// syncFirmwareTopLevelSymlinks live-updates the top-level entries of an
// already-mounted lib/firmware directory (Regenerate mode only): the
// directory itself is bind-mounted, so only its children can be changed.
func syncFirmwareTopLevelSymlinks(kMntPts MountPoints, liveFwDir string) error {
	if err := os.MkdirAll(liveFwDir, 0755); err != nil {
		return err
	}

	desired, err := firmwareSymlinkTargets(kMntPts, liveFwDir)
	if err != nil {
		return err
	}

	// No scratch copy to roll back to here (unlike modules, which swaps).
	// Keep going on error so every entry gets a chance, but remember the
	// first failure so the caller does not advance the marker over a
	// partially-fixed tree.
	var firstErr error
	desiredNames := make(map[string]bool, len(desired))
	for _, d := range desired {
		desiredNames[d.name] = true
		lpath := filepath.Join(liveFwDir, d.name)
		cur, readErr := os.Readlink(lpath)
		if readErr == nil && cur == d.target {
			// Simple case, already correct, nothing to do for this entry.
			continue
		}
		// Either missing, wrong target, or not a symlink at all.
		if fi, statErr := os.Lstat(lpath); statErr == nil && fi.Mode().Type() != os.ModeSymlink {
			// A non-symlink entry may be user-placed content; never
			// destroy it, whatever its type.
			logger.Noticef("skipping a non-symlink entry %q in firmware directory", lpath)
			continue
		}
		// Only a child of the mount point is touched, never lib/firmware
		// itself, so this is safe live. Atomic replace avoids a window
		// where lpath is unresolvable to a concurrent reader.
		if err := atomicSymlink(d.target, lpath); err != nil {
			logger.Noticef("cannot set up firmware symlink %q: %v", lpath, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}

	// Remove stale entries left by an older generator version. Same
	// best-effort handling: keep going, remember the first failure.
	entries, err := os.ReadDir(liveFwDir)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	for _, e := range entries {
		if e.Name() == "updates" || desiredNames[e.Name()] {
			// "updates" is created at runtime or by the user
			continue
		}
		if e.Type()&fs.ModeSymlink == 0 {
			// May be user-created; leave it.
			logger.Noticef("unexpected entry %q found in %q", e.Name(), liveFwDir)
			continue
		}
		if err := os.Remove(filepath.Join(liveFwDir, e.Name())); err != nil {
			logger.Noticef("cannot remove stale firmware symlink %q: %v", filepath.Join(liveFwDir, e.Name()), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}

	return firstErr
}

// DriversTreeDir returns the directory for a given kernel and revision under
// rootdir.
func DriversTreeDir(rootdir, kernelName string, rev snap.Revision) string {
	return filepath.Join(dirs.SnapKernelDriversTreesDirUnder(rootdir),
		kernelName, rev.String())
}

// RemoveKernelDriversTree cleans-up the writable kernel tree in snapd data
// folder, under kernelSubdir/<rev> (kernelSubdir is usually the snap name).
// When called from the kernel package <rev> might be <rev>_tmp.
func RemoveKernelDriversTree(treeRoot string) (err error) {
	return os.RemoveAll(treeRoot)
}

type KernelDriversTreeOptions struct {
	// Set if we are building the tree for a kernel we are installing right now
	KernelInstall bool
	// Regenerate rebuilds and swaps in place the tree of an already
	// installed, active kernel (never a fresh install). Must be used
	// with KernelInstall: false.
	Regenerate bool
}

// MountPoints describes mount points for a snap or a component.
type MountPoints struct {
	// Current is where the container to be installed is currently
	// available
	Current string
	// Target is where the container will be found in a running system
	Target string
}

func (mp *MountPoints) UnderCurrentPath(dirs ...string) string {
	return filepath.Join(append([]string{mp.Current}, dirs...)...)
}

func (mp *MountPoints) UnderTargetPath(dirs ...string) string {
	return filepath.Join(append([]string{mp.Target}, dirs...)...)
}

func (mp *MountPoints) CurrentEqualsTarget() bool {
	return mp.Current == mp.Target
}

// ModulesCompMountPoints contains mount points for a component plus its name.
type ModulesCompMountPoints struct {
	// LinkName is the name of the symlink in the drivers tree that will
	// point to the component modules.
	LinkName string
	MountPoints
}

// EnsureKernelDriversTree creates a drivers tree that can include modules/fw
// from kernel-modules components. opts.KernelInstall tells the function if
// this is a kernel install (which might be installing components at the same
// time) or an only components install.
//
// For kernel installs, this function creates a tree in destDir (should be of
// the form <somedir>/var/lib/snapd/kernel/<ksnapName>/<rev>), which is
// bind-mounted after a reboot to /usr/lib/{modules,firmware} (the currently
// active kernel is using a different path as it has a different revision).
// This tree contains files from the kernel snap content in kSnapRoot, as well
// as symlinks to it. Information from modules is found by looking at
// comps slice.
//
// For components-only install, we want the components to be available without
// rebooting. For this, we work on a temporary tree, and after finishing it we
// swap atomically the affected modules/firmware folders with those of the
// currently active kernel drivers tree.
//
// To make this work in all cases we need to know the current mounts of the
// kernel snap / components to be installed and the final mounts when the
// system is run after installation (as the installing system might be classic
// while the installed system could be hybrid or UC, or we could be installing
// from the initramfs). To consider all cases, we need to run depmod with links
// to the currently available content, and then replace those links with the
// expected mounts in the running system.
func EnsureKernelDriversTree(kMntPts MountPoints, compsMntPts []ModulesCompMountPoints, destDir string, opts *KernelDriversTreeOptions) (retErr error) {
	// The temporal dir when installing only components can be fixed as a
	// task installing/updating a kernel-modules component must conflict
	// with changes containing this same task. This helps with clean-ups if
	// something goes wrong. Note that this folder needs to be in the same
	// filesystem as the final one so we can atomically switch the folders.
	destDir = strings.TrimSuffix(destDir, "/")
	targetDir := destDir + "_tmp"
	if opts.KernelInstall {
		targetDir = destDir
		exists, isDir, err := osutil.DirExists(targetDir)
		if err != nil {
			return err
		}
		if exists && isDir {
			// Require a current marker which is written last when building the
			// tree. Otherwise fall through and rebuild in place (safe: destDir
			// is not yet live-mounted to /lib/modules or /lib/firmware yet).
			needsUpdate, err := DriversTreeOutdated(targetDir)
			if err != nil {
				return err
			}
			if !needsUpdate {
				logger.Debugf("device tree %q already created on installation, not re-creating",
					targetDir)
				return nil
			}
			logger.Debugf("device tree %q exists but is not up to date (missing or stale marker), rebuilding",
				targetDir)
		}
	}
	// Initial clean-up to make the function idempotent. Must not continue
	// on failure: any stale content left behind here could survive into
	// the freshly-built tree, which would then be marked current.
	if rmErr := RemoveKernelDriversTree(targetDir); rmErr != nil &&
		!errors.Is(rmErr, fs.ErrNotExist) {
		return rmErr
	}

	defer func() {
		// Remove on return if error or if temporary tree
		if retErr == nil && opts.KernelInstall {
			return
		}
		if rmErr := RemoveKernelDriversTree(targetDir); rmErr != nil &&
			!errors.Is(rmErr, fs.ErrNotExist) {
			logger.Noticef("while cleaning up kernel tree: %v", rmErr)
		}
	}()

	// Create drivers tree
	kversion, err := KernelVersionFromModulesDir(kMntPts.Current)
	if err == nil {
		if err := createModulesSubtree(kMntPts, targetDir,
			kversion, compsMntPts); err != nil {
			return err
		}
	} else {
		logger.Debugf("no modules found in %q", kMntPts.Current)
	}

	fwDir := filepath.Join(targetDir, "lib", "firmware")
	if opts.KernelInstall {
		// symlinks in /lib/firmware are not affected by components
		if err := createFirmwareSymlinks(kMntPts, fwDir); err != nil {
			return err
		}
	}
	updateFwDir := filepath.Join(fwDir, "updates")
	// This folder needs to exist always to allow for directory swapping
	// in the future, even if right now we don't have components.
	if err := os.MkdirAll(updateFwDir, 0755); err != nil {
		return err
	}
	for _, cmp := range compsMntPts {
		if err := createFirmwareSymlinks(cmp.MountPoints, updateFwDir); err != nil {
			return err
		}
	}

	// Sync before returning successfully (install kernel case) and also
	// for swapping case so we have consistent content before swapping
	// folder.
	doSync()

	if opts.KernelInstall {
		// Record the version of the layout used for the firmware and modules
		// tree.
		if err := writeDriversTreeMeta(targetDir); err != nil {
			return err
		}
		logger.Debugf("device tree %q created", targetDir)
		return nil
	}

	// A crash between the two swaps below just needs a re-run on next boot.

	// oldRoot is the live destDir, shared by the swaps below.
	oldRoot := destDir

	// Swap the firmware "updates" dir unconditionally (also in Regenerate
	// mode, mirroring the modules swap below). osutil.SwapDirs requires
	// both sides to exist; fall back to a plain move if the live one is
	// missing.
	oldFwUpdates := filepath.Join(oldRoot, "lib", "firmware", "updates")
	fwUpdatesExists, fwUpdatesIsDir, err := osutil.DirExists(oldFwUpdates)
	if err != nil {
		return err
	}
	fwUpdatesWasMissing := !(fwUpdatesExists && fwUpdatesIsDir)
	if fwUpdatesWasMissing {
		if err := os.MkdirAll(filepath.Dir(oldFwUpdates), 0755); err != nil {
			return err
		}
		if err := os.Rename(updateFwDir, oldFwUpdates); err != nil {
			return fmt.Errorf("while moving %q to %q: %w", updateFwDir, oldFwUpdates, err)
		}
	} else {
		if err := osutil.SwapDirs(oldFwUpdates, updateFwDir); err != nil {
			return fmt.Errorf("while swapping %q <-> %q: %w", oldFwUpdates, updateFwDir, err)
		}
	}

	newMods := filepath.Join(targetDir, "lib", "modules", kversion)
	oldMods := filepath.Join(oldRoot, "lib", "modules", kversion)

	// Swap the candidate modules subtree in unconditionally when kversion
	// != "" (no comparison: the marker check that triggered this already
	// established a rebuild is needed). Fall back to a plain move if the
	// live directory does not exist yet.
	if kversion != "" {
		undoFwUpdatesSwapOnErr := func(context string) {
			// Undo whichever of swap/move was performed above.
			var undoErr error
			if fwUpdatesWasMissing {
				undoErr = RemoveKernelDriversTree(oldFwUpdates)
			} else {
				undoErr = osutil.SwapDirs(oldFwUpdates, updateFwDir)
			}
			if undoErr != nil {
				logger.Noticef("while reverting %s: %v", context, undoErr)
			}
		}

		exists, isDir, err := osutil.DirExists(oldMods)
		if err != nil {
			undoFwUpdatesSwapOnErr("firmware updates swap")
			return err
		}
		if exists && isDir {
			if err := osutil.SwapDirs(oldMods, newMods); err != nil {
				undoFwUpdatesSwapOnErr("modules swap")
				return fmt.Errorf("while swapping %q <-> %q: %w", newMods, oldMods, err)
			}
		} else {
			// Nothing live to exchange with: move the candidate into place.
			if err := os.MkdirAll(filepath.Dir(oldMods), 0755); err != nil {
				undoFwUpdatesSwapOnErr("firmware updates swap")
				return err
			}
			if err := os.Rename(newMods, oldMods); err != nil {
				undoFwUpdatesSwapOnErr("modules move")
				return fmt.Errorf("while moving %q to %q: %w", newMods, oldMods, err)
			}
		}
	}

	// Make sure that changes are written
	doSync()

	if opts.Regenerate {
		// lib/firmware is a bind-mount source like lib/modules; only its
		// children can be changed live, so sync per-entry instead of
		// swapping the directory as a unit.
		liveFwDir := filepath.Join(oldRoot, "lib", "firmware")
		if err := syncFirmwareTopLevelSymlinks(kMntPts, liveFwDir); err != nil {
			return err
		}

		// Sync before the marker write: a crash must not persist the
		// marker while losing an unsynced live firmware change.
		doSync()
	}

	// A writeDriversTreeMeta failure here only loses the _tmp scratch copy;
	// the already-swapped live tree is unaffected and retried on next
	// restart.
	//
	// Only advance the marker on a full Regenerate pass: a component-only
	// change never touches top-level firmware, so advancing the marker
	// there would mask a still-pending firmware fix.
	if opts.Regenerate {
		if err := writeDriversTreeMeta(oldRoot); err != nil {
			return err
		}
		logger.Noticef("kernel drivers tree %q regenerated", oldRoot)
	}

	return nil
}

// NeedsKernelDriversTree returns true if we need a kernel drivers tree for this model.
func NeedsKernelDriversTree(mod *asserts.Model) bool {
	// Checking if it has modeenv - it must be UC20+ or hybrid
	if mod.Grade() == asserts.ModelGradeUnset {
		return false
	}

	// We assume core24/hybrid 24.04 onwards have the generator, for older
	// boot bases we return false.
	switch mod.Base() {
	case "core22":
		if mod.Classic() {
			// This is a workaround for LP#2104933. The base should
			// never have been core22 in 24.04/24.10.
			return classic24ModelWithWrongBase()
		}
		return false
	case "core20", "core22-desktop":
		return false
	default:
		return true
	}
}

// This is a workaround for LP#2104933. The base should never have been core22
// in classic 24.04/24.10.
func classic24ModelWithWrongBase() bool {
	return release.ReleaseInfo.ID == "ubuntu" &&
		(release.ReleaseInfo.VersionID == "24.04" || release.ReleaseInfo.VersionID == "24.10")
}
