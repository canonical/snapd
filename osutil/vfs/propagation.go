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
	"iter"
	"strings"
)

const flagRecurse = 1 << iota

// makeShared makes a non-shared mount an initial member of a new peer group.
func (v *VFS) makeShared(mountPoint string, flags uint) error {
	return v.propagationChange(mountPoint, "make-shared", flags&flagRecurse != 0, func(m *mount) {
		if m.shared == 0 {
			m.shared = v.allocateGroupID()
		}
		m.unbindable = false
	})
}

// makeSlave converts a shared mount to a slave mount of the same group.
//
// Making a sole member of a shared peer group slave effectively makes
// it private as there is no other member to retain as master.
func (v *VFS) makeSlave(mountPoint string, flags uint) error {
	return v.propagationChange(mountPoint, "make-slave", flags&flagRecurse != 0, func(m *mount) {
		if m.shared != 0 {
			if v.unlockedSharedGroupCount(m.shared) >= 1 {
				m.master = m.shared
			}
			m.shared = 0
		}

		m.unbindable = false
	})
}

// makePrivate makes removes a mount from peer group membership.
func (v *VFS) makePrivate(mountPoint string, flags uint) error {
	return v.propagationChange(mountPoint, "make-private", flags&flagRecurse != 0, func(m *mount) {
		m.shared = 0
		m.master = 0
		m.unbindable = false
	})
}

// makeUnbindable removes a mount from peer group membership, and prevents using it as bind-mount source.
func (v *VFS) makeUnbindable(mountPoint string, flags uint) error {
	return v.propagationChange(mountPoint, "make-private", flags&flagRecurse != 0, func(m *mount) {
		m.shared = 0
		m.master = 0
		m.unbindable = true
	})
}

// MakeShared makes a mount point a new member of a peer group.
//
// The unbindable flag is always cleared.
// If the mount point is not yet shared, it joins a new peer group as the only member.
// When a shared mount is used as a bind-mount source, the shared peer group is retained.
// When a shared mount dominates a mount or unmount operation, the operation is replayed to all the members of the
// shared peer group as well as to all the slaves that were former members.
func (v *VFS) MakeShared(mountPoint string) error {
	return v.makeShared(mountPoint, 0)
}

// MakeRecursivelyShared is the recursive version of MakeShared.
func (v *VFS) MakeRecursivelyShared(mountPoint string) error {
	return v.makeShared(mountPoint, flagRecurse)
}

// MakeSlave makes a mount point a new member of a slave group.
//
// The unbindable flag is always cleared. If the mount is shared then it
// converts from a peer to a slave of the group. Mounts that are slave to a
// peer group receive mount and unmount operations from the group, but do not
// propagate any operations back.
func (v *VFS) MakeSlave(mountPoint string) error {
	return v.makeSlave(mountPoint, 0)
}

// MakeRecursivelySlave is a recursive version of MakeSlave.
func (v *VFS) MakeRecursivelySlave(mountPoint string) error {
	return v.makeSlave(mountPoint, flagRecurse)
}

// MakePrivate removes a mount point from peer groups and makes it bindable again.
func (v *VFS) MakePrivate(mountPoint string) error {
	return v.makePrivate(mountPoint, 0)
}

// MakeRecursivelyPrivate is a recursive version of MakePrivate.
func (v *VFS) MakeRecursivelyPrivate(mountPoint string) error {
	return v.makePrivate(mountPoint, flagRecurse)
}

// MakeUnbindable makes the mount point unbindable.
//
// A bind mount operation that uses an unbindable mount as source immediately
// fails. A recursive bind mount operation ignores any unbindable mounts without
// failure.
func (v *VFS) MakeUnbindable(mountPoint string) error {
	return v.makeUnbindable(mountPoint, 0)
}

// MakeRecursivelyUnbindable is a recursive version of MakeUnbindable.
func (v *VFS) MakeRecursivelyUnbindable(mountPoint string) error {
	return v.makeUnbindable(mountPoint, flagRecurse)
}

