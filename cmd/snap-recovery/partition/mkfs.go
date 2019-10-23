// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package partition

import (
	"fmt"

	"github.com/snapcore/snapd/gadget"
)

func MakeFilesystems(created []deviceStructure) error {
	for _, part := range created {
		if part.VolumeStructure.Filesystem != "" {
			if err := mkfs(part.Node, part.VolumeStructure.Label, part.VolumeStructure.Filesystem); err != nil {
				return err
			}
		}
	}
	return nil
}

// mkfs will create a single filesystem on the given node with
// the given label and filesystem type.
func mkfs(node, label, filesystem string) error {
	switch filesystem {
	case "vfat":
		return gadget.MkfsVfat(node, label, "")
	case "ext4":
		return gadget.MkfsExt4(node, label, "")
	default:
		return fmt.Errorf("cannot create unsupported filesystem %q", filesystem)
	}
}
