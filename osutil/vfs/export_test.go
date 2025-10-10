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
	"fmt"

	"github.com/snapcore/snapd/osutil/vfs/lists"
)

var (
	ErrNotMounted = errNotMounted
)

func (v *VFS) FindMount(id MountID) *mount {
	for _, m := range v.mounts {
		if m.mountID == id {
			return m
		}
	}

	return nil
}

func (v *VFS) MustFindMount(id MountID) *mount {
	if m := v.FindMount(id); m != nil {
		return m
	}
	panic(fmt.Sprintf("mount with with %d was not found", id))
}

func (v *VFS) RootMount() *mount                   { return v.mounts[0] }
func (m *mount) MountPoint() string                { return m.mountPoint() }
func (m *mount) Parent() *mount                    { return m.parent }
func (m *mount) Peers() *lists.HeadlessList[mount] { return &m.peers }
