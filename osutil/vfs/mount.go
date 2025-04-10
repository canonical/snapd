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

package vfs

import (
	"io/fs"
)

// Mount attaches the given file system to given mount point.
//
// Mount attaches a mount entry to the node represented by the mount point. The
// mount point must be an existing directory in the VFS. If the mount point
// contains other mount entries they are shadowed until the new entry is
// unmounted.
func (v *VFS) Mount(fsFS fs.StatFS, mountPoint string) error {
	// Hold lock throughout the function as we need to consistently mutate m.mounts at the end.
	v.mu.Lock()
	defer v.mu.Unlock()

	// Find the mount dominating the mount point.
	_, dom, _, fsPath := v.dom(mountPoint)

	// Stat the mount point through the file system.
	fsFi, err := dom.fsFS.Stat(fsPath)
	if err != nil {
		// Unpack PathError this may have returned as that error
		// contains paths that make sense in the specific [fs.FS] attached
		// to the super-block, but not necessarily in the VFS.
		return &fs.PathError{Op: "mount", Path: mountPoint, Err: unpackPathError(err)}
	}

	// File systems can only be mounted on existing directories.
	if !fsFi.IsDir() {
		return &fs.PathError{Op: "mount", Path: mountPoint, Err: errNotDir}
	}

	// Mount and return.
	v.mounts = append(v.mounts, &mount{
		mountID:    v.nextMountID,
		parentID:   dom.mountID,
		mountPoint: mountPoint,
		isDir:      true,
		fsFS:       fsFS,
	})
	v.nextMountID++

	return nil
}

// BindMount creates a mount connecting mountPoint to another location.
//
// BindMount attaches a mount entry to the node represented by source. The
// mount entry points to the node represented by target. Both paths must point
// to existing files or directories. The type of the mount point must match the
// type of the source.
func (v *VFS) BindMount(sourcePoint, mountPoint string) error {
	// Hold lock throughout the function as we need to consistently mutate m.mounts at the end.
	v.mu.Lock()
	defer v.mu.Unlock()

	// Find the mount dominating the source point and the mount point.
	_, sourceDom, sourceSuffix, sourceFsPath := v.dom(sourcePoint)
	_, dom, _, fsPath := v.dom(mountPoint)

	// Stat the source point through the file system.
	// The source suffix will influence the rootDir or the new mount entry so keep it.
	sourceFsFi, err := sourceDom.fsFS.Stat(sourceFsPath)
	if err != nil {
		// Unpack PathError this may have returned as that error
		// contains paths that make sense in the specific [fs.FS] attached
		// to the super-block, but not necessarily in the VFS.
		return &fs.PathError{Op: "bind-mount", Path: sourcePoint, Err: unpackPathError(err)}
	}

	// Stat the mount point through the file system.
	fsFi, err := dom.fsFS.Stat(fsPath)
	if err != nil {
		// Same as above.
		return &fs.PathError{Op: "bind-mount", Path: mountPoint, Err: unpackPathError(err)}
	}

	// Bind mount must be between two files or two directories.
	if sourceFsFi.IsDir() != fsFi.IsDir() {
		return &fs.PathError{Op: "bind-mount", Path: mountPoint, Err: fs.ErrInvalid}
	}

	// Compute the new root directory for directories.
	// Files just use sourceFsPath as that is always a correct path.
	// For directories combine the path manually to avoid using filepath.Clean,
	// which turns "" into ".".
	var rootDir string
	if sourceDom.isDir {
		switch {
		case sourceDom.rootDir != "" && sourceSuffix != "":
			rootDir = sourceDom.rootDir + "/" + sourceSuffix
		case sourceDom.rootDir != "":
			rootDir = sourceDom.rootDir
		case sourceSuffix != "":
			rootDir = sourceSuffix
		}
	} else {
		rootDir = sourceFsPath
	}

	// Mount and return.
	v.mounts = append(v.mounts, &mount{
		mountID:    v.nextMountID,
		parentID:   dom.mountID,
		mountPoint: mountPoint,
		rootDir:    rootDir,
		isDir:      fsFi.IsDir(),
		fsFS:       sourceDom.fsFS,
	})

	v.nextMountID++

	return nil
}

// Unmount removes a mount attached to a node otherwise named by mountPoint.
//
// Unmount detaches the topmost mount from the node represented by the mount
// point path. The leaf entry is resolved without following mount entires.
func (v *VFS) Unmount(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Find the mount dominating the mount point.
	// Keep track of the index as we may use it to reslice the mounts array.
	idx, dom, suffix, _ := v.dom(mountPoint)

	// If the VFS suffix is not empty then the given path is _not_ a mount point.
	if suffix != "" {
		return &fs.PathError{Op: "unmount", Path: mountPoint, Err: errNotMounted}
	}

	// Is this mount point a parent of any other mount?
	// By special case of the rootfs mount, it cannot ever be unmounted as it is its own parent.
	for _, m := range v.mounts {
		if m.parentID == dom.mountID {
			return &fs.PathError{Op: "unmount", Path: mountPoint, Err: errMountBusy}
		}
	}

	// Actually forget the mount.
	v.mounts = append(v.mounts[:idx], v.mounts[idx+1:]...)

	return nil
}
