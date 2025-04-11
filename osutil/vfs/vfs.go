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

// Package vfs implements a virtual file system sufficient to predict how mount,
// bind-mount and unmount work. The [VFS.Stat] function returns a [fs.FileInfo] whose
// [fs.FileInfo.Sys] function returns information from the underlying [fs.StatFS].
// The [VFS.Open] function panics, allowing the implementation to be simpler.
package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

var (
	errNotDir     = errors.New("not a directory")
	errNotMounted = errors.New("not mounted")
	errMountBusy  = errors.New("mount is busy")
)

// MountID is a 64 bit identifier of a mount.
type MountID int64

// RootMountID is the mount ID of the root file system in any VFS.
const RootMountID MountID = -1 // LSMT_ROOT

// VFS models a virtual file system.
type VFS struct {
	mu          sync.RWMutex
	mounts      []*mount
	nextMountID MountID
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

// dom returns the mount that dominates a given path.
//
// Out of all the mounts in the VFS, the last one that dominates a given path, wins.
// Mounts are searched back-to-front. The search has linear complexity.
//
// The returned index is the index of the returned mount in [VFS.mounts].
func (v *VFS) dom(path string) (idx int, m *mount, suffix, fsPath string) {
	for idx = len(v.mounts) - 1; idx >= 0; idx-- {
		m = v.mounts[idx]
		suffix, fsPath, ok := m.isDom(path)
		if ok {
			return idx, m, suffix, fsPath
		}
	}

	panic("We should have found the rootfs while looking for mount dominating " + path)
}

// mount keeps track of mounted file systems.
type mount struct {
	mountID    MountID
	parentID   MountID
	mountPoint string // Path of the mount point in the VFS. This might be a file.
	rootDir    string // Path of fsFS that is actually mounted.
	isDir      bool   // Mount is attached to a directory.
	fsFS       fs.StatFS
}

// String returns a mountinfo-like representation of the mount.
//
// Elements absent in the VFS implementation are represented as a single underscore.
// Those include: major and minor device numbers and mount options.
// The "optional fields" listed before the single '-' byte (see shared_subtrees.txt in Linux kernel)
// are technically present although at this point they are always empty.
func (m *mount) String() string {
	return fmt.Sprintf("%d %d _:_  /%s /%s _ - %T %p _", m.mountID, m.parentID, m.rootDir, m.mountPoint, m.fsFS, m.fsFS)
}

// isDom returns the dominated suffix and file system path if the mount dominates the given path.
//
// A mount dominates the path in one of two cases:
//
//  1. The path is the same as the mount point.
//  2. The mount point is a directory and the mount point subtree prefix is a prefix of
//     the path.
//
// A mount point subtree prefix is the mount point followed by a directory separator
// except for when the mount point is the empty string to denote the root directory
// which dominates all the paths.
func (m *mount) isDom(path string) (domSuffix, fsPath string, ok bool) {
	// Path cannot be dominated by a mount point that is longer.
	if len(path) < len(m.mountPoint) {
		return "", "", false
	}

	// Exact match works for both files and directories.
	if path == m.mountPoint {
		domSuffix = ""
		// NOTE: filepath.Join uses filepath.Clean which transforms "" to ".", as needed by [fs.Fs].
		fsPath = filepath.Join(m.rootDir, ".")
		return domSuffix, fsPath, true
	}

	// The rest only works for directories.
	if !m.isDir {
		return "", "", false
	}

	// The rootfs dominates everything.
	if m.mountPoint == "" {
		// NOTE: filepath.Clean transforms "" to ".", as needed by [fs.Fs].
		// In practice m.rootDir is going to be empty unless we start to support
		// pivot root, but keep the logic for completion.
		return path, filepath.Join(m.rootDir, path), true
	}

	// The mount point must be a prefix of the path to dominate it.
	if !strings.HasPrefix(path, m.mountPoint) {
		return "", "", false
	}

	if path[len(m.mountPoint)] != '/' {
		// If we don't have a slash in the path then this is not a dominated sup-path
		// but an unrelated path with a common prefix.
		return "", "", false
	}

	domSuffix = path[len(m.mountPoint)+1:]

	// NOTE: filepath.Clean transforms "" to ".", as needed by [fs.Fs].
	fsPath = filepath.Join(m.rootDir, domSuffix)

	return domSuffix, fsPath, true
}

// unpackPathError extracts [fs.PathError] and returns the error store within, if possible.
func unpackPathError(err error) error {
	var pe *fs.PathError

	if errors.As(err, &pe) {
		return pe.Err
	}

	return err
}
