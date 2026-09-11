// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package builtin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// driverLibsSupported reports whether driver-libs interfaces are supported
// on snaps using the given base snap. The direct-connection delivery model
// requires a base snap new enough to carry the expected ld.so.cache layout.
func driverLibsSupported(baseSnap string) bool {
	switch baseSnap {
	case "bare": // not yet
	case "", "core", "core18", "core20", "core22", "core24": // TBD
	case "core22-desktop", "core24-desktop": // TBD
	default:
		return true
	}
	return false
}

// sourceDirAttr contains information about a *-source interface attribute.
type sourceDirAttr struct {
	// attribute naem
	attrName string
	// set if the attribute is optional for the interface
	isOptional bool
}

// validateSourceDirs checks that the list of directories in the "*-source"
// slot attribute specified in sda is valid. sda.isOptional should be set if
// the attribute is optional, so no error is returned if it is not found.
func validateSourceDirs(slot *snap.SlotInfo, sda sourceDirAttr) error {
	// Validate directories and make sure the client driver is around
	libDirs := []string{}
	if err := slot.Attr(sda.attrName, &libDirs); err != nil {
		if sda.isOptional && errors.Is(err, snap.AttributeNotFoundError{}) {
			return nil
		}
		return err
	}
	for _, dir := range libDirs {
		var insidePath string
		const componentPrefix = "$SNAP_COMPONENT("
		if strings.HasPrefix(dir, componentPrefix) {
			compAndPath := strings.SplitN(dir[len(componentPrefix):], ")/", 2)
			if len(compAndPath) != 2 {
				return fmt.Errorf("invalid format in path %q", dir)
			}
			if _, ok := slot.Snap.Components[compAndPath[0]]; !ok {
				return fmt.Errorf("component %s specified in path %q is not defined in the snap",
					compAndPath[0], dir)
			}
			insidePath = compAndPath[1]
		} else {
			const snapPrefix = "$SNAP/"
			const snapPrefixK = "${SNAP}/"
			switch {
			case strings.HasPrefix(dir, snapPrefix):
				insidePath = dir[len(snapPrefix):]
			case strings.HasPrefix(dir, snapPrefixK):
				insidePath = dir[len(snapPrefixK):]
			default:
				return fmt.Errorf(
					"%s %s directory %q must start with $SNAP/ or ${SNAP}/",
					slot.Interface, sda.attrName, dir)
			}
		}
		cleanPath := filepath.Clean(insidePath)
		if strings.HasPrefix(cleanPath, "..") {
			return fmt.Errorf(
				"%s %s directory %q cannot point outside of the snap/component",
				slot.Interface, sda.attrName, dir)
		}
	}

	return nil
}

// addLdconfigLibDirs adds the list of directories with libraries defined by
// some interface slots to the ldconfig backend.
func addLdconfigLibDirs(spec *ldconfig.Specification, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}
	return spec.AddLibDirs(slot.AppSet().ExpandSliceSnapVariablesInRootfs(libDirs))
}

// systemLibrarySourcePath returns the path for files containing directories
// specified in system library-source fields. The file names have instance name
// / slot name and interface name as different interfaces will write to the
// export dir, for different instances and slots.
func systemLibrarySourcePath(instance, slotName, ifaceName string) string {
	return filepath.Join(dirs.SnapExportDirUnder(dirs.GlobalRootDir), fmt.Sprintf(
		"system_%s_%s_%s.library-source", instance, slotName, ifaceName))
}

// addConfigfilesForSystemLibrarySourcePaths adds a file containing a list with
// the system library sources for an interface to the /var/lib/snapd/export
// directory. These files are used by snap-confine on classic for snaps
// connected to the opengl interface.
func addConfigfilesForSystemLibrarySourcePaths(iface string, spec *configfiles.Specification, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}
	content := strings.Join(slot.AppSet().ExpandSliceSnapVariablesInRootfs(libDirs), "\n") + "\n"
	return spec.AddPathContent(systemLibrarySourcePath(slot.Snap().InstanceName(), slot.Name(), iface),
		&osutil.MemoryFileState{Content: []byte(content), Mode: 0644})
}

