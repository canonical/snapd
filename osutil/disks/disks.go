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

import "fmt"

// Options is a set of options used when querying information about
// partition and disk devices.
type Options struct {
	// IsDecryptedDevice indicates that the mountpoint is referring to a
	// decrypted device.
	IsDecryptedDevice bool
}

// Disk is a single physical disk device that contains partitions.
type Disk interface {
	// FindMatchingPartitionUUIDWithFsLabel finds the partition uuid for a
	// partition matching the specified filesystem label on the disk. Note that
	// for non-ascii labels like "Some label", the label will be encoded using
	// \x<hex> for potentially non-safe characters like in "Some\x20Label".
	// If the filesystem label was not found on the disk, and no other errors
	// were encountered, a PartitionNotFoundError will be returned.
	FindMatchingPartitionUUIDWithFsLabel(string) (string, error)

	// FindMatchingPartitionUUIDWithPartLabel is like
	// FindMatchingPartitionUUIDWithFsLabel, but searches for a partition that
	// has a matching partition label instead of the filesystem label. The same
	// encoding scheme is performed on the label as in that function.
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

	// TODO: add function to get some properties like an associated /dev node
	//       for a disk for better user error reporting, i.e. /dev/vda3 is much
	//       more helpful than 252:3
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
