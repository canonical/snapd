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

	"github.com/snapcore/snapd/osutil/vfs/lists"
)

// MakeShared makes a mount point a new member of a peer group.
//
// If the mount point is not yet shared, it joins a new peer group as the only member.
func (v *VFS) MakeShared(mountPoint string) error {
	recursive := false
	return v.makeShared(mountPoint, recursive)
}

// MakeRecursivelyShared is the recursive version of MakeShared.
func (v *VFS) MakeRecursivelyShared(mountPoint string) error {
	recursive := true
	return v.makeShared(mountPoint, recursive)
}

// makeShared makes a non-shared mount an initial member of a new peer group.
func (v *VFS) makeShared(mountPoint string, recursive bool) error {
	// Find the mount dominating the mount point.
	pd := v.pathDominator(mountPoint)

	// If the VFS suffix is not empty then the given path is _not_ a mount point.
	if pd.suffix != "" && pd.suffix != "." {
		return &fs.PathError{Op: "make-shared", Path: mountPoint, Err: errNotMounted}
	}

	v.changePropagationToShared(pd.mount, recursive)

	return nil
}

// changePropagationToShared changes the propagation of a mount to shared.
func (v *VFS) changePropagationToShared(m *mount, recursive bool) {
	// Apply propagation change to the entire mount tree.
	lists.DepthFirstSearch[mountChildren](m)(func(m *mount) bool {
		if !m.shared {
			// Note that we do not need to do anything about [m.peers] because
			// it always links to the containing structure unconditionally.
			m.shared = true
			// Allocate a group ID for the new peer group.
			m.group = v.allocateGroupID()
		}

		// Iterate through the children if a recursive change is requested.
		return recursive
	})
}

// joinPeerGroupOf joins the peer group of the other mount.
func (m *mount) joinPeerGroupOf(other *mount) {
	if !m.shared {
		m.shared = true
		m.group = other.group
		m.peers.LinkAfter(lists.ContainedHeadlessList[viaPeers](other))
	}
}
