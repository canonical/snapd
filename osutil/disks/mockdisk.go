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

var _ = Disk(&MockDiskMapping{})

// MockDiskMapping is an implementation of Disk for mocking purposes, it is
// exported so that other packages can easily mock a specific disk layout
// without needing to mock the mount setup, sysfs, or udev commands just to test
// high level logic.
// DevNum must be a unique string per unique mocked disk, if only one disk is
// being mocked it can be left empty.
type MockDiskMapping struct {
	// TODO: should this be automatically determined if Structure has non-zero
	// len instead?
	DiskHasPartitions bool

	// Structure is the set of partitions or structures on the disk. These
	// partitions are used with Partitions() as well as
	// FindMatchingPartitionWith{Fs,Part}Label
	Structure []Partition

	// static variables for the disk that must be unique for different disks,
	// but note that there are potentially multiple DevNode values that could
	// map to a single disk, but it's not worth encoding that complexity here
	// by making DevNodes a list
	DevNum  string
	DevNode string
	DevPath string

	ID                  string
	DiskSchema          string
	SectorSizeBytes     uint64
	DiskUsableSectorEnd uint64
	DiskSizeInBytes     uint64
}

// FindMatchingPartitionUUIDWithFsLabel returns a matching PartitionUUID
// for the specified filesystem label if it exists. Part of the Disk interface.
func (d *MockDiskMapping) FindMatchingPartitionWithFsLabel(label string) (Partition, error) {
	// TODO: this should just iterate over the static list when that is a thing
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	for _, p := range d.Structure {
		if p.hasFilesystemLabel(label) {
			return p, nil
		}
	}

	return Partition{}, PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: label,
	}
}

// FindMatchingPartitionUUIDWithPartLabel returns a matching PartitionUUID
// for the specified filesystem label if it exists. Part of the Disk interface.
func (d *MockDiskMapping) FindMatchingPartitionWithPartLabel(label string) (Partition, error) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	for _, p := range d.Structure {
		if p.PartitionLabel == label {
			return p, nil
		}
	}

	return Partition{}, PartitionNotFoundError{
		SearchType:  "partition-label",
		SearchQuery: label,
	}
}

func (d *MockDiskMapping) FindMatchingPartitionUUIDWithFsLabel(label string) (string, error) {
	p, err := d.FindMatchingPartitionWithFsLabel(label)
	if err != nil {
		return "", err
	}
	return p.PartitionUUID, nil
}

func (d *MockDiskMapping) FindMatchingPartitionUUIDWithPartLabel(label string) (string, error) {
	p, err := d.FindMatchingPartitionWithPartLabel(label)
	if err != nil {
		return "", err
	}
	return p.PartitionUUID, nil
}

func (d *MockDiskMapping) Partitions() ([]Partition, error) {
	return d.Structure, nil
}

// HasPartitions returns if the mock disk has partitions or not. Part of the
// Disk interface.
func (d *MockDiskMapping) HasPartitions() bool {
	return d.DiskHasPartitions
}

// Dev returns a unique representation of the mock disk that is suitable for
// comparing two mock disks to see if they are the same. Part of the Disk
// interface.
func (d *MockDiskMapping) Dev() string {
	return d.DevNum
}

func (d *MockDiskMapping) KernelDeviceNode() string {
	return d.DevNode
}

func (d *MockDiskMapping) KernelDevicePath() string {
	return d.DevPath
}

func (d *MockDiskMapping) DiskID() string {
	return d.ID
}

func (d *MockDiskMapping) Schema() string {
	return d.DiskSchema
}

func (d *MockDiskMapping) SectorSize() (uint64, error) {
	return d.SectorSizeBytes, nil
}

func (d *MockDiskMapping) UsableSectorsEnd() (uint64, error) {
	return d.DiskUsableSectorEnd, nil
}

func (d *MockDiskMapping) SizeInBytes() (uint64, error) {
	return d.DiskSizeInBytes, nil
}

// Mountpoint is a combination of a mountpoint location and whether that
// mountpoint is a decrypted device. It is only used in identifying mount points
// with DiskFromMountPoint with
// MockMountPointDisksToPartitionMapping.
type Mountpoint struct {
	Mountpoint        string
	IsDecryptedDevice bool
}

