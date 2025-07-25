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

func (v *VFS) FindMount(id MountID) *mount {
	for _, m := range v.mounts {
		if m.mountID == id {
			return m
		}
	}

	return nil
}

// RootMount returns the mount that is the ancestor of all other mounts.
func (v *VFS) RootMount() *mount {
	return v.mounts[0]
}

// MountPoint returns the mount point of the given mount.
func (m *mount) MountPoint() string {
	return m.mountPoint
}

// Parent returns the parent mount.
//
// Parent is nil for detached nodes and for the rootfs.
func (m *mount) Parent() *mount {
	return m.parent
}

// SiblingPtrs returns a pair of pointers to the previous and next mounts with the same parent as the given mount.
func (m *mount) SiblingPtrs() (prev, next *mount) {
	return m.prevSibling, m.nextSibling
}

// ChildrenPtrs returns a pair of pointers to the first and last mounts with the given mount node as the parent.
func (m *mount) ChildrenPtrs() (first, last *mount) {
	return m.firstChild, m.lastChild
}
