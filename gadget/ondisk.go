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
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
)

// TODO: consider looking into merging LaidOutVolume/Structure OnDiskVolume/Structure

// OnDiskStructure represents a gadget structure laid on a block device.
type OnDiskStructure struct {
	// Name, when non empty, provides the name of the structure
	Name string
	// PartitionFSLabel provides the filesystem label
	PartitionFSLabel string
	// Type of the structure, which can be 2-hex digit MBR partition,
	// 36-char GUID partition, comma separated <mbr>,<guid> for hybrid
	// partitioning schemes, or 'bare' when the structure is not considered
	// a partition.
	//
	// For backwards compatibility type 'mbr' is also accepted, and the
	// structure is treated as if it is of role 'mbr'.
	Type string
	// PartitionFSType used for the partition filesystem: 'vfat', 'ext4',
	// 'none' for structures of type 'bare', or 'crypto_LUKS' for encrypted
	// partitions.
	PartitionFSType string
	// StartOffset defines the start offset of the structure within the
	// enclosing volume
	StartOffset quantity.Offset

	// Node identifies the device node of the block device.
	Node string

	// DiskIndex is the index of the structure on the disk - this should be
	// used instead of YamlIndex for an OnDiskStructure, YamlIndex comes from
	// the embedded LaidOutStructure which is 0-based and does not have the same
	// meaning. A LaidOutStructure's YamlIndex position will include that of
	// bare structures which will not show up as an OnDiskStructure, so the
	// range of OnDiskStructure.DiskIndex values is not necessarily the same as
	// the range of LaidOutStructure.YamlIndex values.
	DiskIndex int

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

type OnDiskAndGadgetStructurePair struct {
	DiskStructure   *OnDiskStructure
	GadgetStructure *VolumeStructure
}

// OnDiskVolumeFromDevice obtains the partitioning and filesystem information from
// the block device.
func OnDiskVolumeFromDevice(device string) (*OnDiskVolume, error) {
	disk := mylog.Check2(disks.DiskFromDeviceName(device))

	return OnDiskVolumeFromDisk(disk)
}

func OnDiskVolumeFromDisk(disk disks.Disk) (*OnDiskVolume, error) {
	parts := mylog.Check2(disk.Partitions())

	ds := make([]OnDiskStructure, len(parts))

	for _, p := range parts {
		s := mylog.Check2(OnDiskStructureFromPartition(p))

		// Use the index of the structure on the disk rather than the order in
		// which we iterate over the list of partitions, since the order of the
		// partitions is returned "last seen first" which matches the behavior
		// of udev when picking partitions with the same filesystem label and
		// populating /dev/disk/by-label/ and friends.
		// All that is to say the order that the list of partitions from
		// Partitions() is in is _not_ the same as the order that the structures
		// actually appear in on disk, but this is why the DiskIndex
		// property exists. Also note that DiskIndex starts at 1, as
		// opposed to gadget.LaidOutVolume.Structure's Index which starts at 0.
		i := p.DiskIndex - 1
		ds[i] = s
	}

	diskSz := mylog.Check2(disk.SizeInBytes())

	sectorSz := mylog.Check2(disk.SectorSize())

	sectorsEnd := mylog.Check2(disk.UsableSectorsEnd())

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

func OnDiskStructureFromPartition(p disks.Partition) (OnDiskStructure, error) {
	// the PartitionLabel and FilesystemLabel are encoded, so they must be
	// decoded before they can be used in other gadget functions

	decodedPartLabel := mylog.Check2(disks.BlkIDDecodeLabel(p.PartitionLabel))

	decodedFsLabel := mylog.Check2(disks.BlkIDDecodeLabel(p.FilesystemLabel))

	logger.Debugf("OnDiskStructureFromPartition: p.FilesystemType %q, p.FilesystemLabel %q",
		p.FilesystemType, p.FilesystemLabel)

	// TODO add ID in second part of the gadget refactoring?
	return OnDiskStructure{
		Name:             decodedPartLabel,
		PartitionFSLabel: decodedFsLabel,
		Type:             p.PartitionType,
		PartitionFSType:  p.FilesystemType,
		StartOffset:      quantity.Offset(p.StartInBytes),
		DiskIndex:        int(p.DiskIndex),
		Size:             quantity.Size(p.SizeInBytes),
		Node:             p.KernelDeviceNode,
	}, nil
}

// OnDiskVolumeFromGadgetVol returns the disk volume matching a gadget volume
// that has the Device field set, which implies that this should be called only
// in the context of an installer that set the device in the gadget and
// returned it to snapd.
func OnDiskVolumeFromGadgetVol(vol *Volume) (*OnDiskVolume, error) {
	var diskVol *OnDiskVolume
	for _, vs := range vol.Structure {
		if vs.Device == "" || vs.Role == "mbr" || vs.Type == "bare" {
			continue
		}

		partSysfsPath := mylog.Check2(sysfsPathForBlockDevice(vs.Device))

		// Volume needs to be resolved only once
		diskVol = mylog.Check2(onDiskVolumeFromPartitionSysfsPath(partSysfsPath))

		break
	}

	if diskVol == nil {
		return nil, fmt.Errorf("volume %q has no device assigned", vol.Name)
	}

	return diskVol, nil
}

// sysfsPathForBlockDevice returns the sysfs path for a block device.
var sysfsPathForBlockDevice = func(device string) (string, error) {
	syfsLink := filepath.Join("/sys/class/block", filepath.Base(device))
	partPath := mylog.Check2(os.Readlink(syfsLink))

	// Remove initial ../../ from partPath, and make path absolute
	return filepath.Join("/sys/class/block", partPath), nil
}

// onDiskVolumeFromPartitionSysfsPath creates an OnDiskVolume that
// matches the disk that contains the given partition sysfs path
func onDiskVolumeFromPartitionSysfsPath(partPath string) (*OnDiskVolume, error) {
	// Removing the last component will give us the disk path
	diskPath := filepath.Dir(partPath)
	disk := mylog.Check2(disks.DiskFromDevicePath(diskPath))

	onDiskVol := mylog.Check2(OnDiskVolumeFromDisk(disk))

	return onDiskVol, nil
}

func MockSysfsPathForBlockDevice(f func(device string) (string, error)) (restore func()) {
	old := sysfsPathForBlockDevice
	sysfsPathForBlockDevice = f
	return func() {
		sysfsPathForBlockDevice = old
	}
}

// OnDiskStructsFromGadget builds a map of gadget yaml index to OnDiskStructure
// by assuming that the gadget will match exactly one of the disks of the
// installation device. This is used only at disk image build time as we do not
// know yet the target disk.
func OnDiskStructsFromGadget(volume *Volume) (structures map[int]*OnDiskStructure) {
	structures = map[int]*OnDiskStructure{}
	offset := quantity.Offset(0)
	for idx, vs := range volume.Structure {
		// Offset is end of previous struct unless explicit.
		if volume.Structure[idx].Offset != nil {
			offset = *volume.Structure[idx].Offset
		}
		ods := OnDiskStructure{
			Name:        vs.Name,
			Type:        vs.Type,
			StartOffset: offset,
			Size:        vs.Size,
		}

		// Note that structures are ordered by offset as volume.Structure
		// was ordered when reading the gadget information.
		offset += quantity.Offset(volume.Structure[idx].Size)
		structures[vs.YamlIndex] = &ods
	}

	return structures
}
