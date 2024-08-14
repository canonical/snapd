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

package disks

import (
	"fmt"
	"strings"
)

// Options is a set of options used when querying information about
// partition and disk devices.
type Options struct {
	// IsDecryptedDevice indicates that the mountpoint is referring to a
	// decrypted device.
	IsDecryptedDevice bool
}

// Disk is a single physical disk device that contains partitions.
type Disk interface {
	// FindMatchingPartitionWithFsLabel finds the partition with a matching
	// filesystem label on the disk. Note that for non-ascii labels like
	// "Some label", the label will be encoded using \x<hex> for potentially
	// non-safe characters like in "Some\x20Label". If the filesystem label was
	// not found on the disk, and no other errors were encountered, a
	// PartitionNotFoundError will be returned.
	FindMatchingPartitionWithFsLabel(string) (Partition, error)

	// FindMatchingPartitionWithPartLabel is like
	// FindMatchingPartitionWithFsLabel, but searches for a partition that
	// has a matching partition label instead of the filesystem label. The same
	// encoding scheme is performed on the label as in that function.
	FindMatchingPartitionWithPartLabel(string) (Partition, error)

	// FindMatchingPartitionUUIDWithFsLabel is like
	// FindMatchingPartitionWithFsLabel, but returns specifically the
	// PartitionUUID. This method will be eliminated soon in favor of all
	// clients using FindMatchingPartitionWithFsLabel instead as it is more
	// generically useful.
	FindMatchingPartitionUUIDWithFsLabel(string) (string, error)

	// FindMatchingPartitionUUIDWithPartLabel is like
	// FindMatchingPartitionWithPartLabel, but returns specifically the
	// PartitionUUID. This method will be eliminated soon in favor of all
	// clients using FindMatchingPartitionWithPartLabel instead as it is more
	// generically useful.
	FindMatchingPartitionUUIDWithPartLabel(string) (string, error)

	// MountPointIsFromDisk returns whether the specified mountpoint corresponds
	// to a partition on the disk. Note that this only considers partitions
	// and mountpoints found when the disk was identified with
	// DiskFromMountPoint.
	// TODO: make this function return what a Disk of where the mount point
	//       is actually from if it is not from the same disk for better
	//       error reporting
	MountPointIsFromDisk(string, *Options) (bool, error)

	// Dev returns the string "major:minor" number for the disk device.
	Dev() string

	// HasPartitions returns whether the disk has partitions or not. A physical
	// disk will have partitions, but a mapper device will just be a volume that
	// does not have partitions for example.
	HasPartitions() bool

	// DiskID returns the partition table ID, which is either a hexadecimal
	// number for DOS disks, or a UUID for GPT disks.
	DiskID() string

	// Partitions returns all partitions found on a physical disk device. Note
	// that this method, and all others that require discovering partitions on
	// the disk, caches the partitions once first found and does not re-discover
	// partitions again later on if the disk is re-partitioned.
	Partitions() ([]Partition, error)

	// KernelDeviceNode returns the full device node path in /dev/ for the disk
	// such as /dev/mmcblk0 or /dev/vda.
	KernelDeviceNode() string

	// KernelDevicePath returns the full device path in /sys/devices for the
	// disk such as /sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/.
	KernelDevicePath() string

	// Schema returns the schema for the disk, either DOS or GPT in lowercase.
	Schema() string

	// SectorSize returns the sector size for the disk in bytes, usually 512,
	// sometimes 4096, possibly even 8196 some day.
	// TODO: make this return a quantity.Size when that is doable without
	// importing gadget
	SectorSize() (uint64, error)

	// SizeInBytes returns the overall size of the disk in bytes. Not all of the
	// bytes may be usable for partitions, as some space on disks is reserved
	// for metadata such as the MBR on DOS disks or the GPT headers (and backup)
	// on GPT disks.
	// TODO: make this return a quantity.Size when that is doable without
	// importing gadget
	SizeInBytes() (uint64, error)

	// UsableSectorsEnd returns the exclusive end of usable sectors on the disk
	// where partitions may occupy and be created. Specifically, the end is not
	// itself usable, it is the region immediately after usable space; this sort
	// of measurement is used when partitioning disks to indicate where a given
	// partition ends.
	// The sector unit is the in the native size for the disk, either 512 or
	// 4096 bytes typically.
	// This measurement is distinct from the size of the disk, though for some
	// disks, this measurement may be the size of the disk in sectors - this is
	// the case for DOS disks, but not for GPT disks. GPT disks have a backup
	// header section at the end of the disk that is not usable for partitions.
	UsableSectorsEnd() (uint64, error)
}

