// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
)

// TODO: consider looking into merging LaidOutVolume/Structure OnDiskVolume/Structure

// OnDiskStructure represents a gadget structure laid on a block device.
type OnDiskStructure struct {
	LaidOutStructure

	// Node identifies the device node of the block device.
	Node string

	// Size of the on disk structure, which is at least equal to the
	// LaidOutStructure.Size but may be bigger if the partition was
	// expanded.
	Size quantity.Size
}

// OnDiskVolume holds information about the disk device including its partitioning
// schema, the partition table, and the structure layout it contains.
type OnDiskVolume struct {
	Structure []OnDiskStructure
	// ID is the disk's identifier, it is a UUID for GPT disks or an unsigned
	// integer for DOS disks encoded as a string in hexadecimal as in
	// "0x1212e868".
	ID string
	// Device is the full device node path for the disk, such as /dev/vda.
	Device string
	// Schema is the disk schema, GPT or DOS.
	Schema string
	// size in bytes
	Size quantity.Size
	// UsableSectorsEnd is the end (exclusive) of usable sectors on the disk,
	// this sector specifically is not usable for partitions, though it may be
	// used for i.e. GPT header backups on some disks. This should be used when
	// calculating the size of an auto-expanded partition instead of the Size
	// parameter which does not take this into account.
	UsableSectorsEnd uint64
	// sector size in bytes
	SectorSize quantity.Size
}

// OnDiskVolumeFromDevice obtains the partitioning and filesystem information from
// the block device.
func OnDiskVolumeFromDevice(device string) (*OnDiskVolume, error) {
	disk, err := disks.DiskFromDeviceName(device)
	if err != nil {
		return nil, err
	}

	return OnDiskVolumeFromDisk(disk)
}

func OnDiskVolumeFromDisk(disk disks.Disk) (*OnDiskVolume, error) {
	parts, err := disk.Partitions()
	if err != nil {
		return nil, err
	}

	structure := make([]VolumeStructure, len(parts))
	ds := make([]OnDiskStructure, len(parts))

	for _, p := range parts {
		// use the index of the structure on the disk rather than the order in
		// which we iterate over the list of partitions, since the order of the
		// partitions is returned "last seen first" which matches the behavior
		// of udev when picking partitions with the same filesystem label and
		// populating /dev/disk/by-label/ and friends
		// all that is to say the order that the list of partitions from
		// Partitions() is in is _not_ the same as the order that the structures
		// actually appear in on disk, but this is why the StructureIndex
		// property exists
		i := p.StructureIndex - 1

		// the PartitionLabel and FilesystemLabel are encoded, so they must be
		// decoded before they can be used in other gadget functions

		decodedPartLabel, err := disks.BlkIDDecodeLabel(p.PartitionLabel)
		if err != nil {
			return nil, fmt.Errorf("cannot decode partition label for partition on disk %s: %v", disk.KernelDeviceNode(), err)
		}
		decodedFsLabel, err := disks.BlkIDDecodeLabel(p.FilesystemLabel)
		if err != nil {
			return nil, fmt.Errorf("cannot decode partition label for partition on disk %s: %v", disk.KernelDeviceNode(), err)
		}

		structure[i] = VolumeStructure{
			Name:       decodedPartLabel,
			Size:       quantity.Size(p.SizeInBytes),
			Label:      decodedFsLabel,
			Type:       p.PartitionType,
			Filesystem: p.FilesystemType,
			ID:         p.PartitionUUID,
		}

		ds[i] = OnDiskStructure{
			LaidOutStructure: LaidOutStructure{
				VolumeStructure: &structure[i],
				StartOffset:     quantity.Offset(p.StartInBytes),
				Index:           int(p.StructureIndex),
			},
			Size: quantity.Size(p.SizeInBytes),
			Node: p.KernelDeviceNode,
		}
	}

	diskSz, err := disk.SizeInBytes()
	if err != nil {
		return nil, err
	}

	sectorSz, err := disk.SectorSize()
	if err != nil {
		return nil, err
	}

	sectorsEnd, err := disk.UsableSectorsEnd()
	if err != nil {
		return nil, err
	}

	dl := &OnDiskVolume{
		Structure:        ds,
		ID:               disk.DiskID(),
		Device:           disk.KernelDeviceNode(),
		Schema:           disk.Schema(),
		Size:             quantity.Size(diskSz),
		UsableSectorsEnd: sectorsEnd,
		SectorSize:       quantity.Size(sectorSz),
	}

	return dl, nil
}

// UpdatePartitionList re-reads the partitioning data from the device and
// updates the volume structures in the specified volume.
func UpdatePartitionList(dl *OnDiskVolume) error {
	layout, err := OnDiskVolumeFromDevice(dl.Device)
	if err != nil {
		return fmt.Errorf("cannot read disk layout: %v", err)
	}
	if dl.ID != layout.ID {
		return fmt.Errorf("partition table IDs don't match")
	}

	dl.Structure = layout.Structure
	return nil
}