// filePathInLibDirs returns the path of the first occurrence of fileName in the
// list of library directories of the slot.
func filePathInLibDirs(slot *interfaces.ConnectedSlot, fileName string) (string, error) {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return "", err
	}

	expanded := slot.AppSet().ExpandSliceSnapVariablesInRootfs(libDirs)
	for _, dir := range expanded {
		path := filepath.Join(dir, fileName)
		if osutil.FileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q not found in the library-source directories", fileName)
}

type pathWithDirIdx struct {
	path string
	idx  int
}

// sourceDirsCheck returns a list of file paths found in the directories specified by
// sda, after checking that the library_path in these files matches a file found in
// the directories specified by library-source. Each path has an index attached so
// the source dir can be identified.
func sourceDirsCheck(slot *interfaces.ConnectedSlot, sda sourceDirAttr, checker func(slot *interfaces.ConnectedSlot, content []byte) error) (checked []pathWithDirIdx, err error) {
	var sourceDir []string
	if err := slot.Attr(sda.attrName, &sourceDir); err != nil {
		if sda.isOptional && errors.Is(err, snap.AttributeNotFoundError{}) {
			return checked, nil
		}
		return nil, err
	}

	expanded := slot.AppSet().ExpandSliceSnapVariablesWithOrder(sourceDir)
	for _, dir := range expanded {
		paths, err := sourceDirFilesCheck(slot, dir.Path, checker)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			checked = append(checked, pathWithDirIdx{path: p, idx: dir.Idx})
		}
	}
	return checked, nil
}

// sourceDirFilesCheck does the checks of all source files in a single directory.
func sourceDirFilesCheck(slot *interfaces.ConnectedSlot, sourceDir string, checker func(slot *interfaces.ConnectedSlot, content []byte) error) (checked []string, err error) {
	sourceFiles, err := os.ReadDir(sourceDir)
	if err != nil {
		// We do not care if the directory does not exist
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	for _, entry := range sourceFiles {
		// Only regular files are considered - note that even symlinks
		// are ignored as we eventually will want to use apparmor to
		// allow access to these paths.
		if !entry.Type().IsRegular() {
			continue
		}
		// We are only interested in json files (same as libglvnd).
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if err := checker(slot, content); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}

		// Good enough
		checked = append(checked, filepath.Join(sourceDir, entry.Name()))
	}

	return checked, nil
}

// assemblyRoot is the top-level directory under which the content of driver-libs
// provider slots is bound into the consumer's view, one subtree per interface.
// Libraries are bound under <assemblyRoot>/<iface>/lib/ and ICD/layer/client
// driver metadata under <assemblyRoot>/<iface>/share/. The writable-mimic for
// these paths is authorized via the snap-update-ns apparmor profile, see
// AppArmorConnectedPlug of the driver-libs interfaces.
var assemblyRoot = dirs.SnapInterfacesAssemblyRoot

// libraryRelPath returns the path of a library/source dir relative to its
// $SNAP or $SNAP_COMPONENT(<comp>) prefix, i.e. the path-suffix to preserve
// when pooling under the assembly tree (mirrors classic snap-confine's
// sc_populate_libgl_with_hostfs_symlinks). It reuses the same prefix-strip
// logic as sourceDirEncodedName but keeps the result a real path (no
// systemd-flatten to a single basename).
func libraryRelPath(slot *interfaces.ConnectedSlot, path string) (string, error) {
	relPath, err := filepath.Rel(dirs.SnapMountDir, path)
	if err != nil {
		return "", err
	}
	instance := slot.Snap().InstanceName()
	splitNum := 3
	if strings.HasPrefix(path, snap.ComponentsBaseDir(instance)) {
		splitNum = 6
	}
	dirs := strings.SplitN(relPath, "/", splitNum)
	if len(dirs) < splitNum {
		return "", fmt.Errorf("internal error: wrong library path: %s", relPath)
	}
	return dirs[splitNum-1], nil
}

