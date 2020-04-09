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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

var (
	diskFromMountPoint = diskFromMountPointImpl
	mockedMountPoints  = make(map[string]*mockDisk)
	isSnapdTest        = len(os.Args) > 0 && strings.HasSuffix(os.Args[0], ".test")
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

func lsblkFsInfo(opts ...string) (*lsblkFilesystemInfo, error) {
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
	return lsblkFsInfo(node)
}

func filesystemDeviceNumberInfo(majorNum string) (*lsblkFilesystemInfo, error) {
	return lsblkFsInfo("--include", majorNum)
}

// Disk is a single physical disk device that contains partitions.
type Disk interface {
	// FindMatchingPartitionUUID finds the partition uuid for a partition matching
	// the specified label on the disk. Note that for non-ascii labels like
	// "Some label", the label should be encoded using \x<hex> for potentially
	// non-safe characters like in "Some\x20Label".
	FindMatchingPartitionUUID(string) (string, error)

	// Equals compares two disks to see if they are the same physical disk.
	Equals(Disk) bool
}

type partition struct {
	major    int
	minor    int
	label    string
	partuuid string
	path     string
}

type disk struct {
	major      int
	minor      int
	partitions []*partition
}

type lsblkDisk struct {
	dev lsblkBlockDevice
}

// DiskFromMountPoint finds a matching Disk for the specified mount point.
func DiskFromMountPoint(mountpoint string) (Disk, error) {
	return diskFromMountPoint(mountpoint)
}

func diskFromMountPointImplLsblk(mountpoint string) (Disk, error) {
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

	return &lsblkDisk{dev: info.BlockDevices[0]}, nil
}

func (l *lsblkDisk) FindMatchingPartitionUUID(label string) (string, error) {
	// iterate over the block device children, looking for the specified label
	for _, dev := range l.dev.Children {
		if dev.Label == label {
			return dev.PartitionUUID, nil
		}
	}
	return "", fmt.Errorf("couldn't find label %q", label)
}

func (l *lsblkDisk) Equals(other Disk) bool {
	switch d2 := other.(type) {
	case *lsblkDisk:
		// check that the device major + minor numbers are the same for the
		// block device itself - not the children
		return l.dev.MajorMinor == d2.dev.MajorMinor
	default:
		return false
	}
}

func parseDeviceMajorMinor(s string) (int, int, error) {
	errMsg := fmt.Errorf("invalid device number format: (expected <int>:<int>)")
	devNums := strings.SplitN(s, ":", 2)
	if len(devNums) != 2 {
		return 0, 0, errMsg
	}
	maj, err := strconv.Atoi(devNums[0])
	if err != nil {
		return 0, 0, errMsg
	}
	min, err := strconv.Atoi(devNums[1])
	if err != nil {
		return 0, 0, errMsg
	}
	return maj, min, nil
}

func udevProperties(device string) (map[string]string, error) {
	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	cmd := exec.Command("udevadm", "info", "--name", device, "--query", "property")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}
	r := bytes.NewBuffer(out)

	return parseUdevProperties(r)
}

func parseUdevProperties(r io.Reader) (map[string]string, error) {
	m := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		strs := strings.SplitN(scanner.Text(), "=", 2)
		if len(strs) != 2 {
			// bad udev output?
			continue
		}
		m[strs[0]] = strs[1]
	}

	return m, scanner.Err()
}

