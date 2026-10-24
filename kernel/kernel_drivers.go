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

// driversTreeCheckpoint lets tests interrupt a build at durability boundaries.
var driversTreeCheckpoint = func(string) error { return nil }

// kernelDriversTreeGeneratorVersion identifies the logic that produced a
// kernel drivers tree (the on-disk symlinks/files under
// <destDir>/lib/{modules,firmware}).
//
// IMPORTANT: bump this whenever there is a change to the layout or organization of the
// kernel drivers or firmware trees.
var kernelDriversTreeGeneratorVersion = 2

// driversTreeMeta records the exact inputs used to build a drivers tree.
// An absent input field denotes a legacy version-only marker.
type driversTreeMeta struct {
	GeneratorVersion int                      `json:"generator-version"`
	Kernel           *MountPoints             `json:"kernel,omitempty"`
	Components       []ModulesCompMountPoints `json:"components"`
	Destination      string                   `json:"destination,omitempty"`
}

func driversTreeMetaPath(destDir string) string {
	return filepath.Join(destDir, "kernel.json")
}

func writeDriversTreeMeta(path string, meta driversTreeMeta) error {
	data, err := json.Marshal(&meta)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0644, 0)
}

var (
	errGeneratorMetaCorrupted = errors.New("kernel drivers tree generator metadata file is corrupted")
)

