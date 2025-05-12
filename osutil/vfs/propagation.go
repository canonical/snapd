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

// MakeShared changes propagation of the given mount to "shared".
//
// The function fails if the path is not a mount point.
// If the mount is unbindable, it stops being so.
// If the mount point is not shared then it becomes the first member of a new peer group.
func (v *VFS) MakeShared(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, "make-shared", func(_ int, m *mount, suffix string) error {
		if m.shared == 0 {
			v.lastGroupID++
			m.shared = v.lastGroupID
		}
		m.unbindable = false

		return nil
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

	return v.atMountPoint(mountPoint, "make-private", func(_ int, m *mount, suffix string) error {
		if m.shared != 0 {
			m.master = m.shared
		}
		m.shared = 0
		m.unbindable = false

		return nil
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

	return v.atMountPoint(mountPoint, "make-private", func(_ int, m *mount, suffix string) error {
		m.shared = 0
		m.master = 0
		m.unbindable = false

		return nil
	})
}

func (v *VFS) MakeUnbindable(mountPoint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, "make-unbindable", func(_ int, m *mount, suffix string) error {
		m.shared = 0
		m.master = 0
		m.unbindable = true

		return nil
	})
}
