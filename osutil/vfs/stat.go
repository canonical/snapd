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
	"path/filepath"
)

// Stat returns a [fs.FileInfo] describing the named file.
func (v *VFS) Stat(name string) (fs.FileInfo, error) {
	// Find the mount dominating the given name.
	v.mu.RLock()
	_, dom, _, fsPath := v.dom(name)
	v.mu.RUnlock()

	// Stat the path through the file system.
	fsFi, err := dom.fsFS.Stat(fsPath)

	if err != nil {
		// Unpack PathError this may have returned as that error
		// contains paths that make sense in the specific [fs.FS] attached
		// to the super-block, but not necessarily in the VFS.
		return nil, &fs.PathError{Op: "stat", Path: name, Err: unpackPathError(err)}
	}

	// Wrap the returned [fs.FileInfo] to obscure bind-mounted names.
	vfsFi := &fileInfo{FileInfo: fsFi, name: filepath.Base(name)}

	return vfsFi, nil
}

// file wraps a [fs.FileInfo] to obscure the original name.
type fileInfo struct {
	fs.FileInfo
	name string // Name, possibly obscured by bind-mounts.
}

// Name returns the name of the file.
func (fi *fileInfo) Name() string { return fi.name }
