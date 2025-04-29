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

// MakeShared changes propagation of the given mount to "shared".
//
// The function fails if the path is not a mount point.
// If the mount is unbindable, it stops being so.
// If the mount point is not shared then it becomes the first member of a new peer group.
func (v *VFS) MakeShared(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, "make-shared", func(m *mount, suffix string) error {
		if m.shared == 0 {
			v.lastGroupID++
			m.shared = v.lastGroupID
		}
		m.unbindable = false

		return nil // XXX: should this ever fail?
	})
}

// MakeSlave changes propagation of a given mount to "slave".
//
// The function fails if the path is not a mount point.
// If the mount is unbindable, it stops being so.
// If the mount was shared, it becomes a slave of the former peer group and stops being shared.
func (v *VFS) MakeSlave(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, "make-private", func(m *mount, suffix string) error {
		if m.shared != 0 {
			m.master = m.shared
		}
		m.shared = 0

		m.unbindable = false

		return nil // XXX: should this ever fail?
	})
}

// MakePrivate changes propagation of a given mount to "private".
//
// The function fails if the path is not a mount point.
// If the mount is unbindable, it stops being so.
// If the mount stops being the member of a peer group and stops receiving propagation from master.
func (v *VFS) MakePrivate(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, "make-private", func(m *mount, suffix string) error {
		m.shared = 0
		m.master = 0
		m.unbindable = false // XXX: is this correct?

		return nil
	})
}

func (v *VFS) MakeUnbindable(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, "make-unbindable", func(m *mount, suffix string) error {
		m.shared = 0 // XXX: is this correct?
		m.master = 0 // XXX: is this correct?
		m.unbindable = true

		return nil
	})
}

// atMountPoint calls [fn] if [mountPoint] is an existing mount point.
//
// If [mountPoint] is not a mount point then a [fs.PathError] is returned,
// encapsulating the given path [mountPoint], and operation [op].
func (v *VFS) atMountPoint(mountPoint, op string, fn func(m *mount, suffix string) error) error {
	// Find the mount dominating the mount point.
	_, m, suffix, _ := v.dom(mountPoint)

	// If the VFS suffix is not empty then the given path is _not_ a mount point.
	if suffix != "" && suffix != "." {
		return &fs.PathError{Op: op, Path: mountPoint, Err: errNotMounted}
	}

	return fn(m, suffix)
}
