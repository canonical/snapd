// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package install

import (
	"fmt"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
)

var (
	MakeFilesystem  = makeFilesystem
	WriteContent    = writeContent
	MountFilesystem = mountFilesystem

	CreateMissingPartitions = createMissingPartitions
	BuildPartitionList      = buildPartitionList
	RemoveCreatedPartitions = removeCreatedPartitions
	EnsureNodesExist        = ensureNodesExist

	CreatedDuringInstall = createdDuringInstall
)

func MockContentMountpoint(new string) (restore func()) {
	old := contentMountpoint
	contentMountpoint = new
	return func() {
		contentMountpoint = old
	}
}

func MockSysMount(f func(source, target, fstype string, flags uintptr, data string) error) (restore func()) {
	old := sysMount
	sysMount = f
	return func() {
		sysMount = old
	}
}

func MockSysUnmount(f func(target string, flags int) error) (restore func()) {
	old := sysUnmount
	sysUnmount = f
	return func() {
		sysUnmount = old
	}
}

func MockEnsureNodesExist(f func(dss []gadget.OnDiskStructure, timeout time.Duration) error) (restore func()) {
	old := ensureNodesExist
	ensureNodesExist = f
	return func() {
		ensureNodesExist = old
	}
}

// LaidOutVolumeFromGadget takes a gadget rootdir and lays out the
// partitions as specified. This function does not handle multiple volumes and
// is meant for test helpers only. For runtime users, with multiple volumes
// handled by choosing the ubuntu-* role volume, see LaidOutUbuntuVolumeFromGadget
func LaidOutVolumeFromGadget(gadgetRoot string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	info, err := gadget.ReadInfo(gadgetRoot, model)
	if err != nil {
		return nil, err
	}
	// Limit ourselves to just one volume for now.
	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("cannot position multiple volumes yet")
	}

	constraints := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
		SectorSize:        512,
	}

	for _, vol := range info.Volumes {
		pvol, err := gadget.LayoutVolume(gadgetRoot, vol, constraints)
		if err != nil {
			return nil, err
		}
		// we know  info.Volumes map has size 1 so we can return here
		return pvol, nil
	}
	return nil, fmt.Errorf("internal error in PositionedVolumeFromGadget: this line cannot be reached")
}