// readDriversTreeMeta returns the generator metadata recorded for
// destDir. If no marker value is present a default zero value with
// GeneratorVersion set to 0 is returned and no error.
func readDriversTreeMeta(destDir string) (driversTreeMeta, error) {
	path := driversTreeMetaPath(destDir)
	for _, pending := range []string{path + ".in", path + ".built"} {
		if _, err := os.Lstat(pending); err == nil {
			path = pending
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return driversTreeMeta{}, err
		}
	}
	data, err := os.ReadFile(path)
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
	// Do not treat a pending build as complete just because an older final
	// marker exists. A newer generator remains protected on downgrade.
	for _, suffix := range []string{".in", ".built"} {
		if _, err := os.Lstat(driversTreeMetaPath(destDir) + suffix); err == nil {
			return v.GeneratorVersion <= kernelDriversTreeGeneratorVersion, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
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
// mount, shared by candidate construction and live reconciliation.
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

// generatedFirmwareLink reports whether an absolute link target points into
// the kernel snap's firmware directory. Relative links may also be created by
// the generator, but a stale relative link cannot be distinguished from a
// user-created one, so leave it alone.
func generatedFirmwareLink(target, snapFwDir string) bool {
	if !filepath.IsAbs(target) {
		return false
	}
	rel, err := filepath.Rel(snapFwDir, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func generatedFirmwareFromAny(target string, fwDirs []string) bool {
	for _, dir := range fwDirs {
		if generatedFirmwareLink(target, dir) {
			return true
		}
	}
	return false
}

// syncFirmwareTopLevelSymlinks live-updates children of lib/firmware.
// Its parent can be a bind-mount source and must retain its inode.
func syncFirmwareTopLevelSymlinks(kMntPts MountPoints, liveFwDir string, ownedFwDirs []string) error {
	if err := os.MkdirAll(liveFwDir, 0755); err != nil {
		return err
	}

	desired, err := firmwareSymlinkTargets(kMntPts, liveFwDir)
	if err != nil {
		return err
	}

	// A failed update leaves the pending marker for a repeatable retry.
	// Keep going on error so every entry gets a chance, but remember the
	// first failure so the caller does not advance the marker over a
	// partially-fixed tree.
	var firstErr error
	desiredNames := make(map[string]bool, len(desired))
	for _, d := range desired {
		desiredNames[d.name] = true
		lpath := filepath.Join(liveFwDir, d.name)
		fi, statErr := os.Lstat(lpath)
		switch {
		case errors.Is(statErr, fs.ErrNotExist):
			// Missing: create the link below.
		case statErr != nil:
			if firstErr == nil {
				firstErr = statErr
			}
			continue
		case fi.Mode().Type() != os.ModeSymlink:
			// A non-symlink entry may be user-placed content; never
			// destroy it, whatever its type.
			return fmt.Errorf("cannot replace unowned firmware entry %q", lpath)
		default:
			cur, err := os.Readlink(lpath)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if cur == d.target {
				continue
			}
			if !generatedFirmwareFromAny(cur, ownedFwDirs) {
				return fmt.Errorf("cannot replace unowned firmware symlink %q", lpath)
			}
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
		if err := driversTreeCheckpoint("after-firmware-link"); err != nil {
			return err
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
		lpath := filepath.Join(liveFwDir, e.Name())
		cur, err := os.Readlink(lpath)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !generatedFirmwareFromAny(cur, ownedFwDirs) {
			logger.Noticef("leaving non-kernel firmware symlink %q untouched", lpath)
			continue
		}
		if err := os.Remove(lpath); err != nil {
			logger.Noticef("cannot remove stale firmware symlink %q: %v", lpath, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := driversTreeCheckpoint("after-firmware-unlink"); err != nil {
			return err
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

func dirSwapOrRename(temp, target string) (existed bool, retErr error) {
	exists, isDir, err := osutil.DirExists(target)
	if err != nil {
		return false, err
	}
	missing := !(exists && isDir)
	if missing {
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return false, err
		}
		if err := os.Rename(temp, target); err != nil {
			return false, fmt.Errorf("while moving %q to %q: %w", temp, target, err)
		}
		// Did not exist, but created successfully
		return false, nil
	} else {
		if err := osutil.SwapDirs(temp, target); err != nil {
			return true, fmt.Errorf("while swapping %q <-> %q: %w", temp, target, err)
		}
		return true, nil
	}
}

// EnsureKernelDriversTree reconciles the managed tree with the supplied mounts.
// Current mounts are used for discovery and depmod; links point at Target.
// Existing root and lib/{modules,firmware} inodes remain stable for bind mounts.
func EnsureKernelDriversTree(kMntPts MountPoints, compsMntPts []ModulesCompMountPoints, destDir string) (retErr error) {
	destDir = filepath.Clean(destDir)
	rootInfo, err := os.Lstat(destDir)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if exists && !rootInfo.IsDir() {
		return fmt.Errorf("cannot update kernel drivers tree %q: not a directory", destDir)
	}
	meta := driversTreeMeta{
		GeneratorVersion: kernelDriversTreeGeneratorVersion,
		Kernel:           &kMntPts, Components: compsMntPts, Destination: destDir,
	}
	// A newer generator's output must not be rewritten by an older one.
	// Existing input markers identify previous generated absolute firmware
	// links (including links left by an interrupted earlier pass).
	ownedFwDirs := []string{kMntPts.UnderTargetPath("firmware")}
	for _, suffix := range []string{".in", ".built", ""} {
		path := driversTreeMetaPath(destDir) + suffix
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var old driversTreeMeta
		if err := json.Unmarshal(data, &old); err != nil {
			return fmt.Errorf("cannot read kernel drivers tree marker %q: %w", path, err)
		}
		if old.GeneratorVersion > kernelDriversTreeGeneratorVersion {
			return fmt.Errorf("cannot replace kernel drivers tree %q from newer generator %d", destDir, old.GeneratorVersion)
		}
		if old.Kernel != nil && old.Destination == destDir {
			ownedFwDirs = append(ownedFwDirs, old.Kernel.UnderTargetPath("firmware"))
		}
	}

	// A bare mount directory may have been created by a mount unit without
	// its kernel snap. Do not certify an empty tree for such a source.
	if _, modErr := os.Stat(kMntPts.UnderCurrentPath("modules")); errors.Is(modErr, fs.ErrNotExist) {
		if _, fwErr := os.Stat(kMntPts.UnderCurrentPath("firmware")); errors.Is(fwErr, fs.ErrNotExist) {
			if _, metaErr := os.Stat(kMntPts.UnderCurrentPath("meta", "snap.yaml")); metaErr != nil {
				return fmt.Errorf("cannot read kernel drivers source %q: no mounted kernel content", kMntPts.Current)
			}
		} else if fwErr != nil {
			return fwErr
		}
	} else if modErr != nil {
		return modErr
	}
	if fi, err := os.Stat(kMntPts.Current); err != nil {
		return fmt.Errorf("cannot read kernel drivers source %q: %w", kMntPts.Current, err)
	} else if !fi.IsDir() {
		return fmt.Errorf("cannot read kernel drivers source %q: not a directory", kMntPts.Current)
	}
	for _, cmp := range compsMntPts {
		if fi, err := os.Stat(cmp.Current); err != nil {
			return fmt.Errorf("cannot read kernel component source %q: %w", cmp.Current, err)
		} else if !fi.IsDir() {
			return fmt.Errorf("cannot read kernel component source %q: not a directory", cmp.Current)
		}
		// Dynamic module directories have no component metadata, but must
		// contain drivers. An empty mount directory is not a driverless
		// component: without metadata it can be an unmounted squashfs.
		if !sourceExists(cmp.UnderCurrentPath("meta", "component.yaml")) &&
			!sourceExists(cmp.UnderCurrentPath("modules")) &&
			!sourceExists(cmp.UnderCurrentPath("firmware")) {
			return fmt.Errorf("cannot read kernel component source %q: no mounted component content", cmp.Current)
		}
	}

	// Build beside the destination so a prepared image and a running system
	// both use their own filesystem for atomic renames.
	candidate := destDir + "_tmp"
	if err := os.MkdirAll(filepath.Dir(candidate), 0755); err != nil {
		return err
	}
	if err := os.RemoveAll(candidate); err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(candidate); err != nil {
			logger.Noticef("while removing candidate kernel tree: %v", err)
		}
	}()

	markerRoot := candidate
	if exists {
		markerRoot = destDir
	}
	in := driversTreeMetaPath(markerRoot) + ".in"
	built := driversTreeMetaPath(markerRoot) + ".built"
	if err := os.MkdirAll(candidate, 0755); err != nil {
		return err
	}
	// On an existing tree, persist intent before touching any live child.
	if err := driversTreeCheckpoint("before-in"); err != nil {
		return err
	}
	if err := writeDriversTreeMeta(in, meta); err != nil {
		return err
	}
	doSync()
	if err := driversTreeCheckpoint("after-in"); err != nil {
		return err
	}
	if err := driversTreeCheckpoint("before-build"); err != nil {
		return err
	}

	kversion, err := KernelVersionFromModulesDir(kMntPts.Current)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		logger.Debugf("no modules found in %q", kMntPts.Current)
	} else if err := createModulesSubtree(kMntPts, candidate, kversion, compsMntPts); err != nil {
		return err
	}
	fwDir := filepath.Join(candidate, "lib", "firmware")
	if err := createFirmwareSymlinks(kMntPts, fwDir); err != nil {
		return err
	}
	updateFwDir := filepath.Join(fwDir, "updates")
	if err := os.MkdirAll(updateFwDir, 0755); err != nil {
		return err
	}
	for _, cmp := range compsMntPts {
		if err := createFirmwareSymlinks(cmp.MountPoints, updateFwDir); err != nil {
			return err
		}
	}
	if err := driversTreeCheckpoint("after-build"); err != nil {
		return err
	}
	// Preflight all firmware conflicts before mutating any live subtree.
	if exists {
		if err := checkFirmwareCollisions(kMntPts, filepath.Join(destDir, "lib", "firmware"), ownedFwDirs); err != nil {
			return err
		}
	}
	doSync()
	if err := driversTreeCheckpoint("before-built"); err != nil {
		return err
	}
	if err := os.Rename(in, built); err != nil {
		return err
	}
	doSync()
	if err := driversTreeCheckpoint("after-built"); err != nil {
		return err
	}

	if !exists {
		if err := driversTreeCheckpoint("before-install-rename"); err != nil {
			return err
		}
		if err := os.Rename(candidate, destDir); err != nil {
			return err
		}
		// The marker moved together with the candidate.
		built = driversTreeMetaPath(destDir) + ".built"
		doSync()
		if err := driversTreeCheckpoint("after-install-rename"); err != nil {
			return err
		}
	} else {
		liveLib := filepath.Join(destDir, "lib")
		liveMods := filepath.Join(liveLib, "modules")
		liveFw := filepath.Join(liveLib, "firmware")
		for _, dir := range []string{liveMods, liveFw} {
			if fi, err := os.Lstat(dir); err == nil && !fi.IsDir() {
				return fmt.Errorf("cannot update kernel drivers tree %q: parent is not a directory", dir)
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
		// The versioned modules directory and firmware updates directory are
		// safe swap units. Never swap a bind-mounted parent directory.
		if err := driversTreeCheckpoint("before-firmware-swap"); err != nil {
			return err
		}
		if err := replaceManagedDir(updateFwDir, filepath.Join(liveFw, "updates")); err != nil {
			return err
		}
		if err := driversTreeCheckpoint("after-firmware-swap"); err != nil {
			return err
		}
		if kversion != "" {
			if err := driversTreeCheckpoint("before-modules-swap"); err != nil {
				return err
			}
			if err := replaceManagedDir(filepath.Join(candidate, "lib", "modules", kversion), filepath.Join(liveMods, kversion)); err != nil {
				return err
			}
			if err := driversTreeCheckpoint("after-modules-swap"); err != nil {
				return err
			}
		}
		entries, err := os.ReadDir(liveMods)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() != kversion {
				if err := os.RemoveAll(filepath.Join(liveMods, entry.Name())); err != nil {
					return err
				}
			}
		}
		if err := driversTreeCheckpoint("before-firmware-links"); err != nil {
			return err
		}
		if err := syncFirmwareTopLevelSymlinks(kMntPts, liveFw, ownedFwDirs); err != nil {
			return err
		}
		if err := driversTreeCheckpoint("after-firmware-links"); err != nil {
			return err
		}
		if err := driversTreeCheckpoint("before-live-sync"); err != nil {
			return err
		}
		doSync()
		if err := driversTreeCheckpoint("after-live-sync"); err != nil {
			return err
		}
	}
	if err := driversTreeCheckpoint("before-final"); err != nil {
		return err
	}
	if err := os.Rename(built, driversTreeMetaPath(destDir)); err != nil {
		return err
	}
	doSync()
	if err := driversTreeCheckpoint("after-final"); err != nil {
		return err
	}
	return nil
}

func sourceExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// replaceManagedDir atomically exchanges a candidate directory with any
// existing managed child, even one left with the wrong type by an old run.
func replaceManagedDir(candidate, live string) error {
	if _, err := os.Lstat(live); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(candidate, live)
	} else if err != nil {
		return err
	}
	return osutil.SwapDirs(candidate, live)
}

func checkFirmwareCollisions(kMntPts MountPoints, liveFw string, ownedFwDirs []string) error {
	desired, err := firmwareSymlinkTargets(kMntPts, liveFw)
	if err != nil {
		return err
	}
	for _, d := range desired {
		path := filepath.Join(liveFw, d.name)
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if fi.Mode().Type() != os.ModeSymlink {
			return fmt.Errorf("cannot replace unowned firmware entry %q", path)
		}
		old, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if old != d.target && !generatedFirmwareFromAny(old, ownedFwDirs) {
			return fmt.Errorf("cannot replace unowned firmware symlink %q", path)
		}
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
