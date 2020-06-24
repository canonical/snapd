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

	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/osutil"
)

// MockDiskMapping is an implementation of Disk for mocking purposes, it is
// exported so that other packages can easily mock a specific disk layout
// without needing to mock the mount setup, sysfs, or udev commands just to test
// high level logic.
// DevNum must be a unique string per unique mocked disk, if only one disk is
// being mocked it can be left empty.
type MockDiskMapping struct {
	FilesystemLabelToPartUUID map[string]string
	DiskHasPartitions         bool
	DevNum                    string
}

// FindMatchingPartitionUUID returns a matching PartitionUUID for the specified
// label if it exists. Part of the Disk interface.
func (d *MockDiskMapping) FindMatchingPartitionUUID(label string) (string, error) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")
	if partuuid, ok := d.FilesystemLabelToPartUUID[label]; ok {
		return partuuid, nil
	}
	fmt := "could not find label %q: %w"
	return "", xerrors.Errorf(fmt, label, ErrFilesystemLabelNotFound)
}

// HasPartitions returns if the mock disk has partitions or not. Part of the
// Disk interface.
func (d *MockDiskMapping) HasPartitions() bool {
	osutil.MustBeTestBinary("mock disks only to be used in tests")
	return d.DiskHasPartitions
}

// MountPointIsFromDisk returns if the disk that the specified mount point comes
// from is the same disk as the object. Part of the Disk interface.
func (d *MockDiskMapping) MountPointIsFromDisk(mountpoint string, opts *Options) (bool, error) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	// this is relying on the fact that DiskFromMountPoint should have been
	// mocked for us to be using this mockDisk method anyways
	otherDisk, err := DiskFromMountPoint(mountpoint, opts)
	if err != nil {
		return false, err
	}

	if otherDisk.Dev() == d.Dev() && otherDisk.HasPartitions() == d.HasPartitions() {
		return true, nil
	}

	return false, nil
}

// Dev returns a unique representation of the mock disk, it is a hash of the
// mock disk struct string representation. Part of the Disk interface.
func (d *MockDiskMapping) Dev() string {
	osutil.MustBeTestBinary("mock disks only to be used in tests")
	return d.DevNum
}

// Mountpoint is a combination of a mountpoint location and whether that
// mountpoint is a decrypted device. It is only used in identifying mount points
// with MountPointIsFromDisk and DiskFromMountPoint with
// MockMountPointDisksToPartionMapping.
type Mountpoint struct {
	Mountpoint        string
	IsDecryptedDevice bool
}

// MockMountPointDisksToPartionMapping will mock DiskFromMountPoint such that
// the specified mapping is returned/used. Specifically, keys in the provided
// map are mountpoints, and the values for those keys are the disks that will
// be returned from DiskFromMountPoint or used internally in
// MountPointIsFromDisk.
func MockMountPointDisksToPartionMapping(mockedMountPoints map[Mountpoint]MockDiskMapping) (restore func()) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	old := diskFromMountPoint

	diskFromMountPoint = func(mountpoint string, opts *Options) (Disk, error) {
		if opts == nil {
			opts = &Options{}
		}
		m := Mountpoint{mountpoint, opts.IsDecryptedDevice}
		if mockedDisk, ok := mockedMountPoints[m]; ok {
			return &mockedDisk, nil
		}
		return nil, fmt.Errorf("mountpoint %s not mocked", mountpoint)
	}
	return func() {
		diskFromMountPoint = old
	}
}