func diskFromMountPointImpl(mountpoint string) (Disk, error) {
	// first get the mount entry for the mountpoint
	mounts, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, err
	}
	found := false
	d := &disk{}
	mountpointPart := partition{}
	for _, mount := range mounts {
		if mount.MountDir == mountpoint {
			mountpointPart.major = mount.DevMajor
			mountpointPart.minor = mount.DevMinor
			mountpointPart.path = mount.MountSource
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("couldn't find mountpoint %q", mountpoint)
	}

	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	props, err := udevProperties(mountpointPart.path)
	if err != nil && props == nil {
		// only fail here if props is nil, if it's available we validate it
		// below
		return nil, fmt.Errorf("cannot find disk for partition %s: %v", mountpointPart.path, err)
	}

	// ID_PART_ENTRY_DISK will give us the major and minor of the disk that this
	// partition originated from
	if majorMinor, ok := props["ID_PART_ENTRY_DISK"]; ok {
		maj, min, err := parseDeviceMajorMinor(majorMinor)
		if err != nil {
			// bad udev output?
			return nil, fmt.Errorf("cannot find disk for partition %s, bad udev output: %v", mountpointPart.path, err)
		}
		d.major = maj
		d.minor = min
	} else {
		// didn't find the property we need
		return nil, fmt.Errorf("cannot find disk for partition %s, incomplete udev output", mountpointPart.path)
	}

	// now we have the major and minor of the disk, so we have the arduous task
	// to identify all partitions that come from this disk using sysfs
	// step 1. find all devices with a matching major number
	// step 2. start at the major + minor device for the disk, and iterate over
	//         all devices that have a partition attribute, starting with the
	//         device with major same as disk and minor equal to disk minor + 1
	// step 3. if we hit a device that does not have a partition attribute, then
	//         we hit another disk, and shall stop searching

	pattern := fmt.Sprintf("/sys/dev/block/%d:*", d.major)
	allDevices, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("internal error: %v", err)
	}

	// glob does not sort, but we need the list of devices to be sorted
	sort.Strings(allDevices)

	// populate all the partitions from our candidate devices
	for _, dev := range allDevices {
		base := filepath.Base(dev)
		maj, min, err := parseDeviceMajorMinor(base)
		if err != nil {
			continue
		}

		// ignore any devices that have minor numbers less than the disk itself
		// the disk will have the same minor so ignore that too
		if min <= d.minor {
			continue
		}

		// now if there is a partition file, this is a partition for our disk
		if osutil.FileExists(filepath.Join(dev, "partition")) {
			// read the uevent file for this partition to get the devname
			p := &partition{
				major: maj,
				minor: min,
			}
			f, err := os.Open(filepath.Join(dev, "uevent"))
			if err != nil {
				continue
			}

			// now get the full set of udev properties for this partition
			eventProps, err := parseUdevProperties(f)
			if err != nil {
				continue
			}

			// get the name of the device path to call udevadm
			if name, ok := eventProps["DEVNAME"]; ok {
				p.path = filepath.Join("/dev", name)
			} else {
				continue
			}

			props, err := udevProperties(p.path)
			if err != nil && props == nil {
				// only error here if we didn't get a map, we validate the map
				// in the next steps
				continue
			}

			// get the label
			if labelEncoded, ok := props["ID_FS_LABEL_ENC"]; ok {
				p.label = labelEncoded
			} else {
				continue
			}

			// finally get the partition uuid
			if partuuid, ok := props["ID_PART_ENTRY_UUID"]; ok {
				p.partuuid = partuuid
			} else {
				continue
			}

			d.partitions = append(d.partitions, p)
		} else {
			// if there was not a partition file, we hit another disk and must
			// stop searching (the disk we are looking at will be ignored with
			// the minor number <= check above)
			break
		}
	}

	// if we didn't find any partitions from above then return an error
	if len(d.partitions) == 0 {
		return nil, fmt.Errorf("no partitions found for disk %d:%d", d.major, d.minor)
	}

	return d, nil
}

func (d *disk) FindMatchingPartitionUUID(label string) (string, error) {
	// iterate over the partitions looking for the specified label
	for _, part := range d.partitions {
		if part.label == label {
			return part.partuuid, nil
		}
	}

	return "", fmt.Errorf("couldn't find label %q", label)
}

func (d *disk) Equals(other Disk) bool {
	switch d2 := other.(type) {
	case *disk:
		// check that the device major + minor numbers are the same
		return d.major == d2.major && d.minor == d2.minor
	default:
		return false
	}
}

// mockDisk is an implementation of Disk for mocking purposes, it is exported
// so that other packages can easily mock a specific disk layout without
// needing to mock the mount setup, sysfs, and udevadm commands just to test
// high level logic.
type mockDisk struct {
	mountpoint       string
	labelsToPartUUID map[string]string
}

func (d *mockDisk) FindMatchingPartitionUUID(label string) (string, error) {
	return d.labelsToPartUUID[label], nil
}

func (d *mockDisk) Equals(other Disk) bool {
	switch d2 := other.(type) {
	case *mockDisk:
		// compare that the map of partitions between the two Disk's matches
		// as that's the only unique info that would be included in mocked tests
		for k, v := range d.labelsToPartUUID {
			if v2, ok := d2.labelsToPartUUID[k]; !ok || v2 != v {
				return false
			}
		}
		for k, v := range d2.labelsToPartUUID {
			if v1, ok := d.labelsToPartUUID[k]; !ok || v1 != v {
				return false
			}
		}
		return true
	default:
		return false
	}

}

// MockMountPointDisksToPartionMapping will mock DiskFromMountPoint such that
// the specified mapping is returned/used. Specifically, keys in the provided
// map are mountpoints, and the values for those keys are the partitions that
// are used to identify the disk.
func MockMountPointDisksToPartionMapping(mockedMountPoints map[string]map[string]string) (restore func()) {
	// only to be used in tests!!!!
	if !isSnapdTest {
		panic("mocking functions only to be used in tests!")
	}

	diskFromMountPoint = func(mountpoint string) (Disk, error) {
		if partitions, ok := mockedMountPoints[mountpoint]; ok {
			return &mockDisk{
				mountpoint:       mountpoint,
				labelsToPartUUID: partitions,
			}, nil
		}
		return nil, fmt.Errorf("mountpoint %s not mocked", mountpoint)
	}
	return func() {
		diskFromMountPoint = diskFromMountPointImpl
	}
}