func checkMockDiskMappingsForDuplicates(mockedDisks map[string]*MockDiskMapping) {
	// we do the minimal amount of validation here, where if things are
	// specified as non-zero value we check that they make sense, but we don't
	// require that every field is set for every partition since many tests
	// don't care about every field

	// check partition uuid's and partition labels for duplication inter-disk
	// we could have valid cloned disks where the same partition uuid/label
	// appears on two disks, but never on the same disk
	for _, disk := range mockedDisks {
		seenPartUUID := make(map[string]bool, len(disk.Structure))
		seenPartLabel := make(map[string]bool, len(disk.Structure))
		for _, p := range disk.Structure {
			if p.PartitionUUID != "" {
				if seenPartUUID[p.PartitionUUID] {
					panic("mock error: disk has duplicated partition uuids in its structure")
				}
				seenPartUUID[p.PartitionUUID] = true
			}

			if p.PartitionLabel != "" {
				if seenPartLabel[p.PartitionLabel] {
					panic("mock error: disk has duplicated partition labels in its structure")
				}
				seenPartLabel[p.PartitionLabel] = true
			}
		}
	}

	// check major/minors across each disk
	for _, disk := range mockedDisks {
		type majmin struct{ maj, min int }
		seenMajorMinors := map[majmin]bool{}
		for _, p := range disk.Structure {
			if p.Major == 0 && p.Minor == 0 {
				continue
			}

			m := majmin{maj: p.Major, min: p.Minor}
			if seenMajorMinors[m] {
				panic("mock error: duplicated major minor numbers for partitions in disk mapping")
			}
			seenMajorMinors[m] = true
		}
	}

	// check DiskIndex across each disk
	for _, disk := range mockedDisks {
		seenIndices := map[uint64]bool{}
		for _, p := range disk.Structure {
			if p.DiskIndex == 0 {
				continue
			}

			if seenIndices[p.DiskIndex] {
				panic("mock error: duplicated structure indices for partitions in disk mapping")
			}
			seenIndices[p.DiskIndex] = true
		}
	}

	// check device paths across each disk
	for _, disk := range mockedDisks {
		seenDevPaths := map[string]bool{}
		for _, p := range disk.Structure {
			if p.KernelDevicePath == "" {
				continue
			}
			if seenDevPaths[p.KernelDevicePath] {
				panic("mock error: duplicated kernel device paths for partitions in disk mapping")
			}
			seenDevPaths[p.KernelDevicePath] = true
		}
	}

	// check device nodes across each disk
	for _, disk := range mockedDisks {
		sendDevNodes := map[string]bool{}
		for _, p := range disk.Structure {
			if p.KernelDeviceNode == "" {
				continue
			}

			if sendDevNodes[p.KernelDeviceNode] {
				panic("mock error: duplicated kernel device nodes for partitions in disk mapping")
			}
			sendDevNodes[p.KernelDeviceNode] = true
		}
	}

	// no checking of filesystem label/uuid since those could be duplicated as
	// they exist independent of any other structure
}

// MockPartitionDeviceNodeToDiskMapping will mock DiskFromPartitionDeviceNode
// such that the provided map of device names to mock disks is used instead of
// the actual implementation using udev.
func MockPartitionDeviceNodeToDiskMapping(mockedDisks map[string]*MockDiskMapping) (restore func()) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	checkMockDiskMappingsForDuplicates(mockedDisks)

	// note that there can be multiple partitions that map to the same disk, so
	// we don't really validate the keys of the provided mapping

	old := diskFromPartitionDeviceNode
	diskFromPartitionDeviceNode = func(node string) (Disk, error) {
		disk, ok := mockedDisks[node]
		if !ok {
			return nil, fmt.Errorf("partition device node %q not mocked", node)
		}
		return disk, nil
	}
	return func() {
		diskFromPartitionDeviceNode = old
	}
}

// MockDeviceNameToDiskMapping will mock DiskFromDeviceName such that the
// provided map of device names to mock disks is used instead of the actual
// implementation using udev.
func MockDeviceNameToDiskMapping(mockedDisks map[string]*MockDiskMapping) (restore func()) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	checkMockDiskMappingsForDuplicates(mockedDisks)

	// note that devices can have multiple names that are recognized by
	// udev/kernel, so we don't do any validation of the mapping here like we do
	// for MockMountPointDisksToPartitionMapping

	old := diskFromDeviceName
	diskFromDeviceName = func(deviceName string) (Disk, error) {
		disk, ok := mockedDisks[deviceName]
		if !ok {
			return nil, fmt.Errorf("device name %q not mocked", deviceName)
		}
		return disk, nil
	}

	return func() {
		diskFromDeviceName = old
	}
}

// MockDevicePathToDiskMapping will mock DiskFromDevicePath such that the
// provided map of device names to mock disks is used instead of the actual
// implementation using udev.
func MockDevicePathToDiskMapping(mockedDisks map[string]*MockDiskMapping) (restore func()) {
	osutil.MustBeTestBinary("mock disks only to be used in tests")

	checkMockDiskMappingsForDuplicates(mockedDisks)

	// note that devices can have multiple paths that are recognized by
	// udev/kernel, so we don't do any validation of the mapping here like we do
	// for MockMountPointDisksToPartitionMapping

	old := diskFromDevicePath
	diskFromDevicePath = func(devicePath string) (Disk, error) {
		disk, ok := mockedDisks[devicePath]
		if !ok {
			return nil, fmt.Errorf("device path %q not mocked", devicePath)
		}
		return disk, nil
	}

	return func() {
		diskFromDevicePath = old
	}
}

// MockMountPointDisksToPartitionMapping will mock DiskFromMountPoint such that
// the specified mapping is returned/used. Specifically, keys in the provided
// map are mountpoints, and the values for those keys are the disks that will
// be returned from DiskFromMountPoint.
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
		return nil, fmt.Errorf("mountpoint %+v not mocked", m)
	}
	return func() {
		diskFromMountPoint = old
	}
}