// sourceDirEncodedName returns the escaped name used to refer to a file found
// in a *-source attribute of a driver-libs slot, in the export/assembly
// directories. The name is derived from the snap/component instance name, the
// slot name and the relative path of the file; when withPriority is set, it is
// prefixed with the priority attribute plus the index of the source directory
// in the *-source list, which determines lookup order.
func sourceDirEncodedName(slot *interfaces.ConnectedSlot, pathDirIdx pathWithDirIdx, withPriority bool) (string, error) {
	// First strip out mount dir
	relPath, err := filepath.Rel(dirs.SnapMountDir, pathDirIdx.path)
	if err != nil {
		return "", err
	}

	// If path is in the snap, we ignore below the snap name and revision
	// when building the name, if in component, we ignore
	// <snap_name>/components/mnt/<comp_name>/<comp_rev>/ (5 dirs)
	instance := slot.Snap().InstanceName()
	splitNum := 3
	compSuffix := ""
	if strings.HasPrefix(pathDirIdx.path, snap.ComponentsBaseDir(instance)) {
		splitNum = 6
		compSuffix = "+"
	}
	dirs := strings.SplitN(relPath, "/", splitNum)
	if len(dirs) < splitNum {
		return "", fmt.Errorf("internal error: wrong file path: %s", relPath)
	}
	if compSuffix != "" {
		compSuffix += dirs[3]
	}

	// Get last component from dirs and make path an easier to handle name
	escapedRelPath := systemd.EscapeUnitNamePath(dirs[splitNum-1])
	prefix := ""
	if withPriority {
		var priority int64
		if err := slot.Attr("priority", &priority); err != nil {
			return "", fmt.Errorf("invalid priority: %w", err)
		}
		// The priority depends on the list order of the directories
		// in the *-source attribute.
		prefix = fmt.Sprintf("%d_", priority+int64(pathDirIdx.idx))
	}
	return fmt.Sprintf("%ssnap_%s%s_%s_%s",
		prefix, instance, compSuffix, slot.Name(), escapedRelPath), nil
}

// mountAssemblyLibDirs adds mount entries that bind each expanded library-source
// directory of the slot into the per-interface assembly tree under
// <assemblyRoot>/<iface>/lib/<provider>_<slot>/<rel-path-after-$SNAP>/, pooling
// entries by path-suffix (mirrors classic snap-confine's
// sc_populate_libgl_with_hostfs_symlinks), so that the libraries keep their
// original relative layout (ld.so opens libraries by exact name). Each target
// dir is also recorded via AddLibraryPathDir for the SNAP_LIBRARY_PATH
// derivation (Pass 3).
// mountAssemblyRoot adds the shared tmpfs mount entry anchoring the assembly
// tree, mounted one level up from assemblyRoot itself (i.e. at
// filepath.Dir(assemblyRoot) == /run/snapd/snap), so that /run/snapd/snap is
// a private per-snap tmpfs and assemblyRoot ("interfaces") is just its
// current sole subdirectory, leaving room for other private, per-snap state
// at the same /run/snapd/snap/ level in the future.
//
// /run/snapd/snap is a real (writable, non-read-only) host directory.
// Creating the assembly tree's *content* directly there would land on the
// host's actual filesystem, persistent and shared across every consuming
// snap (see the trespassing whitelist for /run/snapd/snap in
// cmd/snap-update-ns/system.go, which is what allows the *mountpoint*
// directory itself to be created without tripping the trespassing check on
// the ancestor /run). This entry mounts a fresh tmpfs exactly at
// /run/snapd/snap so that the mountpoint directory is the only thing that is
// ever host-visible (an empty placeholder); everything created underneath —
// assemblyRoot and its library dirs, ICD metadata — lives on this tmpfs,
// which is a brand new mount and therefore private to the connecting snap's
// own mount namespace by default (new mounts are never automatically shared
// with their siblings or the host).
//
// Explicit mode/uid/gid options are required, not cosmetic: an entry with an
// empty Options list serializes to the fstab placeholder string "defaults"
// (osutil/mountentry.go's String()); once the mount profile round-trips
// through the persisted fstab file, that placeholder is read back as a
// literal (single) option string "defaults", which is not a real tmpfs mount
// option and makes the mount(2) syscall fail with EINVAL. Giving explicit,
// real tmpfs options (recognized by the kernel's tmpfs option parser) avoids
// this entirely, and also makes the tmpfs look like an ordinary root:root
// 0755 directory, matching planWritableMimic's synthetic tmpfs entries
// (cmd/snap-update-ns/utils.go).
//
// Every driver-libs interface's MountConnectedPlug calls this before adding
// its own entries under assemblyRoot. If a snap connects to more than one
// driver-libs interface the identical entry gets added multiple times, but
// mount.Specification.MountEntries (via unclashMountEntries) merges entries
// that share the same Dir, Name and Type into one, so only a single tmpfs
// mount is ever actually applied.
//
// This entry is deliberately left with x-snapd.origin unset ("non-layout"),
// while every assembly bind and redistribution bind entry under it is tagged
// osutil.XSnapdOriginLayout() (see mountAssemblyLibDirs/mountAssemblySourceFiles/
// mountAssemblyClientDriver). cmd/snap-update-ns/update.go applies mount
// changes in origin-based passes: "non-layout" (untagged) entries are fully
// prepared *and* applied as one bucket before the separate "layout" bucket's
// own prepare-then-apply cycle even starts. If our tmpfs root shared the
// "non-layout" bucket with its own children, the children's directory
// creation (during the shared prepare sub-pass) would happen *before* the
// tmpfs root's own mount (deferred to the separate, later apply sub-pass) --
// landing on the real host directory, which the tmpfs then hides once it
// mounts, causing every nested bind to fail with ENOENT (reproduced on
// device). Keeping the root alone in "non-layout" guarantees it is fully
// mounted (prepared *and* applied) before the "layout" bucket's own prepare
// step for its children even begins.
func mountAssemblyRoot(spec *mount.Specification) error {
	return spec.AddMountEntry(osutil.MountEntry{
		Name:    "tmpfs",
		Dir:     filepath.Dir(assemblyRoot),
		Type:    "tmpfs",
		Options: []string{"mode=0755", "uid=0", "gid=0"},
	})
}

