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

package partition

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/snapcore/snapd/osutil"
)

var (
	diskFromMountPoint = diskFromMountPointImpl
)

// lsblkFilesystemInfo represents the lsblk --fs JSON output format.
type lsblkFilesystemInfo struct {
	BlockDevices []lsblkBlockDevice `json:"blockdevices"`
}

type lsblkBlockDevice struct {
	Name          string             `json:"name"`
	FSType        string             `json:"fstype"`
	Label         string             `json:"label"`
	UUID          string             `json:"uuid"`
	Mountpoint    string             `json:"mountpoint"`
	PartitionUUID string             `json:"partuuid"`
	Children      []lsblkBlockDevice `json:"children"`
	MajorMinor    string             `json:"maj:min"`
}

func lsblckFsInfo(opts ...string) (*lsblkFilesystemInfo, error) {
	args := append(
		[]string{
			"--json",
			// same options as --fs, but also with partuuid
			"-o", "MAJ:MIN,NAME,FSTYPE,LABEL,UUID,MOUNTPOINT,PARTUUID",
		},
		opts...,
	)
	output, err := exec.Command("lsblk", args...).CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	var info lsblkFilesystemInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("cannot parse lsblk output: %v", err)
	}

	return &info, nil
}

func filesystemInfo(node string) (*lsblkFilesystemInfo, error) {
	return lsblckFsInfo(node)
}

func filesystemDeviceNumberInfo(majorNum string) (*lsblkFilesystemInfo, error) {
	return lsblckFsInfo("--include", majorNum)
}

// Disk is a single physical disk device that contains partitions.
type Disk interface {
	// FindMatchingPartitionUUID finds the partition uuid for a partition matching
	// the specified label on the disk.
	FindMatchingPartitionUUID(string) (string, error)

	// Equals compares two disks to see if they are the same physical disk.
	Equals(Disk) bool
}

type disk struct {
	dev lsblkBlockDevice
}

// DiskFromMountPoint finds a matching Disk for the specified mount point.
func DiskFromMountPoint(mountpoint string) (Disk, error) {
	return diskFromMountPoint(mountpoint)
}

func diskFromMountPointImpl(mountpoint string) (Disk, error) {
	// first get the mount entry for the mountpoint
	mounts, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, err
	}
	found := false
	var devMajor int
	for _, mount := range mounts {
		if mount.MountDir == mountpoint {
			devMajor = mount.DevMajor
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("couldn't find mountpoint %q", mountpoint)
	}

	info, err := filesystemDeviceNumberInfo(strconv.Itoa(devMajor))
	if err != nil {
		return nil, err
	}

	switch {
	case len(info.BlockDevices) == 0:
		// unknown device number to lsblk
		return nil, fmt.Errorf("lsblk couldn't find device with major number %d", devMajor)
	case len(info.BlockDevices) > 1:
		// unclear how this could happen? one mount point from multiple devices?
		return nil, fmt.Errorf("internal error: multiple block devices for single mountpoint")
	}

	return &disk{dev: info.BlockDevices[0]}, nil
}

func (d *disk) FindMatchingPartitionUUID(label string) (string, error) {
	// iterate over the block device children, looking for the specified label
	for _, dev := range d.dev.Children {
		if dev.Label == label {
			return dev.PartitionUUID, nil
		}
	}
	return "", fmt.Errorf("couldn't find label %q", label)
}

func (d *disk) Equals(other Disk) bool {
	switch d2 := other.(type) {
	case *disk:
		// check that the device major + minor numbers are the same for the
		// block device itself - not the children
		return d.dev.MajorMinor == d2.dev.MajorMinor
	default:
		return false
	}
}
