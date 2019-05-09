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
package gadget

import (
	"errors"
	"fmt"
)

var (
	ErrNoUpdate = errors.New("no update needed")
)

func resolveVolume(old *Info, new *Info) (oldVol, newVol *Volume, err error) {
	// support only one volume
	if len(new.Volumes) != 1 || len(old.Volumes) != 1 {
		return nil, nil, errors.New("cannot update with more than one volume")
	}

	var name string
	for n := range old.Volumes {
		name = n
		break
	}
	oldV := old.Volumes[name]

	newV, ok := new.Volumes[name]
	if !ok {
		return nil, nil, fmt.Errorf("cannot find entry for volume %q in updated gadget info", name)
	}

	return &oldV, &newV, nil
}

func isSameOffset(one *Size, two *Size) bool {
	if one == nil && two == nil {
		return true
	}
	if one != nil && two != nil {
		return *one == *two
	}
	return false
}

func isSameRelativeOffset(one *RelativeOffset, two *RelativeOffset) bool {
	if one == nil && two == nil {
		return true
	}
	if one != nil && two != nil {
		return *one == *two
	}
	return false
}

func isLegacyMBRTransition(from *PositionedStructure, to *PositionedStructure) bool {
	// legacy MBR could have been specified by setting type: mbr, with no
	// role
	return from.Type == MBR && to.EffectiveRole() == MBR
}

func canUpdateStructure(from *PositionedStructure, to *PositionedStructure) error {
	if from.Size != to.Size {
		return fmt.Errorf("cannot change structure size from %v to %v", from.Size, to.Size)
	}
	if !isSameOffset(from.Offset, to.Offset) {
		return fmt.Errorf("cannot change structure offset from %v to %v", from.Offset, to.Offset)
	}
	if from.StartOffset != to.StartOffset {
		return fmt.Errorf("cannot change structure start offset from %v to %v", from.StartOffset, to.StartOffset)
	}
	// TODO: should this limitation be lifted?
	if !isSameRelativeOffset(from.OffsetWrite, to.OffsetWrite) {
		return fmt.Errorf("cannot change structure offset-write from %v to %v", from.OffsetWrite, to.OffsetWrite)
	}
	if from.EffectiveRole() != to.EffectiveRole() {
		return fmt.Errorf("cannot change structure role from %q to %q", from.EffectiveRole(), to.EffectiveRole())
	}
	if from.Type != to.Type {
		if !isLegacyMBRTransition(from, to) {
			return fmt.Errorf("cannot change structure type from %q to %q", from.Type, to.Type)
		}
	}
	if from.ID != to.ID {
		return fmt.Errorf("cannot change structure ID from %q to %q", from.ID, to.ID)
	}
	if !to.IsBare() {
		if from.IsBare() {
			return fmt.Errorf("cannot change a bare structure to filesystem one")
		}
		if from.Filesystem != to.Filesystem {
			return fmt.Errorf("cannot change filesystem from %q to %q",
				from.Filesystem, to.Filesystem)
		}
		if from.EffectiveFilesystemLabel() != to.EffectiveFilesystemLabel() {
			return fmt.Errorf("cannot change filesystem label from %q to %q",
				from.Label, to.Label)
		}
	} else {
		if !from.IsBare() {
			return fmt.Errorf("cannot change a filesystem structure to a bare one")
		}
	}

	return nil
}