func mountAssemblyLibDirs(spec *mount.Specification, slot *interfaces.ConnectedSlot, ifaceName string) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}

	providerSlot := slot.Snap().InstanceName() + "_" + slot.Name()
	expanded := slot.AppSet().ExpandSliceSnapVariablesWithOrder(libDirs)
	for _, dir := range expanded {
		rel, err := libraryRelPath(slot, dir.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(assemblyRoot, ifaceName, "lib", providerSlot, rel)
		if err := spec.AddMountEntry(osutil.MountEntry{
			Name:    dir.Path,
			Dir:     target,
			Options: []string{"bind", "ro", osutil.XSnapdOriginLayout()},
		}); err != nil {
			return err
		}
		spec.AddLibraryPathDir(target)
	}
	return nil
}

// loaderScannedTarget returns the canonical loader-scanned system path that
// the given assembly share/ subdir is redistributed to (Pass 2), or "" if the
// subdir is not redistributed (e.g. library dirs, which are discovered via
// SNAP_LIBRARY_PATH in Pass 3).
func loaderScannedTarget(subdir string) string {
	switch subdir {
	case "egl_vendor.d":
		return "/usr/share/glvnd/egl_vendor.d"
	case "vulkan/icd.d":
		return "/usr/share/vulkan/icd.d"
	case "vulkan/implicit_layer.d":
		return "/usr/share/vulkan/implicit_layer.d"
	case "vulkan/explicit_layer.d":
		return "/usr/share/vulkan/explicit_layer.d"
	case "gbm":
		return fmt.Sprintf("/usr/lib/%s-linux-gnu/gbm", osutil.MachineName())
	}
	return ""
}