// Partition represents a partition on a Disk device.
type Partition struct {
	// FilesystemLabel is the encoded filesystem label, this should only be
	// compared with normal Go strings that are encoded with BlkIDEncodeLabel.
	FilesystemLabel string
	// FilesystemUUID is the encoded filesystem UUID, this should be compared
	// with normal Go strings that are encoded with BlkIDEncodeLabel.
	FilesystemUUID string
	// PartitionLabel is the encoded partition label, this should only be
	// compared with normal Go strings that are encoded with BlkIDEncodeLabel.
	PartitionLabel string
	// the partition UUID
	PartitionUUID string

	// TODO: Major and Minor should be uints, they are required to be uints by
	// the kernel, so it makes sense to match that here

	// Major is the major number for this partition.
	Major int
	// Minor is the minor number for this partition.
	Minor int
	// KernelDevicePath is the kernel device path for this device in /sys for
	// this partition.
	KernelDevicePath string
	// KernelDeviceNode is the kernel device node in /dev.
	KernelDeviceNode string
	// PartitionType is the type of structure, for example 0C in the case of a
	// vfat partition on a DOS disk, or 0FC63DAF-8483-4772-8E79-3D69D8477DE4,
	// which is ext4 on a GPT disk. This is always upper case.
	PartitionType string
	// FilesystemType is the type of filesystem i.e. ext4 or vfat, etc.
	FilesystemType string
	// DiskIndex is the index of the structure on the disk, where the first
	// partition/structure has index of 1.
	DiskIndex uint64
	// StartInBytes is the beginning of the partition/structure in bytes.
	StartInBytes uint64
	// SizeInBytes is the overall size of the partition/structure in bytes.
	SizeInBytes uint64

	// TODO: also include a Disk field for finding what Disk this partition came
	// from?
}

func (p *Partition) hasFilesystemLabel(encodedLabel string) bool {
	if p.FilesystemType == "vfat" {
		return strings.EqualFold(p.FilesystemLabel, encodedLabel)
	}
	return p.FilesystemLabel == encodedLabel
}

// MountPointsForPartitionRoot returns all mounts from the mount table which are
// for the root directory of the specified partition and have matching mount
// options as specified. Options not specified in the map argument can have any
// value or be set or unset, but any option set in the map must match exactly.
// The order in which they are returned is the same order that they appear in
// the mount table.
func MountPointsForPartitionRoot(p Partition, matchingMountOptions map[string]string) ([]string, error) {
	return mountPointsForPartitionRoot(p, matchingMountOptions)
}

// PartitionNotFoundError is an error where a partition matching the SearchType
// was not found. SearchType can be either "partition-label" or
// "filesystem-label" to indicate searching by the partition label or the
// filesystem label on a given disk. SearchQuery is the specific query
// parameter attempted to be used.
type PartitionNotFoundError struct {
	SearchType  string
	SearchQuery string
}

func (e PartitionNotFoundError) Error() string {
	t := ""
	switch e.SearchType {
	case "partition-label":
		t = "partition label"
	case "filesystem-label":
		t = "filesystem label"
	default:
		return fmt.Sprintf("searching with unknown search type %q and search query %q did not return a partition", e.SearchType, e.SearchQuery)
	}
	return fmt.Sprintf("%s %q not found", t, e.SearchQuery)
}

var (
	_ = error(PartitionNotFoundError{})
)
