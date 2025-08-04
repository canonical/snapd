// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

// Package vfs implements a virtual file system sufficient to predict how
// mount, bind-mount and unmount work. The [VFS.Stat] function returns a
// [fs.FileInfo] whose [fs.FileInfo.Sys] function returns information from the
// underlying [fs.StatFS].  The [VFS.Open] function panics, allowing the
// implementation to be simpler.
//
// # POSIX path semantics vs Go path semantics
//
// The package does not support typical POSIX path semantics and instead it
// leans heavily on the simplified Go model. The list of important distinctions
// that are worth pointing out are:
//
//   - All paths are absolute (rooted at the start of the VFS) while using
//     relative path syntax (no leading slash). This reduces friction with
//     [fs.FS] and removes some edge cases.
//   - The root directory is represented by an empty string.
//   - As a special exception for [fs.FS] support, "." refers to the root
//     directory. This comes up in some path transformations where a longer
//     path in the VFS corresponds to the root directory of a [fs.FS]. File
//     operations on that VFS path are translated to operate on "." in the Go
//     file system.
//   - Mount operations refuse to work with trailing slashes. This removes the
//     need for a full-blown path resolution logic, and is in sync with Go.
//
// # Limitations
//
// ## No symbolic links
//
// Go 1.25 introduces [fs.ReadLinkFS]. This implementation does not support it
// and would require significant rework to allow it correctly.
//
// ## No writable file systems
//
// The implementation does not support mutable file systems. For correct
// implementation all mutations would have to traverse the VFS first. Even if
// supported in the future modifying any of the mounted file systems behind the
// scenes is not supported.
//
// ## No node cache
//
// The implementation does not contain any caching facilities so all operations
// have a cost proportional to the number of mount entries with linear
// complexity.
package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"path/filepath"
	"strings"
	"sync"
)

var (
	errNotDir        = errors.New("not a directory")
	errNotMounted    = errors.New("not mounted")
	errMountBusy     = errors.New("mount is busy")
	errTrailingSlash = errors.New("path ends with a slash")
)

// MountID is a 64 bit identifier of a mount.
type MountID int64

// GroupID is a 64 bit identifier of a mount peer group.
type GroupID int64

// RootMountID is the mount ID of the root file system in any VFS.
const RootMountID MountID = -1 // LSMT_ROOT

// mount keeps track of mounted file systems.
//
// The key relation is [mount.parent] pointer and [mount.attachedAt] string.
// The parent mount is only nil for the root file system. The attached string
// may be empty if the attachment point is directly shadowing its parent. Such
// attachment chains may more than one mount, if many mounts are made with the
// same path.
type mount struct {
	mountID    MountID
	parentID   MountID
	attachedAt string // Path where the mount is attached in the parent mount, possibly empty.
	rootDir    string // Path of fsFS that is actually mounted.
	isDir      bool   // Mount is attached to a directory.
	fsFS       fs.StatFS
	shared     GroupID // ID of the shared peer group, zero is invalid.
	master     GroupID // ID of the peer group that is the master, zero is invalid.
	unbindable bool    // The mount cannot be used as source.

	mountPointCache *string

	// Links to parent, children and siblings. Any of those may be nil.
	parent      *mount
	firstChild  *mount
	lastChild   *mount
	nextSibling *mount
	prevSibling *mount
}

// ancestors returns an iterator from the node through all of its parents.
//
// The iteration starts at m and continues until the rootfs is reached.
func (m *mount) ancestors() iter.Seq[*mount] {
	return func(yield func(*mount) bool) {
		for ; m != nil; m = m.parent {
			if !yield(m) {
				return
			}
		}
	}
}

// children returns an iterator over the children of the given mount.
//
// Internally the order of iteration is from the first child.
func (m *mount) children() iter.Seq[*mount] {
	return func(yield func(*mount) bool) {
		for m = m.firstChild; m != nil; m = m.nextSibling {
			if !yield(m) {
				return
			}
		}
	}
}

func (m *mount) mountPoint() string {
	if m == nil {
		return ""
	}

	if m.mountPointCache != nil {
		return *m.mountPointCache
	}

	// This is safe to call on nil parent due to the check at the top of the function.
	p := m.parent.mountPoint()
	var mp string
	switch {
	case p != "" && m.attachedAt != "":
		mp = p + "/" + m.attachedAt
	case p != "":
		mp = p
	case m.attachedAt != "":
		mp = m.attachedAt
	}

	m.mountPointCache = &mp

	return mp
}

// pathDominator contains information about the mount that dominates a given path.
type pathDominator struct {
	path   string // the path for which domInfo was computed
	index  int    // index of mount within [VFS.mounts] valid while lock is held.
	mount  *mount
	suffix string // [path] with [mount.mountPoint] removed.
	fsPath string // path for [mount.fsFS]
}