// prpagationChange applies a propagation change function fn at a given mount point.
//
// The string [op] is used to construct an error value in case [mountPoint] is
// not a mount point.  The [recurse] flag causes the function to be called
// recursively on all the descendants of the mount denoted by [mountPoint]. The
// descendants are all the mounts whose parentID is that can be traced back to
// the selected starting mount.
func (v *VFS) propagationChange(mountPoint, op string, recurse bool, fn func(m *mount)) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.atMountPoint(mountPoint, op, func(pd pathDominator) error {
		fn(pd.mount)

		if recurse {
			ancestorID := pd.mount.mountID
			for _, m := range v.mounts[pd.index+1:] {
				if m.hasAncestor(ancestorID) {
					fn(m)
				}
			}
		}

		return nil
	})
}

// atMountPoint calls [fn] if [mountPoint] is an existing mount point.
//
// If [mountPoint] is not a mount point then a [fs.PathError] is returned,
// encapsulating the given path [mountPoint], and operation [op].
func (v *VFS) atMountPoint(mountPoint, op string, fn func(pd pathDominator) error) error {
	// Find the mount dominating the mount point.
	pd := v.pathDominator(mountPoint)

	// If the VFS suffix is not empty then the given path is _not_ a mount point.
	if pd.suffix != "" && pd.suffix != "." {
		return &fs.PathError{Op: op, Path: mountPoint, Err: errNotMounted}
	}

	return fn(pd)
}

// sliceContains returns true if the given slice [sl] contains element [elem].
func sliceContains[T comparable](sl []T, elem T) bool {
	for i := range sl {
		if sl[i] == elem {
			return true
		}
	}

	return false
}

// unlockedSharedGroupCount countns the members of the shared peer group [id].
func (v *VFS) unlockedSharedGroupCount(id GroupID) (count int) {
	for _, m := range v.mounts {
		if m.shared == id {
			count++
		}
	}

	return count
}

// maybePropagateMount propagates the mount m among the members of the parent's
// peer group and their slaves. The function does nothing if the parent mount
// is not shared.
func (v *VFS) propagateMount(m *mount, skipFn func(m *mount) bool) {
	if m.parent == nil {
		panic("m.parent == nil")
	}

	if m.parent.shared == 0 {
		panic("m.parent.shared == 0")
	}

	// TODO: use range syntax once we upgrade to go 1.23
	v.peersAndSlavesOf(m.parent.shared)(func(peer *mount) bool {
		if skipFn(peer) {
			return true
		}

		// Skip this peer if it doesn't see the part of the file system that
		// the event originated from. Here m.attachedAt s the place where the
		// event originated within the parent mount, and peer.rootDir is the
		// sub-tree of the parent mount that is actually visible in the peer.
		if !strings.HasPrefix(m.attachedAt, peer.rootDir) {
			return true
		}

		// Re-locate the attachment point based on the sub-tree that the peer
		// is using. Note that we just checked the presence of the prefix in
		// the test above.
		attachedAt := strings.TrimPrefix(m.attachedAt, peer.rootDir)

		// Compute how the new propagated mount is shared based on the type of
		// peer (peer or slave peer) is the target of the propagation.
		var master, shared GroupID
		if peer.shared == m.parent.shared {
			// Propagating into a peer. The shared group is retained.
			shared = m.shared
		} else {
			// Propagating into a slave. The shared group becomes slave.
			master = m.shared
			if shared == 0 && peer.shared != 0 {
				// The slave is also shared (independently of the peer group
				// that undergoes propagation) so the propagated mount joins a
				// new peer group.
				//
				// FIXME(propagation): Should this propagate again? After all,
				// we are mounting something here so ... yes?
				shared = v.allocateGroupID()
			}
		}

		newM := &mount{
			attachedAt: attachedAt,
			rootDir:    m.rootDir,
			isDir:      m.isDir,
			fsFS:       m.fsFS,
			shared:     shared,
			master:     master,
		}
		v.attachMount(peer, newM)

		return true
	})
}

func (v *VFS) peersAndSlavesOf(id GroupID) iter.Seq[*mount] {
	if id == 0 {
		panic("cannot call peersAndSlavesOf(0)")
	}

	return func(yield func(*mount) bool) {
		for _, m := range v.mounts[:] {
			if m.shared == id || m.master == id {
				if !yield(m) {
					return
				}
			}
		}
	}
}
