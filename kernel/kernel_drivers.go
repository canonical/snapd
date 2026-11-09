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
	"github.com/snapcore/snapd/strutil"
)

// For testing purposes
var osSymlink = os.Symlink

// doSync is a mockable wrapper around syscall.Sync for tests.
var doSync = syscall.Sync

// atomicWriteFile is a mockable wrapper around osutil.AtomicWriteFile, used
// by atomicWriteDriversTreeMeta, so tests can simulate a marker-write failure
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

// driversTreeMeta records the generator and inputs for a completed tree.
// Absent inputs denote a legacy version-only marker.
type driversTreeMeta struct {
	GeneratorVersion int                      `json:"generator-version"`
	Kernel           *MountPoints             `json:"kernel,omitempty"`
	Components       []ModulesCompMountPoints `json:"components"`
	Destination      string                   `json:"destination,omitempty"`
}

func driversTreeMetaPath(destDir string) string {
	return filepath.Join(destDir, "kernel.json")
}

func atomicWriteDriversTreeMeta(path string, meta driversTreeMeta) error {
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
	if _, err := os.Lstat(path + ".pending"); err == nil {
		path += ".pending"
	} else if !errors.Is(err, fs.ErrNotExist) {
		return driversTreeMeta{}, err
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
	// Pending means the live tree may be incomplete, even if a newer
	// generator wrote it. Only a newer completed tree blocks a downgrade.
	pending := false
	for _, suffix := range []string{".pending", ""} {
		data, err := os.ReadFile(driversTreeMetaPath(destDir) + suffix)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		var marker driversTreeMeta
		if json.Unmarshal(data, &marker) != nil {
			return true, nil
		}
		if suffix == "" && marker.GeneratorVersion > kernelDriversTreeGeneratorVersion {
			return false, nil
		}
		pending = pending || suffix != ""
	}
	return pending || kernelDriversTreeGeneratorVersion > v.GeneratorVersion, nil
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

// componentFirmwareLink recognizes the target layout of component and
// dynamic-module firmware. Relative aliases are ambiguous and left alone.
func componentFirmwareLink(destDir, target string) bool {
	if !filepath.IsAbs(target) {
		return false
	}

	// The symlinks are created mostly by snapd, so it is unlikely that the path
	// would not be clean in the first place.
	if filepath.Clean(target) != target {
		// pointing outside?
		return false
	}

	kernelName := filepath.Base(filepath.Dir(destDir))
	return strings.HasPrefix(target, filepath.Join(dirs.SnapMountDir, kernelName)+"/") ||
		strings.HasPrefix(target, filepath.Join(dirs.SnapDataDir, kernelName)+"/")
}

// firmwareLinkState checks whether a desired link is already present or safe
// to write. An ownership check is needed only for firmware/updates; top-level
// firmware symlinks are all managed.
func firmwareLinkState(path string, d firmwareSymlinkTarget, owned func(string) bool) (ok, fatal bool, err error) {
	fi, statErr := os.Lstat(path)
	if errors.Is(statErr, fs.ErrNotExist) {
		return false, false, nil
	}
	if statErr != nil {
		return false, false, statErr
	}
	if fi.Mode().Type() != os.ModeSymlink {
		// A non-symlink entry may be user-placed content; never destroy
		// it, whatever its type.
		return false, true, fmt.Errorf("cannot replace unowned firmware entry %q", path)
	}
	cur, err := os.Readlink(path)
	if err != nil {
		return false, false, err
	}
	if cur == d.target {
		return true, false, nil
	}
	if owned != nil && !owned(cur) {
		return false, true, fmt.Errorf("cannot replace unowned firmware symlink %q", path)
	}
	return false, false, nil
}

// syncFirmwareSymlinks updates live children without replacing their parent.
func syncFirmwareSymlinks(desired []firmwareSymlinkTarget, liveFwDir, protectedName string, owned func(string) bool) error {
	if err := os.MkdirAll(liveFwDir, 0755); err != nil {
		return err
	}

	// A failed update leaves pending for retry. Keep going so other links
	// can be repaired, but report every error before certifying completion.
	var errs []error
	desiredNames := make(map[string]bool, len(desired))
	for _, d := range desired {
		desiredNames[d.name] = true
		lpath := filepath.Join(liveFwDir, d.name)
		ok, fatal, err := firmwareLinkState(lpath, d, owned)
		if fatal {
			return strutil.JoinErrors(append(errs, err)...)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			continue
		}
		// Only a child of the mount point is touched, never lib/firmware
		// itself, so this is safe live. Atomic replace avoids a window
		// where lpath is unresolvable to a concurrent reader.
		if err := atomicSymlink(d.target, lpath); err != nil {
			logger.Noticef("cannot set up firmware symlink %q: %v", lpath, err)
			errs = append(errs, err)
			continue
		}
	}

	// Remove stale entries left by an older generator version.
	entries, err := os.ReadDir(liveFwDir)
	if err != nil {
		return strutil.JoinErrors(append(errs, err)...)
	}
	for _, e := range entries {
		if e.Name() == protectedName || desiredNames[e.Name()] {
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
			errs = append(errs, err)
			continue
		}
		if owned != nil && !owned(cur) {
			logger.Noticef("leaving unrelated firmware symlink %q untouched", lpath)
			continue
		}
		if err := os.Remove(lpath); err != nil {
			logger.Noticef("cannot remove stale firmware symlink %q: %v", lpath, err)
			errs = append(errs, err)
			continue
		}
	}

	return strutil.JoinErrors(errs...)
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

// EnsureKernelDriversTree builds or updates the kernel modules and firmware
// with the supplied mounts. The tree is built under destDir. kMntPts.Current
// mounts are used for discovery and depmod; links point at Target. When
// updating a currently used tree, Existing root and lib/{modules,firmware}
// inodes remain stable for bind mounts.
func EnsureKernelDriversTree(kMntPts MountPoints, compsMntPts []ModulesCompMountPoints, destDir string) (retErr error) {
	destDir = filepath.Clean(destDir)
	rootInfo, err := os.Lstat(destDir)

	// if destination exists, we're rebuilding an existing kernel tree
	isRebuild := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if isRebuild && !rootInfo.IsDir() {
		return fmt.Errorf("cannot update kernel drivers tree %q: not a directory", destDir)
	}

	var liveMods, liveFw string
	if isRebuild {
		// Is this needed? the lib/modules lib/firmware are most likely
		// directories anyway, even on hybrid system
		_, liveMods, liveFw, err = checkLiveKernelTree(destDir)
		if err != nil {
			return err
		}
	}

	if err := checkKernelDriversSources(kMntPts, compsMntPts); err != nil {
		return err
	}

	kversion, err := KernelVersionFromModulesDir(kMntPts.Current)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		logger.Debugf("no modules found in %q", kMntPts.Current)
	}

	meta := driversTreeMeta{
		GeneratorVersion: kernelDriversTreeGeneratorVersion,
		// The following entries are purely informational
		Kernel:      &kMntPts,
		Components:  compsMntPts,
		Destination: destDir,
	}

	// Pending means a previous run may have changed live children. Leave it
	// in place until the replacement candidate is ready, even if unreadable.
	if _, err := os.Lstat(driversTreeMetaPath(destDir) + ".pending"); err == nil {
		logger.Debugf("kernel drivers tree %q has an incomplete update marker", destDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Completed metadata must still be readable; its version does not prevent
	// rebuilding after a snapd downgrade.
	final := driversTreeMetaPath(destDir)
	data, err := os.ReadFile(final)
	if err == nil {
		var old driversTreeMeta
		if err := json.Unmarshal(data, &old); err != nil {
			return fmt.Errorf("cannot read kernel drivers tree marker %q: %w", final, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	// TODO: If the completed marker matches the inputs, pending is absent,
	// and links match, skip the candidate only after verifying copied
	// modules.* and depmod output, including dynamic-module changes.

	// Build beside the destination so a prepared image and a running system
	// both use their own filesystem for atomic renames.
	candidate := destDir + "_tmp"
	if err := os.RemoveAll(candidate); err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(candidate); err != nil {
			logger.Noticef("while removing candidate kernel tree: %v", err)
		}
	}()

	if err := os.MkdirAll(candidate, 0755); err != nil {
		return err
	}

	if kversion != "" {
		// We have kernel modules, set them up.
		if err := createModulesSubtree(kMntPts, candidate, kversion, compsMntPts); err != nil {
			return err
		}
	}

	candidateFwDir := filepath.Join(candidate, "lib", "firmware")
	// "updates" directory is where firmware from components is deployed
	candidateFwUpdatesDir := filepath.Join(candidateFwDir, "updates")

	// Top level symlinks from the kernel mount.
	if err := createFirmwareSymlinks(kMntPts, candidateFwDir); err != nil {
		return err
	}

	if err := os.MkdirAll(candidateFwUpdatesDir, 0755); err != nil {
		return err
	}

	// Pick up firmware symlinks from components.
	for _, cmp := range compsMntPts {
		if err := createFirmwareSymlinks(cmp.MountPoints, candidateFwUpdatesDir); err != nil {
			return err
		}
	}

	if !isRebuild {
		// The rename syncs directory entries, not the candidate's contents.
		doSync()
		if err := atomicWriteDriversTreeMeta(driversTreeMetaPath(candidate), meta); err != nil {
			return err
		}
		if err := osutil.AtomicRename(candidate, destDir); err != nil {
			return err
		}
		return nil
	}

	// Preflight both live firmware directories before publishing pending. The
	// checks could be done earlier, but it's clearer to keep them here.
	topLevel, err := firmwareSymlinkTargets(kMntPts, liveFw)
	if err != nil {
		return err
	}
	updates, err := os.ReadDir(candidateFwUpdatesDir)
	if err != nil {
		return err
	}
	var desiredUpdates []firmwareSymlinkTarget
	for _, entry := range updates {
		target, err := os.Readlink(filepath.Join(candidateFwUpdatesDir, entry.Name()))
		if err != nil {
			return err
		}
		desiredUpdates = append(desiredUpdates, firmwareSymlinkTarget{name: entry.Name(), target: target})
	}
	componentLink := func(target string) bool {
		return componentFirmwareLink(destDir, target)
	}

	if err := checkFirmwareCollisions(topLevel, liveFw, nil); err != nil {
		return err
	}
	if err := checkFirmwareCollisions(desiredUpdates, filepath.Join(liveFw, "updates"), componentLink); err != nil {
		return err
	}

	// Sync could be expensive, so do it now after the late preflight checks are
	// fine.
	doSync()

	// Write the metadata and move it under the target tree, so if things fail we could inspect it.
	pending := final + ".pending"
	if err := atomicWriteDriversTreeMeta(driversTreeMetaPath(candidate)+".pending", meta); err != nil {
		return err
	}

	if err := osutil.AtomicRename(driversTreeMetaPath(candidate)+".pending", pending); err != nil {
		return err
	}

	for _, dir := range []string{liveMods, liveFw} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// Only the parents of the versioned modules directory are bind-mounted.
	// The /lib/firmware/updates can contain user entries, so reconcile its links
	// individually rather than replacing the directory.
	//
	// make the component firmware appear before any modules from components can
	// make use of it.
	if err := syncFirmwareSymlinks(desiredUpdates, filepath.Join(liveFw, "updates"), "", componentLink); err != nil {
		return err
	}
	// Swap kernel modules.
	if kversion != "" {
		if err := atomicReplaceManagedDir(filepath.Join(candidate, "lib", "modules", kversion),
			filepath.Join(liveMods, kversion)); err != nil {
			return err
		}
	}
	// And the remaining firmwares.
	if err := syncFirmwareSymlinks(topLevel, liveFw, "updates", nil); err != nil {
		return err
	}
	doSync()

	if err := osutil.AtomicRename(pending, final); err != nil {
		return err
	}
	return nil
}

// checkLiveKernelTree preflights parents before any live changes; firmware
// updates must remain a directory so user content there is not overwritten.
func checkLiveKernelTree(destDir string) (liveLib, liveMods, liveFw string, err error) {
	liveLib = filepath.Join(destDir, "lib")
	liveMods = filepath.Join(liveLib, "modules")
	liveFw = filepath.Join(liveLib, "firmware")
	for _, dir := range []string{liveLib, liveMods, liveFw, filepath.Join(liveFw, "updates")} {
		if fi, err := os.Lstat(dir); err == nil && !fi.IsDir() {
			return "", "", "", fmt.Errorf("cannot update kernel drivers tree %q: parent is not a directory", dir)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", "", "", err
		}
	}
	return liveLib, liveMods, liveFw, nil
}

// checkKernelDriversSources rejects mount directories without mounted content.
func checkKernelDriversSources(kMntPts MountPoints, compsMntPts []ModulesCompMountPoints) error {
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
	return nil
}

func sourceExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// atomicReplaceManagedDir atomically exchanges a candidate directory with any
// existing managed child, even one left with the wrong type by an old run.
func atomicReplaceManagedDir(candidate, live string) error {
	if _, err := os.Lstat(live); errors.Is(err, fs.ErrNotExist) {
		return osutil.AtomicRename(candidate, live)
	} else if err != nil {
		return err
	}
	return osutil.SwapDirs(candidate, live)
}

// checkFirmwareCollisions rejects desired names occupied by non-symlinks
// or, in firmware/updates, by unrelated symlinks before live changes.
func checkFirmwareCollisions(desired []firmwareSymlinkTarget, liveFw string, owned func(string) bool) error {
	for _, d := range desired {
		if _, _, err := firmwareLinkState(filepath.Join(liveFw, d.name), d, owned); err != nil {
			return err
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