// VFS models a virtual file system.
type VFS struct {
	mu          sync.RWMutex
	mounts      []*mount
	nextMountID MountID
	lastGroupID GroupID // Groups IDs start with 1 so that zero value is not a valid group.
}

// NewVFS returns a VFS with the given root file system mounted.
func NewVFS(rootFS fs.StatFS) *VFS {
	return &VFS{mounts: []*mount{{
		mountID:  RootMountID,
		parentID: RootMountID, // The rootfs is its own parent to prevent being unmounted.
		isDir:    true,
		fsFS:     rootFS,
	}}}
}

// attachMount attaches a new mount to the VFS.
//
// The main responsibility of the function is to allocate a new mount ID and parent ID
// for the child mount and to update linkage between parent and siblings.
func (v *VFS) attachMount(parent *mount, child *mount) {
	if child.mountID != 0 {
		panic("attachMount called with mountID != 0")
	}
	if child.parentID != 0 {
		panic("attachMount called with parentID != 0")
	}

	child.parentID = parent.mountID
	child.mountID = v.allocateMountID()

	child.parent = parent

	if parent.firstChild == nil {
		parent.firstChild = child
	}

	if parent.lastChild != nil {
		child.prevSibling = parent.lastChild
		parent.lastChild.nextSibling = child
	}

	parent.lastChild = child

	v.mounts = append(v.mounts, child)
}

func (v *VFS) detachMount(m *mount, idx int) {
	if m.parent == nil {
		panic("cannot detach rootfs")
	}

	if m.parent.firstChild == m {
		m.parent.firstChild = m.nextSibling
	}
	if m.parent.lastChild == m {
		m.parent.lastChild = m.prevSibling
	}

	if m.prevSibling != nil {
		m.prevSibling.nextSibling = m.nextSibling
	}
	if m.nextSibling != nil {
		m.nextSibling.prevSibling = m.prevSibling
	}

	m.nextSibling = nil
	m.prevSibling = nil
	m.parent = nil

	m.parentID = 0
	m.mountID = 0

	// TODO: use slices from future go to avoid this hand-crafted surgery.
	v.mounts = append(v.mounts[:idx], v.mounts[idx+1:]...)
}

// allocateMountID returns a new mount ID.
func (v *VFS) allocateMountID() MountID {
	id := v.nextMountID
	v.nextMountID++
	return id
}

// allocateGroupID returns a new group ID.
func (v *VFS) allocateGroupID() GroupID {
	// Groups IDs start with 1
	v.lastGroupID++
	id := v.lastGroupID
	return id
}

// pathDominator returns information about the mount that dominates a given path.
//
// Out of all the mounts in the VFS, the last one that dominates a given path,
// wins. Mounts are searched back-to-front. The search has linear complexity.
func (v *VFS) pathDominator(path string) pathDominator {
	for idx := len(v.mounts) - 1; idx >= 0; idx-- {
		m := v.mounts[idx]
		suffix, fsPath, ok := m.isDom(path)
		if ok {
			return pathDominator{path: path, index: idx, mount: m, suffix: suffix, fsPath: fsPath}
		}
	}

	panic("We should have found the rootfs while looking for mount dominating " + path)
}

// isDom returns the dominated suffix and file system path if the mount dominates the given path.
//
// A mount dominates the path in one of two cases:
//
//  1. The path is the same as the mount point.
//  2. The mount point is a directory and the mount point subtree prefix is a
//     prefix of the path.
//
// A mount point subtree prefix is the mount point followed by a directory
// separator except for when the mount point is the empty string to denote the
// root directory which dominates all the paths.
func (m *mount) isDom(path string) (domSuffix, fsPath string, ok bool) {
	mountPoint := m.mountPoint()

	// Path cannot be dominated by a mount point that is longer.
	if len(path) < len(mountPoint) {
		return "", "", false
	}

	// Exact match works for both files and directories.
	if path == mountPoint {
		domSuffix = ""
		// NOTE: [filepath.Join] uses [filepath.Clean] which transforms "" to ".", as needed by [fs.Fs].
		fsPath = filepath.Join(m.rootDir, ".")
		return domSuffix, fsPath, true
	}

	// The rest only works for directories.
	if !m.isDir {
		return "", "", false
	}

	// The rootfs dominates everything.
	if mountPoint == "" {
		// NOTE: [filepath.Clean] transforms "" to ".", as needed by [fs.Fs].
		// In practice m.rootDir is going to be empty unless we start to support
		// pivot root, but keep the logic for completion.
		return path, filepath.Join(m.rootDir, path), true
	}

	// The mount point must be a prefix of the path to dominate it.
	if !strings.HasPrefix(path, mountPoint) {
		return "", "", false
	}

	if path[len(mountPoint)] != '/' {
		// If we don't have a slash in the path then this is not a dominated sub-path
		// but an unrelated path with a common prefix.
		return "", "", false
	}

	// Get the path relative to the mount point.
	domSuffix = path[len(mountPoint)+1:]

	// NOTE: [filepath.Clean] transforms "" to ".", as needed by [fs.Fs].
	fsPath = filepath.Join(m.rootDir, domSuffix)

	return domSuffix, fsPath, true
}

