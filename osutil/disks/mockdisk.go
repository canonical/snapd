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

	"github.com/snapcore/snapd/osutil"
)

// MockDiskMapping is an implementation of Disk for mocking purposes, it is
// exported so that other packages can easily mock a specific disk layout
// without needing to mock the mount setup, sysfs, or udev commands just to test
// high level logic.
// DevNum must be a unique string per unique mocked disk, if only one disk is
// being mocked it can be left empty.
type MockDiskMapping struct {
	// FilesystemLabelToPartUUID is a mapping of the udev encoded filesystem
	// labels to the expected partition uuids.
	FilesystemLabelToPartUUID map[string]string
	// PartitionLabelToPartUUID is a mapping of the udev encoded partition
	// labels to the expected partition uuids.
	PartitionLabelToPartUUID map[string]string
	DiskHasPartitions        bool
	DevNum                   string
}

// FindMatchingPartitionUUIDWithFsLabel returns a matching PartitionUUID
// for the specified filesystem label if it exists. Part of the Disk interface.
func (d *MockDiskMapping) FindMatchingPartitionUUIDWithFsLabel(label string) (string, error) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")
	if partuuid, ok := d.FilesystemLabelToPartUUID[label]; ok {
		return partuuid, nil
	}
	return "", PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: label,
	}
}

// FindMatchingPartitionUUIDWithPartLabel returns a matching PartitionUUID
// for the specified filesystem label if it exists. Part of the Disk interface.
func (d *MockDiskMapping) FindMatchingPartitionUUIDWithPartLabel(label string) (string, error) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")
	if partuuid, ok := d.PartitionLabelToPartUUID[label]; ok {
		return partuuid, nil
	}
	return "", PartitionNotFoundError{
		SearchType:  "partition-label",
		SearchQuery: label,
	}
}

// HasPartitions returns if the mock disk has partitions or not. Part of the
// Disk interface.
func (d *MockDiskMapping) HasPartitions() bool {
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

// Dev returns a unique representation of the mock disk that is suitable for
// comparing two mock disks to see if they are the same. Part of the Disk
// interface.
func (d *MockDiskMapping) Dev() string {
	return d.DevNum
}

// Mountpoint is a combination of a mountpoint location and whether that
// mountpoint is a decrypted device. It is only used in identifying mount points
// with MountPointIsFromDisk and DiskFromMountPoint with
// MockMountPointDisksToPartitionMapping.
type Mountpoint struct {
	Mountpoint        string
	IsDecryptedDevice bool
}

// MockDeviceNameDisksToPartitionMapping will mock DiskFromDeviceName such that
// the provided map of device names to mock disks is used instead of the actual
// implementation using udev.
func MockDeviceNameDisksToPartitionMapping(mockedMountPoints map[string]*MockDiskMapping) (restore func()) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	// note that devices can have many names that are recognized by
	// udev/kernel, so we don't do any validation of the mapping here like we do
	// for MockMountPointDisksToPartitionMapping

	old := diskFromDeviceName
	diskFromDeviceName = func(deviceName string) (Disk, error) {
		disk, ok := mockedMountPoints[deviceName]
		if !ok {
			return nil, fmt.Errorf("device name %q not mocked", deviceName)
		}
		return disk, nil
	}

	return func() {
		diskFromDeviceName = old
	}
}

// MockMountPointDisksToPartitionMapping will mock DiskFromMountPoint such that
// the specified mapping is returned/used. Specifically, keys in the provided
// map are mountpoints, and the values for those keys are the disks that will
// be returned from DiskFromMountPoint or used internally in
// MountPointIsFromDisk.
func MockMountPointDisksToPartitionMapping(mockedMountPoints map[Mountpoint]*MockDiskMapping) (restore func()) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	// verify that all unique MockDiskMapping's have unique DevNum's and that
	// the srcMntPt's are all consistent
	// we can't have the same mountpoint exist both as a decrypted device and
	// not as a decrypted device, this is an impossible mapping, but we need to
	// expose functionality to mock the same mountpoint as a decrypted device
	// and as an unencrypyted device for different tests, but never at the same
	// time with the same mapping
	alreadySeen := make(map[string]*MockDiskMapping, len(mockedMountPoints))
	seenSrcMntPts := make(map[string]bool, len(mockedMountPoints))
	for srcMntPt, mockDisk := range mockedMountPoints {
		if decryptedVal, ok := seenSrcMntPts[srcMntPt.Mountpoint]; ok {
			if decryptedVal != srcMntPt.IsDecryptedDevice {
				msg := fmt.Sprintf("mocked source mountpoint %s is duplicated with different options - previous option for IsDecryptedDevice was %t, current option is %t", srcMntPt.Mountpoint, decryptedVal, srcMntPt.IsDecryptedDevice)
				panic(msg)
			}
		}
		seenSrcMntPts[srcMntPt.Mountpoint] = srcMntPt.IsDecryptedDevice
		if old, ok := alreadySeen[mockDisk.DevNum]; ok {
			if mockDisk != old {
				// we already saw a disk with this DevNum as a different pointer
				// so just assume it's different
				msg := fmt.Sprintf("mocked disks %+v and %+v have the same DevNum (%s) but are not the same object", old, mockDisk, mockDisk.DevNum)
				panic(msg)
			}
			// otherwise same ptr, no point in comparing them
		} else {
			// didn't see it before, save it now
			alreadySeen[mockDisk.DevNum] = mockDisk
		}
	}

	old := diskFromMountPoint

	diskFromMountPoint = func(mountpoint string, opts *Options) (Disk, error) {
		if opts == nil {
			opts = &Options{}
		}
		m := Mountpoint{mountpoint, opts.IsDecryptedDevice}
		if mockedDisk, ok := mockedMountPoints[m]; ok {
			return mockedDisk, nil
		}
		return nil, fmt.Errorf("mountpoint %s not mocked", mountpoint)
	}
	return func() {
		diskFromMountPoint = old
	}
}