// mountAssemblySourceFiles mounts each source file found by sourceDirsCheck in
// the sda attribute (icd-source / *-layer-source) as a read-only file bind into
// the per-interface assembly tree under
// <assemblyRoot>/<iface>/share/<subdir>/, using the very same encoded file
// names as the classic symlinks (sourceDirEncodedName).
func mountAssemblySourceFiles(
	spec *mount.Specification, slot *interfaces.ConnectedSlot, ifaceName string,
	sda sourceDirAttr, subdir string,
	checker func(slot *interfaces.ConnectedSlot, content []byte) error,
	withPriority bool,
) error {
	if withPriority {
		var priority int64
		if err := slot.Attr("priority", &priority); err != nil {
			return fmt.Errorf("invalid priority: %w", err)
		}
	}

	sourcePaths, err := sourceDirsCheck(slot, sda, checker)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", sda.attrName, err)
	}

	for _, pathDirIdx := range sourcePaths {
		encodedName, err := sourceDirEncodedName(slot, pathDirIdx, withPriority)
		if err != nil {
			return err
		}
		target := filepath.Join(assemblyRoot, ifaceName, "share", subdir, encodedName)
		if err := spec.AddMountEntry(osutil.MountEntry{
			Name:    pathDirIdx.path,
			Dir:     target,
			Options: []string{"bind", "ro", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
		}); err != nil {
			return err
		}
		// Pass 2: the metadata is also redistributed to the canonical
		// loader-scanned system location, re-binding the assembly target above
		// (single source of truth) so the Vulkan/GLVND/GBM loaders discover it
		// env-free.
		if loaderDir := loaderScannedTarget(subdir); loaderDir != "" {
			loaderTarget := filepath.Join(loaderDir, encodedName)
			if err := spec.AddMountEntry(osutil.MountEntry{
				Name:    target, // source = assembly target (bound above)
				Dir:     loaderTarget,
				Options: []string{"bind", "ro", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// mountAssemblyClientDriver binds the client-driver library of the gbm slot as a
// file into <assemblyRoot>/<iface>/share/gbm/, keeping its original file name.
func mountAssemblyClientDriver(spec *mount.Specification, slot *interfaces.ConnectedSlot, ifaceName string) error {
	var clientDriver string
	if err := slot.Attr("client-driver", &clientDriver); err != nil {
		return fmt.Errorf("invalid client-driver: %w", err)
	}
	path, err := filePathInLibDirs(slot, clientDriver)
	if err != nil {
		return err
	}
	target := filepath.Join(assemblyRoot, ifaceName, "share", "gbm", clientDriver)
	if err := spec.AddMountEntry(osutil.MountEntry{
		Name:    path,
		Dir:     target,
		Options: []string{"bind", "ro", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
	}); err != nil {
		return err
	}
	// Pass 2: the client-driver metadata is also redistributed to the
	// loader-scanned GBM directory, re-binding the assembly target above.
	loaderTarget := filepath.Join(loaderScannedTarget("gbm"), clientDriver)
	return spec.AddMountEntry(osutil.MountEntry{
		Name:    target, // source = assembly target (bound above)
		Dir:     loaderTarget,
		Options: []string{"bind", "ro", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
	})
}

// assemblyAppArmorEntry emits the snap-update-ns apparmor rules authorizing a
// read-only bind of source into target inside the snap-update-ns mount
// namespace, following the pattern used by the content interface (see
// content.go). The {,-[0-9]*} suffix grants the same permissions to the
// unclash-renamed targets (-2, -3, ...). isDir selects the directory vs file
// bind variant (trailing slashes and directory semantics).
func assemblyAppArmorEntry(emit func(f string, args ...any), source, target string, isDir bool) {
	// Escape backslashes for AppArmor double-quoted strings. Metadata file
	// targets use systemd.EscapeUnitNamePath for their filenames, which
	// produces literal \xNN sequences (e.g. hyphen becomes \x2d). Inside
	// AppArmor double quotes, \xNN is interpreted as a hex escape, so the
	// literal \xNN in the path must be escaped as \\xNN to match the actual
	// filesystem path.
	source = strings.ReplaceAll(source, "\\", "\\\\")
	target = strings.ReplaceAll(target, "\\", "\\\\")
	if isDir {
		emit("  mount options=(rw, bind) \"%s/\" -> \"%s{,-[0-9]*}/\",\n", source, target)
		emit("  remount options=(bind, ro) \"%s{,-[0-9]*}/\",\n", target)
		emit("  mount options=(rprivate) -> \"%s{,-[0-9]*}/\",\n", target)
		emit("  umount \"%s{,-[0-9]*}/\",\n", target)
	} else {
		emit("  mount options=(rw, bind) \"%s\" -> \"%s{,-[0-9]*}\",\n", source, target)
		emit("  remount options=(bind, ro) \"%s{,-[0-9]*}\",\n", target)
		emit("  mount options=(rprivate) -> \"%s{,-[0-9]*}\",\n", target)
		emit("  umount \"%s{,-[0-9]*}\",\n", target)
	}
	// Authorize snap-update-ns to construct the writable-mimic of the target
	// (and of any unclash-renamed variant) at runtime; the mimic scope itself
	// is decided by snap-update-ns.
	apparmor.GenWritableProfile(emit, target, 1)
	apparmor.GenWritableProfile(emit, fmt.Sprintf("%s-[0-9]*", target), 1)
}

// addAppArmorRedistributionAccess grants the application read access to the
// loader-scanned system locations where Pass 2 redistributes this interface's
// metadata, so the in-process loaders (Vulkan/GLVND/GBM) can discover the ICDs
// and layers env-free. Only the specific subtrees the interface populates are
// granted (least privilege, per-interface).
func addAppArmorRedistributionAccess(spec *apparmor.Specification, ifaceName string) {
	switch ifaceName {
	case eglDriverLibs:
		spec.AddSnippet(`
  /usr/share/glvnd/ r,
  /usr/share/glvnd/egl_vendor.d/{,**} r,`)
	case vulkanDriverLibs:
		spec.AddSnippet(`
  /usr/share/vulkan/ r,
  /usr/share/vulkan/{,icd.d,implicit_layer.d,explicit_layer.d}/{,**} r,`)
	case gbmDriverLibs:
		spec.AddSnippet("  /usr/lib/@{multiarch}/gbm/{,**} r,\n")
	}
}

// addAppArmorAssemblyAccess grants the application read access to the per-interface
// driver-libs assembly tree under assemblyRoot/<iface>/. The core base
// template (unlike the non-core/classic template) does not grant /opt/** or /run/** to
// apps, so each driver-libs interface must grant access to its own subtree explicitly.
// Only the library/metadata files bind-mounted by the mount backend are exposed; the
// app can read its own interface's subtree but not other interfaces' or assemblyRoot broadly.
// addAppArmorAssemblyRoot authorizes snap-update-ns to mount the shared tmpfs
// at assemblyRoot (see mountAssemblyRoot), mirroring the apparmor rules a
// snap.yaml "layout: {type: tmpfs}" entry gets (interfaces/apparmor/spec.go's
// emitLayout, case layout.Type == "tmpfs"). Unlike a layout tmpfs mimic target,
// assemblyRoot's parent (/run/snapd) is already writable and covered by the
// trespassing whitelist, so no GenWritableProfile/mimic authorization is
// needed here for creating the mountpoint itself.
func addAppArmorAssemblyRoot(spec *apparmor.Specification) {
	spec.AddUpdateNSf(`
  # Driver-libs assembly tree root: a private tmpfs so that only the bare
  # mountpoint directory, not its content, is ever host-visible.
  mount fstype=tmpfs tmpfs -> "%[1]s/",
  mount options=(rprivate) -> "%[1]s/",
  umount "%[1]s/",
`, filepath.Dir(assemblyRoot))
}

func addAppArmorAssemblyAccess(spec *apparmor.Specification, ifaceName string) {
	spec.AddSnippet(fmt.Sprintf(`
  # Driver-libs assembly tree for %[3]s: read the bind-mounted provider
  # libraries and ICD/layer metadata under %[2]s/%[3]s/.
  %[1]s/ r,
  %[2]s/ r,
  %[2]s/%[3]s/ r,
  %[2]s/%[3]s/** mrkix,
`, filepath.Dir(assemblyRoot), assemblyRoot, ifaceName))
}

// addAppArmorAssemblyLibDirs emits the snap-update-ns apparmor rules for the
// library dirs bound into the assembly tree (see mountAssemblyLibDirs for the
// mount side).
func addAppArmorAssemblyLibDirs(spec *apparmor.Specification, slot *interfaces.ConnectedSlot, ifaceName string) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}

	providerSlot := slot.Snap().InstanceName() + "_" + slot.Name()
	emit := spec.AddUpdateNSf
	expanded := slot.AppSet().ExpandSliceSnapVariablesWithOrder(libDirs)
	for _, dir := range expanded {
		rel, err := libraryRelPath(slot, dir.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(assemblyRoot, ifaceName, "lib", providerSlot, rel)
		emit("  # Driver-libs assembly library dir %s\n", target)
		assemblyAppArmorEntry(emit, dir.Path, target, true)
	}
	return nil
}

// addAppArmorAssemblySourceFiles emits the snap-update-ns apparmor rules for the
// metadata source files (icd-source / *-layer-source) bound into the assembly
// tree (see mountAssemblySourceFiles for the mount side).
func addAppArmorAssemblySourceFiles(
	spec *apparmor.Specification, slot *interfaces.ConnectedSlot, ifaceName string,
	sda sourceDirAttr, subdir string,
	checker func(slot *interfaces.ConnectedSlot, content []byte) error,
	withPriority bool,
) error {
	if withPriority {
		var priority int64
		if err := slot.Attr("priority", &priority); err != nil {
			return fmt.Errorf("invalid priority: %w", err)
		}
	}

	sourcePaths, err := sourceDirsCheck(slot, sda, checker)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", sda.attrName, err)
	}

	emit := spec.AddUpdateNSf
	for _, pathDirIdx := range sourcePaths {
		encodedName, err := sourceDirEncodedName(slot, pathDirIdx, withPriority)
		if err != nil {
			return err
		}
		target := filepath.Join(assemblyRoot, ifaceName, "share", subdir, encodedName)
		emit("  # Driver-libs assembly metadata file %s\n", target)
		assemblyAppArmorEntry(emit, pathDirIdx.path, target, false)
		// Pass 2: authorize snap-update-ns to redistribute the metadata to the
		// canonical loader-scanned system location (see loaderScannedTarget).
		if loaderDir := loaderScannedTarget(subdir); loaderDir != "" {
			loaderTarget := filepath.Join(loaderDir, encodedName)
			emit("  # Driver-libs redistribution %s -> %s\n", target, loaderTarget)
			assemblyAppArmorEntry(emit, target, loaderTarget, false)
		}
	}
	return nil
}

// addAppArmorAssemblyClientDriver emits the snap-update-ns apparmor rules for
// the gbm client-driver file bound into the assembly tree (see
// mountAssemblyClientDriver for the mount side).
func addAppArmorAssemblyClientDriver(spec *apparmor.Specification, slot *interfaces.ConnectedSlot, ifaceName string) error {
	var clientDriver string
	if err := slot.Attr("client-driver", &clientDriver); err != nil {
		return fmt.Errorf("invalid client-driver: %w", err)
	}
	path, err := filePathInLibDirs(slot, clientDriver)
	if err != nil {
		return err
	}
	target := filepath.Join(assemblyRoot, ifaceName, "share", "gbm", clientDriver)
	spec.AddUpdateNSf("  # Driver-libs assembly client driver %s\n", target)
	assemblyAppArmorEntry(spec.AddUpdateNSf, path, target, false)
	// Pass 2: authorize snap-update-ns to redistribute the client-driver
	// metadata to the loader-scanned GBM directory.
	loaderTarget := filepath.Join(loaderScannedTarget("gbm"), clientDriver)
	spec.AddUpdateNSf("  # Driver-libs redistribution %s -> %s\n", target, loaderTarget)
	assemblyAppArmorEntry(spec.AddUpdateNSf, target, loaderTarget, false)
	return nil
}

// symlinksForSourceDir adds symlinks to be created in targetDir to spec, for the
// files in the directories found in the sda attribute of slot. The checker function
// function ensures that the files we are going to point to have the right content.
// withPriority tells the function if there is a priority attribute that needs to be
// considered when creating the symlink name.
func symlinksForSourceDir(
	spec *symlinks.Specification, slot *interfaces.ConnectedSlot,
	sda sourceDirAttr,
	targetDir string,
	checker func(slot *interfaces.ConnectedSlot, content []byte) error,
	withPriority bool,
) error {
	if withPriority {
		var priority int64
		if err := slot.Attr("priority", &priority); err != nil {
			return fmt.Errorf("invalid priority: %w", err)
		}
	}

	sourcePaths, err := sourceDirsCheck(slot, sda, checker)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", sda.attrName, err)
	}

	// Create symlinks to snap content (which is fine as this is for super-privileged slots)
	for _, pathDirIdx := range sourcePaths {
		encodedName, err := sourceDirEncodedName(slot, pathDirIdx, withPriority)
		if err != nil {
			return err
		}
		linkPath := filepath.Join(targetDir, encodedName)
		if err := spec.AddSymlink(pathDirIdx.path, linkPath); err != nil {
			return err
		}
	}

	return nil
}