// hasAncestor returns true if the mount's parent (recursively) has the given [id].
func (m *mount) hasAncestor(id MountID) bool {
	if m.parentID == id {
		return true
	}

	if m.parent == nil {
		return false
	}

	return m.parent.hasAncestor(id)
}

// combinedRootDir returns the combination of [mount.rootDir] and [pathDominator.suffix]
//
// The returned value has the correct semantics for a rootDir for bind-mounts,
// unlike [pathDominator.fsPath] which is only valid for use within [fs.FS].
func (pd *pathDominator) combinedRootDir() string {
	// Compute the new root directory for directories.
	// Files just use sourceFsPath as that is always a correct path.
	// For directories combine the path manually to avoid using [filepath.Clean],
	// which turns "" into ".".
	if pd.mount.isDir {
		switch {
		case pd.mount.rootDir != "" && pd.suffix != "":
			// The source mount point is a sub-tree of a whole file system. In Linux
			// terms, it has root directory that is different from / (e.g. /subdir).
			// At the same time, source suffix is a non-empty path within the view.
			//
			// For example if /a is a mount of a slice of a device (dev)/slice, then
			// the directory /a/dir is represents the path (dev)/slice/dir. When we
			// are asked to bind mount /a/dir then the target mount point should
			// attach (dev)/slice/dir.
			return pd.mount.rootDir + "/" + pd.suffix
		case pd.mount.rootDir != "":
			// If sourceSuffix is empty then we are just attaching (dev)/slice.
			return pd.mount.rootDir
		case pd.suffix != "":
			// If sourceSuffix is not empty then we are attaching (dev)/dir.
			return pd.suffix
		default:
			// Root dir just stays empty.
			return ""
		}
		// In all other cases we are attaching all of (dev) so in the Go semantics,
		// the empty directory is sufficient to represent this.
	} else {
		// This is similar to the logic above in principle but the sourceFsPath
		// that was computed by [VFS.dom] is enough. We are attaching a specific
		// file and the path of a file is unambiguous.  It is not affected by / vs
		// // or other path traversal complexities.
		//
		// We cannot use this simple approach above as then we will use the
		// function [filepath.Clean] which causes doom and despair when used with
		// fstest.TestFS that is very clear about indicating what is allowed and
		// what is not allowed. Using Clean papers over all of that, breaking the
		// requirements.
		return pd.fsPath
	}
}

// String returns a mountinfo-like representation of the mount.
//
// Elements absent in the VFS implementation are represented as a single underscore.
// Those include: major and minor device numbers and mount options.
// The "optional fields" listed before the single '-' byte (see shared_subtrees.txt in Linux kernel)
// are technically present although at this point they are always empty.
func (m *mount) String() string {
	var sb strings.Builder

	var (
		major  int
		minor  int
		source string
	)

	// This interface is not implemented by anything in practice but tests
	// may use it to wrap fstest.MapFS to make logs clearer.
	if fs, ok := m.fsFS.(interface{ MajorMinor() (int, int) }); ok {
		major, minor = fs.MajorMinor()
	}

	if fs, ok := m.fsFS.(interface{ Source() string }); ok {
		source = fs.Source()
	}

	if source == "" {
		source = "(source)"
	}

	const (
		mountOpts = "rw"
		sbOpts    = "rw"
		fsType    = "(fstype)"
	)
	fmt.Fprintf(&sb, "%-2d %d %d:%d /%s /%s %s", m.mountID, m.parentID, major, minor, m.rootDir, m.mountPoint(), mountOpts)
	if m.shared != 0 {
		fmt.Fprintf(&sb, " shared:%d", m.shared)
	}
	if m.master != 0 {
		fmt.Fprintf(&sb, " master:%d", m.master)
	}
	// TODO: propagate_from:nnn if master is invisible (not possible yet, needs pivot or chroot or namespaces).
	if m.unbindable {
		fmt.Fprintf(&sb, " unbindable")
	}

	fmt.Fprintf(&sb, " - %s %s %s", fsType, source, sbOpts)
	return sb.String()
}

// String returns a mountinfo-like representation of mount table.
func (v *VFS) String() string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	var sb strings.Builder

	_ = sb.WriteByte('\n') // This reads better when the VFS is printed with leading text.

	for _, m := range v.mounts {
		_, _ = sb.WriteString(m.String())
		_ = sb.WriteByte('\n')
	}

	return sb.String()
}

// unpackPathError extracts [fs.PathError] and returns the error stored within, if possible.
func unpackPathError(err error) error {
	var pe *fs.PathError

	if errors.As(err, &pe) {
		return pe.Err
	}

	return err
}
